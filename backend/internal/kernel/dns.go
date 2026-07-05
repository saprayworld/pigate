//go:build linux

package kernel

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
)

type RealDNSManager struct{}

func NewDNSManager(mock bool) DNSManager {
	if mock {
		return &MockDNSManager{
			mockLinkDNS: make(map[string][]string),
		}
	}
	return &RealDNSManager{}
}

type dbusDNS struct {
	Family int32
	Addr   []byte
}

// GetLinkDNS retrieves DNS server IPs configured for a link using D-Bus properties of systemd-resolved
func (r *RealDNSManager) GetLinkDNS(ifaceName string) ([]string, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("interface %s not found: %w", ifaceName, err)
	}
	ifIndex := int32(iface.Index)

	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to D-Bus system bus: %w", err)
	}

	obj := conn.Object("org.freedesktop.resolve1", "/org/freedesktop/resolve1")
	var linkPath dbus.ObjectPath
	err = obj.Call("org.freedesktop.resolve1.Manager.GetLink", 0, ifIndex).Store(&linkPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get D-Bus link path for interface %s: %w", ifaceName, err)
	}

	linkObj := conn.Object("org.freedesktop.resolve1", linkPath)
	dnsVal, err := linkObj.GetProperty("org.freedesktop.resolve1.Link.DNS")
	if err != nil {
		return nil, fmt.Errorf("failed to read DNS property via D-Bus: %w", err)
	}

	var dnsList []dbusDNS
	err = dnsVal.Store(&dnsList)
	if err != nil {
		return nil, fmt.Errorf("failed to decode DNS list from D-Bus payload: %w", err)
	}

	var servers []string
	for _, item := range dnsList {
		ip := net.IP(item.Addr)
		servers = append(servers, ip.String())
	}
	return servers, nil
}

// SetLinkDNS applies custom DNS servers to a specific interface using D-Bus SetDNS
func (r *RealDNSManager) SetLinkDNS(ifaceName string, servers []string) error {
	if len(servers) == 0 {
		return r.RevertLinkDNS(ifaceName)
	}

	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifaceName, err)
	}
	ifIndex := int32(iface.Index)

	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to D-Bus system bus: %w", err)
	}

	obj := conn.Object("org.freedesktop.resolve1", "/org/freedesktop/resolve1")
	var linkPath dbus.ObjectPath
	err = obj.Call("org.freedesktop.resolve1.Manager.GetLink", 0, ifIndex).Store(&linkPath)
	if err != nil {
		return fmt.Errorf("failed to get D-Bus link path for interface %s: %w", ifaceName, err)
	}

	// Build the structure matching a(iay)
	var dnsServers []struct {
		Family int32
		Addr   []byte
	}
	for _, s := range servers {
		ip := net.ParseIP(s)
		if ip == nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 != nil {
			dnsServers = append(dnsServers, struct {
				Family int32
				Addr   []byte
			}{2, ip4})
		} else {
			ip6 := ip.To16()
			if ip6 != nil {
				dnsServers = append(dnsServers, struct {
					Family int32
					Addr   []byte
				}{10, ip6})
			}
		}
	}

	linkObj := conn.Object("org.freedesktop.resolve1", linkPath)
	err = linkObj.Call("org.freedesktop.resolve1.Link.SetDNS", 0, dnsServers).Err
	if err != nil {
		return fmt.Errorf("failed to set link DNS via D-Bus: %w", err)
	}
	return nil
}

// RevertLinkDNS clears custom DNS settings for an interface using D-Bus Revert
func (r *RealDNSManager) RevertLinkDNS(ifaceName string) error {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifaceName, err)
	}
	ifIndex := int32(iface.Index)

	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to D-Bus system bus: %w", err)
	}

	obj := conn.Object("org.freedesktop.resolve1", "/org/freedesktop/resolve1")
	var linkPath dbus.ObjectPath
	err = obj.Call("org.freedesktop.resolve1.Manager.GetLink", 0, ifIndex).Store(&linkPath)
	if err != nil {
		return fmt.Errorf("failed to get D-Bus link path for interface %s: %w", ifaceName, err)
	}

	linkObj := conn.Object("org.freedesktop.resolve1", linkPath)
	err = linkObj.Call("org.freedesktop.resolve1.Link.Revert", 0).Err
	if err != nil {
		return fmt.Errorf("failed to revert link DNS via D-Bus: %w", err)
	}
	return nil
}

