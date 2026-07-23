//go:build linux

package kernel

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pigate/internal/model"

	"github.com/mdlayher/wifi"
	"github.com/vishvananda/netlink"
	"golang.org/x/sync/singleflight"
)

// RealNetwork implements NetworkManager using netlink for direct kernel interaction.
// This avoids shell command execution (no Command Injection risk).
// Requires cap_net_admin capability on the binary:
//
//	sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend
var execCommand = exec.Command

type RealNetwork struct {
	// scanGroup dedups concurrent ScanWifi calls per interface name so that
	// spamming the scan HTTP endpoint cannot fire overlapping TRIGGER_SCAN
	// requests on the same interface. Zero value is ready to use.
	scanGroup singleflight.Group
}

func NewRealNetwork() *RealNetwork {
	return &RealNetwork{}
}

// ToggleInterface brings a network interface up or down via netlink socket.
// Equivalent to `ip link set <name> up/down` but without shell execution.
func (r *RealNetwork) ToggleInterface(name string, up bool) error {
	log.Printf("[RealNetwork] ToggleInterface called: interface=%s, up=%t", name, up)
	link, err := netlink.LinkByName(name)
	if err != nil {
		log.Printf("[RealNetwork] Interface %q not found: %v", name, err)
		return fmt.Errorf("interface %q not found: %w", name, err)
	}

	isWireless := strings.HasPrefix(name, "w")

	if up {
		log.Printf("[RealNetwork] Bringing interface %s UP via netlink link...", name)
		if err := netlink.LinkSetUp(link); err != nil {
			log.Printf("[RealNetwork] Failed to set LinkSetUp for %s: %v", name, err)
			return fmt.Errorf("failed to bring interface %q up: %w", name, err)
		}
		if isWireless {
			serviceName := fmt.Sprintf("wpa_supplicant@%s.service", name)
			log.Printf("[RealNetwork] Interface is wireless. Verifying service state: %s", serviceName)
			// if execCommand("sudo", "systemctl", "is-active", "--quiet", serviceName).Run() != nil {
			// 	// Clean up stale socket file before starting the service
			// 	socketPath := filepath.Join(wpaSocketDir, name)
			// 	log.Printf("[RealNetwork] Cleaning up stale wpa_supplicant socket if exists: %s", socketPath)
			// 	_ = os.Remove(socketPath)
			// 	// _ = execCommand("sudo", "rm", "-f", socketPath).Run()

			// 	log.Printf("[RealNetwork] Service %s is not active, starting it...", serviceName)
			// 	_ = execCommand("sudo", "systemctl", "start", serviceName).Run()
			// } else {
			// 	log.Printf("[RealNetwork] Service %s is already active", serviceName)
			// }

			// 🛠️ เปลี่ยนมาใช้ D-Bus เช็กสถานะ
			if !IsServiceActiveViaDBus(serviceName) {
				socketPath := filepath.Join(wpaSocketDir, name)
				_ = os.Remove(socketPath)

				log.Printf("[RealNetwork] Service %s is not active, starting it via D-Bus...", serviceName)
				// 🛠️ เปลี่ยนมาใช้ D-Bus Start
				_ = StartServiceViaDBus(serviceName)
			} else {
				log.Printf("[RealNetwork] Service %s is already active", serviceName)
			}

		}
		return nil
	}

	if isWireless {
		serviceName := fmt.Sprintf("wpa_supplicant@%s.service", name)
		log.Printf("[RealNetwork] Interface %s is wireless. Stopping wpa_supplicant service: %s", name, serviceName)
		// _ = execCommand("sudo", "systemctl", "stop", serviceName).Run()
		_ = StopServiceViaDBus(serviceName)
	}

	log.Printf("[RealNetwork] Bringing interface %s DOWN via netlink link...", name)
	if err := netlink.LinkSetDown(link); err != nil {
		log.Printf("[RealNetwork] Failed to set LinkSetDown for %s: %v", name, err)
		return fmt.Errorf("failed to bring interface %q down: %w", name, err)
	}
	return nil
}

