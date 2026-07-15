package service

import (
	"context"
	"log"
	"net"
	"sync"

	"pigate/internal/db"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// NetlinkMonitor listens to Linux kernel networking events (link, address, route
// updates) and translates the raw netlink messages into semantic NetEvents on the
// NetEventBus. It owns no business logic of its own — interested services subscribe
// to the bus (see main.go wiring). This keeps the monitor a thin translator: adding
// a new self-healing consumer no longer means threading another dependency through
// its constructor.
//
// The key translation is distinguishing a genuinely new interface from a mere flag
// change: the kernel emits RTM_NEWLINK for every attribute/flag transition (an
// up/down blink is also a NEWLINK), so the monitor tracks the set of link indexes it
// has already seen and only emits InterfaceAdded for an index it has never seen. A
// flag change on a known index becomes LinkChanged. Without this, every link blink
// would trigger a full per-interface re-apply storm (issue #48).
type NetlinkMonitor struct {
	repo   *db.Repository
	bus    *NetEventBus
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewNetlinkMonitor(repo *db.Repository, bus *NetEventBus) *NetlinkMonitor {
	return &NetlinkMonitor{
		repo: repo,
		bus:  bus,
	}
}

// Start initiates netlink event listeners. Subscriptions are skipped in mock mode
// (no real kernel to observe). Start should be called only after every subsystem's
// startup apply has completed, so boot-time link events (dhcpcd bringing links up
// fires a flurry of them) don't race the startup path (see issue #48 caution).
func (m *NetlinkMonitor) Start(ctx context.Context) {
	if m.repo.IsMockMode() {
		log.Printf("[NetlinkMonitor] Running in mock mode. Netlink subscriptions disabled.")
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	linkChan := make(chan netlink.LinkUpdate)
	addrChan := make(chan netlink.AddrUpdate)
	routeChan := make(chan netlink.RouteUpdate)

	done := ctx.Done()

	// 1. Subscribe to Link state events (e.g. interface up/down, plugged/unplugged)
	if err := netlink.LinkSubscribe(linkChan, done); err != nil {
		log.Printf("[NetlinkMonitor] Failed to subscribe to Netlink Link updates: %v", err)
		cancel()
		return
	}

	// 2. Subscribe to Address events (e.g. IP address configured/removed)
	if err := netlink.AddrSubscribe(addrChan, done); err != nil {
		log.Printf("[NetlinkMonitor] Failed to subscribe to Netlink Address updates: %v", err)
		cancel()
		return
	}

	// 3. Subscribe to Route changes (e.g. route created or deleted externally)
	if err := netlink.RouteSubscribe(routeChan, done); err != nil {
		log.Printf("[NetlinkMonitor] Failed to subscribe to Netlink Route updates: %v", err)
		cancel()
		return
	}

	log.Printf("[NetlinkMonitor] Successfully subscribed to Netlink updates (Link, Addr, Route)")

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		// known maps kernel link index -> name for links present at Start plus any
		// added since. Seeding it means the links that already exist at boot are NOT
		// reported as InterfaceAdded. Accessed only from this goroutine — no locking.
		known := seedKnownLinks()

		for {
			select {
			case linkUpdate, ok := <-linkChan:
				if !ok {
					return
				}
				m.handleLinkUpdate(linkUpdate, known)

			case addrUpdate, ok := <-addrChan:
				if !ok {
					return
				}
				name := known[addrUpdate.LinkIndex]
				log.Printf("[NetlinkMonitor] Address event: iface=%q LinkIndex=%d Address=%s NewAddr=%t",
					name, addrUpdate.LinkIndex, addrUpdate.LinkAddress.String(), addrUpdate.NewAddr)
				m.bus.Publish(NetEvent{Kind: AddrRouteChanged, Name: name})

			case routeUpdate, ok := <-routeChan:
				if !ok {
					return
				}
				dstStr := "default"
				if routeUpdate.Dst != nil {
					dstStr = routeUpdate.Dst.String()
				}
				name := known[routeUpdate.LinkIndex]
				log.Printf("[NetlinkMonitor] Route event: iface=%q Type=%d Dst=%s Protocol=%d",
					name, routeUpdate.Type, dstStr, routeUpdate.Protocol)
				m.bus.Publish(NetEvent{Kind: AddrRouteChanged, Name: name})

			case <-done:
				log.Printf("[NetlinkMonitor] Netlink event loops terminated")
				return
			}
		}
	}()
}

// handleLinkUpdate classifies a raw link event into a semantic NetEvent and updates
// the known-index set. It distinguishes a brand-new interface (index never seen ->
// InterfaceAdded) from a flag change on a known one (-> LinkChanged), and handles
// removal (RTM_DELLINK -> InterfaceRemoved).
func (m *NetlinkMonitor) handleLinkUpdate(u netlink.LinkUpdate, known map[int]string) {
	attrs := u.Attrs()
	name := ""
	var isUp, isRunning bool
	if attrs != nil {
		name = attrs.Name
		isUp = attrs.Flags&net.FlagUp != 0
		isRunning = attrs.Flags&net.FlagRunning != 0
	}
	idx := int(u.Index)

	switch u.Header.Type {
	case unix.RTM_DELLINK:
		delete(known, idx)
		log.Printf("[NetlinkMonitor] Link removed: iface=%q index=%d", name, idx)
		m.bus.Publish(NetEvent{Kind: InterfaceRemoved, Name: name})

	case unix.RTM_NEWLINK:
		if _, seen := known[idx]; !seen {
			known[idx] = name
			log.Printf("[NetlinkMonitor] Interface added: iface=%q index=%d up=%t running=%t", name, idx, isUp, isRunning)
			m.bus.Publish(NetEvent{Kind: InterfaceAdded, Name: name, Up: isUp, Running: isRunning})
		} else {
			known[idx] = name // keep name current (a rename also arrives as NEWLINK)
			log.Printf("[NetlinkMonitor] Link changed: iface=%q index=%d up=%t running=%t", name, idx, isUp, isRunning)
			m.bus.Publish(NetEvent{Kind: LinkChanged, Name: name, Up: isUp, Running: isRunning})
		}
	}
}

// seedKnownLinks returns index->name for every link currently in the kernel, so the
// monitor treats them as already-known (no spurious InterfaceAdded at Start).
func seedKnownLinks() map[int]string {
	known := make(map[int]string)
	links, err := netlink.LinkList()
	if err != nil {
		log.Printf("[NetlinkMonitor] Warning: failed to seed known links: %v", err)
		return known
	}
	for _, l := range links {
		a := l.Attrs()
		known[a.Index] = a.Name
	}
	return known
}

// Pause suppresses bus dispatch until Resume. Used to bracket a config import so
// subscribers don't re-apply against a DB that is being replaced. Delegates to the
// bus, which now owns the pause semantics for all subscribers (not just reconcile).
func (m *NetlinkMonitor) Pause() {
	m.bus.Pause()
}

// Resume re-enables dispatch after a Pause. Callers should defer Resume.
func (m *NetlinkMonitor) Resume() {
	m.bus.Resume()
}

// Stop halts the monitor loop and waits for resources to clean up.
func (m *NetlinkMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	log.Printf("[NetlinkMonitor] Netlink monitor stopped")
}
