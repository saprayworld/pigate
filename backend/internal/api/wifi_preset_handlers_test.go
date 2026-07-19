package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"pigate/internal/model"
)

// assertNoPlaintextPassword is the Definition of Done check from
// docs/ref/todo/wifi-presets-plan.md: a Wi-Fi preset (or the interface an
// apply mutated) response body must never carry the plaintext password —
// neither as the literal secret string anywhere in the body, nor as a
// non-empty "password" JSON value.
func assertNoPlaintextPassword(t *testing.T, body []byte, secret string) {
	t.Helper()
	s := string(body)
	if secret != "" && strings.Contains(s, secret) {
		t.Fatalf("response body leaked the plaintext password %q: %s", secret, s)
	}

	// Belt-and-suspenders: also make sure no "password" key carries a
	// non-empty value, whether the body is a single object or a list.
	checkObj := func(obj map[string]interface{}) {
		if pw, ok := obj["password"]; ok {
			if str, ok := pw.(string); ok && str != "" {
				t.Fatalf("response body contains a non-empty password field: %v", obj)
			}
		}
	}
	var list []map[string]interface{}
	if err := json.Unmarshal(body, &list); err == nil {
		for _, obj := range list {
			checkObj(obj)
		}
		return
	}
	var single map[string]interface{}
	if err := json.Unmarshal(body, &single); err == nil {
		checkObj(single)
	}
}

