package service

import (
	"fmt"
	"log"
	"net"
	"strings"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

type InterfaceService struct {
	repo    *db.Repository
	network kernel.NetworkManager
}

func NewInterfaceService(repo *db.Repository, network kernel.NetworkManager) *InterfaceService {
	return &InterfaceService{
		repo:    repo,
		network: network,
	}
}

// InitApplyConfigurationAtStartup pulls configuration from the database and applies it to the kernel on startup.
func (s *InterfaceService) InitApplyConfigurationAtStartup() error {
	log.Printf("[Startup] Fetching interfaces from kernel...")
	kernelIfaces, err := s.GetKernelInterfaces()
	if err != nil {
		return fmt.Errorf("failed to load interfaces from kernel: %w", err)
	}

	kernelMap := make(map[string]bool)
	for _, kIface := range kernelIfaces {
		kernelMap[kIface.Name] = true
	}

	log.Printf("[Startup] Fetching interfaces configuration from database...")
	ifaces, err := s.repo.GetInterfacesFromDB()
	if err != nil {
		return fmt.Errorf("failed to load interfaces from DB: %w", err)
	}

	for _, iface := range ifaces {
		if !kernelMap[iface.Name] {
			log.Printf("[Startup] Warning: Interface %s configured in database does not exist in kernel. Skipping.", iface.Name)
			continue
		}

		log.Printf("[Startup] Applying configuration to kernel for interface: %s (Type: %s, Role: %s)", iface.Name, iface.Type, iface.Role)

		if iface.Type == "wireless" {
			ssid := ""
			if iface.WifiSSID != nil {
				ssid = *iface.WifiSSID
			}
			password := ""
			if iface.WifiPassword != nil {
				password = *iface.WifiPassword
			}
			security := "WPA2"
			if iface.WifiSecurity != nil {
				security = *iface.WifiSecurity
			}
			backupSSID := ""
			if iface.BackupSSID != nil {
				backupSSID = *iface.BackupSSID
			}
			backupPassword := ""
			if iface.BackupWifiPassword != nil {
				backupPassword = *iface.BackupWifiPassword
			}
			backupSecurity := "WPA2"
			if iface.BackupWifiSecurity != nil {
				backupSecurity = *iface.BackupWifiSecurity
			}
			macMode := "hardware"
			if iface.MacMode != nil {
				macMode = *iface.MacMode
			}

			log.Printf("[Startup] Configuring Wi-Fi for interface %s...", iface.Name)
			if err := s.network.ConfigureWifi(iface.Name, ssid, password, security, backupSSID, backupPassword, backupSecurity, macMode); err != nil {
				log.Printf("[Startup] Warning: Failed to configure Wi-Fi for interface %s: %v", iface.Name, err)
			}
		}

		// Toggle the interface to its desired administrative state FIRST. ConfigureInterface
		// no longer forces the link up and defers the gateway route while the link is down,
		// so the link must already be up for the static default route to be installed.
		isUp := iface.Status == "up"
		log.Printf("[Startup] Toggling interface %s state to up=%t...", iface.Name, isUp)
		if err := s.network.ToggleInterface(iface.Name, isUp); err != nil {
			log.Printf("[Startup] Warning: Failed to toggle interface %s state: %v", iface.Name, err)
		}

		metric := 0
		if iface.Metric != nil {
			metric = *iface.Metric
		}
		log.Printf("[Startup] Configuring IP/mode for interface %s (Mode: %s, IP: %s, Netmask: %s, Gateway: %s, Metric: %d)...",
			iface.Name, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, metric)
		if err := s.network.ConfigureInterface(iface.Name, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, metric); err != nil {
			log.Printf("[Startup] Warning: Failed to configure interface %s: %v", iface.Name, err)
		}
	}
	log.Printf("[Startup] Successfully applied interface configuration at startup.")
	return nil
}

// GetKernelInterfaces scans the OS for active interfaces (or returns mocked interfaces if mockMode is enabled).
func (s *InterfaceService) GetKernelInterfaces() ([]model.NetworkInterface, error) {
	if s.repo.IsMockMode() && !s.repo.IsMockFromReal() {
		// Mock kernel interfaces
		mockSSID := "MyHome_5G"
		mockSec := "WPA2-PSK"
		mockMode := "randomized"
		realMac := "DC:A6:32:AA:BB:C2"
		randMac := "4E:88:2F:BC:A1:90"
		laaMac := "9A:11:22:33:44:55"
		recon := true
		fo := false
		backupSSID := "MyHome_2G"
		backupPass := "backupPassword123"
		backupSec := "WPA2"
		timeout := 15
		retries := 3
		cooldown := 60

		list := []model.NetworkInterface{
			{
				ID:             "iface-eth0",
				Name:           "eth0",
				Alias:          "eth0",
				Role:           "LAN",
				Type:           "ethernet",
				Subtype:        "device",
				AddressingMode: "static",
				IP:             "192.168.1.1",
				Netmask:        "24",
				Gateway:        "",
				MacAddress:     "DC:A6:32:AA:BB:C1",
				AdminAccess:    []string{"PING", "HTTP", "SSH"},
				Status:         "up",
				Speed:          "1000 Mbps",
			},
			{
				ID:                   "iface-wlan0",
				Name:                 "wlan0",
				Alias:                "wlan0",
				Role:                 "WAN",
				Type:                 "wireless",
				Subtype:              "device",
				AddressingMode:       "dhcp",
				IP:                   "10.0.0.45",
				Netmask:              "24",
				Gateway:              "10.0.0.1",
				MacAddress:           "4E:88:2F:BC:A1:90",
				AdminAccess:          []string{"PING"},
				Status:               "up",
				Speed:                "72 Mbps",
				WifiSSID:             &mockSSID,
				WifiSecurity:         &mockSec,
				MacMode:              &mockMode,
				RealMacAddress:       &realMac,
				RandomizedMac:        &randMac,
				LaaMacAddress:        &laaMac,
				RandomizeOnReconnect: &recon,
				FailoverEnabled:      &fo,
				BackupSSID:           &backupSSID,
				BackupWifiPassword:   &backupPass,
				BackupWifiSecurity:   &backupSec,
				IPCheckTimeout:       &timeout,
				PrimaryMaxRetries:    &retries,
				FailoverCooldown:     &cooldown,
			},
			// Added later interface (exists in mock kernel but not in db seed)
			{
				ID:             "iface-eth1",
				Name:           "eth1",
				Alias:          "eth1",
				Role:           "LAN",
				Type:           "ethernet",
				Subtype:        "device",
				AddressingMode: "dhcp",
				IP:             "192.168.2.100",
				Netmask:        "24",
				Gateway:        "192.168.2.1",
				MacAddress:     "DC:A6:32:AA:BB:C3",
				AdminAccess:    []string{"PING", "HTTP", "SSH"},
				Status:         "up",
				Speed:          "100 Mbps",
			},
		}

		// Ensure dynamically added interfaces in DB (like unit tests setup) are present in the mock kernel list
		dbIfaces, err := s.repo.GetInterfacesFromDB()
		if err == nil {
			namesMap := make(map[string]bool)
			for _, item := range list {
				namesMap[item.Name] = true
			}
			for _, dbIface := range dbIfaces {
				if !namesMap[dbIface.Name] {
					list = append(list, dbIface)
				}
			}
		}

		return list, nil
	}

	// Real OS Kernel reading
	netIfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get system interfaces: %w", err)
	}

	var list []model.NetworkInterface
	for _, netIface := range netIfaces {
		// Skip loopback and ifb QoS virtual interfaces
		if netIface.Flags&net.FlagLoopback != 0 || strings.HasPrefix(netIface.Name, "lo") || strings.HasPrefix(netIface.Name, "ifb") {
			continue
		}

		macAddr := netIface.HardwareAddr.String()
		if macAddr == "" {
			macAddr = "00:00:00:00:00:00"
		}

		// Query IP and netmask
		ipStr := "0.0.0.0"
		netmaskStr := "24"
		addrs, err := netIface.Addrs()
		if err == nil {
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok {
					if ipNet.IP.To4() != nil {
						ipStr = ipNet.IP.String()
						ones, _ := ipNet.Mask.Size()
						netmaskStr = fmt.Sprintf("%d", ones)
						break
					}
				}
			}
		}

		status := "down"
		if netIface.Flags&net.FlagUp != 0 {
			status = "up"
		}

		ifaceType := "ethernet"
		if strings.HasPrefix(netIface.Name, "w") {
			ifaceType = "wireless"
		}

		subtype := db.GetDeviceType(netIface.Name)
		speed := db.GetInterfaceSpeed(netIface.Name)
		gateway := db.GetGatewayForInterface(netIface.Name)
		addrMode := db.DetectAddressingMode(netIface.Name, netIface.Index)

		iface := model.NetworkInterface{
			ID:             "iface-" + netIface.Name,
			Name:           netIface.Name,
			Alias:          netIface.Name,
			Role:           "LAN",
			Type:           ifaceType,
			Subtype:        subtype,
			AddressingMode: addrMode,
			IP:             ipStr,
			Netmask:        netmaskStr,
			Gateway:        gateway,
			MacAddress:     macAddr,
			AdminAccess:    []string{"PING", "HTTP", "SSH"},
			Status:         status,
			Speed:          speed,
		}
		if strings.Contains(netIface.Name, "wan") {
			iface.Role = "WAN"
			iface.AdminAccess = []string{"PING"}
		}
		list = append(list, iface)
	}

	return list, nil
}