// ConfigureWifi writes the wpa_supplicant config file atomically and reloads/starts the service.
func (r *RealNetwork) ConfigureWifi(name string, ssid string, password string, security string, backupSSID string, backupPassword string, backupSecurity string, macMode string, prefer5GHz bool) error {
	log.Printf("[RealNetwork] ConfigureWifi started: interface=%s, SSID=%q, Security=%s, BackupSSID=%q, BackupSecurity=%s, MacMode=%s, Prefer5GHz=%t",
		name, ssid, security, backupSSID, backupSecurity, macMode, prefer5GHz)

	// Validate interface name to prevent traversal or command parameter injection
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid interface name: %q", name)
	}
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_') {
			return fmt.Errorf("interface name %q contains disallowed characters", name)
		}
	}

	// Generate the wpa_supplicant config content
	configContent := GenerateWpaConfig(ssid, password, security, backupSSID, backupPassword, backupSecurity, macMode, prefer5GHz)

	// Determine the paths
	configPath := filepath.Join(wpaConfigDir, fmt.Sprintf("wpa_supplicant-%s.conf", name))
	tmpPath := configPath + ".tmp"

	// Ensure config directory exists
	log.Printf("[RealNetwork] Ensuring directory exists: %s", filepath.Dir(configPath))
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		log.Printf("[RealNetwork] MkdirAll config directory failed: %v", err)
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write atomically: write to temp file with 0600 permissions
	log.Printf("[RealNetwork] Writing temporary config file: %s", tmpPath)
	if err := os.WriteFile(tmpPath, []byte(configContent), 0600); err != nil {
		log.Printf("[RealNetwork] Write temporary config file failed: %v", err)
		return fmt.Errorf("failed to write temporary config file: %w", err)
	}

	// Rename atomically
	log.Printf("[RealNetwork] Overwriting main config atomically: %s -> %s", tmpPath, configPath)
	if err := os.Rename(tmpPath, configPath); err != nil {
		log.Printf("[RealNetwork] Rename temporary config file failed: %v", err)
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to replace config file: %w", err)
	}

	// Systemd service management
	serviceName := fmt.Sprintf("wpa_supplicant@%s.service", name)
	// isActive := execCommand("sudo", "systemctl", "is-active", "--quiet", serviceName).Run() == nil
	isActive := IsServiceActiveViaDBus(serviceName)

	if isActive {
		// Send RECONFIGURE command via wpa_supplicant UNIX socket
		log.Printf("[RealNetwork] Service %s is already running. Triggering socket RECONFIGURE...", serviceName)
		if _, err := SendWpaCommand(name, "RECONFIGURE"); err != nil {
			log.Printf("[RealNetwork] RECONFIGURE socket command failed: %v", err)
			return fmt.Errorf("failed to reload wpa_supplicant config: %w", err)
		}
		log.Printf("[RealNetwork] wpa_supplicant config reloaded successfully")
	} else {
		// The config file is now written to disk (persistence done). Starting the
		// wpa_supplicant service would drive the link up, which must not happen while
		// the interface is administratively down — that is the Wi-Fi equivalent of the
		// Save-silently-enables bug. Only start the service when the link is up; when the
		// interface is later toggled up, ToggleInterface starts the service itself.
		if link, err := netlink.LinkByName(name); err == nil {
			if link.Attrs().Flags&net.FlagUp == 0 {
				log.Printf("[RealNetwork] %s is down; wrote wpa_supplicant config but not starting %s (will start on toggle up)", name, serviceName)
				return nil
			}
		} else {
			log.Printf("[RealNetwork] Warning: could not read link state for %s before starting %s: %v", name, serviceName, err)
		}

		// Start service via systemd
		log.Printf("[RealNetwork] Service %s is inactive. Initiating systemd start...", serviceName)

		// Clean up stale socket file before starting the service
		socketPath := filepath.Join(wpaSocketDir, name)
		log.Printf("[RealNetwork] Cleaning up stale wpa_supplicant socket if exists: %s", socketPath)
		_ = os.Remove(socketPath)
		// _ = execCommand("sudo", "rm", "-f", socketPath).Run()

		// if err := execCommand("sudo", "systemctl", "start", serviceName).Run(); err != nil {
		// 	log.Printf("[RealNetwork] systemd start %s failed: %v", serviceName, err)
		// 	return fmt.Errorf("failed to start %s service: %w", serviceName, err)
		// }
		if err := StartServiceViaDBus(serviceName); err != nil {
			log.Printf("[RealNetwork] D-Bus start %s failed: %v", serviceName, err)
			return fmt.Errorf("failed to start %s service: %w", serviceName, err)
		}

		log.Printf("[RealNetwork] Service %s started successfully", serviceName)
	}

	return nil
}

