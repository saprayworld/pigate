package service

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// NetEventKind classifies a semantic network event produced by NetlinkMonitor.
// It abstracts the raw netlink message types (RTM_NEWLINK/RTM_DELLINK/…) so that
// subscribers reason about intent ("an interface came back") rather than kernel
// wire semantics.
type NetEventKind int

const (
	// InterfaceAdded fires when a link index that was never seen before appears
	// (a NIC plugged in, a VLAN/IFB recreated). It does NOT fire for a flag flip
	// on a known link — that is LinkChanged. Subscribers that re-apply heavy
	// per-interface state (address/DHCP-range/QoS) key off this to avoid a
	// re-apply storm every time a link merely blinks up/down (see issue #48).
	InterfaceAdded NetEventKind = iota
	// InterfaceRemoved fires on RTM_DELLINK. Handlers may only touch runtime
	// state (kernel/dnsmasq/nft) in response — never delete user config from the
	// DB (a USB NIC unplug or a VLAN about to be recreated must not lose intent).
	InterfaceRemoved
	// LinkChanged fires when a known link's flags/attributes change (up/down,
	// running). dhcpcd needs every one of these transitions in order.
	LinkChanged
	// AddrRouteChanged fires on address or route netlink events. Routing/DNS
	// reconciliation keys off this. Its Name is best-effort and may be empty.
	AddrRouteChanged
)

func (k NetEventKind) String() string {
	switch k {
	case InterfaceAdded:
		return "InterfaceAdded"
	case InterfaceRemoved:
		return "InterfaceRemoved"
	case LinkChanged:
		return "LinkChanged"
	case AddrRouteChanged:
		return "AddrRouteChanged"
	default:
		return "Unknown"
	}
}

// NetEvent is a single semantic network event delivered to subscribers.
type NetEvent struct {
	Kind    NetEventKind
	Name    string // interface name; may be empty for AddrRouteChanged
	Up      bool   // link IFF_UP  (meaningful for LinkChanged/InterfaceAdded)
	Running bool   // link IFF_RUNNING (Wi-Fi association state)
}

// SubMode selects how a subscriber receives events.
type SubMode int

const (
	// Debounced coalesces a burst of events over a cooldown window and then
	// delivers the handler once per distinct interface name. Use for expensive,
	// idempotent full re-syncs (routing/DNS/DHCP/QoS re-apply) that don't need to
	// observe every intermediate transition.
	Debounced SubMode = iota
	// Immediate delivers every event, in order, on the subscriber's own
	// goroutine. Use when intermediate transitions matter — dhcpcd must see the
	// "UP but not yet RUNNING" → "RUNNING" sequence for Wi-Fi to obtain a lease.
	Immediate
)

const defaultDebounceInterval = 500 * time.Millisecond

// immediate subscribers get a generous buffer so a slow-ish handler never stalls
// the netlink event loop that feeds Publish (see issue #48 caution on blocking the
// loop). Debounced subscribers don't use the queue at all.
const immediateQueueSize = 64

// NetEventBus is an in-process pub/sub fan-out for semantic network events. It is
// intentionally stdlib-only (channels + timers) — the requirement is a
// single-process fan-out, not a message queue. Adding a new self-healing
// subscriber no longer means threading another dependency through NetlinkMonitor's
// constructor: it just calls Subscribe.
type NetEventBus struct {
	mu          sync.Mutex
	subscribers []*subscriber
	interval    time.Duration
	// paused suppresses all dispatch while a bulk config change (e.g. a backup
	// import that replaces the DB wholesale) is in flight, so subscribers don't
	// re-apply against a half-written DB. Mirrors the old NetlinkMonitor pause,
	// but now covers every subscriber, not just route/DNS reconcile.
	paused atomic.Bool
}

type subscriber struct {
	label  string
	kinds  map[NetEventKind]bool
	mode   SubMode
	fn     func(NetEvent)
	paused *atomic.Bool // shared with the owning bus

	// immediate delivery
	queue chan NetEvent

	// debounced delivery
	interval time.Duration
	mu       sync.Mutex // guards pending + timer
	pending  map[string]NetEvent
	timer    *time.Timer
	flushMu  sync.Mutex // serializes handler invocations across overlapping flushes
}

// NewNetEventBus creates a bus with the default 500ms debounce window.
func NewNetEventBus() *NetEventBus {
	return newNetEventBus(defaultDebounceInterval)
}