// GetDataLayerInterface returns unified interfaces by overwriting OS-level interfaces with configuration in database.
func (s *InterfaceService) GetDataLayerInterface() ([]model.NetworkInterface, error) {
	kernelIfaces, err := s.GetKernelInterfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get kernel interfaces: %w", err)
	}

	dbIfaces, err := s.repo.GetInterfacesFromDB()
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces configuration from DB: %w", err)
	}

	dbMap := make(map[string]model.NetworkInterface)
	for _, dbIface := range dbIfaces {
		dbMap[dbIface.Name] = dbIface
	}

	var result []model.NetworkInterface
	for _, kIface := range kernelIfaces {
		dbIface, exists := dbMap[kIface.Name]
		// managed = has a config row in DB (pigate has configured this interface).
		// Computed from the presence of the DB row, never persisted.
		kIface.Managed = exists
		if exists {
			// Overwrite database config fields into the data layer interface
			kIface.ID = dbIface.ID
			kIface.Alias = dbIface.Alias
			kIface.Role = dbIface.Role
			kIface.AddressingMode = dbIface.AddressingMode

			// If static addressing, use database static configuration
			if dbIface.AddressingMode == "static" {
				kIface.IP = dbIface.IP
				kIface.Netmask = dbIface.Netmask
				kIface.Gateway = dbIface.Gateway
			}

			// Route metric applies to both static and dhcp (WAN failover ordering),
			// so it is copied regardless of addressing mode.
			kIface.Metric = dbIface.Metric

			// Admin access and Wi-Fi fields
			kIface.AdminAccess = dbIface.AdminAccess
			kIface.MacMode = dbIface.MacMode
			kIface.RealMacAddress = dbIface.RealMacAddress
			kIface.RandomizedMac = dbIface.RandomizedMac
			kIface.LaaMacAddress = dbIface.LaaMacAddress
			kIface.RandomizeOnReconnect = dbIface.RandomizeOnReconnect
			kIface.WifiSSID = dbIface.WifiSSID
			kIface.WifiPassword = dbIface.WifiPassword
			kIface.WifiSecurity = dbIface.WifiSecurity
			kIface.FailoverEnabled = dbIface.FailoverEnabled
			kIface.BackupSSID = dbIface.BackupSSID
			kIface.BackupWifiPassword = dbIface.BackupWifiPassword
			kIface.BackupWifiSecurity = dbIface.BackupWifiSecurity
			kIface.IPCheckTimeout = dbIface.IPCheckTimeout
			kIface.PrimaryMaxRetries = dbIface.PrimaryMaxRetries
			kIface.FailoverCooldown = dbIface.FailoverCooldown
		} else {
			// Interface added later (not in DB yet)
			// Ensure it has valid defaults
			if kIface.Role == "" {
				kIface.Role = "LAN"
				if strings.Contains(kIface.Name, "wan") || strings.Contains(kIface.Name, "wlan") {
					kIface.Role = "WAN"
				}
			}
			if len(kIface.AdminAccess) == 0 {
				if kIface.Role == "WAN" {
					kIface.AdminAccess = []string{"PING"}
				} else {
					kIface.AdminAccess = []string{"PING", "HTTP", "SSH"}
				}
			}
		}
		result = append(result, kIface)
	}

	return result, nil
}