// ConfigureInterface configures the IP address, netmask, gateway, and addressing mode of an interface using Netlink.
// For DHCP mode, it clears static IPs and routes, and spawns/signals dhclient/dhcpcd to request an address.
// For Static mode, it clears existing IPv4 addresses and sets the specified static IP/Netmask and gateway route.
func (r *RealNetwork) ConfigureInterface(name string, mode string, ip string, netmask string, gateway string, metric int) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("interface %q not found: %w", name, err)
	}

	// "configure" and "activate" are separate actions: configuring an interface must
	// never force the link up (that is what causes a Save to silently re-enable an
	// interface the user has turned off). The link state is the desired state stored in
	// the DB and only changed via ToggleInterface / SetInterfaceState.
	//
	// Address assignment (netlink AddrAdd) works on a down link, so static IPs are still
	// applied. The gateway default route, however, cannot be installed while the link is
	// down (the kernel reports the network as unreachable, and would drop the route on
	// down anyway); it is deferred until the interface is toggled up.
	linkIsUp := link.Attrs().Flags&net.FlagUp != 0

	// Always clear existing IPv4 addresses from the interface
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err == nil {
		for _, a := range addrs {
			_ = netlink.AddrDel(link, &a)
		}
	}

	if mode == "dhcp" {
		return nil
	}

	// Static mode configuration
	if ip == "" || netmask == "" {
		return fmt.Errorf("IP address and netmask are required for static configuration")
	}

	// Add static IP address
	addr, err := netlink.ParseAddr(fmt.Sprintf("%s/%s", ip, netmask))
	if err != nil {
		return fmt.Errorf("invalid CIDR address %s/%s: %w", ip, netmask, err)
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("failed to set static IP %s/%s: %w", ip, netmask, err)
	}

	// Configure default gateway route if specified
	if gateway != "" {
		// A default route cannot be installed while the link is down; defer it until
		// the interface is toggled up (SetInterfaceState reapplies this configuration).
		if !linkIsUp {
			log.Printf("[RealNetwork] %s is down; deferring gateway route %s until the interface is toggled up", name, gateway)
			return nil
		}

		gwIP := net.ParseIP(gateway)
		if gwIP == nil {
			return fmt.Errorf("invalid gateway IP format: %q", gateway)
		}

		// Delete existing default routes to prevent conflict
		routes, err := netlink.RouteList(link, netlink.FAMILY_V4)
		if err == nil {
			for _, rt := range routes {
				if rt.Dst == nil || rt.Dst.String() == "0.0.0.0/0" {
					_ = netlink.RouteDel(&rt)
				}
			}
		}

		// Determine route priority: metric <= 0 means "unset", keep the historical default of 100
		priority := 100
		if metric > 0 {
			priority = metric
		}

		// Parse default destination (0.0.0.0/0)
		_, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
		route := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       defaultNet,
			Gw:        gwIP,
			Priority:  priority,
		}

		if err := netlink.RouteAdd(route); err != nil {
			return fmt.Errorf("failed to add default gateway route: %w", err)
		}
	}

	return nil
}

// CreateVlan creates an 802.1Q VLAN sub-interface named "<parent>.<vlanID>" via netlink.
// Equivalent to `ip link add link <parent> name <parent>.<vlanID> type vlan id <vlanID>`
// but as a direct netlink syscall (no shell execution). Requires cap_net_admin.
func (r *RealNetwork) CreateVlan(parent string, vlanID int) error {
	log.Printf("[RealNetwork] CreateVlan called: parent=%s, vlanID=%d", parent, vlanID)
	if vlanID < 1 || vlanID > 4094 {
		return fmt.Errorf("invalid VLAN ID %d: must be between 1 and 4094", vlanID)
	}

	parentLink, err := netlink.LinkByName(parent)
	if err != nil {
		return fmt.Errorf("parent interface %q not found: %w", parent, err)
	}

	name := fmt.Sprintf("%s.%d", parent, vlanID)
	vlan := &netlink.Vlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:        name,
			ParentIndex: parentLink.Attrs().Index,
		},
		VlanId:       vlanID,
		VlanProtocol: netlink.VLAN_PROTOCOL_8021Q,
	}

	if err := netlink.LinkAdd(vlan); err != nil {
		if errors.Is(err, os.ErrExist) || strings.Contains(strings.ToLower(err.Error()), "file exists") {
			return fmt.Errorf("vlan %q already exists", name)
		}
		return fmt.Errorf("failed to create vlan %q: %w", name, err)
	}
	log.Printf("[RealNetwork] VLAN %s created successfully", name)
	return nil
}

