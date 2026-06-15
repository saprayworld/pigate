//go:build linux

package kernel

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"

	"github.com/vishvananda/netlink"
	"pigate/internal/model"
)

// RealNetwork implements NetworkManager using netlink for direct kernel interaction.
// This avoids shell command execution (no Command Injection risk).
// Requires cap_net_admin capability on the binary:
//
//	sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend
type RealNetwork struct{}

func NewRealNetwork() *RealNetwork {
	return &RealNetwork{}
}

// ToggleInterface brings a network interface up or down via netlink socket.
// Equivalent to `ip link set <name> up/down` but without shell execution.
func (r *RealNetwork) ToggleInterface(name string, up bool) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("interface %q not found: %w", name, err)
	}

	if up {
		if err := netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("failed to bring interface %q up: %w", name, err)
		}
		return nil
	}

	if err := netlink.LinkSetDown(link); err != nil {
		return fmt.Errorf("failed to bring interface %q down: %w", name, err)
	}
	return nil
}

// ScanWifi scans for nearby Wi-Fi networks using iw (or nmcli as fallback).
// iw does not require root — only cap_net_raw for raw socket access.
func (r *RealNetwork) ScanWifi(name string) ([]model.WifiScanResult, error) {
	// Try iw first (lightweight, no D-Bus dependency)
	results, err := scanWifiWithIW(name)
	if err == nil && len(results) > 0 {
		return results, nil
	}

	// Fallback: nmcli (requires NetworkManager running)
	return scanWifiWithNmcli(name)
}

// scanWifiWithIW uses `iw dev <name> scan` to list nearby APs.
func scanWifiWithIW(name string) ([]model.WifiScanResult, error) {
	out, err := exec.Command("iw", "dev", name, "scan").Output()
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
				current.Signal = int(2*(dBm+100))
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

// scanWifiWithNmcli uses nmcli as fallback Wi-Fi scanner.
func scanWifiWithNmcli(name string) ([]model.WifiScanResult, error) {
	// Trigger a fresh scan first
	_ = exec.Command("nmcli", "dev", "wifi", "rescan", "ifname", name).Run()

	// Fields: SSID, SIGNAL, SECURITY, CHAN, FREQ
	out, err := exec.Command(
		"nmcli", "--terse", "--fields", "SSID,SIGNAL,SECURITY,CHAN,FREQ",
		"dev", "wifi", "list", "ifname", name,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("nmcli wifi list failed: %w", err)
	}

	var results []model.WifiScanResult
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		// nmcli --terse separates fields with ":"
		// Escaped colons in SSID appear as "\:"
		parts := strings.Split(line, ":")
		if len(parts) < 5 {
			continue
		}

		ssid := parts[0]
		if ssid == "" {
			continue
		}

		var signal int
		fmt.Sscanf(parts[1], "%d", &signal)

		security := parts[2]
		if security == "" || security == "--" {
			security = "Open"
		}

		var channel int
		fmt.Sscanf(parts[3], "%d", &channel)

		freq := parts[4]
		frequency := "2.4 GHz"
		if strings.HasPrefix(freq, "5") {
			frequency = "5 GHz"
		}

		results = append(results, model.WifiScanResult{
			SSID:      ssid,
			Signal:    signal,
			Security:  security,
			Channel:   channel,
			Frequency: frequency,
		})
	}

	return results, nil
}
