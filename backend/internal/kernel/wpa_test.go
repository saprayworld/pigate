package kernel

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSanitizeWpaInput tests the input cleaning logic to prevent configuration injection
func TestSanitizeWpaInput(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"NormalSSID", "NormalSSID"},
		{"SSID\nInjection", "SSIDInjection"},
		{"SSID\rInjection", "SSIDInjection"},
		{"SSID\"DoubleQuotes\"", "SSIDDoubleQuotes"},
		{"SSID\n\"\rMixed", "SSIDMixed"},
	}

	for _, tc := range cases {
		got := SanitizeWpaInput(tc.input)
		if got != tc.expected {
			t.Errorf("SanitizeWpaInput(%q) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}

// TestGenerateWpaConfig tests configuration file structure generation
func TestGenerateWpaConfig(t *testing.T) {
	// 1. Open Wifi
	cfgOpen := GenerateWpaConfig("OpenSSID", "", "Open", "", "")
	if !strings.Contains(cfgOpen, "ssid=\"OpenSSID\"") {
		t.Errorf("Expected ssid OpenSSID in config, got:\n%s", cfgOpen)
	}
	if !strings.Contains(cfgOpen, "key_mgmt=NONE") {
		t.Errorf("Expected key_mgmt=NONE in config, got:\n%s", cfgOpen)
	}

	// 2. WPA2-PSK
	cfgWpa2 := GenerateWpaConfig("Wpa2SSID", "wpa2pass", "WPA2-PSK", "", "")
	if !strings.Contains(cfgWpa2, "ssid=\"Wpa2SSID\"") {
		t.Errorf("Expected ssid Wpa2SSID in config, got:\n%s", cfgWpa2)
	}
	if !strings.Contains(cfgWpa2, "psk=\"wpa2pass\"") {
		t.Errorf("Expected psk in config, got:\n%s", cfgWpa2)
	}
	if !strings.Contains(cfgWpa2, "key_mgmt=WPA-PSK") {
		t.Errorf("Expected key_mgmt=WPA-PSK in config, got:\n%s", cfgWpa2)
	}

	// 3. WPA3-SAE
	cfgWpa3 := GenerateWpaConfig("Wpa3SSID", "wpa3pass", "WPA3", "", "")
	if !strings.Contains(cfgWpa3, "key_mgmt=WPA-PSK SAE") {
		t.Errorf("Expected key_mgmt=WPA-PSK SAE in config, got:\n%s", cfgWpa3)
	}
	if !strings.Contains(cfgWpa3, "ieee80211w=2") {
		t.Errorf("Expected ieee80211w=2 in config, got:\n%s", cfgWpa3)
	}

	// 4. Backup SSIDs
	cfgBackup := GenerateWpaConfig("PrimarySSID", "primpass", "WPA2-PSK", "BackupSSID", "backpass")
	if !strings.Contains(cfgBackup, "priority=10") {
		t.Errorf("Expected priority 10 in config, got:\n%s", cfgBackup)
	}
	if !strings.Contains(cfgBackup, "ssid=\"BackupSSID\"") {
		t.Errorf("Expected backup ssid in config, got:\n%s", cfgBackup)
	}
	if !strings.Contains(cfgBackup, "psk=\"backpass\"") {
		t.Errorf("Expected backup psk in config, got:\n%s", cfgBackup)
	}
	if !strings.Contains(cfgBackup, "priority=5") {
		t.Errorf("Expected priority 5 in config, got:\n%s", cfgBackup)
	}
}

// TestSendWpaCommand tests UNIX domain datagram socket communication
func TestSendWpaCommand(t *testing.T) {
	// Create a temporary directory for UNIX sockets
	tmpDir, err := os.MkdirTemp("", "wpa_socket_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override socket path configurations
	oldSocketDir := wpaSocketDir
	wpaSocketDir = tmpDir
	defer func() { wpaSocketDir = oldSocketDir }()

	destSocketPath := filepath.Join(tmpDir, "wlan0_test")

	// Set up mock wpa_supplicant server using UNIX gram
	lAddr, err := net.ResolveUnixAddr("unixgram", destSocketPath)
	if err != nil {
		t.Fatalf("ResolveUnixAddr failed: %v", err)
	}

	conn, err := net.ListenUnixgram("unixgram", lAddr)
	if err != nil {
		t.Fatalf("ListenUnixgram failed: %v", err)
	}
	defer conn.Close()

	// Goroutine that listens on the mock wpa_supplicant socket and replies
	go func() {
		buf := make([]byte, 1024)
		n, rAddr, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		cmd := string(buf[:n])
		if cmd == "PING" {
			_, _ = conn.WriteTo([]byte("PONG"), rAddr)
		} else if cmd == "RECONFIGURE" {
			_, _ = conn.WriteTo([]byte("OK"), rAddr)
		}
	}()

	// Send command to the mock receiver
	resp, err := SendWpaCommand("wlan0_test", "PING")
	if err != nil {
		t.Fatalf("SendWpaCommand returned unexpected error: %v", err)
	}

	if resp != "PONG" {
		t.Errorf("Expected PONG, got %q", resp)
	}
}

// TestConfigureWifiAtomicWrite tests the atomic file writing configuration of wifi
func TestConfigureWifiAtomicWrite(t *testing.T) {
	// Create a temporary directory for wpa_supplicant configuration files
	tmpDir, err := os.MkdirTemp("", "wpa_config_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override config directory path
	oldConfigDir := wpaConfigDir
	wpaConfigDir = tmpDir
	defer func() { wpaConfigDir = oldConfigDir }()

	netMgr := NewRealNetwork()
	
	// Create an interface wlan_test, we stub/mock the systemctl check implicitly by catching the failed start 
	// (or we can see if it writes the configuration file cleanly since we check file existence).
	// To prevent executing real 'systemctl' command errors failing the test, we'll verify the config is created.
	// Since wlan_test is not a systemctl service, starting it will fail, which is expected.
	// Let's call ConfigureWifi and expect an error from systemctl, but check if the config file was written correctly!
	err = netMgr.ConfigureWifi("wlan_test", "MyHomeSSID", "secpass", "WPA2-PSK", "BackupSSID", "backpass")
	
	// The file should have been written despite systemctl service failing to start
	configPath := filepath.Join(tmpDir, "wpa_supplicant-wlan_test.conf")
	info, errStat := os.Stat(configPath)
	if errStat != nil {
		t.Errorf("Expected wpa_supplicant config file to be written, but got error: %v", errStat)
	} else {
		// Verify file permissions are 0600 (read/write only by owner)
		mode := info.Mode().Perm()
		if mode != 0600 {
			t.Errorf("Expected config file permissions to be 0600, got: %04o", mode)
		}

		// Verify content
		data, errRead := os.ReadFile(configPath)
		if errRead != nil {
			t.Fatalf("Failed to read generated config file: %v", errRead)
		}
		content := string(data)
		if !strings.Contains(content, "ssid=\"MyHomeSSID\"") || !strings.Contains(content, "psk=\"secpass\"") {
			t.Errorf("Configuration file content mismatch:\n%s", content)
		}
	}
}