// DeleteVlan removes a VLAN link. It first verifies the link's kernel type is "vlan"
// so a stray DELETE can never remove a physical interface (eth0/wlan0). This guard
// is enforced here at the kernel layer independently of any handler-level check.
func (r *RealNetwork) DeleteVlan(name string) error {
	log.Printf("[RealNetwork] DeleteVlan called: name=%s", name)
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("interface %q not found: %w", name, err)
	}
	if link.Type() != "vlan" {
		return fmt.Errorf("refusing to delete %q: not a vlan interface (type=%q)", name, link.Type())
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete vlan %q: %w", name, err)
	}
	log.Printf("[RealNetwork] VLAN %s deleted successfully", name)
	return nil
}

// GetIPv4Addresses returns the current IPv4 addresses assigned to the
// interface as CIDR strings (e.g. "169.254.1.2/16"). Part of the DHCP
// health-checker (issue #78) support added to NetworkManager.
func (r *RealNetwork) GetIPv4Addresses(name string) ([]string, error) {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("interface %q not found: %w", name, err)
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to list IPv4 addresses for %q: %w", name, err)
	}

	result := make([]string, 0, len(addrs))
	for _, a := range addrs {
		result = append(result, a.IPNet.String())
	}
	return result, nil
}

// DeleteAddress removes a single address (given as CIDR) from the interface,
// leaving any other addresses untouched. Part of the DHCP health-checker
// (issue #78) support: used to strip a stray 169.254.x.x APIPA address while
// a real IP coexists on the same interface, without going through
// ConfigureInterface (which clears every IPv4 address on the link).
func (r *RealNetwork) DeleteAddress(name string, cidr string) error {
	log.Printf("[RealNetwork] DeleteAddress called: interface=%s, cidr=%s", name, cidr)
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("interface %q not found: %w", name, err)
	}

	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR address %q: %w", cidr, err)
	}

	if err := netlink.AddrDel(link, addr); err != nil {
		// The address may have already disappeared (e.g. dhcpcd obtained a
		// real lease in the split-second between reading the address list
		// and issuing this delete). That is not a failure — the desired
		// outcome (no stray address) has already happened — so log and
		// swallow the error rather than surfacing a false-positive failure.
		if errors.Is(err, syscall.EADDRNOTAVAIL) || errors.Is(err, syscall.ENOENT) || strings.Contains(strings.ToLower(err.Error()), "cannot assign requested address") {
			log.Printf("[RealNetwork] DeleteAddress: %s no longer has %s (already removed, likely raced with a new lease): %v", name, cidr, err)
			return nil
		}
		return fmt.Errorf("failed to delete address %q from %q: %w", cidr, name, err)
	}
	log.Printf("[RealNetwork] Address %s removed from %s", cidr, name)
	return nil
}

// ScanWifi scans for nearby Wi-Fi networks using Netlink (nl80211).
//
// AccessPoints() only dumps whatever BSS entries the kernel currently has cached
// (GET_SCAN) — it does not itself trigger a scan. On a cold cache (e.g. right after the
// interface came up, or after wpa_supplicant flushed it) a single trigger-then-read-once
// races the kernel and returns empty. So ScanWifi fires TRIGGER_SCAN at most once, then
// polls the (read-only) cache for up to ~10s until it sees a non-empty result, tolerating
// EBUSY from a concurrent scan (most commonly wpa_supplicant associating on the same
// interface) instead of failing immediately.
func (r *RealNetwork) ScanWifi(name string) ([]model.WifiScanResult, error) {
	// Dedup concurrent scans per interface: if a scan for this interface is
	// already in flight, join it instead of firing another TRIGGER_SCAN.
	// Keyed by interface name (not a constant) so unrelated interfaces
	// (e.g. wlan0 vs. a USB dongle) can still scan concurrently.
	v, err, shared := r.scanGroup.Do(name, func() (interface{}, error) {
		return r.scanWifiOnce(name)
	})
	if shared {
		// shared is true for every caller (the one that actually triggered the
		// scan included) whenever at least one other concurrent request joined
		// it instead of firing its own TRIGGER_SCAN — see the "triggering scan"
		// log for which goroutine was the one that actually did the work.
		log.Printf("[RealNetwork] ScanWifi: %s result was shared across concurrent callers (dedup by singleflight)", name)
	}
	if err != nil {
		return nil, err
	}
	return v.([]model.WifiScanResult), nil
}

