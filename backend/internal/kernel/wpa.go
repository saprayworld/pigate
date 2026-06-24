package kernel

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Global variables that can be overridden in tests to redirect outputs
var wpaConfigDir = "/etc/wpa_supplicant"
var wpaSocketDir = "/var/run/wpa_supplicant"
var wpaLocalSocketDir = "/run/pigate"

// SanitizeWpaInput strips newlines and double quotes to prevent configuration injection
func SanitizeWpaInput(val string) string {
	val = strings.ReplaceAll(val, "\n", "")
	val = strings.ReplaceAll(val, "\r", "")
	val = strings.ReplaceAll(val, "\"", "")
	return val
}

// GenerateWpaConfig constructs the raw text content for a wpa_supplicant configuration file.
// It incorporates security options (Open, WPA2, WPA3) and weight-based priorities.
func GenerateWpaConfig(ssid, password, security, backupSSID, backupPassword string) string {
	log.Printf("[WPA Config] Building config layout for SSID=%q (Security=%s, HasPassword=%t), BackupSSID=%q (HasBackupPassword=%t)",
		ssid, security, password != "", backupSSID, backupPassword != "")

	var sb strings.Builder
	sb.WriteString("ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev\n")
	sb.WriteString("update_config=1\n")
	sb.WriteString("country=TH\n\n")

	cleanSSID := SanitizeWpaInput(ssid)
	cleanPassword := SanitizeWpaInput(password)

	// Primary network block
	sb.WriteString("network={\n")
	sb.WriteString(fmt.Sprintf("    ssid=\"%s\"\n", cleanSSID))
	if cleanPassword != "" && security != "Open" {
		sb.WriteString(fmt.Sprintf("    psk=\"%s\"\n", cleanPassword))
		if security == "WPA3" {
			sb.WriteString("    key_mgmt=WPA-PSK SAE\n")
			sb.WriteString("    ieee80211w=2\n") // PMF required for WPA3
		} else {
			sb.WriteString("    key_mgmt=WPA-PSK\n")
		}
	} else {
		sb.WriteString("    key_mgmt=NONE\n")
	}
	sb.WriteString("    priority=10\n")
	sb.WriteString("}\n")

	// Backup network block
	cleanBackupSSID := SanitizeWpaInput(backupSSID)
	cleanBackupPassword := SanitizeWpaInput(backupPassword)
	if cleanBackupSSID != "" {
		sb.WriteString("\nnetwork={\n")
		sb.WriteString(fmt.Sprintf("    ssid=\"%s\"\n", cleanBackupSSID))
		if cleanBackupPassword != "" {
			sb.WriteString(fmt.Sprintf("    psk=\"%s\"\n", cleanBackupPassword))
			// Default backup key_mgmt to WPA-PSK SAE for maximum compatibility with both WPA2/WPA3
			sb.WriteString("    key_mgmt=WPA-PSK\n")
			sb.WriteString("    ieee80211w=2\n")
		} else {
			sb.WriteString("    key_mgmt=NONE\n")
		}
		sb.WriteString("    priority=5\n")
		sb.WriteString("}\n")
	}

	return sb.String()
}

// SendWpaCommand sends a control command to the wpa_supplicant UNIX domain datagram socket.
func SendWpaCommand(ifaceName string, command string) (string, error) {
	destAddr := fmt.Sprintf("%s/%s", wpaSocketDir, ifaceName)
	log.Printf("[WPA Socket] Resolving socket address: destination=%s", destAddr)

	// Ensure the local socket directory exists (fall back to /tmp if write to /run is denied or not writable)
	localDir := wpaLocalSocketDir
	useTemp := false
	if err := os.MkdirAll(localDir, 0755); err != nil {
		useTemp = true
	} else {
		// Test writability by creating a temporary test file
		testFile := filepath.Join(localDir, fmt.Sprintf(".write_test_%d", time.Now().UnixNano()))
		if f, err := os.OpenFile(testFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600); err != nil {
			useTemp = true
		} else {
			f.Close()
			_ = os.Remove(testFile)
		}
	}
	if useTemp {
		log.Printf("[WPA Socket] Directory %s is not writable or cannot be created, falling back to temp dir", localDir)
		localDir = os.TempDir()
	}
	localAddr := fmt.Sprintf("%s/wpa_ctrl_%d_%d", localDir, os.Getpid(), time.Now().UnixNano())
	log.Printf("[WPA Socket] Binding local temporary socket: %s", localAddr)

	// Clean up any stale socket file
	_ = os.Remove(localAddr)

	lAddr, err := net.ResolveUnixAddr("unixgram", localAddr)
	if err != nil {
		log.Printf("[WPA Socket] Resolve local unixgram addr failed: %v", err)
		return "", err
	}
	rAddr, err := net.ResolveUnixAddr("unixgram", destAddr)
	if err != nil {
		log.Printf("[WPA Socket] Resolve remote unixgram addr failed: %v", err)
		return "", err
	}

	conn, err := net.DialUnix("unixgram", lAddr, rAddr)
	if err != nil {
		log.Printf("[WPA Socket] Dial unixgram failed: %v", err)
		return "", fmt.Errorf("failed to dial wpa_supplicant socket: %w", err)
	}
	defer func() {
		_ = conn.Close()
		_ = os.Remove(localAddr)
	}()

	// 2-second timeout as specified in instructions
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return "", err
	}

	log.Printf("[WPA Socket] Writing command datagram: %q", command)
	_, err = conn.Write([]byte(command))
	if err != nil {
		log.Printf("[WPA Socket] Write datagram failed: %v", err)
		return "", fmt.Errorf("failed to send command: %w", err)
	}

	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("[WPA Socket] Read response datagram failed: %v", err)
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	respStr := string(buf[:n])
	log.Printf("[WPA Socket] Received response: %q", respStr)
	return respStr, nil
}
