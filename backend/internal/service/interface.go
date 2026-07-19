package service

import (
	"errors"
	"fmt"
	"log"
	"net"
	"regexp"
	"strings"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// Sentinel errors so the API layer can map VLAN failures to the right HTTP status:
// ErrVlanExists -> 409 Conflict, ErrVlanInvalid -> 400 Bad Request. Any other error
// returned by the VLAN methods is an internal failure (500).
var (
	ErrVlanExists  = errors.New("vlan interface already exists")
	ErrVlanInvalid = errors.New("invalid vlan configuration")
)

// Sentinel errors for alias validation, mapped by the API layer the same way:
// ErrAliasConflict -> 409 Conflict, ErrAliasInvalid -> 400 Bad Request.
var (
	ErrAliasConflict = errors.New("interface alias already in use")
	ErrAliasInvalid  = errors.New("invalid interface alias")
)

var aliasPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

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

	// Recreate VLAN sub-interfaces that are configured in the DB but missing from the
	// kernel (e.g. after a reboot — VLAN links are not persistent, they only live in
	// the running kernel). This is the fix for issue #20: without it, VLANs saved in
	// the DB would silently disappear on every reboot. A recreate failure (e.g. the
	// parent interface is absent) is logged and skipped, never a fatal startup error.
	for _, iface := range ifaces {
		if iface.Subtype != "vlan" || kernelMap[iface.Name] {
			continue
		}
		s.recreateVlanIfPossible(iface, kernelMap)
	}

	for _, iface := range ifaces {
		if !kernelMap[iface.Name] {
			log.Printf("[Startup] Warning: Interface %s configured in database does not exist in kernel. Skipping.", iface.Name)
			continue
		}
		s.applyOneInterface(iface)
	}
	log.Printf("[Startup] Successfully applied interface configuration at startup.")
	return nil
}

// applyOneInterface pushes a single interface's DB configuration to the kernel:
// Wi-Fi association (if wireless), then the desired admin up/down state, then the
// IP/mode/gateway. Every step is best-effort (warn-and-continue) so one bad field
// never aborts the caller. The link is assumed to already exist in the kernel — the
// caller is responsible for skipping/recreating a missing link first. Shared by
// startup (InitApplyConfigurationAtStartup) and the running-state self-heal path
// (ReapplyInterfaceByName), so the desired-state application lives in one place.
func (s *InterfaceService) applyOneInterface(iface model.NetworkInterface) {
	log.Printf("[ApplyInterface] Applying configuration to kernel for interface: %s (Type: %s, Role: %s)", iface.Name, iface.Type, iface.Role)

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
		prefer5GHz := false
		if iface.Prefer5GHz != nil {
			prefer5GHz = *iface.Prefer5GHz
		}

		log.Printf("[ApplyInterface] Configuring Wi-Fi for interface %s...", iface.Name)
		if err := s.network.ConfigureWifi(iface.Name, ssid, password, security, backupSSID, backupPassword, backupSecurity, macMode, prefer5GHz); err != nil {
			log.Printf("[ApplyInterface] Warning: Failed to configure Wi-Fi for interface %s: %v", iface.Name, err)
		}
	}

	// Toggle the interface to its desired administrative state FIRST. ConfigureInterface
	// no longer forces the link up and defers the gateway route while the link is down,
	// so the link must already be up for the static default route to be installed.
	isUp := iface.Status == "up"
	log.Printf("[ApplyInterface] Toggling interface %s state to up=%t...", iface.Name, isUp)
	if err := s.network.ToggleInterface(iface.Name, isUp); err != nil {
		log.Printf("[ApplyInterface] Warning: Failed to toggle interface %s state: %v", iface.Name, err)
	}

	metric := 0
	if iface.Metric != nil {
		metric = *iface.Metric
	}
	log.Printf("[ApplyInterface] Configuring IP/mode for interface %s (Mode: %s, IP: %s, Netmask: %s, Gateway: %s, Metric: %d)...",
		iface.Name, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, metric)
	if err := s.network.ConfigureInterface(iface.Name, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, metric); err != nil {
		log.Printf("[ApplyInterface] Warning: Failed to configure interface %s: %v", iface.Name, err)
	}
}

