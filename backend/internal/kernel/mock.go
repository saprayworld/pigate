package kernel

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

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
	dhcpServerIfaces []string,
	dnsServerIfaces []string,
) error {
	m.ApplyCount++
	log.Printf("[MockFirewall] Applying %d rules to mock kernel (Docker Compatibility: %t, Addresses: %d, Services: %d):", len(rules), m.dockerCompat, len(addrs), len(svcs))
	if m.dockerCompat {
		log.Printf("  [Docker Compat] Bypassing docker0 and br-* interfaces")
	}
	if len(dhcpServerIfaces) > 0 {
		log.Printf("  [DHCP Server] Opening udp/67 on interfaces: %v", dhcpServerIfaces)
	}
	if len(dnsServerIfaces) > 0 {
		log.Printf("  [DNS Server] Opening tcp+udp/53 on interfaces: %v", dnsServerIfaces)
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

func (m *MockNetwork) ConfigureInterface(name string, mode string, ip string, netmask string, gateway string, metric int) error {
	// Mock success
	log.Printf("[MockNetwork] ConfigureInterface: %s mode=%s ip=%s gateway=%s metric=%d", name, mode, ip, gateway, metric)
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

func (m *MockRouting) EnforceDefaultRouteMetric(ifaceName string, metric int) error {
	log.Printf("[MockRouting] EnforceDefaultRouteMetric called: Interface: %s, Metric: %d", ifaceName, metric)
	return nil
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

func (m *MockDhcp) ApplyConfig(cfgs []model.DhcpConfig, reservations []model.DhcpReservation) error {
	return nil
}

func (m *MockDhcp) ReloadConfig() error {
	return nil
}

func (m *MockDhcp) WatchLeases(ctx context.Context, callback func(event string, lease model.ActiveDhcpLease)) error {
	// Mock no-op watcher
	<-ctx.Done()
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

type MockDNSServerManager struct{}

func NewMockDNSServerManager() *MockDNSServerManager {
	return &MockDNSServerManager{}
}

func (m *MockDNSServerManager) ApplyZones(zones []model.DNSZone, interfaces []string, upstreamServers []string) error {
	log.Printf("[MockDNSServer] ApplyZones called with %d zones, interfaces: %v, upstream servers: %v", len(zones), interfaces, upstreamServers)
	return nil
}

func (m *MockDNSServerManager) ClearCache() error {
	log.Printf("[MockDNSServer] ClearCache called")
	return nil
}

// MockDhcpcdManager implements DhcpcdManager for local testing
type MockDhcpcdManager struct{}

func NewMockDhcpcdManager() *MockDhcpcdManager {
	return &MockDhcpcdManager{}
}

func (m *MockDhcpcdManager) StartDhcpcd(ifaceName string) error {
	log.Printf("[MockDhcpcd] Simulating starting dhcpcd for %s", ifaceName)
	return nil
}

func (m *MockDhcpcdManager) StopDhcpcd(ifaceName string) error {
	log.Printf("[MockDhcpcd] Simulating stopping/releasing dhcpcd for %s", ifaceName)
	return nil
}

func (m *MockDhcpcdManager) SetShareHostname(share bool) error {
	log.Printf("[MockDhcpcd] Simulating writing dhcpcd.conf (share hostname: %t)", share)
	return nil
}

func (m *MockDhcpcdManager) RestartDhcpcd(ifaceName string) error {
	log.Printf("[MockDhcpcd] Simulating restarting dhcpcd for %s", ifaceName)
	return nil
}

// MockHostnameManager implements HostnameManager in-memory for local testing
type MockHostnameManager struct {
	hostname string
}

func NewMockHostnameManager() *MockHostnameManager {
	return &MockHostnameManager{hostname: "pigate-mock"}
}

func (m *MockHostnameManager) GetHostname() (string, error) {
	return m.hostname, nil
}

func (m *MockHostnameManager) SetHostname(name string) error {
	log.Printf("[MockHostname] Simulating setting hostname to %q", name)
	m.hostname = name
	return nil
}

// MockPowerManager implements PowerManager for local testing. It MUST NOT have
// any side effect — dev machines run with -mock=true and a real reboot/poweroff
// here would take down the developer's own workstation. It only logs.
type MockPowerManager struct{}

func NewMockPowerManager() *MockPowerManager {
	return &MockPowerManager{}
}

func (m *MockPowerManager) Reboot() error {
	log.Printf("[MockPower] Simulating system reboot (no-op)")
	return nil
}

func (m *MockPowerManager) PowerOff() error {
	log.Printf("[MockPower] Simulating system power-off (no-op)")
	return nil
}

// MockTimeManager implements TimeManager in-memory for local testing. It keeps
// the last-applied timezone/NTP/server values and simulates a synced clock.
type MockTimeManager struct {
	timezone  string
	ntpOn     bool
	ntpServer string
	manual    time.Time // zero unless SetTime was called
}

func NewMockTimeManager() *MockTimeManager {
	return &MockTimeManager{timezone: "Asia/Bangkok", ntpOn: true, ntpServer: "pool.ntp.org"}
}

func (m *MockTimeManager) GetTimeStatus() (*model.TimeStatus, error) {
	now := time.Now()
	if !m.ntpOn && !m.manual.IsZero() {
		now = m.manual
	}
	return &model.TimeStatus{
		CurrentTime:     now.Format(time.RFC3339),
		NTPSynchronized: m.ntpOn,
	}, nil
}

func (m *MockTimeManager) SetTimezone(tz string) error {
	log.Printf("[MockTime] Simulating set timezone to %q", tz)
	m.timezone = tz
	return nil
}

func (m *MockTimeManager) SetNTP(enable bool) error {
	log.Printf("[MockTime] Simulating set NTP to %t", enable)
	m.ntpOn = enable
	return nil
}

func (m *MockTimeManager) SetTime(t time.Time) error {
	log.Printf("[MockTime] Simulating set clock to %s", t.Format(time.RFC3339))
	m.manual = t
	return nil
}

func (m *MockTimeManager) SetNTPServer(server string) error {
	log.Printf("[MockTime] Simulating set NTP server to %q", server)
	m.ntpServer = server
	return nil
}

// MockSystemStats implements SystemStatsManager with simulated values that drift
// over time, so the dashboard visibly moves during -mock development on WSL. The
// CPU snapshot advances monotonically each call with a busy/idle mix that swings
// gently, which the service's delta logic turns into a plausible usage%.
type MockSystemStats struct {
	mu       sync.Mutex
	tick     uint64
	totalJif uint64
	idleJif  uint64
}

func NewMockSystemStats() *MockSystemStats {
	return &MockSystemStats{}
}

// wave returns a smooth 0..1 oscillation offset by phase, driven by tick.
func (m *MockSystemStats) wave(phase float64) float64 {
	return (math.Sin(float64(m.tick)/6.0+phase) + 1) / 2
}

func (m *MockSystemStats) GetCPUSnapshot() (*model.CPUSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tick++
	// Each tick adds ~1000 jiffies total; the busy fraction swings ~10-45%.
	busyFrac := 0.10 + 0.35*m.wave(0)
	const step = 1000
	m.totalJif += step
	m.idleJif += uint64(float64(step) * (1 - busyFrac))
	return &model.CPUSnapshot{Idle: m.idleJif, Total: m.totalJif}, nil
}

func (m *MockSystemStats) GetCPUInfo() (*model.CPUInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &model.CPUInfo{
		Cores:         4,
		ModelName:     "Mock Cortex-A76 (simulated)",
		FreqMHz:       round1(1500 + 900*m.wave(1)),
		FreqAvailable: true,
	}, nil
}

func (m *MockSystemStats) GetMemoryInfo() (*model.MemoryInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	const total = uint64(8) * 1024 * 1024 * 1024 // 8 GB
	pct := 45 + 20*m.wave(2)
	used := uint64(float64(total) * pct / 100.0)
	return &model.MemoryInfo{UsedBytes: used, TotalBytes: total, Percent: round1(pct)}, nil
}

func (m *MockSystemStats) GetTemperature() (*model.TemperatureInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &model.TemperatureInfo{
		Celsius:         round1(52 + 10*m.wave(3)),
		ThrottleCelsius: 80,
		Available:       true,
	}, nil
}

func (m *MockSystemStats) GetDiskUsage(path string) (*model.DiskUsage, error) {
	const total = uint64(128) * 1024 * 1024 * 1024 // 128 GB
	used := total * 32 / 100                       // ~32% used
	return &model.DiskUsage{
		Path:       path,
		UsedBytes:  used,
		TotalBytes: total,
		Percent:    32.0,
	}, nil
}

func (m *MockSystemStats) GetHostInfo() (*model.HostInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &model.HostInfo{
		OSName:        "PiGate Mock OS (simulated)",
		BoardModel:    "Raspberry Pi 5 Model B (mock)",
		KernelVersion: "6.6.31-mock",
		// Fixed base + drift so the uptime advances during a session.
		UptimeSeconds: int64(273153 + m.tick),
	}, nil
}

func (m *MockSystemStats) GetNetCounters() (map[string]model.NetCounters, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Advance counters monotonically so the traffic collector sees positive
	// deltas. rx grows faster than tx (typical download-heavy gateway). Counters
	// are emitted for every interface the mock DB might mark WAN (eth0/eth1/
	// wlan0) so the traffic chart moves regardless of which one is the WAN role.
	rx := uint64(float64(m.tick) * (400_000 + 300_000*m.wave(4)))
	tx := uint64(float64(m.tick) * (120_000 + 80_000*m.wave(5)))
	return map[string]model.NetCounters{
		"eth0":  {RxBytes: rx, TxBytes: tx},
		"eth1":  {RxBytes: rx / 3, TxBytes: tx / 3},
		"wlan0": {RxBytes: rx / 2, TxBytes: tx / 2},
		"lo":    {RxBytes: rx * 4, TxBytes: rx * 4}, // must be excluded by collector
	}, nil
}

// MockTrafficLog implements TrafficLogManager for local/mock testing. It
// synthesizes forward-traffic events on a timer (no netlink socket is ever
// opened — safe to run on a dev workstation) so the Forward Traffic page and
// the Dashboard Recent Logs widget have a live feed without a real kernel.
type MockTrafficLog struct{}

func NewMockTrafficLog() *MockTrafficLog {
	return &MockTrafficLog{}
}

func (m *MockTrafficLog) WatchForwardTraffic(ctx context.Context, cb func(model.FirewallLog)) error {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	type sample struct {
		action string
		dest   string
		port   string
		proto  string
		reason string
	}
	samples := []sample{
		{"PASS", "8.8.8.8", "53", "UDP", "Allowed (forward)"},
		{"PASS", "142.250.80.46", "443", "TCP", "Allowed (forward)"},
		{"PASS", "1.1.1.1", "443", "TCP", "Allowed (forward)"},
		{"DROP", "185.220.101.4", "23", "TCP", "Blocked (forward)"},
		{"DROP", "45.13.104.9", "3389", "TCP", "Blocked (forward)"},
		{"PASS", "140.82.113.3", "22", "TCP", "Allowed (forward)"},
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s := samples[rng.Intn(len(samples))]
			cb(model.FirewallLog{
				Action: s.action,
				Src:    fmt.Sprintf("192.168.1.%d", 100+rng.Intn(50)),
				Dest:   s.dest,
				Port:   s.port,
				Proto:  s.proto,
				Reason: s.reason,
			})
		}
	}
}