// GetDataLayerInterfaceByID finds a specific interface in the data layer.
func (s *InterfaceService) GetDataLayerInterfaceByID(id string) (*model.NetworkInterface, error) {
	list, err := s.GetDataLayerInterface()
	if err != nil {
		return nil, err
	}
	for _, item := range list {
		if item.ID == id {
			return &item, nil
		}
	}
	return nil, nil
}

// ApplyInterfaceConfig saves the specified interface configuration to both kernel and DB.
func (s *InterfaceService) ApplyInterfaceConfig(iface model.NetworkInterface) error {
	if iface.Type == "wireless" {
		ssid := ""
		if iface.WifiSSID != nil {
			ssid = *iface.WifiSSID
		}
		password := ""
		if iface.WifiPassword != nil {
			password = *iface.WifiPassword
		}
		security := "WPA2"
		if iface.WifiSecurity != nil {
			security = *iface.WifiSecurity
		}
		backupSSID := ""
		if iface.BackupSSID != nil {
			backupSSID = *iface.BackupSSID
		}
		backupPassword := ""
		if iface.BackupWifiPassword != nil {
			backupPassword = *iface.BackupWifiPassword
		}
		backupSecurity := "WPA2"
		if iface.BackupWifiSecurity != nil {
			backupSecurity = *iface.BackupWifiSecurity
		}
		macMode := "hardware"
		if iface.MacMode != nil {
			macMode = *iface.MacMode
		}

		if err := s.network.ConfigureWifi(iface.Name, ssid, password, security, backupSSID, backupPassword, backupSecurity, macMode); err != nil {
			return fmt.Errorf("OS level Wi-Fi configuration failed: %w", err)
		}
	}

	// Validate metric range if set. Priority is a uint32 on the kernel side; we
	// constrain it to 1–9999 so "unset" (nil) stays distinct from a valid value
	// and negative/overflow values are rejected before reaching netlink.
	if iface.Metric != nil {
		if *iface.Metric < 1 || *iface.Metric > 9999 {
			return fmt.Errorf("metric must be between 1 and 9999, got %d", *iface.Metric)
		}
	}

	metric := 0
	if iface.Metric != nil {
		metric = *iface.Metric
	}
	if err := s.network.ConfigureInterface(iface.Name, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, metric); err != nil {
		return fmt.Errorf("OS level interface configuration failed: %w", err)
	}

	if err := s.repo.UpdateInterface(iface); err != nil {
		return fmt.Errorf("database update failed: %w", err)
	}

	return nil
}