// ReapplyInterfaceByName re-pushes a single interface's DB configuration to the
// kernel when that link (re)appears at runtime — the running-state counterpart to
// the boot-time InitApplyConfigurationAtStartup. It is driven by the InterfaceAdded
// event so an interface that vanished and came back (a USB NIC re-plugged, a VLAN
// deleted and recreated outside PiGate) returns to its intended admin state + static
// IP on its own, without the user touching the UI (issue #48 acceptance test).
//
// Invariant (issue #48): this only ever writes runtime/kernel state. It NEVER
// deletes or mutates user config in the DB — a link that has no DB row is simply not
// managed by PiGate and is left alone.
//
// If name is itself a VLAN parent, any child VLAN configured in the DB that is
// missing from the kernel is recreated first, so bringing a parent NIC back also
// restores the VLANs stacked on top of it.
func (s *InterfaceService) ReapplyInterfaceByName(name string) {
	ifaces, err := s.repo.GetInterfacesFromDB()
	if err != nil {
		log.Printf("[ApplyInterface] ReapplyInterfaceByName(%s): failed to load interfaces from DB: %v", name, err)
		return
	}

	var target *model.NetworkInterface
	for i := range ifaces {
		if ifaces[i].Name == name {
			target = &ifaces[i]
			break
		}
	}
	if target == nil {
		// Not a PiGate-managed link — nothing to re-apply, and we must not create config.
		log.Printf("[ApplyInterface] ReapplyInterfaceByName(%s): not a managed interface, ignoring", name)
		return
	}

	kernelMap := s.kernelInterfaceNameSet()

	// If the reappeared link is a VLAN that is (still) missing from the kernel — e.g.
	// the event was for a flag change on a stale reference — recreate it before applying.
	if target.Subtype == "vlan" && !kernelMap[target.Name] {
		s.recreateVlanIfPossible(*target, kernelMap)
	}

	// If this link is a VLAN parent, restore any child VLANs the kernel lost. Bringing
	// eth0 back should bring eth0.10 back too, otherwise its InterfaceAdded never fires.
	for _, child := range ifaces {
		if child.Subtype == "vlan" && child.VlanParent != nil && *child.VlanParent == name && !kernelMap[child.Name] {
			if s.recreateVlanIfPossible(child, kernelMap) {
				s.applyOneInterface(child)
			}
		}
	}

	s.applyOneInterface(*target)
}

// kernelInterfaceNameSet returns the set of interface names currently present in the
// kernel, or an empty set on error (best-effort, callers treat absence as "missing").
func (s *InterfaceService) kernelInterfaceNameSet() map[string]bool {
	set := make(map[string]bool)
	kernelIfaces, err := s.GetKernelInterfaces()
	if err != nil {
		log.Printf("[ApplyInterface] Warning: failed to load kernel interfaces: %v", err)
		return set
	}
	for _, k := range kernelIfaces {
		set[k.Name] = true
	}
	return set
}

// recreateVlanIfPossible recreates a DB-configured VLAN sub-interface that is missing
// from the kernel, mirroring the startup recreate (issue #20). Returns true if the VLAN
// is present in the kernel afterwards (already there or successfully created). It marks
// the name in kernelMap on success so callers can chain further work.
func (s *InterfaceService) recreateVlanIfPossible(iface model.NetworkInterface, kernelMap map[string]bool) bool {
	if kernelMap[iface.Name] {
		return true
	}
	if iface.VlanParent == nil || iface.VlanID == nil {
		log.Printf("[ApplyInterface] Warning: VLAN %s is missing parent/id metadata; skipping recreate.", iface.Name)
		return false
	}
	log.Printf("[ApplyInterface] Recreating missing VLAN %s (parent=%s, id=%d)...", iface.Name, *iface.VlanParent, *iface.VlanID)
	if err := s.network.CreateVlan(*iface.VlanParent, *iface.VlanID); err != nil {
		log.Printf("[ApplyInterface] Warning: failed to recreate VLAN %s: %v", iface.Name, err)
		return false
	}
	kernelMap[iface.Name] = true
	return true
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
				AdminAccess:    []string{"PING", "HTTP", "HTTPS", "SSH"},
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
				AdminAccess:    []string{"PING", "HTTP", "HTTPS", "SSH"},
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
			AdminAccess:    []string{"PING", "HTTP", "HTTPS", "SSH"},
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
					kIface.AdminAccess = []string{"PING", "HTTP", "HTTPS", "SSH"}
				}
			}
		}
		result = append(result, kIface)
	}

	return result, nil
}

