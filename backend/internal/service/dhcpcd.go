package service

import (
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"

	"github.com/vishvananda/netlink"
)

// stopSettleDelay is how long a "down" decision waits before actually calling
// StopDhcpcd, so a link flap (down then back up within the window) never stops
// the client at all. See docs/ref/todo/dhcpcd-event-debounce-plan.md.
const stopSettleDelay = 2 * time.Second

// pendingStop tracks the in-flight settle timer for a single interface's deferred
// stop. seq guards against a stale timer firing after the entry has already been
// replaced/cancelled (timer.Stop() can race a callback that already started).
type pendingStop struct {
	timer *time.Timer
	seq   uint64
}

type DhcpcdService struct {
	repo         *db.Repository
	ifaceService *InterfaceService
	manager      kernel.DhcpcdManager

	mu              sync.Mutex
	pendingStops    map[string]*pendingStop
	stopSettleDelay time.Duration
	nextSeq         uint64
}

func NewDhcpcdService(repo *db.Repository, ifaceService *InterfaceService, manager kernel.DhcpcdManager) *DhcpcdService {
	return &DhcpcdService{
		repo:            repo,
		ifaceService:    ifaceService,
		manager:         manager,
		pendingStops:    make(map[string]*pendingStop),
		stopSettleDelay: stopSettleDelay,
	}
}

func (s *DhcpcdService) startDhcpcd(ifaceName string) error {
	log.Printf("[DhcpcdService] Starting dhcpcd for %s...", ifaceName)
	if err := s.manager.StartDhcpcd(ifaceName); err != nil {
		log.Printf("[DhcpcdService] Failed to start dhcpcd for %s: %v", ifaceName, err)
		return err
	}
	log.Printf("[DhcpcdService] dhcpcd start requested for %s", ifaceName)
	return nil
}

func (s *DhcpcdService) stopDhcpcd(ifaceName string) error {
	log.Printf("[DhcpcdService] Stopping/Releasing dhcpcd for %s...", ifaceName)
	if err := s.manager.StopDhcpcd(ifaceName); err != nil {
		log.Printf("[DhcpcdService] Failed to stop/release dhcpcd for %s: %v", ifaceName, err)
		return err
	}
	log.Printf("[DhcpcdService] dhcpcd stopped/released successfully for %s", ifaceName)
	return nil
}

// applyDhcpcdDecision starts or stops the per-interface dhcpcd client based on the
// interface's live link flags. Callers must have already filtered to DHCP-mode
// interfaces. This is the self-locking form; it exists for callers (and tests) that
// only need to run the decision in isolation. SyncActiveInterfaces and SyncInterface
// call applyDhcpcdDecisionLocked directly instead, so the preceding
// cancelPendingStopLocked and this decision run under a single critical section
// (Caution 3 in the plan: every touch of pendingStops and every
// manager.Start/StopDhcpcd call must share one mutex section, or a stop from one
// goroutine can slip in after a start from another).
func (s *DhcpcdService) applyDhcpcdDecision(name string, isWifi, isUp, isRunning bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyDhcpcdDecisionLocked(name, isWifi, isUp, isRunning)
}

// applyDhcpcdDecisionLocked is the lock-free core of applyDhcpcdDecision. Must be
// called with mu held.
func (s *DhcpcdService) applyDhcpcdDecisionLocked(name string, isWifi, isUp, isRunning bool) {
	switch {
	case !isUp:
		log.Printf("[DhcpcdService] %s is DOWN. Stopping dhcpcd.", name)
		_ = s.stopDhcpcd(name)
	case isWifi && !isRunning:
		// Wi-Fi is up but not yet associated to an AP; wait for the RUNNING flag
		// (delivered by a later link event) before requesting a lease.
		log.Printf("[DhcpcdService] Wi-Fi %s is UP but not running (waiting for connection).", name)
	default:
		log.Printf("[DhcpcdService] %s is UP. Starting dhcpcd.", name)
		_ = s.startDhcpcd(name)
	}
}

// cancelPendingStopLocked stops and drops any settle timer scheduled for name. Must
// be called with mu held.
func (s *DhcpcdService) cancelPendingStopLocked(name string) {
	if ps, ok := s.pendingStops[name]; ok {
		ps.timer.Stop()
		delete(s.pendingStops, name)
	}
}

// scheduleOrResetStopLocked (re)starts the settle-window timer for name. A repeated
// "down" event (still not up) resets the clock rather than firing sooner — see plan
// §2. Must be called with mu held.
func (s *DhcpcdService) scheduleOrResetStopLocked(name string) {
	s.cancelPendingStopLocked(name)
	s.nextSeq++
	seq := s.nextSeq
	ps := &pendingStop{}
	ps.seq = seq
	ps.timer = time.AfterFunc(s.stopSettleDelay, func() { s.fireDeferredStop(name, seq) })
	s.pendingStops[name] = ps
	log.Printf("[DhcpcdService] %s is DOWN. Deferring stop for %s (settle window).", name, s.stopSettleDelay)
}

