package kernel

import (
	"net"
	"os"
	"os/exec"
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

// assertGlobalHeaderAndScanStability verifies the mandatory global header
// ordering (ctrl_interface first, update_config second) and that the
// scan-stability lines added for issue #72 are present outside any
// network={} block.
func assertGlobalHeaderAndScanStability(t *testing.T, cfg string) {
	t.Helper()

	lines := strings.Split(cfg, "\n")
	if len(lines) < 2 {
		t.Fatalf("Config too short to contain mandatory header lines:\n%s", cfg)
	}
	if lines[0] != "ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev" {
		t.Errorf("Expected ctrl_interface as the first line, got: %q", lines[0])
	}
	if lines[1] != "update_config=1" {
		t.Errorf("Expected update_config=1 as the second line, got: %q", lines[1])
	}

	// The scan-stability lines must appear before the first network={} block
	// (i.e. in the global header section), not inside it.
	headerEnd := strings.Index(cfg, "network={")
	if headerEnd == -1 {
		t.Fatalf("Expected at least one network={} block in config:\n%s", cfg)
	}
	header := cfg[:headerEnd]

	for _, want := range []string{"ap_scan=1", "autoscan=periodic:10", "disable_scan_offload=1"} {
		if !strings.Contains(header, want) {
			t.Errorf("Expected global header to contain %q, got header:\n%s", want, header)
		}
	}
}

// TestGenerateWpaConfig tests configuration file structure generation
func TestGenerateWpaConfig(t *testing.T) {
	// 1. Open Wifi
	cfgOpen := GenerateWpaConfig("OpenSSID", "", "Open", "", "", "Open", "hardware", false)
	if !strings.Contains(cfgOpen, "ssid=\"OpenSSID\"") {
		t.Errorf("Expected ssid OpenSSID in config, got:\n%s", cfgOpen)
	}
	if !strings.Contains(cfgOpen, "key_mgmt=NONE") {
		t.Errorf("Expected key_mgmt=NONE in config, got:\n%s", cfgOpen)
	}
	assertGlobalHeaderAndScanStability(t, cfgOpen)

	// 2. WPA2-PSK
	cfgWpa2 := GenerateWpaConfig("Wpa2SSID", "wpa2pass", "WPA2-PSK", "", "", "WPA2", "hardware", false)
	if !strings.Contains(cfgWpa2, "ssid=\"Wpa2SSID\"") {
		t.Errorf("Expected ssid Wpa2SSID in config, got:\n%s", cfgWpa2)
	}
	if !strings.Contains(cfgWpa2, "psk=\"wpa2pass\"") {
		t.Errorf("Expected psk in config, got:\n%s", cfgWpa2)
	}
	if !strings.Contains(cfgWpa2, "key_mgmt=WPA-PSK") {
		t.Errorf("Expected key_mgmt=WPA-PSK in config, got:\n%s", cfgWpa2)
	}
	assertGlobalHeaderAndScanStability(t, cfgWpa2)

	// 3. WPA3-SAE
	cfgWpa3 := GenerateWpaConfig("Wpa3SSID", "wpa3pass", "WPA3", "", "", "WPA2", "hardware", false)
	if !strings.Contains(cfgWpa3, "key_mgmt=SAE") {
		t.Errorf("Expected key_mgmt=SAE in config, got:\n%s", cfgWpa3)
	}
	if !strings.Contains(cfgWpa3, "ieee80211w=2") {
		t.Errorf("Expected ieee80211w=2 in config, got:\n%s", cfgWpa3)
	}
	assertGlobalHeaderAndScanStability(t, cfgWpa3)

	// 4. Backup SSIDs
	cfgBackup := GenerateWpaConfig("PrimarySSID", "primpass", "WPA2-PSK", "BackupSSID", "backpass", "WPA2", "hardware", false)
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
	assertGlobalHeaderAndScanStability(t, cfgBackup)

	// 5. Randomized MAC Mode
	cfgRand := GenerateWpaConfig("PrimarySSID", "primpass", "WPA2-PSK", "BackupSSID", "backpass", "WPA2", "randomized", false)
	if !strings.Contains(cfgRand, "preassoc_mac_addr=1") {
		t.Errorf("Expected preassoc_mac_addr=1 in config, got:\n%s", cfgRand)
	}
	// Check that both network blocks have mac_addr=1
	count := strings.Count(cfgRand, "    mac_addr=1")
	if count != 2 {
		t.Errorf("Expected mac_addr=1 to appear exactly 2 times (primary & backup), got %d times in config:\n%s", count, cfgRand)
	}
	assertGlobalHeaderAndScanStability(t, cfgRand)

	// 6. Prefer 5GHz disabled: freq_list must not appear anywhere
	cfgNo5GHz := GenerateWpaConfig("PrimarySSID", "primpass", "WPA2-PSK", "BackupSSID", "backpass", "WPA2", "hardware", false)
	if strings.Contains(cfgNo5GHz, "freq_list") {
		t.Errorf("Expected no freq_list when prefer5GHz=false, got:\n%s", cfgNo5GHz)
	}
	assertGlobalHeaderAndScanStability(t, cfgNo5GHz)

	// 7. Prefer 5GHz enabled: freq_list must appear inside both the primary and
	// backup network={} blocks, never in the global header.
	cfg5GHz := GenerateWpaConfig("PrimarySSID", "primpass", "WPA2-PSK", "BackupSSID", "backpass", "WPA2", "hardware", true)
	freqLine := "    freq_list=" + freqList5GHz
	freqCount := strings.Count(cfg5GHz, freqLine)
	if freqCount != 2 {
		t.Errorf("Expected freq_list to appear exactly 2 times (primary & backup), got %d times in config:\n%s", freqCount, cfg5GHz)
	}
	headerEnd := strings.Index(cfg5GHz, "network={")
	if headerEnd == -1 {
		t.Fatalf("Expected at least one network={} block in config:\n%s", cfg5GHz)
	}
	if strings.Contains(cfg5GHz[:headerEnd], "freq_list") {
		t.Errorf("freq_list must not appear in the global header, got header:\n%s", cfg5GHz[:headerEnd])
	}
	// Verify freq_list sits inside a network={} block, before its closing "}".
	blocks := strings.Split(cfg5GHz, "network={")
	for i, block := range blocks {
		if i == 0 {
			continue // pre-block header, already checked above
		}
		closeIdx := strings.Index(block, "}")
		if closeIdx == -1 {
			t.Fatalf("network={} block %d missing closing brace:\n%s", i, block)
		}
		if !strings.Contains(block[:closeIdx], "freq_list="+freqList5GHz) {
			t.Errorf("Expected freq_list inside network={} block %d, got:\n%s", i, block[:closeIdx])
		}
	}
	assertGlobalHeaderAndScanStability(t, cfg5GHz)
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
	oldLocalSocketDir := wpaLocalSocketDir
	wpaLocalSocketDir = tmpDir
	defer func() {
		wpaSocketDir = oldSocketDir
		wpaLocalSocketDir = oldLocalSocketDir
	}()

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

// TestGetWifiStatus tests the RealNetwork.GetWifiStatus method and parsing
func TestGetWifiStatus(t *testing.T) {
	// Create a temporary directory for UNIX sockets
	tmpDir, err := os.MkdirTemp("", "wpa_status_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override socket path configurations
	oldSocketDir := wpaSocketDir
	wpaSocketDir = tmpDir
	oldLocalSocketDir := wpaLocalSocketDir
	wpaLocalSocketDir = tmpDir
	defer func() {
		wpaSocketDir = oldSocketDir
		wpaLocalSocketDir = oldLocalSocketDir
	}()

	destSocketPath := filepath.Join(tmpDir, "wlan0_status_test")

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
		if cmd == "STATUS" {
			reply := "bssid=00:11:22:33:44:55\nfreq=5180\nssid=MyHome_5G\nid=0\nmode=station\npairwise_cipher=CCMP\ngroup_cipher=CCMP\nkey_mgmt=WPA2-PSK\nwpa_state=COMPLETED\nip_address=10.0.0.45\naddress=dc:a6:32:aa:bb:c1\n"
			_, _ = conn.WriteTo([]byte(reply), rAddr)
		}
	}()

	netMgr := NewRealNetwork()
	status, err := netMgr.GetWifiStatus("wlan0_status_test")
	if err != nil {
		t.Fatalf("GetWifiStatus returned unexpected error: %v", err)
	}

	if status.State != "COMPLETED" {
		t.Errorf("Expected State COMPLETED, got %q", status.State)
	}
	if status.SSID != "MyHome_5G" {
		t.Errorf("Expected SSID MyHome_5G, got %q", status.SSID)
	}
	if status.BSSID != "00:11:22:33:44:55" {
		t.Errorf("Expected BSSID 00:11:22:33:44:55, got %q", status.BSSID)
	}
	if status.Freq != 5180 {
		t.Errorf("Expected Freq 5180, got %d", status.Freq)
	}
	if status.KeyMgmt != "WPA2" {
		t.Errorf("Expected KeyMgmt WPA2, got %q", status.KeyMgmt)
	}
	if status.WifiGen != "WiFi 5" {
		t.Errorf("Expected WifiGen WiFi 5, got %q", status.WifiGen)
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

	// Override config directory path and mock system command execution
	oldConfigDir := wpaConfigDir
	wpaConfigDir = tmpDir
	oldExecCommand := execCommand
	execCommand = fakeExecCommand
	defer func() {
		wpaConfigDir = oldConfigDir
		execCommand = oldExecCommand
	}()

	netMgr := NewRealNetwork()

	// Create an interface wlan_test, we stub/mock the systemctl check implicitly by catching the failed start
	// (or we can see if it writes the configuration file cleanly since we check file existence).
	// To prevent executing real 'systemctl' command errors failing the test, we'll verify the config is created.
	// Since wlan_test is not a systemctl service, starting it will fail, which is expected.
	// Let's call ConfigureWifi and expect an error from systemctl, but check if the config file was written correctly!
	err = netMgr.ConfigureWifi("wlan_test", "MyHomeSSID", "secpass", "WPA2-PSK", "BackupSSID", "backpass", "WPA2", "hardware", false)

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

// Helper process for mocking exec.Command in tests
func fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(0)
	}

	subCmd := args[0]
	switch subCmd {
	case "sudo":
		if len(args) > 2 && args[1] == "systemctl" {
			action := args[2]
			if action == "is-active" {
				os.Exit(1) // Return inactive (non-zero) to trigger systemctl start path
			}
			os.Exit(0) // Success for start/stop
		}
		os.Exit(0)
	default:
		os.Exit(0)
	}
}

// TestConfigureWifiCleansStaleSocket tests that ConfigureWifi cleans up any stale socket files
// in the socket directory before starting the service.
func TestConfigureWifiCleansStaleSocket(t *testing.T) {
	// Create a temporary directory for UNIX sockets
	tmpDir, err := os.MkdirTemp("", "wpa_stale_socket_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override socket path configurations and mock system command execution
	oldSocketDir := wpaSocketDir
	wpaSocketDir = tmpDir
	oldConfigDir := wpaConfigDir
	wpaConfigDir = tmpDir
	oldLocalSocketDir := wpaLocalSocketDir
	wpaLocalSocketDir = tmpDir
	oldExecCommand := execCommand
	execCommand = fakeExecCommand
	defer func() {
		wpaSocketDir = oldSocketDir
		wpaConfigDir = oldConfigDir
		wpaLocalSocketDir = oldLocalSocketDir
		execCommand = oldExecCommand
	}()

	// Pre-create a stale socket file
	staleSocketPath := filepath.Join(tmpDir, "wlan_test_stale")
	if err := os.WriteFile(staleSocketPath, []byte("stale-data"), 0600); err != nil {
		t.Fatalf("Failed to create mock stale socket file: %v", err)
	}

	netMgr := NewRealNetwork()
	// Call ConfigureWifi which will trigger the start path (since fake systemctl returns inactive)
	_ = netMgr.ConfigureWifi("wlan_test_stale", "MyHomeSSID", "secpass", "WPA2-PSK", "", "", "WPA2", "hardware", false)

	// The stale socket file should have been deleted
	if _, err := os.Stat(staleSocketPath); !os.IsNotExist(err) {
		t.Errorf("Expected stale socket file to be deleted, but it still exists")
	}
}

// TestFrequencyToChannel tests the frequency-to-channel calculation helper
func TestFrequencyToChannel(t *testing.T) {
	cases := []struct {
		freq     int
		expected int
	}{
		{2412, 1},
		{2437, 6},
		{2472, 13},
		{2484, 14},
		{5180, 36},
		{5500, 100},
		{5825, 165},
		{0, 0},
		{9999, 0},
	}

	for _, tc := range cases {
		got := frequencyToChannel(tc.freq)
		if got != tc.expected {
			t.Errorf("frequencyToChannel(%d) = %d; expected %d", tc.freq, got, tc.expected)
		}
	}
}

// TestRedactWpaCommand verifies the allowlist semantics: only known-safe
// commands (and bare verbs, which carry no argument) are logged verbatim —
// every other command with arguments is redacted to its verb, so redaction
// fails closed for secret-bearing commands the code has never seen.
func TestRedactWpaCommand(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want string
	}{
		{"reconfigure passes through", "RECONFIGURE", "RECONFIGURE"},
		{"reconnect passes through", "RECONNECT", "RECONNECT"},
		{"bare unknown verb passes through", "STATUS", "STATUS"},
		{"psk redacted", `SET_NETWORK 0 psk "supersecret"`, "SET_NETWORK [redacted]"},
		{"password redacted", `SET_NETWORK 0 password "hunter2"`, "SET_NETWORK [redacted]"},
		{"sae_password redacted", `SET_NETWORK 0 sae_password "pw"`, "SET_NETWORK [redacted]"},
		{"lowercase verb still redacted", `set_network 0 PSK "x"`, "set_network [redacted]"},
		{"unlisted keyword fails closed (wps pin)", "WPS_PIN any 12345670", "WPS_PIN [redacted]"},
		{"unlisted keyword fails closed (private_key_passwd)", `SET_NETWORK 0 private_key_passwd "kp"`, "SET_NETWORK [redacted]"},
		{"any unknown command with args redacted", "SCAN freq=2412", "SCAN [redacted]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactWpaCommand(tc.cmd)
			if got != tc.want {
				t.Errorf("redactWpaCommand(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
			// A redacted result must never leak the secret material.
			for _, secret := range []string{"supersecret", "hunter2", "12345670", `"kp"`} {
				if strings.Contains(got, secret) {
					t.Errorf("redacted output still contains secret %q: %q", secret, got)
				}
			}
		})
	}
}