// scanWifiOnce performs a single trigger-then-poll Wi-Fi scan for the given
// interface. It is only ever invoked once per in-flight singleflight call
// for that interface name; concurrent callers join the same call via
// RealNetwork.scanGroup instead of re-entering here.
func (r *RealNetwork) scanWifiOnce(name string) ([]model.WifiScanResult, error) {
	c, err := wifi.New()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize wifi client: %w", err)
	}
	defer c.Close()

	ifis, err := c.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to list wifi interfaces: %w", err)
	}

	var ifi *wifi.Interface
	for _, i := range ifis {
		if i.Name == name {
			ifi = i
			break
		}
	}
	if ifi == nil {
		return nil, fmt.Errorf("wifi interface %s not found", name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Trigger a scan in the kernel. This must happen at most once per call: the poll loop
	// below only issues read-only GET_SCAN dumps so we never fight wpa_supplicant's own
	// scans while it is trying to associate.
	scanStart := time.Now()
	log.Printf("[RealNetwork] ScanWifi: triggering scan on %s", name)
	scanErr := c.Scan(ctx, ifi)
	var pendingErr error
	switch {
	case scanErr == nil:
		// Triggered successfully.
	case errors.Is(scanErr, syscall.EBUSY):
		// Another scan (commonly wpa_supplicant associating) is already in flight on this
		// interface. Not fatal: it will populate the same kernel cache we are about to poll.
		log.Printf("[RealNetwork] ScanWifi: %s scan trigger returned EBUSY (scan already in progress), polling existing cache", name)
	case errors.Is(scanErr, syscall.ENETDOWN):
		return nil, fmt.Errorf("interface %s is down; bring it up before scanning", name)
	case errors.Is(scanErr, syscall.ERFKILL), strings.Contains(strings.ToLower(scanErr.Error()), "rf-kill"):
		return nil, fmt.Errorf("radio for %s is blocked by rfkill", name)
	default:
		// Unknown error: best-effort, keep polling the cache in case a concurrent scan still
		// fills it. Only surfaced if polling ultimately ends up empty.
		log.Printf("[RealNetwork] ScanWifi: %s scan trigger failed, will keep polling cache: %v", name, scanErr)
		pendingErr = scanErr
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	pollStart := time.Now()
	for attempt := 0; ; attempt++ {
		if bssList, err := c.AccessPoints(ifi); err == nil {
			if results := mapWifiScanResults(bssList); len(results) > 0 {
				log.Printf("[RealNetwork] ScanWifi: %s found %d network(s) after %d poll(s), %s polling (%s total since trigger)",
					name, len(results), attempt, time.Since(pollStart).Round(time.Millisecond), time.Since(scanStart).Round(time.Millisecond))
				return results, nil
			}
		}

		select {
		case <-ctx.Done():
			log.Printf("[RealNetwork] ScanWifi: %s scan timed out with no networks found", name)
			if pendingErr != nil {
				return nil, fmt.Errorf("wifi scan on %s failed: %w", name, pendingErr)
			}
			return []model.WifiScanResult{}, nil
		case <-ticker.C:
			// poll again
		}
	}
}

// mapWifiScanResults converts a raw BSS dump from the kernel into the API-facing
// model.WifiScanResult shape, sorted by signal strength descending. Hidden (empty-SSID)
// BSS entries are skipped.
func mapWifiScanResults(bssList []*wifi.BSS) []model.WifiScanResult {
	var results []model.WifiScanResult
	for _, b := range bssList {
		if b.SSID == "" {
			continue // skip hidden SSIDs
		}

		// Convert Signal strength (in mBm) to dBm
		dBm := float64(b.Signal) / 100.0
		var signalPercent int
		if dBm <= -100 {
			signalPercent = 0
		} else if dBm >= -30 {
			signalPercent = 100
		} else {
			signalPercent = int(2 * (dBm + 100))
		}

		// Map Frequency (MHz) to Channel
		channel := frequencyToChannel(b.Frequency)

		// Map Frequency to Band Type (2.4 GHz / 5 GHz)
		frequencyBand := "2.4 GHz"
		if b.Frequency >= 5000 {
			frequencyBand = "5 GHz"
		}

		// Detect Security capability from RSN (Robust Security Network)
		security := "Open"
		if b.RSN.IsInitialized() {
			security = "WPA2-PSK" // default fallback

			// Check AKMs (Authentication and Key Management)
			for _, akm := range b.RSN.AKMs {
				akmStr := akm.String()
				if strings.Contains(akmStr, "SAE") {
					security = "WPA3"
					break
				} else if strings.Contains(akmStr, "802.1X") {
					security = "WPA2-Enterprise"
					break
				}
			}
		}

		results = append(results, model.WifiScanResult{
			SSID:      b.SSID,
			Signal:    signalPercent,
			Security:  security,
			Channel:   channel,
			Frequency: frequencyBand,
		})
	}

	// Sort results by signal strength descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Signal > results[j].Signal
	})

	return results
}

