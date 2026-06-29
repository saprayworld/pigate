package kernel

import (
	"fmt"
	"log"
	"os"
	"strings"

	"pigate/internal/model"
)

// MockFirewall implements FirewallManager for local testing
type MockFirewall struct {
	dockerCompat bool
	ApplyCount   int
}

func NewMockFirewall(dockerCompat bool) *MockFirewall {
	return &MockFirewall{
		dockerCompat: dockerCompat,
		ApplyCount:   0,
	}
}

func (m *MockFirewall) ApplyRules(
	rules []model.PolicyRule,
	ifaces []model.NetworkInterface,
	addrs []model.AddressObject,
	svcs []model.ServiceObject,
) error {
	m.ApplyCount++
	log.Printf("[MockFirewall] Applying %d rules to mock kernel (Docker Compatibility: %t, Addresses: %d, Services: %d):", len(rules), m.dockerCompat, len(addrs), len(svcs))
	if m.dockerCompat {
		log.Printf("  [Docker Compat] Bypassing docker0 and br-* interfaces")
	}
	for _, r := range rules {
		statusStr := "DISABLED"
		if r.Status {
			statusStr = "ENABLED"
		}
		log.Printf("  [%s] Name: %s, In: %s, Out: %s, Src: %v, Dest: %v, Svc: %v, Action: %s, Log: %t",
			statusStr, r.Name, r.InInterface, r.OutInterface, r.Source, r.Destination, r.Service, r.Action, r.Log)
	}
	return nil
}

// MockNetwork implements NetworkManager for local testing
type MockNetwork struct{}

func NewMockNetwork() *MockNetwork {
	return &MockNetwork{}
}

func (m *MockNetwork) ToggleInterface(name string, up bool) error {
	// Mock success
	return nil
}

func (m *MockNetwork) ConfigureInterface(name string, mode string, ip string, netmask string, gateway string) error {
	// Mock success
	return nil
}

func (m *MockNetwork) ConfigureWifi(name string, ssid string, password string, security string, backupSSID string, backupPassword string, backupSecurity string, macMode string) error {
	// Mock success
	return nil
}

func (m *MockNetwork) ScanWifi(name string) ([]model.WifiScanResult, error) {
	return []model.WifiScanResult{
		{SSID: "MyHome_5G", Signal: 85, Security: "WPA2-PSK", Channel: 36, Frequency: "5 GHz"},
		{SSID: "MyHome_2G", Signal: 72, Security: "WPA2-PSK", Channel: 6, Frequency: "2.4 GHz"},
		{SSID: "Neighbor_AP", Signal: 45, Security: "WPA3", Channel: 11, Frequency: "2.4 GHz"},
		{SSID: "Cafe_Free_WiFi", Signal: 30, Security: "Open", Channel: 1, Frequency: "2.4 GHz"},
		{SSID: "Office_5G_Secured", Signal: 62, Security: "WPA2-Enterprise", Channel: 149, Frequency: "5 GHz"},
	}, nil
}

func (m *MockNetwork) GetWifiStatus(name string) (*model.WifiConnectionStatus, error) {
	return &model.WifiConnectionStatus{
		State:     "COMPLETED",
		SSID:      "MyHome_5G",
		BSSID:     "00:11:22:33:44:55",
		ActiveMac: "00:11:22:33:44:55",
		Freq:      5745,
		KeyMgmt:   "WPA3",
		WifiGen:   "WiFi 6",
	}, nil
}

// MockRouting implements RoutingManager for local testing
type MockRouting struct {
	enableEditSystemRoute bool
}

func NewMockRouting() *MockRouting {
	return &MockRouting{}
}

func (m *MockRouting) SetEnableEditSystemRoute(enable bool) {
	m.enableEditSystemRoute = enable
}

func (m *MockRouting) AddRoute(route model.StaticRoute) error {
	log.Printf("[MockRouting] AddRoute called: Dest: %s, Gateway: %s, Interface: %s", route.Destination, route.Gateway, route.Interface)
	return nil
}

func (m *MockRouting) DeleteRoute(route model.StaticRoute) error {
	log.Printf("[MockRouting] DeleteRoute called: Dest: %s, Gateway: %s, Interface: %s", route.Destination, route.Gateway, route.Interface)
	return nil
}

func (m *MockRouting) ApplyRoutes(routes []model.StaticRoute) error {
	log.Printf("[MockRouting] Applying %d static routes to mock kernel:", len(routes))
	for _, rt := range routes {
		statusStr := "DISABLED"
		if rt.Status {
			statusStr = "ENABLED"
		}
		log.Printf("  [%s] Dest: %s, Gateway: %s, Interface: %s, Metric: %d, Type: %s",
			statusStr, rt.Destination, rt.Gateway, rt.Interface, rt.Metric, rt.Type)
	}
	return nil
}

// MockDhcp implements DhcpManager for local testing
type MockDhcp struct {
	MockFromReal bool
}