// appendOfflineRows returns dataLayer plus any DB-configured interface whose name is not
// present in the data layer (i.e. it has a config row but no live kernel link — a VLAN
// whose parent vanished, a USB NIC that was unplugged). Offline rows are marked
// Status="offline" and Managed=true so the UI can show them with a badge and offer delete.
// Pure function: it does not touch the kernel or DB, so it can be unit-tested directly
// (the mock kernel mirrors every DB row into the kernel list, so offline can't arise there).
func appendOfflineRows(dataLayer []model.NetworkInterface, dbIfaces []model.NetworkInterface) []model.NetworkInterface {
	present := make(map[string]bool, len(dataLayer))
	for _, item := range dataLayer {
		present[item.Name] = true
	}
	result := dataLayer
	for _, dbIface := range dbIfaces {
		if present[dbIface.Name] {
			continue
		}
		dbIface.Status = "offline"
		dbIface.Managed = true
		result = append(result, dbIface)
	}
	return result
}

// GetDataLayerInterfaceIncludingOffline returns the normal data layer plus DB-configured
// interfaces that have no live kernel link (see appendOfflineRows). This is used ONLY by
// the interfaces API surface — other consumers (firewall sync, dhcpcd, hostname, dashboard)
// must keep using GetDataLayerInterface so they never act on a phantom interface.
func (s *InterfaceService) GetDataLayerInterfaceIncludingOffline() ([]model.NetworkInterface, error) {
	list, err := s.GetDataLayerInterface()
	if err != nil {
		return nil, err
	}
	dbIfaces, err := s.repo.GetInterfacesFromDB()
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces configuration from DB: %w", err)
	}
	return appendOfflineRows(list, dbIfaces), nil
}