// SetInterfaceState is the single path that changes an interface's administrative
// state (up/down). It is separate from ApplyInterfaceConfig ("configure") so that
// saving configuration never toggles the link.
//
// On the "up" leg it brings the link up first, then reapplies the DB configuration
// (static IP, gateway route, metric) which the "configure" path deferred while the
// link was down. Reapplying the route may fail on a link that is up but has no carrier
// yet (unplugged ethernet, wireless still associating); this is logged as a non-fatal
// warning — the same behaviour as startup — rather than failing the whole request.
//
// On the "down" leg it simply brings the link down; the kernel drops the interface's
// routes on down, so no configuration replay is needed.
//
// Status is persisted with ToggleInterfaceStatus (a targeted UPDATE), not an upsert, so
// toggling an unmanaged interface (no DB row) does not silently create one.
func (s *InterfaceService) SetInterfaceState(iface model.NetworkInterface, up bool) error {
	if err := s.network.ToggleInterface(iface.Name, up); err != nil {
		return fmt.Errorf("failed to set interface %s state: %w", iface.Name, err)
	}

	if up {
		metric := 0
		if iface.Metric != nil {
			metric = *iface.Metric
		}
		if err := s.network.ConfigureInterface(iface.Name, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, metric); err != nil {
			log.Printf("[Interface] Warning: reapply config for %s after toggle up failed: %v", iface.Name, err)
		}
	}

	status := "down"
	if up {
		status = "up"
	}
	return s.repo.ToggleInterfaceStatus(iface.ID, status)
}

// FlushInterfaceConfig flushes configuration of the specified interface from the database.
func (s *InterfaceService) FlushInterfaceConfig(id string) error {
	return s.repo.DeleteInterface(id)
}
