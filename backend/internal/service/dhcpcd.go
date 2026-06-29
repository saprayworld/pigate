package service

import (
	"log"
	"net"
	"os/exec"
	"strings"

	"pigate/internal/db"
	"pigate/internal/model"

	"github.com/vishvananda/netlink"
)

var execCommand = exec.Command

type DhcpcdService struct {
	repo         *db.Repository
	ifaceService *InterfaceService
}

func NewDhcpcdService(repo *db.Repository, ifaceService *InterfaceService) *DhcpcdService {
	return &DhcpcdService{
		repo:         repo,
		ifaceService: ifaceService,
	}
}

func (s *DhcpcdService) startDhcpcd(ifaceName string) error {
	log.Printf("[DhcpcdService] Starting dhcpcd for %s...", ifaceName)
	if s.repo.IsMockMode() {
		log.Printf("[DhcpcdService] [Mock] Simulating starting dhcpcd for %s", ifaceName)
		return nil
	}
	cmd := execCommand("sudo", "dhcpcd", ifaceName)
	if err := cmd.Start(); err != nil {
		log.Printf("[DhcpcdService] Failed to start dhcpcd for %s: %v", ifaceName, err)
		return err
	}
	log.Printf("[DhcpcdService] dhcpcd started successfully for %s", ifaceName)
	return nil
}

func (s *DhcpcdService) stopDhcpcd(ifaceName string) error {
	log.Printf("[DhcpcdService] Stopping/Releasing dhcpcd for %s...", ifaceName)
	if s.repo.IsMockMode() {
		log.Printf("[DhcpcdService] [Mock] Simulating stopping/releasing dhcpcd for %s", ifaceName)
		return nil
	}
	cmd := execCommand("sudo", "dhcpcd", "-k", ifaceName)
	if err := cmd.Run(); err != nil {
		log.Printf("[DhcpcdService] Failed to stop/release dhcpcd for %s: %v", ifaceName, err)
		return err
	}
	log.Printf("[DhcpcdService] dhcpcd stopped/released successfully for %s", ifaceName)
	return nil
}

func (s *DhcpcdService) HandleLinkUpdate(update netlink.LinkUpdate) {
	attrs := update.Attrs()
	if attrs == nil {
		return
	}
	name := attrs.Name
	flags := attrs.Flags

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

	isUp := flags&net.FlagUp != 0
	isRunning := flags&net.FlagRunning != 0

	if isWifi {
		// Wi-Fi logic:
		// - down (not up): stop dhcpcd
		// - up: do nothing (waiting for SSID connection)
		// - running: start dhcpcd
		if !isUp {
			log.Printf("[DhcpcdService] Wi-Fi interface %s is DOWN. Stopping dhcpcd.", name)
			_ = s.stopDhcpcd(name)
		} else if isRunning {
			log.Printf("[DhcpcdService] Wi-Fi interface %s is UP and RUNNING (SSID connected). Starting dhcpcd.", name)
			_ = s.startDhcpcd(name)
		} else {
			log.Printf("[DhcpcdService] Wi-Fi interface %s is UP but not running (waiting for connection).", name)
		}
	} else {
		// Ethernet logic:
		// - down: stop dhcpcd
		// - up: start dhcpcd
		if !isUp {
			log.Printf("[DhcpcdService] Ethernet interface %s is DOWN. Stopping dhcpcd.", name)
			_ = s.stopDhcpcd(name)
		} else {
			log.Printf("[DhcpcdService] Ethernet interface %s is UP. Starting dhcpcd.", name)
			_ = s.startDhcpcd(name)
		}
	}
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

		if isWifi {
			if !isUp {
				log.Printf("[DhcpcdService] Sync: Wi-Fi %s is DOWN. Stopping dhcpcd.", iface.Name)
				_ = s.stopDhcpcd(iface.Name)
			} else if isRunning {
				log.Printf("[DhcpcdService] Sync: Wi-Fi %s is UP and RUNNING. Starting dhcpcd.", iface.Name)
				_ = s.startDhcpcd(iface.Name)
			} else {
				log.Printf("[DhcpcdService] Sync: Wi-Fi %s is UP but not running (waiting for connection).", iface.Name)
			}
		} else {
			if !isUp {
				log.Printf("[DhcpcdService] Sync: Ethernet %s is DOWN. Stopping dhcpcd.", iface.Name)
				_ = s.stopDhcpcd(iface.Name)
			} else {
				log.Printf("[DhcpcdService] Sync: Ethernet %s is UP. Starting dhcpcd.", iface.Name)
				_ = s.startDhcpcd(iface.Name)
			}
		}
	}
}
