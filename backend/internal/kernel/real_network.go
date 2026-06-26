//go:build linux

package kernel

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pigate/internal/model"

	"github.com/vishvananda/netlink"
)

// RealNetwork implements NetworkManager using netlink for direct kernel interaction.
// This avoids shell command execution (no Command Injection risk).
// Requires cap_net_admin capability on the binary:
//
//	sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend
var execCommand = exec.Command

type RealNetwork struct{}

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
			serviceName := fmt.Sprintf("wpa_supplicant@%s", name)
			log.Printf("[RealNetwork] Interface is wireless. Verifying service state: %s", serviceName)
			if execCommand("sudo", "systemctl", "is-active", "--quiet", serviceName).Run() != nil {
				// Clean up stale socket file before starting the service
				socketPath := filepath.Join(wpaSocketDir, name)
				log.Printf("[RealNetwork] Cleaning up stale wpa_supplicant socket if exists: %s", socketPath)
				_ = os.Remove(socketPath)
				// _ = execCommand("sudo", "rm", "-f", socketPath).Run()

				log.Printf("[RealNetwork] Service %s is not active, starting it...", serviceName)
				_ = execCommand("sudo", "systemctl", "start", serviceName).Run()
			} else {
				log.Printf("[RealNetwork] Service %s is already active", serviceName)
			}

			// Clean up old DHCP client instances first
			_ = execCommand("sudo", "dhclient", "-r", name).Run()
			_ = execCommand("sudo", "dhcpcd", "-k", name).Run()

			// Launch DHCP client in background to request an IP address on association
			go func() {
				time.Sleep(1 * time.Second)
				log.Printf("[RealNetwork] Launching DHCP client for %s...", name)
				if err := execCommand("sudo", "dhclient", name).Start(); err == nil {
					log.Printf("[RealNetwork] dhclient started for %s via ToggleInterface", name)
					return
				}
				if err := execCommand("sudo", "dhcpcd", name).Start(); err == nil {
					log.Printf("[RealNetwork] dhcpcd started for %s via ToggleInterface", name)
					return
				}
				log.Printf("[RealNetwork] Failed to start dhclient or dhcpcd on %s via ToggleInterface", name)
			}()
		}
		return nil
	}

	if isWireless {
		serviceName := fmt.Sprintf("wpa_supplicant@%s", name)
		log.Printf("[RealNetwork] Interface %s is wireless. Stopping wpa_supplicant service: %s", name, serviceName)
		_ = execCommand("sudo", "systemctl", "stop", serviceName).Run()

		// Release DHCP lease when bringing interface DOWN
		log.Printf("[RealNetwork] Releasing DHCP client for %s...", name)
		_ = execCommand("sudo", "dhclient", "-r", name).Run()
		_ = execCommand("sudo", "dhcpcd", "-k", name).Run()
	}

	log.Printf("[RealNetwork] Bringing interface %s DOWN via netlink link...", name)
	if err := netlink.LinkSetDown(link); err != nil {
		log.Printf("[RealNetwork] Failed to set LinkSetDown for %s: %v", name, err)
		return fmt.Errorf("failed to bring interface %q down: %w", name, err)
	}
	return nil
}

// ConfigureWifi writes the wpa_supplicant config file atomically and reloads/starts the service.
func (r *RealNetwork) ConfigureWifi(name string, ssid string, password string, security string, backupSSID string, backupPassword string, backupSecurity string, macMode string) error {
	log.Printf("[RealNetwork] ConfigureWifi started: interface=%s, SSID=%q, Security=%s, BackupSSID=%q, BackupSecurity=%s, MacMode=%s",
		name, ssid, security, backupSSID, backupSecurity, macMode)

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
	configContent := GenerateWpaConfig(ssid, password, security, backupSSID, backupPassword, backupSecurity, macMode)

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
	serviceName := fmt.Sprintf("wpa_supplicant@%s", name)
	isActive := execCommand("sudo", "systemctl", "is-active", "--quiet", serviceName).Run() == nil

	if isActive {
		// Send RECONFIGURE command via wpa_supplicant UNIX socket
		log.Printf("[RealNetwork] Service %s is already running. Triggering socket RECONFIGURE...", serviceName)
		if _, err := SendWpaCommand(name, "RECONFIGURE"); err != nil {
			log.Printf("[RealNetwork] RECONFIGURE socket command failed: %v", err)
			return fmt.Errorf("failed to reload wpa_supplicant config: %w", err)
		}
		log.Printf("[RealNetwork] wpa_supplicant config reloaded successfully")
	} else {
		// Start service via systemd
		log.Printf("[RealNetwork] Service %s is inactive. Initiating systemd start...", serviceName)

		// Clean up stale socket file before starting the service
		socketPath := filepath.Join(wpaSocketDir, name)
		log.Printf("[RealNetwork] Cleaning up stale wpa_supplicant socket if exists: %s", socketPath)
		_ = os.Remove(socketPath)
		// _ = execCommand("sudo", "rm", "-f", socketPath).Run()

		if err := execCommand("sudo", "systemctl", "start", serviceName).Run(); err != nil {
			log.Printf("[RealNetwork] systemd start %s failed: %v", serviceName, err)
			return fmt.Errorf("failed to start %s service: %w", serviceName, err)
		}
		log.Printf("[RealNetwork] Service %s started successfully", serviceName)
	}

	return nil
}