// fireDeferredStop is the settle-timer callback. It re-checks the entry under mu
// (the "seq" second line of defense noted in the plan's Cautions: timer.Stop() can
// race a callback that already started, so a stale fire must be detected here even
// if the caller tried to cancel it) before actually stopping dhcpcd, so an event that
// cancelled/replaced this timer after it already fired doesn't cause a spurious stop.
func (s *DhcpcdService) fireDeferredStop(name string, seq uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, ok := s.pendingStops[name]
	if !ok || ps.seq != seq {
		// Cancelled or superseded since this timer was scheduled — nothing to do.
		return
	}
	delete(s.pendingStops, name)
	_ = s.stopDhcpcd(name)
}

// applyDhcpcdDecisionDeferred is the event-path counterpart of applyDhcpcdDecision:
// a "down" decision is deferred behind a settle-window timer instead of stopping
// dhcpcd immediately, so a brief link flap (down then back up within the window)
// never stops the client at all. An "up" decision always cancels any pending stop
// first (the link is back, the stop must not fire later) and then runs the same
// start/wait logic as applyDhcpcdDecision — starting is never deferred: StartUnit is
// idempotent, so requesting it again has no side effect, and delaying it would add
// latency to Wi-Fi lease acquisition for no benefit (see plan §2, alternative 2).
func (s *DhcpcdService) applyDhcpcdDecisionDeferred(name string, isWifi, isUp, isRunning bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !isUp {
		s.scheduleOrResetStopLocked(name)
		return
	}

	// Link is up (or up-but-not-running): the interface is present, so any pending
	// stop from an earlier flap must not fire.
	s.cancelPendingStopLocked(name)

	if isWifi && !isRunning {
		log.Printf("[DhcpcdService] Wi-Fi %s is UP but not running (waiting for connection).", name)
		return
	}

	log.Printf("[DhcpcdService] %s is UP. Starting dhcpcd.", name)
	_ = s.startDhcpcd(name)
}

// HandleLinkEvent starts/stops the per-interface dhcpcd client from a semantic link
// event (name + live up/running flags) delivered by the NetEventBus. It must be
// subscribed in Immediate mode: the Wi-Fi "UP but not yet RUNNING" → "RUNNING"
// transition has to be observed in order (a debounced/coalesced subscription would
// swallow the intermediate state and the client would never request a lease).
// Stop decisions from this path are deferred behind a settle-window timer
// (applyDhcpcdDecisionDeferred) so a brief link flap never actually stops dhcpcd;
// start decisions are never deferred (StartUnit is idempotent, so an immediate start
// has no downside and keeps Wi-Fi lease acquisition latency unchanged).
func (s *DhcpcdService) HandleLinkEvent(name string, isUp, isRunning bool) {
	// 1. Get current interface details from interface service (data layer)
	ifaces, err := s.ifaceService.GetDataLayerInterface()
	if err != nil {
		log.Printf("[DhcpcdService] Failed to get data layer interfaces: %v", err)
		return
	}

	var targetIface *model.NetworkInterface
	for _, iface := range ifaces {
		if iface.Name == name {
			targetIface = &iface
			break
		}
	}

	if targetIface == nil {
		// Not a managed interface, skip
		return
	}

	// 2. Check if addressing mode is DHCP
	if targetIface.AddressingMode != "dhcp" {
		return
	}

	isWifi := targetIface.Type == "wireless" || strings.HasPrefix(name, "w")

	s.applyDhcpcdDecisionDeferred(name, isWifi, isUp, isRunning)
}

// SyncActiveInterfaces checks all managed interfaces and starts/stops dhcpcd based on their current actual state
func (s *DhcpcdService) SyncActiveInterfaces() {
	ifaces, err := s.ifaceService.GetDataLayerInterface()
	if err != nil {
		log.Printf("[DhcpcdService] Failed to get data layer interfaces for sync: %v", err)
		return
	}

	for _, iface := range ifaces {
		if iface.AddressingMode != "dhcp" {
			continue
		}
		s.syncActiveInterface(iface)
	}
}

// syncActiveInterface reconciles a single DHCP-mode interface for
// SyncActiveInterfaces. The whole cancel-pending-stop -> read-kernel-flags ->
// decide -> call-manager sequence runs under one s.mu section (Caution 3 in the
// plan): if the lock were released between cancelPendingStopLocked and the eventual
// start/stop call, a concurrent event-path goroutine (HandleLinkEvent ->
// applyDhcpcdDecisionDeferred, e.g. from a link flap arriving mid-sync) could
// interleave its own manager.Start/StopDhcpcd call in between, so the two
// D-Bus calls would no longer be serialized in program order.
func (s *DhcpcdService) syncActiveInterface(iface model.NetworkInterface) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Sync reads authoritative kernel state below; any settle timer still
	// pending from an earlier event must not be allowed to fire a stale stop
	// after this reconcile decides the interface is up (Caution 5 in the plan).
	s.cancelPendingStopLocked(iface.Name)

	if s.repo.IsMockMode() {
		log.Printf("[DhcpcdService] Sync: [Mock] Simulating sync for interface %s", iface.Name)
		return
	}

	// Find the link in the kernel to get its current flags
	link, err := netlink.LinkByName(iface.Name)
	if err != nil {
		log.Printf("[DhcpcdService] Interface %s not found in kernel: %v", iface.Name, err)
		return
	}

	attrs := link.Attrs()
	if attrs == nil {
		return
	}

	isUp := attrs.Flags&net.FlagUp != 0
	isRunning := attrs.Flags&net.FlagRunning != 0
	isWifi := iface.Type == "wireless" || strings.HasPrefix(iface.Name, "w")

	s.applyDhcpcdDecisionLocked(iface.Name, isWifi, isUp, isRunning)
}