func frequencyToChannel(freq int) int {
	if freq >= 2412 && freq <= 2472 {
		return (freq - 2407) / 5
	} else if freq == 2484 {
		return 14
	} else if freq >= 5000 && freq <= 5900 {
		return (freq - 5000) / 5
	}
	return 0
}

// GetWifiStatus queries wpa_supplicant via socket to fetch live status details.
func (r *RealNetwork) GetWifiStatus(name string) (*model.WifiConnectionStatus, error) {
	log.Printf("[RealNetwork] GetWifiStatus called for interface %s", name)

	// Validate interface name to prevent traversal or parameter injection
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return nil, fmt.Errorf("invalid interface name: %q", name)
	}

	resp, err := SendWpaCommand(name, "STATUS")
	if err != nil {
		log.Printf("[RealNetwork] SendWpaCommand STATUS failed: %v", err)
		return nil, fmt.Errorf("failed to send STATUS command to wpa_supplicant: %w", err)
	}

	status := &model.WifiConnectionStatus{
		State: "DISCONNECTED",
	}

	lines := strings.Split(resp, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := parts[1]

		switch key {
		case "wpa_state":
			status.State = val
		case "ssid":
			status.SSID = val
		case "bssid":
			status.BSSID = val
		case "freq":
			if f, err := strconv.Atoi(val); err == nil {
				status.Freq = f
			}
		case "key_mgmt":
			// Normalize key_mgmt to standard security modes
			normalized := val
			switch val {
			case "WPA2-PSK", "WPA-PSK":
				normalized = "WPA2"
			case "SAE":
				normalized = "WPA3"
			case "WPA-PSK SAE":
				normalized = "WPA2/WPA3"
			case "NONE":
				normalized = "Open"
			}
			status.KeyMgmt = normalized
		case "wifi_generation":
			switch val {
			case "4":
				status.WifiGen = "WiFi 4"
			case "5":
				status.WifiGen = "WiFi 5"
			case "6":
				status.WifiGen = "WiFi 6"
			case "7":
				status.WifiGen = "WiFi 7"
			}
		case "ieee80211ax":
			if val == "1" {
				status.WifiGen = "WiFi 6"
			}
		case "ieee80211ac":
			if val == "1" && status.WifiGen == "" {
				status.WifiGen = "WiFi 5"
			}
		case "ieee80211n":
			if val == "1" && status.WifiGen == "" {
				status.WifiGen = "WiFi 4"
			}
		}
	}

	// Fallback heuristic for wifi generation based on frequency if not explicitly provided
	if status.WifiGen == "" && status.State == "COMPLETED" {
		if status.Freq > 5000 {
			status.WifiGen = "WiFi 5"
		} else if status.Freq > 0 {
			status.WifiGen = "WiFi 4"
		}
	}

	// Fetch actual active MAC address of the interface
	if iface, err := net.InterfaceByName(name); err == nil {
		status.ActiveMac = iface.HardwareAddr.String()
	}

	log.Printf("[RealNetwork] GetWifiStatus result: State=%s, SSID=%s, BSSID=%s, ActiveMac=%s, Freq=%d, KeyMgmt=%s, WifiGen=%s",
		status.State, status.SSID, status.BSSID, status.ActiveMac, status.Freq, status.KeyMgmt, status.WifiGen)
	return status, nil
}
