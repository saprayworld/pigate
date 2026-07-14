package service

import (
	"log"
	"net"
	"strings"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"

	"github.com/vishvananda/netlink"
)

type DhcpcdService struct {
	repo         *db.Repository
	ifaceService *InterfaceService
	manager      kernel.DhcpcdManager
}

func NewDhcpcdService(repo *db.Repository, ifaceService *InterfaceService, manager kernel.DhcpcdManager) *DhcpcdService {
	return &DhcpcdService{
		repo:         repo,
		ifaceService: ifaceService,
		manager:      manager,
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
// interfaces. Shared by HandleLinkUpdate (flags from the netlink event),
// SyncActiveInterfaces (flags read from the kernel at startup), and SyncInterface
// (flags read from the kernel after a configuration Save) so the decision lives in
// one place.
func (s *DhcpcdService) applyDhcpcdDecision(name string, isWifi, isUp, isRunning bool) {
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

// HandleLinkEvent starts/stops the per-interface dhcpcd client from a semantic link
// event (name + live up/running flags) delivered by the NetEventBus. It must be
// subscribed in Immediate mode: the Wi-Fi "UP but not yet RUNNING" → "RUNNING"
// transition has to be observed in order (a debounced/coalesced subscription would
// swallow the intermediate state and the client would never request a lease).
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

	s.applyDhcpcdDecision(name, isWifi, isUp, isRunning)
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

		if s.repo.IsMockMode() {
			log.Printf("[DhcpcdService] Sync: [Mock] Simulating sync for interface %s", iface.Name)
			continue
		}

		// Find the link in the kernel to get its current flags
		link, err := netlink.LinkByName(iface.Name)
		if err != nil {
			log.Printf("[DhcpcdService] Interface %s not found in kernel: %v", iface.Name, err)
			continue
		}

		attrs := link.Attrs()
		if attrs == nil {
			continue
		}

		isUp := attrs.Flags&net.FlagUp != 0
		isRunning := attrs.Flags&net.FlagRunning != 0
		isWifi := iface.Type == "wireless" || strings.HasPrefix(iface.Name, "w")

		s.applyDhcpcdDecision(iface.Name, isWifi, isUp, isRunning)
	}
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

	s.applyDhcpcdDecision(name, isWifi, isUp, isRunning)
}