// newNetEventBus allows tests to shrink the debounce window.
func newNetEventBus(interval time.Duration) *NetEventBus {
	return &NetEventBus{interval: interval}
}

// Subscribe registers a handler for the given event kinds. label is used only for
// logging. The handler runs on a goroutine owned by the bus, so a slow handler
// (e.g. a dnsmasq restart over D-Bus) never blocks the publisher or other
// subscribers. Subscribe is expected to be called during startup wiring, before
// the monitor begins publishing.
func (b *NetEventBus) Subscribe(label string, kinds []NetEventKind, mode SubMode, fn func(NetEvent)) {
	kindSet := make(map[NetEventKind]bool, len(kinds))
	for _, k := range kinds {
		kindSet[k] = true
	}
	s := &subscriber{
		label:    label,
		kinds:    kindSet,
		mode:     mode,
		fn:       fn,
		paused:   &b.paused,
		interval: b.interval,
	}
	if mode == Immediate {
		s.queue = make(chan NetEvent, immediateQueueSize)
		go s.runImmediate()
	}

	b.mu.Lock()
	b.subscribers = append(b.subscribers, s)
	b.mu.Unlock()
}

// Publish fans an event out to every interested subscriber. It never blocks on a
// handler: immediate subscribers get a non-blocking enqueue (a full queue logs and
// drops rather than stalling the netlink loop), debounced subscribers get their
// timer reset. While paused, events are dropped outright.
func (b *NetEventBus) Publish(e NetEvent) {
	if b.paused.Load() {
		return
	}
	b.mu.Lock()
	subs := make([]*subscriber, len(b.subscribers))
	copy(subs, b.subscribers)
	b.mu.Unlock()

	for _, s := range subs {
		if !s.kinds[e.Kind] {
			continue
		}
		if s.mode == Immediate {
			select {
			case s.queue <- e:
			default:
				log.Printf("[NetEventBus] subscriber %q queue full, dropping %s event for %q", s.label, e.Kind, e.Name)
			}
		} else {
			s.enqueueDebounced(e)
		}
	}
}

// Pause suppresses all dispatch until Resume. Used to bracket a config import so
// subscribers don't re-apply against a DB that is being replaced. Safe in mock
// mode (no events fire anyway).
func (b *NetEventBus) Pause() {
	b.paused.Store(true)
	log.Printf("[NetEventBus] dispatch paused")
}

// Resume re-enables dispatch after Pause. Callers should defer Resume.
func (b *NetEventBus) Resume() {
	b.paused.Store(false)
	log.Printf("[NetEventBus] dispatch resumed")
}

// IsPaused reports whether dispatch is currently suppressed (e.g. a backup
// import bracketed by Pause/Resume is in progress). Used by the DHCP
// health-checker (issue #78) to skip an entire tick rather than restart
// dhcpcd/delete addresses against a DB that is mid-restore.
func (b *NetEventBus) IsPaused() bool {
	return b.paused.Load()
}

func (s *subscriber) runImmediate() {
	for e := range s.queue {
		if s.paused.Load() {
			// A pause that began after this event was queued: drop it, matching
			// the drop-while-paused semantics of Publish.
			continue
		}
		s.fn(e)
	}
}

func (s *subscriber) enqueueDebounced(e NetEvent) {
	s.mu.Lock()
	if s.pending == nil {
		s.pending = make(map[string]NetEvent)
	}
	// Coalesce: the latest event for a given interface wins; distinct interfaces
	// each survive so a multi-interface flap doesn't lose any of them.
	s.pending[e.Name] = e
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.interval, s.flush)
	s.mu.Unlock()
}

func (s *subscriber) flush() {
	if s.paused.Load() {
		// Drop anything that was pending from before the pause, matching the
		// drop-while-paused semantics of Publish: nothing that predates a config
		// import may fire after it.
		s.mu.Lock()
		s.pending = nil
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	evs := s.pending
	s.pending = nil
	s.mu.Unlock()
	if len(evs) == 0 {
		return
	}
	// Serialize handler invocations: an event arriving mid-flush schedules another
	// flush on its own goroutine; flushMu keeps those from running the (possibly
	// D-Bus-slow, non-reentrant) handler concurrently.
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	for _, e := range evs {
		s.fn(e)
	}
}
