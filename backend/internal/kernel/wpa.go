package kernel

import (
	"crypto/rand"
	"encoding/hex"
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

// freqList5GHz is the full set of 5GHz channel center frequencies (MHz) used to
// populate wpa_supplicant's freq_list=... when the "Prefer 5GHz" toggle
// (issue #72) is enabled, restricting the radio to associate only on 5GHz.
const freqList5GHz = "5160 5180 5200 5220 5240 5260 5280 5300 5320 5340 5480 5500 5520 5540 5560 5580 5600 5620 5640 5660 5680 5700 5720 5745 5765 5785 5805 5825 5845 5865 5885"

// SanitizeWpaInput strips newlines and double quotes to prevent configuration injection
func SanitizeWpaInput(val string) string {
	val = strings.ReplaceAll(val, "\n", "")
	val = strings.ReplaceAll(val, "\r", "")
	val = strings.ReplaceAll(val, "\"", "")
	return val
}

// wpaLoggableCommands is the allowlist of complete control commands that are safe
// to log verbatim — exactly the commands PiGate sends today (PSKs go through the
// config file, not the socket). Redaction fails CLOSED: any command not on this
// list that carries arguments is logged as verb+"[redacted]", so a future
// secret-bearing command (`SET_NETWORK 0 psk "..."`, `WPS_PIN ...`,
// `SET_NETWORK 0 private_key_passwd "..."`) can never reach the journal even if
// nobody remembers to update this list. (A secret-keyword blocklist was rejected:
// it fails open for keywords it doesn't know about.)
var wpaLoggableCommands = map[string]bool{
	"RECONFIGURE": true,
	"RECONNECT":   true,
}

// redactWpaCommand returns a log-safe form of a wpa control command. Allowlisted
// commands and bare verbs (a lone verb carries no argument to leak) are returned
// as-is so ordinary commands stay debuggable; anything else keeps only its verb
// followed by "[redacted]".
func redactWpaCommand(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) <= 1 || wpaLoggableCommands[strings.ToUpper(strings.TrimSpace(cmd))] {
		return cmd
	}
	return fields[0] + " [redacted]"
}

// writeWpaHeader writes the wpa_supplicant global directives shared by every
// config file PiGate generates — both full user configs (GenerateWpaConfig)
// and the bootstrap placeholder-only config (GenerateMinimalWpaConfig) — so
// the country code and scan-stability directives (issue #72) are defined in
// exactly one place instead of being duplicated across generators.
func writeWpaHeader(sb *strings.Builder) {
	sb.WriteString("ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev\n")
	sb.WriteString("update_config=1\n")
	sb.WriteString("country=TH\n")
	// Scan-stability settings (issue #72): background scans left uncontrolled
	// cause periodic Wi-Fi stalls, so pin these explicitly and unconditionally.
	sb.WriteString("ap_scan=1\n")
	sb.WriteString("autoscan=periodic:10\n")
	sb.WriteString("disable_scan_offload=1\n")
}

// GenerateWpaConfig constructs the raw text content for a wpa_supplicant configuration file.
// It incorporates security options (Open, WPA2, WPA3, WPA2/WPA3), MAC randomization, and weight-based priorities.
func GenerateWpaConfig(ssid, password, security, backupSSID, backupPassword, backupSecurity, macMode string, prefer5GHz bool) string {
	log.Printf("[WPA Config] Building config layout for SSID=%q (Security=%s, HasPassword=%t), BackupSSID=%q (BackupSecurity=%s, HasBackupPassword=%t), MacMode=%s, Prefer5GHz=%t",
		ssid, security, password != "", backupSSID, backupSecurity, backupPassword != "", macMode, prefer5GHz)

	var sb strings.Builder
	writeWpaHeader(&sb)
	if macMode == "randomized" {
		sb.WriteString("preassoc_mac_addr=1\n")
	}
	sb.WriteString("\n")

	// Primary network block (priority 10)
	writeNetworkBlock(&sb, ssid, password, security, 10, macMode == "randomized", prefer5GHz)

	// Backup network block (priority 5)
	if SanitizeWpaInput(backupSSID) != "" {
		sb.WriteString("\n")
		writeNetworkBlock(&sb, backupSSID, backupPassword, backupSecurity, 5, macMode == "randomized", prefer5GHz)
	}

	return sb.String()
}