func NewMockDhcp() *MockDhcp {
	return &MockDhcp{}
}

func (m *MockDhcp) ApplyConfig(cfg model.DhcpConfig) error {
	return nil
}

func parseDnsmasqLeases(filePath string) ([]model.ActiveDhcpLease, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var leases []model.ActiveDhcpLease
	lines := strings.Split(string(data), "\n")
	for idx, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		mac := fields[1]
		ip := fields[2]
		hostname := fields[3]
		if hostname == "*" {
			hostname = "Unknown"
		}
		leases = append(leases, model.ActiveDhcpLease{
			ID:         fmt.Sprintf("lease-real-%d", idx),
			IPAddress:  ip,
			MacAddress: mac,
			Hostname:   hostname,
			ExpiresIn:  "Active (Real)",
		})
	}
	return leases, nil
}

func parseDhcpdLeases(filePath string) ([]model.ActiveDhcpLease, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var leases []model.ActiveDhcpLease
	content := string(data)
	parts := strings.Split(content, "lease ")
	idx := 0
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || !strings.Contains(part, "{") {
			continue
		}
		subParts := strings.SplitN(part, "{", 2)
		ip := strings.TrimSpace(subParts[0])
		body := subParts[1]
		
		var mac, hostname string
		lines := strings.Split(body, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "hardware ethernet ") {
				mac = strings.TrimPrefix(line, "hardware ethernet ")
				mac = strings.TrimSuffix(mac, ";")
				mac = strings.TrimSpace(mac)
			} else if strings.HasPrefix(line, "client-hostname ") {
				hostname = strings.TrimPrefix(line, "client-hostname ")
				hostname = strings.TrimSuffix(hostname, ";")
				hostname = strings.Trim(hostname, "\"")
				hostname = strings.TrimSpace(hostname)
			}
		}
		if mac != "" {
			if hostname == "" {
				hostname = "Unknown"
			}
			leases = append(leases, model.ActiveDhcpLease{
				ID:         fmt.Sprintf("lease-real-%d", idx),
				IPAddress:  ip,
				MacAddress: mac,
				Hostname:   hostname,
				ExpiresIn:  "Active (Real)",
			})
			idx++
		}
	}
	return leases, nil
}

func (m *MockDhcp) GetActiveLeases() ([]model.ActiveDhcpLease, error) {
	if m.MockFromReal {
		// Try parsing dnsmasq leases
		if leases, err := parseDnsmasqLeases("/var/lib/misc/dnsmasq.leases"); err == nil && len(leases) > 0 {
			return leases, nil
		}
		// Try parsing dhcpd leases
		if leases, err := parseDhcpdLeases("/var/lib/dhcp/dhcpd.leases"); err == nil && len(leases) > 0 {
			return leases, nil
		}
	}

	return []model.ActiveDhcpLease{
		{ID: "lease-1", IPAddress: "192.168.1.101", MacAddress: "99:88:77:66:55:44", Hostname: "iPhone-13", ExpiresIn: "11 hours, 45 mins"},
		{ID: "lease-2", IPAddress: "192.168.1.102", MacAddress: "AA:BB:CC:DD:EE:FF", Hostname: "Android-SmartTV", ExpiresIn: "23 hours, 10 mins"},
		{ID: "lease-3", IPAddress: "192.168.1.105", MacAddress: "B4:F1:DA:C8:E2:10", Hostname: "iPad-Pro", ExpiresIn: "2 hours, 15 mins"},
	}, nil
}

// MockQos implements QosManager for local development and testing.
// All operations are no-ops but log their parameters for visibility.
type MockQos struct{}

func NewMockQos() *MockQos {
	return &MockQos{}
}

func (m *MockQos) ApplyQosRules(rules []model.QosRule) error {
	log.Printf("[MockQos] ApplyQosRules called with %d rule(s):", len(rules))
	for _, r := range rules {
		statusStr := "DISABLED"
		if r.Status {
			statusStr = "ENABLED"
		}
		log.Printf("  [%s] %s — iface=%s src=%s dst=%s egress=%d/%dMbps ingress=%d/%dMbps prio=%d",
			statusStr, r.Name, r.Interface,
			r.MatchSrcIP, r.MatchDstIP,
			r.EgressRateMbps, r.EgressCeilMbps,
			r.IngressRateMbps, r.IngressCeilMbps,
			r.Priority,
		)
	}
	return nil
}

func (m *MockQos) ClearQosRules(ifaceName string) error {
	log.Printf("[MockQos] ClearQosRules called for interface: %s", ifaceName)
	return nil
}

func (m *MockQos) GetIfaceQosStatus(ifaceName string) (*model.QosIfaceStatus, error) {
	log.Printf("[MockQos] GetIfaceQosStatus called for interface: %s", ifaceName)
	return &model.QosIfaceStatus{
		Interface: ifaceName,
		HasQdisc:  false,
		Classes:   []model.QosClass{},
	}, nil
}

