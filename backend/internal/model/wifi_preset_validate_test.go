package model

import "testing"

func TestValidateWifiPreset(t *testing.T) {
	tests := []struct {
		name    string
		preset  WifiPreset
		wantErr bool
	}{
		{"valid open no password", WifiPreset{Name: "Cafe", SSID: "FreeWifi", Security: "Open"}, false},
		{"valid wpa2 with password", WifiPreset{Name: "Home", SSID: "MyHome_5G", Security: "WPA2", Password: "supersecret", MacMode: "randomized"}, false},
		{"valid wpa2-psk", WifiPreset{Name: "Home2", SSID: "MyHome_2G", Security: "WPA2-PSK", Password: "supersecret"}, false},
		{"valid wpa3", WifiPreset{Name: "Home3", SSID: "MyHome_6E", Security: "WPA3", Password: "supersecret", MacMode: "hardware"}, false},
		{"valid mixed wpa2/wpa3", WifiPreset{Name: "Home4", SSID: "MyHome_Mixed", Security: "WPA2/WPA3", Password: "supersecret", MacMode: "laa"}, false},
		{"valid empty macMode", WifiPreset{Name: "Home5", SSID: "MyHome", Security: "WPA2", Password: "abc12345", MacMode: ""}, false},

		{"empty name", WifiPreset{Name: "  ", SSID: "MyHome", Security: "WPA2", Password: "abc12345"}, true},
		{"empty ssid", WifiPreset{Name: "Home", SSID: "  ", Security: "WPA2", Password: "abc12345"}, true},
		{"invalid security", WifiPreset{Name: "Home", SSID: "MyHome", Security: "WEP", Password: "abc12345"}, true},
		{"empty security", WifiPreset{Name: "Home", SSID: "MyHome", Security: "", Password: "abc12345"}, true},
		{"invalid macMode", WifiPreset{Name: "Home", SSID: "MyHome", Security: "WPA2", Password: "abc12345", MacMode: "spoofed"}, true},

		{"newline in ssid", WifiPreset{Name: "Home", SSID: "evil\nssid", Security: "WPA2", Password: "abc12345"}, true},
		{"quote in ssid", WifiPreset{Name: "Home", SSID: `evil"ssid`, Security: "WPA2", Password: "abc12345"}, true},
		{"carriage return in ssid", WifiPreset{Name: "Home", SSID: "evil\rssid", Security: "WPA2", Password: "abc12345"}, true},
		{"newline in password", WifiPreset{Name: "Home", SSID: "MyHome", Security: "WPA2", Password: "evil\npassword"}, true},
		{"quote in password", WifiPreset{Name: "Home", SSID: "MyHome", Security: "WPA2", Password: `evil"password`}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWifiPreset(tt.preset)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWifiPreset(%+v) err = %v, wantErr %v", tt.preset, err, tt.wantErr)
			}
		})
	}
}

func TestSanitizeWifiPresetForRead(t *testing.T) {
	p := WifiPreset{ID: "p1", Name: "Home", SSID: "MyHome", Security: "WPA2", Password: "supersecret"}
	sanitized := SanitizeWifiPresetForRead(p)

	if sanitized.Password != "" {
		t.Errorf("expected Password to be cleared, got %q", sanitized.Password)
	}
	if !sanitized.HasPassword {
		t.Errorf("expected HasPassword to be true when a password was set")
	}
	// Original struct passed by value must be untouched.
	if p.Password != "supersecret" {
		t.Errorf("SanitizeWifiPresetForRead must not mutate its input, got Password=%q", p.Password)
	}

	empty := WifiPreset{ID: "p2", Name: "Open", SSID: "FreeWifi", Security: "Open"}
	sanitizedEmpty := SanitizeWifiPresetForRead(empty)
	if sanitizedEmpty.HasPassword {
		t.Errorf("expected HasPassword to be false when no password was set")
	}
}