func writeNetworkBlock(sb *strings.Builder, ssid, password, security string, priority int, randomizeMac bool, prefer5GHz bool) {
	cleanSSID := SanitizeWpaInput(ssid)
	cleanPassword := SanitizeWpaInput(password)

	sb.WriteString("network={\n")
	sb.WriteString(fmt.Sprintf("    ssid=\"%s\"\n", cleanSSID))

	switch security {
	case "WPA3":
		sb.WriteString("    key_mgmt=SAE\n")
		sb.WriteString("    ieee80211w=2\n") // PMF required for WPA3
		if cleanPassword != "" {
			sb.WriteString(fmt.Sprintf("    psk=\"%s\"\n", cleanPassword))
		}
	case "WPA2/WPA3":
		sb.WriteString("    key_mgmt=WPA-PSK SAE\n")
		sb.WriteString("    ieee80211w=1\n") // PMF capable/optional for transition mode
		if cleanPassword != "" {
			sb.WriteString(fmt.Sprintf("    psk=\"%s\"\n", cleanPassword))
		}
	case "WPA2", "WPA2-PSK":
		sb.WriteString("    key_mgmt=WPA-PSK\n")
		if cleanPassword != "" {
			sb.WriteString(fmt.Sprintf("    psk=\"%s\"\n", cleanPassword))
		}
	case "Open":
		sb.WriteString("    key_mgmt=NONE\n")
	default:
		// Fallback
		if cleanPassword != "" {
			sb.WriteString("    key_mgmt=WPA-PSK\n")
			sb.WriteString(fmt.Sprintf("    psk=\"%s\"\n", cleanPassword))
		} else {
			sb.WriteString("    key_mgmt=NONE\n")
		}
	}

	if randomizeMac {
		sb.WriteString("    mac_addr=1\n")
	}
	if prefer5GHz {
		sb.WriteString(fmt.Sprintf("    freq_list=%s\n", freqList5GHz))
	}
	sb.WriteString(fmt.Sprintf("    priority=%d\n", priority))
	sb.WriteString("}\n")
}

// randomHexChars returns a cryptographically random hex string of exactly
// length characters, generated via crypto/rand. Hex output is guaranteed safe
// to embed in a quoted wpa_supplicant config value (no '"' or newline can ever
// appear).
func randomHexChars(length int) (string, error) {
	buf := make([]byte, (length+1)/2)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf)[:length], nil
}

// GenerateMinimalWpaConfig builds the content of a bootstrap wpa_supplicant
// config used only to get wpa_supplicant to start and attach to a wireless
// interface that has never had a real network saved for it (issue #88 —
// the on-board brcmfmac chip only returns nl80211 scan results once
// wpa_supplicant is attached to the interface; a header-only config was
// confirmed on real hardware to NOT be enough to reach that attached state).
//
// It contains the same header as GenerateWpaConfig plus exactly one
// placeholder network block that is enabled (required) but can never
// actually associate anywhere:
//   - key_mgmt=WPA-PSK (never NONE): forces a WPA 4-way handshake, so even if
//     an unrelated nearby AP happens to broadcast a colliding SSID, the
//     handshake can never succeed against it — an open/NONE network would
//     instead associate successfully to such an AP.
//   - never disabled=1: a disabled network is equivalent to having no
//     enabled network block at all, which is the header-only case already
//     confirmed on hardware to not unlock scanning.
//   - ssid/psk are generated fresh via crypto/rand every time this is called
//     — never hardcoded, since this is a public repository and a fixed
//     value could be pre-matched by an attacker-controlled AP.
//
// EnsureWpaConfig is the only intended caller, and only writes this output
// once, when no config file exists yet for the interface.
func GenerateMinimalWpaConfig() (string, error) {
	ssid, err := randomHexChars(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate placeholder ssid: %w", err)
	}
	psk, err := randomHexChars(63)
	if err != nil {
		return "", fmt.Errorf("failed to generate placeholder psk: %w", err)
	}

	var sb strings.Builder
	writeWpaHeader(&sb)
	sb.WriteString("\n")
	sb.WriteString("network={\n")
	sb.WriteString(fmt.Sprintf("    ssid=\"%s\"\n", ssid))
	sb.WriteString("    key_mgmt=WPA-PSK\n")
	sb.WriteString(fmt.Sprintf("    psk=\"%s\"\n", psk))
	sb.WriteString("    priority=0\n")
	sb.WriteString("}\n")

	return sb.String(), nil
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

	loggedCmd := redactWpaCommand(command)
	log.Printf("[WPA Socket] Writing command datagram: %q", loggedCmd)
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
	// If the command was redacted, its response may echo the secret back —
	// redact the logged response too (the caller still gets the real string).
	loggedResp := respStr
	if loggedCmd != command {
		loggedResp = "[redacted]"
	}
	log.Printf("[WPA Socket] Received response: %q", loggedResp)
	return respStr, nil
}
