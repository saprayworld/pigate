package model

// WifiPreset represents a saved Wi-Fi network ("known network") that can later
// be applied to the primary or backup Wi-Fi slot of a wireless interface (see
// docs/ref/todo/wifi-presets-plan.md). It is persisted config, not runtime
// kernel state — same category as NetworkInterface's wifi fields.
//
// Password is write-only, mirroring NetworkInterface.WifiPassword: it is
// accepted on create/update requests but MUST NEVER be sent back to the
// frontend. HasPassword is the read-only substitute a GET response carries
// instead. Every read path (list/create/update/apply responses) MUST run the
// value through SanitizeWifiPresetForRead before writeJSON — do not rely on
// `omitempty` alone, since a non-empty plaintext password still marshals.
type WifiPreset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SSID        string `json:"ssid"`
	Security    string `json:"security"`
	Password    string `json:"password,omitempty"` // write-only: accepted on input, cleared before being sent back
	MacMode     string `json:"macMode,omitempty"`  // "", "hardware", "randomized", "laa"
	HasPassword bool   `json:"hasPassword"`        // read-only: true when a password is stored, in place of the plaintext value
}

// SanitizeWifiPresetForRead returns a copy of p with Password cleared and
// HasPassword derived from whether a password was actually stored. Callers in
// the api layer must run every WifiPreset returned to the frontend (list,
// create/update echo, apply's masked interface aside) through this.
func SanitizeWifiPresetForRead(p WifiPreset) WifiPreset {
	p.HasPassword = p.Password != ""
	p.Password = ""
	return p
}
