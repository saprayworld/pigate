package kernel

import (
	"pigate/internal/model"
)

// MockFirewall implements FirewallManager for local testing
type MockFirewall struct{}

func NewMockFirewall() *MockFirewall {
	return &MockFirewall{}
}

func (m *MockFirewall) ApplyRules(rules []model.PolicyRule) error {
	// Mock success
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

func (m *MockNetwork) ScanWifi(name string) ([]model.WifiScanResult, error) {
	return []model.WifiScanResult{
		{SSID: "MyHome_5G", Signal: 85, Security: "WPA2-PSK", Channel: 36, Frequency: "5 GHz"},
		{SSID: "MyHome_2G", Signal: 72, Security: "WPA2-PSK", Channel: 6, Frequency: "2.4 GHz"},
		{SSID: "Neighbor_AP", Signal: 45, Security: "WPA3", Channel: 11, Frequency: "2.4 GHz"},
		{SSID: "Cafe_Free_WiFi", Signal: 30, Security: "Open", Channel: 1, Frequency: "2.4 GHz"},
		{SSID: "Office_5G_Secured", Signal: 62, Security: "WPA2-Enterprise", Channel: 149, Frequency: "5 GHz"},
	}, nil
}

// MockRouting implements RoutingManager for local testing
type MockRouting struct{}

func NewMockRouting() *MockRouting {
	return &MockRouting{}
}

func (m *MockRouting) ApplyRoutes(routes []model.StaticRoute) error {
	return nil
}

// MockDhcp implements DhcpManager for local testing
type MockDhcp struct{}

func NewMockDhcp() *MockDhcp {
	return &MockDhcp{}
}

func (m *MockDhcp) ApplyConfig(cfg model.DhcpConfig) error {
	return nil
}

func (m *MockDhcp) GetActiveLeases() ([]model.ActiveDhcpLease, error) {
	return []model.ActiveDhcpLease{
		{ID: "lease-1", IPAddress: "192.168.1.101", MacAddress: "99:88:77:66:55:44", Hostname: "iPhone-13", ExpiresIn: "11 hours, 45 mins"},
		{ID: "lease-2", IPAddress: "192.168.1.102", MacAddress: "AA:BB:CC:DD:EE:FF", Hostname: "Android-SmartTV", ExpiresIn: "23 hours, 10 mins"},
		{ID: "lease-3", IPAddress: "192.168.1.105", MacAddress: "B4:F1:DA:C8:E2:10", Hostname: "iPad-Pro", ExpiresIn: "2 hours, 15 mins"},
	}, nil
}