// SetGlobalDNS writes global DNS servers and domains to a systemd-resolved drop-in config and restarts it
func (r *RealDNSManager) SetGlobalDNS(servers []string, searchDomain string) error {
	if len(servers) == 0 && searchDomain == "" {
		// Clean up drop-in config to revert to defaults
		targetPath := "/etc/systemd/resolved.conf.d/pigate.conf"

		// 1. ลบไฟล์ด้วยคำสั่งของ Go โดยตรง (ใช้ os.Remove)
		// เช็ก os.IsNotExist ด้วย เผื่อกรณีที่ไฟล์ไม่มีอยู่แล้ว จะได้ไม่แจ้ง Error
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove resolved config: %w", err)
		}

		// 2. สั่ง Restart Service ผ่าน D-Bus (ฟังก์ชันที่เราสร้างไว้)
		if err := RestartServiceViaDBus("systemd-resolved.service"); err != nil {
			return fmt.Errorf("failed to restart systemd-resolved: %w", err)
		}

		return nil
	}

	var buf bytes.Buffer
	buf.WriteString("[Resolve]\n")
	if len(servers) > 0 {
		buf.WriteString(fmt.Sprintf("DNS=%s\n", strings.Join(servers, " ")))
	}
	if searchDomain != "" {
		buf.WriteString(fmt.Sprintf("Domains=%s\n", searchDomain))
	}

	tempFile := "/var/lib/pigate/pigate-resolved-temp.conf"
	if err := os.WriteFile(tempFile, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write temporary resolved config: %w", err)
	}
	defer os.Remove(tempFile)

	// if err := exec.Command("sudo", "mkdir", "-p", "/etc/systemd/resolved.conf.d").Run(); err != nil {
	// 	return fmt.Errorf("failed to create resolved.conf.d directory: %w", err)
	// }

	targetPath := "/etc/systemd/resolved.conf.d/pigate.conf"
	// if err := exec.Command("sudo", "cp", tempFile, targetPath).Run(); err != nil {
	// 	return fmt.Errorf("failed to copy resolved config to target path: %w", err)
	// }

	if err := os.Rename(tempFile, targetPath); err != nil {
		return fmt.Errorf("failed to rename resolved config to target path: %w", err)
	}

	// ใช้ command
	// if err := exec.Command("sudo", "systemctl", "restart", "systemd-resolved").Run(); err != nil {
	// 	return fmt.Errorf("failed to restart systemd-resolved: %w", err)
	// }

	// ใช้ D-Bus แทนการเรียก sudo systemctl restart
	if err := RestartServiceViaDBus("systemd-resolved.service"); err != nil {
		return fmt.Errorf("failed to restart systemd-resolved via D-Bus: %w", err)
	}

	return nil
}

// MockDNSManager mocks DNS settings in memory for development and tests
type MockDNSManager struct {
	mockLinkDNS map[string][]string
	globalDNS   []string
	globalDom   string
}

func (m *MockDNSManager) GetLinkDNS(ifaceName string) ([]string, error) {
	if ifaceName == "eth0" {
		return []string{"172.20.160.1", "8.8.8.8"}, nil
	}
	return m.mockLinkDNS[ifaceName], nil
}

func (m *MockDNSManager) SetLinkDNS(ifaceName string, servers []string) error {
	if m.mockLinkDNS == nil {
		m.mockLinkDNS = make(map[string][]string)
	}
	m.mockLinkDNS[ifaceName] = servers
	return nil
}

func (m *MockDNSManager) RevertLinkDNS(ifaceName string) error {
	if m.mockLinkDNS != nil {
		delete(m.mockLinkDNS, ifaceName)
	}
	return nil
}

func (m *MockDNSManager) SetGlobalDNS(servers []string, searchDomain string) error {
	m.globalDNS = servers
	m.globalDom = searchDomain
	return nil
}