// TestWifiPresetCreateListUpdateNoPasswordLeak covers the create/list/update
// HTTP surface: normal create succeeds, duplicate names are rejected (409),
// an SSID containing wpa_supplicant-config-breaking characters is rejected
// (400), and — most importantly — no response body (create echo, list,
// update echo) ever carries the plaintext password.
func TestWifiPresetCreateListUpdateNoPasswordLeak(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"
	const secret = "SuperSecret123!"

	createBody := map[string]interface{}{
		"name":     "Home_5G",
		"ssid":     "MyHomeNetwork",
		"security": "WPA2",
		"password": secret,
	}
	rec := vlanReq(t, handler, "POST", "/api/wifi-presets", token, createBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on create, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	assertNoPlaintextPassword(t, rec.Body.Bytes(), secret)

	var created model.WifiPreset
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected server-assigned ID on create")
	}
	if created.Password != "" {
		t.Errorf("expected Password field cleared on create response, got %q", created.Password)
	}
	if !created.HasPassword {
		t.Errorf("expected HasPassword=true on create response")
	}

	// Duplicate name -> 409.
	rec = vlanReq(t, handler, "POST", "/api/wifi-presets", token, createBody)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate preset name, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Invalid SSID (embedded newline -> wpa_supplicant config injection) -> 400.
	rec = vlanReq(t, handler, "POST", "/api/wifi-presets", token, map[string]interface{}{
		"name":     "Bad_SSID",
		"ssid":     "evil\nssid",
		"security": "WPA2",
		"password": "whatever",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for ssid containing a newline, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// GET list -> no password leak anywhere, HasPassword survives.
	rec = vlanReq(t, handler, "GET", "/api/wifi-presets", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on list, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	assertNoPlaintextPassword(t, rec.Body.Bytes(), secret)

	var list []model.WifiPreset
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 preset in list, got %d", len(list))
	}
	if list[0].Password != "" {
		t.Errorf("expected Password field cleared in list response, got %q", list[0].Password)
	}
	if !list[0].HasPassword {
		t.Errorf("expected HasPassword=true in list response")
	}

	// Update: rename, leave password blank ("keep existing") -> HasPassword
	// must still read true (regression check for the "echo input verbatim"
	// bug: an empty submitted password does not mean the stored one is gone).
	rec = vlanReq(t, handler, "PUT", "/api/wifi-presets/"+created.ID, token, map[string]interface{}{
		"name":     "Home_5G_Renamed",
		"ssid":     "MyHomeNetwork",
		"security": "WPA2",
		"password": "",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	assertNoPlaintextPassword(t, rec.Body.Bytes(), secret)

	var updated model.WifiPreset
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to decode update response: %v", err)
	}
	if updated.Name != "Home_5G_Renamed" {
		t.Errorf("expected renamed preset, got %q", updated.Name)
	}
	if !updated.HasPassword {
		t.Errorf("expected HasPassword still true after a blank-password update (password should be kept)")
	}

	// Update -> duplicate name (not itself) -> 409.
	rec = vlanReq(t, handler, "POST", "/api/wifi-presets", token, map[string]interface{}{
		"name":     "Office_2G",
		"ssid":     "OfficeNet",
		"security": "Open",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating second preset, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var second model.WifiPreset
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatalf("failed to decode second create response: %v", err)
	}

	rec = vlanReq(t, handler, "PUT", "/api/wifi-presets/"+second.ID, token, map[string]interface{}{
		"name":     "Home_5G_Renamed",
		"ssid":     "OfficeNet",
		"security": "Open",
	})
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 renaming a preset into another preset's name, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Delete -> 404 for an unknown ID.
	rec = vlanReq(t, handler, "DELETE", "/api/wifi-presets/no-such-preset", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 deleting an unknown preset, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Delete -> success.
	rec = vlanReq(t, handler, "DELETE", "/api/wifi-presets/"+created.ID, token, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 deleting an existing preset, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestWifiPresetApplyFlow covers the SENSITIVE POST /api/wifi-presets/{id}/apply
// HTTP mapping: bad slot -> 400, unknown preset/interface -> 404, non-wireless
// interface -> 400, and a successful apply that (a) actually mutates the
// target interface (visible via the repo, not just the response) and (b)
// never returns the plaintext password in the response body.
func TestWifiPresetApplyFlow(t *testing.T) {
	handler, repo := setupTestServer(t)
	token := "mock_session_id_test_token"
	const secret = "ApplySecret456"

	wifiIface := model.NetworkInterface{
		ID:             "iface-wifi-apply",
		Name:           "wlan5",
		Alias:          "wlan5",
		Role:           "WAN",
		Type:           "wireless",
		AddressingMode: "dhcp",
		MacAddress:     "AA:BB:CC:DD:EE:01",
		AdminAccess:    []string{"PING"},
		Status:         "up",
		Speed:          "72 Mbps",
	}
	if err := repo.CreateInterfaceForTest(wifiIface); err != nil {
		t.Fatalf("failed to seed wireless interface: %v", err)
	}
	ethIface := model.NetworkInterface{
		ID:             "iface-eth-apply",
		Name:           "eth5",
		Alias:          "eth5",
		Role:           "LAN",
		Type:           "ethernet",
		AddressingMode: "static",
		IP:             "192.168.5.1",
		Netmask:        "24",
		MacAddress:     "AA:BB:CC:DD:EE:02",
		AdminAccess:    []string{"PING"},
		Status:         "up",
		Speed:          "1000 Mbps",
	}
	if err := repo.CreateInterfaceForTest(ethIface); err != nil {
		t.Fatalf("failed to seed ethernet interface: %v", err)
	}

	rec := vlanReq(t, handler, "POST", "/api/wifi-presets", token, map[string]interface{}{
		"name":     "Apply_Target",
		"ssid":     "ApplySSID",
		"security": "WPA2",
		"password": secret,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating preset, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var preset model.WifiPreset
	if err := json.Unmarshal(rec.Body.Bytes(), &preset); err != nil {
		t.Fatalf("failed to decode preset create response: %v", err)
	}

	// Invalid slot -> 400.
	rec = vlanReq(t, handler, "POST", "/api/wifi-presets/"+preset.ID+"/apply", token, map[string]interface{}{
		"interfaceId": wifiIface.ID, "slot": "tertiary",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid slot, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Unknown preset -> 404.
	rec = vlanReq(t, handler, "POST", "/api/wifi-presets/no-such-preset/apply", token, map[string]interface{}{
		"interfaceId": wifiIface.ID, "slot": "primary",
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing preset, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Unknown interface -> 404.
	rec = vlanReq(t, handler, "POST", "/api/wifi-presets/"+preset.ID+"/apply", token, map[string]interface{}{
		"interfaceId": "no-such-iface", "slot": "primary",
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing interface, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Non-wireless interface -> 400.
	rec = vlanReq(t, handler, "POST", "/api/wifi-presets/"+preset.ID+"/apply", token, map[string]interface{}{
		"interfaceId": ethIface.ID, "slot": "primary",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a non-wireless interface, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Successful apply -> 200, response carries the masked interface.
	rec = vlanReq(t, handler, "POST", "/api/wifi-presets/"+preset.ID+"/apply", token, map[string]interface{}{
		"interfaceId": wifiIface.ID, "slot": "primary",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on successful apply, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	assertNoPlaintextPassword(t, rec.Body.Bytes(), secret)

	var applied model.NetworkInterface
	if err := json.Unmarshal(rec.Body.Bytes(), &applied); err != nil {
		t.Fatalf("failed to decode apply response: %v", err)
	}
	if applied.WifiSSID == nil || *applied.WifiSSID != "ApplySSID" {
		t.Errorf("expected applied interface WifiSSID = ApplySSID, got %+v", applied.WifiSSID)
	}
	if applied.WifiPassword == nil || *applied.WifiPassword != "••••••••" {
		t.Errorf("expected applied interface WifiPassword to be masked in the response, got %+v", applied.WifiPassword)
	}

	// The mutation must be real (server-side), not just reflected in the
	// response: reload straight from the repo.
	persisted, err := repo.GetInterfaceByID(wifiIface.ID)
	if err != nil || persisted == nil {
		t.Fatalf("failed to reload persisted interface: %v", err)
	}
	if persisted.WifiSSID == nil || *persisted.WifiSSID != "ApplySSID" {
		t.Errorf("expected persisted interface to carry the applied SSID, got %+v", persisted.WifiSSID)
	}
	if persisted.WifiPassword == nil || *persisted.WifiPassword != secret {
		t.Errorf("expected the interface's stored (unmasked) password to be the preset's password server-side")
	}
}

// TestWifiPresetNonSuperAdminForbidden asserts that all /api/wifi-presets
// routes are registered as superAdminRoute (router.go), not authRoute: a
// non-super_admin must be blocked even on a plain GET, which authRoute's
// RoleReadOnlyMiddleware would have allowed through.
func TestWifiPresetNonSuperAdminForbidden(t *testing.T) {
	handler, repo := setupTestServer(t)

	if err := repo.CreateUser(model.User{
		ID: "user-readonly-wifi", Username: "wifiviewer", PasswordHash: "x",
		Role: model.RoleAdminReadonly, Status: model.StatusActive,
	}); err != nil {
		t.Fatalf("create readonly user: %v", err)
	}
	AddSession("wifiviewer_token", "wifiviewer")

	rec := vlanReq(t, handler, "GET", "/api/wifi-presets", "wifiviewer_token", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-super_admin GET /api/wifi-presets, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	rec = vlanReq(t, handler, "POST", "/api/wifi-presets", "wifiviewer_token", map[string]interface{}{
		"name": "X", "ssid": "X", "security": "WPA2",
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-super_admin POST /api/wifi-presets, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}