// GetDataLayerInterfaceByID finds a specific interface in the data layer. It includes
// offline rows so the delete/reset/guard paths in the API can resolve a DB-only interface
// (returning nil, as before, when nothing matches).
func (s *InterfaceService) GetDataLayerInterfaceByID(id string) (*model.NetworkInterface, error) {
	list, err := s.GetDataLayerInterfaceIncludingOffline()
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

// validateAlias enforces the server-side alias rules: character set, case-insensitive
// uniqueness against other DB rows (mirroring the NOCASE unique index so violations
// surface as 409 instead of a DB error), and no collision with another interface's
// OS name — the index cannot see those, and an alias like "eth0" on a different
// interface would make every label ambiguous. selfID/selfName exempt the interface's
// own row and its own name: alias == own name is the default state.
func (s *InterfaceService) validateAlias(alias, selfID, selfName string) error {
	// alias == own OS name is the default state (VLAN create, empty-alias
	// normalization) and must always be accepted, even when the name itself has
	// characters outside the alias pattern (e.g. "eth0.100").
	if alias != selfName && !aliasPattern.MatchString(alias) {
		return fmt.Errorf("%w: %q must contain only letters, numbers and underscores", ErrAliasInvalid, alias)
	}
	exists, err := s.repo.AliasExists(alias, selfID)
	if err != nil {
		return fmt.Errorf("failed to check alias uniqueness: %w", err)
	}
	if exists {
		return fmt.Errorf("%w: %q", ErrAliasConflict, alias)
	}
	kernelIfaces, err := s.GetKernelInterfaces()
	if err != nil {
		return fmt.Errorf("failed to list interfaces for alias check: %w", err)
	}
	for _, k := range kernelIfaces {
		if k.Name != selfName && strings.EqualFold(alias, k.Name) {
			return fmt.Errorf("%w: %q is the name of another interface", ErrAliasConflict, alias)
		}
	}
	return nil
}

// ApplyInterfaceConfig saves the specified interface configuration to both kernel and DB.
func (s *InterfaceService) ApplyInterfaceConfig(iface model.NetworkInterface) error {
	// Empty alias means "default to the OS name" (same rule as CreateVlanInterface):
	// a PUT without an alias must neither persist "" nor be rejected.
	iface.Alias = strings.TrimSpace(iface.Alias)
	if iface.Alias == "" {
		iface.Alias = iface.Name
	}
	if err := s.validateAlias(iface.Alias, iface.ID, iface.Name); err != nil {
		return err
	}

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
		prefer5GHz := false
		if iface.Prefer5GHz != nil {
			prefer5GHz = *iface.Prefer5GHz
		}

		if err := s.network.ConfigureWifi(iface.Name, ssid, password, security, backupSSID, backupPassword, backupSecurity, macMode, prefer5GHz); err != nil {
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

// CreateVlanInterface creates an 802.1Q VLAN sub-interface (link name "<parent>.<id>"),
// brings it up, persists it to the DB (so it is re-created on every boot), and applies
// its IP/mode. The parent must be an existing non-VLAN ethernet interface. Returns the
// created interface. Duplicate names yield ErrVlanExists; bad input yields ErrVlanInvalid.
func (s *InterfaceService) CreateVlanInterface(input model.CreateVlanInput) (*model.NetworkInterface, error) {
	if input.VlanID < 1 || input.VlanID > 4094 {
		return nil, fmt.Errorf("%w: VLAN ID must be between 1 and 4094, got %d", ErrVlanInvalid, input.VlanID)
	}
	parent := strings.TrimSpace(input.Parent)
	if parent == "" {
		return nil, fmt.Errorf("%w: parent interface is required", ErrVlanInvalid)
	}

	kernelIfaces, err := s.GetKernelInterfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to load kernel interfaces: %w", err)
	}

	var parentIface *model.NetworkInterface
	for i := range kernelIfaces {
		if kernelIfaces[i].Name == parent {
			parentIface = &kernelIfaces[i]
			break
		}
	}
	if parentIface == nil {
		return nil, fmt.Errorf("%w: parent interface %q does not exist", ErrVlanInvalid, parent)
	}
	if parentIface.Type != "ethernet" {
		return nil, fmt.Errorf("%w: VLAN parent must be an ethernet interface (%q is %s)", ErrVlanInvalid, parent, parentIface.Type)
	}
	if parentIface.Subtype == "vlan" {
		return nil, fmt.Errorf("%w: cannot create a VLAN on top of another VLAN (%q)", ErrVlanInvalid, parent)
	}

	name := fmt.Sprintf("%s.%d", parent, input.VlanID)

	// Reject duplicates in both the kernel and the DB.
	for _, k := range kernelIfaces {
		if k.Name == name {
			return nil, fmt.Errorf("%w: %q already exists in the kernel", ErrVlanExists, name)
		}
	}
	dbIfaces, err := s.repo.GetInterfacesFromDB()
	if err != nil {
		return nil, fmt.Errorf("failed to load DB interfaces: %w", err)
	}
	for _, d := range dbIfaces {
		if d.Name == name {
			return nil, fmt.Errorf("%w: %q already exists in the database", ErrVlanExists, name)
		}
	}

	// Normalise and validate config fields.
	mode := input.AddressingMode
	if mode == "" {
		mode = "dhcp"
	}
	if mode != "dhcp" && mode != "static" {
		return nil, fmt.Errorf("%w: addressing mode must be dhcp or static", ErrVlanInvalid)
	}
	role := input.Role
	if role == "" {
		role = "LAN"
	}
	if role != "LAN" && role != "WAN" {
		return nil, fmt.Errorf("%w: role must be LAN or WAN", ErrVlanInvalid)
	}

	ip, netmask, gateway := "0.0.0.0", "24", ""
	if mode == "static" {
		if net.ParseIP(input.IP) == nil {
			return nil, fmt.Errorf("%w: a valid IP address is required for static mode", ErrVlanInvalid)
		}
		ip, netmask, gateway = input.IP, input.Netmask, input.Gateway
		if netmask == "" {
			netmask = "24"
		}
	}

	adminAccess := input.AdminAccess
	if len(adminAccess) == 0 {
		if role == "WAN" {
			adminAccess = []string{"PING"}
		} else {
			adminAccess = []string{"PING", "HTTP", "HTTPS", "SSH"}
		}
	}
	alias := strings.TrimSpace(input.Alias)
	if alias == "" {
		alias = name
	}
	if err := s.validateAlias(alias, "iface-"+name, name); err != nil {
		return nil, err
	}

	vlanParent := parent
	vlanID := input.VlanID
	iface := model.NetworkInterface{
		ID:             "iface-" + name,
		Name:           name,
		Alias:          alias,
		Role:           role,
		Type:           "ethernet",
		Subtype:        "vlan",
		AddressingMode: mode,
		IP:             ip,
		Netmask:        netmask,
		Gateway:        gateway,
		MacAddress:     parentIface.MacAddress,
		AdminAccess:    adminAccess,
		Status:         "up",
		Speed:          parentIface.Speed,
		VlanParent:     &vlanParent,
		VlanID:         &vlanID,
	}

	// Create the kernel link, then bring it up.
	if err := s.network.CreateVlan(parent, vlanID); err != nil {
		return nil, fmt.Errorf("failed to create vlan link: %w", err)
	}
	if err := s.network.ToggleInterface(name, true); err != nil {
		log.Printf("[Interface] Warning: failed to bring up new VLAN %s: %v", name, err)
	}

	// Persist the config row (source of truth for boot-time recreate). Roll back the
	// kernel link if persistence fails so we don't leave an orphan interface behind.
	if err := s.repo.UpdateInterface(iface); err != nil {
		if delErr := s.network.DeleteVlan(name); delErr != nil {
			log.Printf("[Interface] Warning: failed to roll back VLAN %s after DB error: %v", name, delErr)
		}
		return nil, fmt.Errorf("failed to persist vlan config: %w", err)
	}

	// Apply IP/mode. A route failure (e.g. no carrier yet) is non-fatal: the link and
	// config are already saved and will be re-applied on toggle/boot, same as startup.
	if err := s.network.ConfigureInterface(name, mode, ip, netmask, gateway, 0); err != nil {
		log.Printf("[Interface] Warning: failed to apply IP config to new VLAN %s: %v", name, err)
	}

	return &iface, nil
}

// DeleteVlanInterface removes a VLAN sub-interface: it deletes the kernel link (guarded
// by a link-type check inside the kernel layer) and then removes the DB config row.
func (s *InterfaceService) DeleteVlanInterface(id string) error {
	iface, err := s.repo.GetInterfaceByID(id)
	if err != nil {
		return fmt.Errorf("failed to load interface: %w", err)
	}
	if iface == nil {
		return fmt.Errorf("interface %q not found", id)
	}
	if iface.Subtype != "vlan" {
		return fmt.Errorf("interface %q is not a vlan", iface.Name)
	}

	// The kernel link may already be gone (parent interface removed, offline VLAN). The
	// goal here is to delete the config, so only attempt the netlink delete when the link
	// is actually present; otherwise skip straight to removing the DB row.
	if s.kernelInterfaceNameSet()[iface.Name] {
		if err := s.network.DeleteVlan(iface.Name); err != nil {
			return fmt.Errorf("failed to delete vlan link: %w", err)
		}
	} else {
		log.Printf("[Interface] VLAN %s has no kernel link; deleting DB config only.", iface.Name)
	}

	if err := s.repo.DeleteInterface(id); err != nil {
		return err
	}

	s.PruneDNSServerBinding(iface.Name)
	return nil
}

// PruneDNSServerBinding removes name from the persisted DNS Server interface bindings, so
// an explicitly deleted interface doesn't linger as a dangling "Missing" chip. This is an
// explicit user action (not auto-heal), which is why it's allowed to drop user config. A
// failure is non-fatal: the interface is already gone and the leftover name is tolerated by
// HandleUpdateDNSServerSettings anyway.
func (s *InterfaceService) PruneDNSServerBinding(name string) {
	saved, err := s.repo.GetDNSServerInterfaces()
	if err != nil {
		log.Printf("[Interface] Warning: failed to load DNS server settings after deleting %s: %v", name, err)
		return
	}
	pruned := make([]string, 0, len(saved))
	changed := false
	for _, saved := range saved {
		if saved == name {
			changed = true
			continue
		}
		pruned = append(pruned, saved)
	}
	if changed {
		if err := s.repo.SetDNSServerInterfaces(pruned); err != nil {
			log.Printf("[Interface] Warning: failed to prune DNS server settings after deleting %s: %v", name, err)
		}
	}
}