// SyncInterface reconciles the dhcpcd client for a single interface based on its
// current addressing mode and live kernel link flags. It is meant to be called after
// a configuration Save (e.g. an addressing-mode change): a Static->DHCP switch on an
// interface that is already up produces no netlink Link event, so without this the
// dhcpcd client would not start until the next link event (the user had to toggle the
// interface off/on manually).
func (s *DhcpcdService) SyncInterface(name string) {
	ifaces, err := s.ifaceService.GetDataLayerInterface()
	if err != nil {
		log.Printf("[DhcpcdService] SyncInterface: failed to get data layer interfaces: %v", err)
		return
	}

	var targetIface *model.NetworkInterface
	for _, iface := range ifaces {
		if iface.Name == name {
			targetIface = &iface
			break
		}
	}
	if targetIface == nil {
		// Not a managed interface (no data-layer entry), skip.
		return
	}

	// The whole cancel-pending-stop -> decide/stop sequence below runs under one s.mu
	// section (Caution 3 in the plan): SyncInterface is called from handlers.go (admin
	// save) and backup.go (restore), which can race a concurrent event-path goroutine
	// (HandleLinkEvent -> applyDhcpcdDecisionDeferred) for the same interface. Releasing
	// the lock between cancelPendingStopLocked and the eventual manager.Start/StopDhcpcd
	// call would let that goroutine's own start/stop slip in between, so the two D-Bus
	// calls would no longer be serialized in program order.
	s.mu.Lock()
	defer s.mu.Unlock()

	// Sync is authoritative (config Save / restore just happened); any settle timer
	// still pending from an earlier event must not fire a stale stop behind this
	// reconcile's back (Caution 5 in the plan), regardless of which branch below runs.
	s.cancelPendingStopLocked(name)

	// Switching to a non-DHCP mode: release any dhcpcd lease that was running for this
	// interface. HandleLinkUpdate/SyncActiveInterfaces intentionally skip non-DHCP
	// interfaces (a static interface flapping should not spam stop calls), so the
	// "static -> stop" transition is handled only here, on the explicit mode change.
	// stopDhcpcd is safe in mock mode (the mock manager is a no-op).
	if targetIface.AddressingMode != "dhcp" {
		log.Printf("[DhcpcdService] SyncInterface: %s is not DHCP. Stopping dhcpcd (releasing any lease).", name)
		_ = s.stopDhcpcd(name)
		return
	}

	// DHCP mode needs the live link flags to decide. Reading them touches the real
	// kernel, so guard mock mode (mirrors SyncActiveInterfaces): the mock kernel has no
	// real link to query and the mock dhcpcd manager is a no-op anyway.
	if s.repo.IsMockMode() {
		log.Printf("[DhcpcdService] SyncInterface: [Mock] Simulating DHCP sync for interface %s", name)
		return
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		log.Printf("[DhcpcdService] SyncInterface: interface %s not found in kernel: %v", name, err)
		return
	}
	attrs := link.Attrs()
	if attrs == nil {
		return
	}

	isUp := attrs.Flags&net.FlagUp != 0
	isRunning := attrs.Flags&net.FlagRunning != 0
	isWifi := targetIface.Type == "wireless" || strings.HasPrefix(name, "w")

	s.applyDhcpcdDecisionLocked(name, isWifi, isUp, isRunning)
}

// RestartForHealthCheck restarts the per-interface dhcpcd client on behalf of
// the DHCP health-checker (issue #78), which detects an interface stuck with
// only a link-local (169.254.x.x) address or no IPv4 address at all despite
// being carrier-ready. It must never call kernel.DhcpcdManager.RestartDhcpcd
// directly (Caution 1 in the plan): the cancel-pending-stop -> restart
// sequence below shares the exact same s.mu critical section as every other
// start/stop path in this file (SyncInterface, syncActiveInterface,
// applyDhcpcdDecisionDeferred), so a restart triggered by the health-checker
// can never race a concurrent HandleLinkEvent/SyncActiveInterfaces call for
// the same interface.
func (s *DhcpcdService) RestartForHealthCheck(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cancelPendingStopLocked(name)

	log.Printf("[DhcpcdService] RestartForHealthCheck: restarting dhcpcd for %s", name)
	if err := s.manager.RestartDhcpcd(name); err != nil {
		log.Printf("[DhcpcdService] RestartForHealthCheck: failed to restart dhcpcd for %s: %v", name, err)
		return err
	}
	log.Printf("[DhcpcdService] RestartForHealthCheck: dhcpcd restart requested for %s", name)
	return nil
}