// ConfigureInterface configures the IP address, netmask, gateway, and addressing mode of an interface using Netlink.
// For DHCP mode, it clears static IPs and routes, and spawns/signals dhclient/dhcpcd to request an address.
// For Static mode, it clears existing IPv4 addresses and sets the specified static IP/Netmask and gateway route.
func (r *RealNetwork) ConfigureInterface(name string, mode string, ip string, netmask string, gateway string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("interface %q not found: %w", name, err)
	}

	// Bring the interface up if it is not up, as IP configuration and routing require an active link
	if link.Attrs().Flags&net.FlagUp == 0 {
		if err := netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("failed to bring interface %q up: %w", name, err)
		}
	}

	// Always release DHCP client and clean up first to prevent conflicts/duplicates
	_ = execCommand("sudo", "dhclient", "-r", name).Run()
	_ = execCommand("sudo", "dhcpcd", "-k", name).Run()
	log.Printf("[RealNetwork] dhclient released for %s", name)

	// Always clear existing IPv4 addresses from the interface
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err == nil {
		for _, a := range addrs {
			_ = netlink.AddrDel(link, &a)
		}
	}

	if mode == "dhcp" {
		// Try starting dhclient in background
		if err := execCommand("sudo", "dhclient", name).Start(); err == nil {
			log.Printf("[RealNetwork] dhclient started for %s", name)
			return nil
		}
		// Try fallback to dhcpcd
		if err := execCommand("sudo", "dhcpcd", name).Start(); err == nil {
			log.Printf("[RealNetwork] dhcpcd started for %s", name)
			return nil
		}
		log.Printf("[RealNetwork] failed to start dhclient or dhcpcd for DHCP mode on %s", name)
		// If neither dhclient nor dhcpcd exists or starts, we return error
		return fmt.Errorf("failed to start dhclient or dhcpcd for DHCP mode on %s", name)
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

		// Parse default destination (0.0.0.0/0)
		_, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
		route := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       defaultNet,
			Gw:        gwIP,
			Priority:  100,
		}

		if err := netlink.RouteAdd(route); err != nil {
			return fmt.Errorf("failed to add default gateway route: %w", err)
		}
	}

	return nil
}

// ScanWifi scans for nearby Wi-Fi networks using iw.
// iw does not require root — only cap_net_raw for raw socket access.
func (r *RealNetwork) ScanWifi(name string) ([]model.WifiScanResult, error) {
	// Use iw (lightweight, no D-Bus dependency)
	return scanWifiWithIW(name)
}

// scanWifiWithIW uses `iw dev <name> scan` to list nearby APs.
func scanWifiWithIW(name string) ([]model.WifiScanResult, error) {
	out, err := execCommand("sudo", "iw", "dev", name, "scan").Output()
	if err != nil {
		return nil, fmt.Errorf("iw scan failed: %w", err)
	}

	var results []model.WifiScanResult
	var current *model.WifiScanResult

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Start of a new BSS block
		if strings.HasPrefix(line, "BSS ") {
			if current != nil {
				results = append(results, *current)
			}
			current = &model.WifiScanResult{}
			continue
		}
		if current == nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "SSID: "):
			current.SSID = strings.TrimPrefix(line, "SSID: ")

		case strings.HasPrefix(line, "signal: "):
			// Format: "signal: -65.00 dBm"
			raw := strings.TrimPrefix(line, "signal: ")
			raw = strings.Fields(raw)[0]
			var dBm float64
			fmt.Sscanf(raw, "%f", &dBm)
			// Convert dBm to 0–100% signal quality
			// Range: -100 dBm (0%) to -30 dBm (100%)
			if dBm <= -100 {
				current.Signal = 0
			} else if dBm >= -30 {
				current.Signal = 100
			} else {
				current.Signal = int(2 * (dBm + 100))
			}

		case strings.HasPrefix(line, "DS Parameter set: channel "):
			fmt.Sscanf(strings.TrimPrefix(line, "DS Parameter set: channel "), "%d", &current.Channel)

		case strings.Contains(line, "WPA"):
			if current.Security == "" || current.Security == "Open" {
				if strings.Contains(line, "WPA3") {
					current.Security = "WPA3"
				} else if strings.Contains(line, "WPA2") {
					current.Security = "WPA2-PSK"
				} else {
					current.Security = "WPA"
				}
			}

		case strings.HasPrefix(line, "* primary channel:"):
			// 5 GHz channels are > 14
			if current.Channel > 14 {
				current.Frequency = "5 GHz"
			} else {
				current.Frequency = "2.4 GHz"
			}
		}
	}

	// Append last block
	if current != nil {
		if current.Security == "" {
			current.Security = "Open"
		}
		if current.Frequency == "" {
			if current.Channel > 14 {
				current.Frequency = "5 GHz"
			} else {
				current.Frequency = "2.4 GHz"
			}
		}
		results = append(results, *current)
	}

	// Remove entries with no SSID (hidden networks)
	filtered := results[:0]
	for _, r := range results {
		if r.SSID != "" {
			filtered = append(filtered, r)
		}
	}

	return filtered, nil
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
