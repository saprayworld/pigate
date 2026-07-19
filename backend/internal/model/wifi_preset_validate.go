package model

import (
	"fmt"
	"strings"
)

// validWifiPresetSecurities mirrors the switch inside writeNetworkBlock
// (kernel/wpa.go) — the only security strings that produce a defined
// wpa_supplicant key_mgmt block. Anything else falls through to that
// function's "fallback" branch silently, which we would rather reject here.
var validWifiPresetSecurities = map[string]bool{
	"Open":      true,
	"WPA2":      true,
	"WPA2-PSK":  true,
	"WPA3":      true,
	"WPA2/WPA3": true,
}

// validWifiPresetMacModes mirrors the CHECK constraint on
// network_interfaces.mac_mode, plus "" for "unset/don't touch mac_mode".
var validWifiPresetMacModes = map[string]bool{
	"":           true,
	"hardware":   true,
	"randomized": true,
	"laa":        true,
}

// sanitizeWpaLikeInput mirrors kernel.SanitizeWpaInput (strip \n, \r, ") byte
// for byte. It is duplicated here (not imported) because kernel already
// imports model — importing kernel from model would create an import cycle.
// Keep this in sync with kernel/wpa.go's SanitizeWpaInput if that ever
// changes.
func sanitizeWpaLikeInput(val string) string {
	val = strings.ReplaceAll(val, "\n", "")
	val = strings.ReplaceAll(val, "\r", "")
	val = strings.ReplaceAll(val, "\"", "")
	return val
}

// ValidateWifiPreset checks a saved Wi-Fi preset before it is persisted. It is
// pure (no DB/kernel access) so it can be unit tested in isolation and reused
// by the repository (create/update), the /apply flow, and the backup
// import's fail-closed validation pass.
//
// name/ssid must be non-empty; security/macMode must be one of the values the
// wpa_supplicant config generator (kernel/wpa.go) understands. ssid/password
// are REJECTED (not silently stripped) if they contain characters
// kernel.SanitizeWpaInput would otherwise strip — those characters could
// inject arbitrary wpa_supplicant directives if this preset is later applied
// to an interface. kernel.SanitizeWpaInput remains a second line of defense
// at apply time; do not remove it just because this validator exists
// (defense in depth, see wifi-presets-plan.md Caution "anti-injection").
func ValidateWifiPreset(p WifiPreset) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("preset name must not be empty")
	}
	if strings.TrimSpace(p.SSID) == "" {
		return fmt.Errorf("ssid must not be empty")
	}
	if !validWifiPresetSecurities[p.Security] {
		return fmt.Errorf("invalid security %q: must be one of Open, WPA2, WPA2-PSK, WPA3, WPA2/WPA3", p.Security)
	}
	if !validWifiPresetMacModes[p.MacMode] {
		return fmt.Errorf("invalid macMode %q: must be one of \"\", hardware, randomized, laa", p.MacMode)
	}
	if sanitizeWpaLikeInput(p.SSID) != p.SSID {
		return fmt.Errorf("ssid contains invalid characters (newline, carriage return, or double-quote not allowed)")
	}
	if sanitizeWpaLikeInput(p.Password) != p.Password {
		return fmt.Errorf("password contains invalid characters (newline, carriage return, or double-quote not allowed)")
	}
	return nil
}
