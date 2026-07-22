package db

import (
	"strings"
	"testing"

	"pigate/internal/model"
)

func TestWifiPresetCRUD(t *testing.T) {
	sqliteDB, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize memory database: %v", err)
	}
	defer sqliteDB.Close()
	repo := NewRepository(sqliteDB)

	// Fresh DB: no presets yet.
	presets, err := repo.GetWifiPresets()
	if err != nil {
		t.Fatalf("GetWifiPresets failed: %v", err)
	}
	if len(presets) != 0 {
		t.Fatalf("expected 0 seeded presets, got %d", len(presets))
	}

	// Create
	p := model.WifiPreset{
		ID:       "preset-1",
		Name:     "Office_5G",
		SSID:     "Office_5G",
		Security: "WPA2",
		Password: "s3cr3tpass",
		MacMode:  "randomized",
	}
	if err := repo.CreateWifiPreset(p); err != nil {
		t.Fatalf("CreateWifiPreset failed: %v", err)
	}

	// Reject invalid preset (validator wired in)
	bad := model.WifiPreset{ID: "preset-bad", Name: "Bad", SSID: "Bad", Security: "WEP"}
	if err := repo.CreateWifiPreset(bad); err == nil {
		t.Error("expected CreateWifiPreset to reject an invalid security value")
	}

	// GetByID returns the plaintext password (service-facing read path)
	fetched, err := repo.GetWifiPresetByID("preset-1")
	if err != nil || fetched == nil {
		t.Fatalf("GetWifiPresetByID failed: %v", err)
	}
	if fetched.Password != "s3cr3tpass" {
		t.Errorf("expected repo read to carry plaintext password, got %q", fetched.Password)
	}
	if !fetched.HasPassword {
		t.Errorf("expected HasPassword=true")
	}
	if fetched.MacMode != "randomized" {
		t.Errorf("expected macMode 'randomized', got %q", fetched.MacMode)
	}

	// GetWifiPresets lists it too
	presets, err = repo.GetWifiPresets()
	if err != nil {
		t.Fatalf("GetWifiPresets failed: %v", err)
	}
	if len(presets) != 1 || presets[0].ID != "preset-1" {
		t.Fatalf("expected 1 preset with id preset-1, got %+v", presets)
	}

	// Unique name enforcement
	exists, err := repo.WifiPresetNameExists("Office_5G")
	if err != nil || !exists {
		t.Errorf("expected WifiPresetNameExists('Office_5G') = true, got %v, err=%v", exists, err)
	}
	notExists, err := repo.WifiPresetNameExists("Nonexistent")
	if err != nil || notExists {
		t.Errorf("expected WifiPresetNameExists('Nonexistent') = false, got %v, err=%v", notExists, err)
	}

	dup := model.WifiPreset{ID: "preset-2", Name: "Office_5G", SSID: "Dup", Security: "WPA2", Password: "x"}
	if err := repo.CreateWifiPreset(dup); err == nil {
		t.Error("expected CreateWifiPreset to fail on duplicate name (UNIQUE constraint)")
	} else if !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Errorf("expected a UNIQUE constraint error, got: %v", err)
	}

	// Update: change name/ssid, but leave Password empty -> must keep the
	// previously stored password unchanged.
	updated := model.WifiPreset{
		ID:       "preset-1",
		Name:     "Office_5G_Renamed",
		SSID:     "Office_5G_v2",
		Security: "WPA3",
		Password: "", // "leave unchanged"
		MacMode:  "hardware",
	}
	if err := repo.UpdateWifiPreset(updated); err != nil {
		t.Fatalf("UpdateWifiPreset failed: %v", err)
	}
	fetched, err = repo.GetWifiPresetByID("preset-1")
	if err != nil || fetched == nil {
		t.Fatalf("GetWifiPresetByID after update failed: %v", err)
	}
	if fetched.Name != "Office_5G_Renamed" || fetched.SSID != "Office_5G_v2" || fetched.Security != "WPA3" || fetched.MacMode != "hardware" {
		t.Errorf("update did not apply expected fields, got %+v", fetched)
	}
	if fetched.Password != "s3cr3tpass" {
		t.Errorf("expected password to remain unchanged after empty-password update, got %q", fetched.Password)
	}

	// Update with a real password DOES change it.
	updated2 := model.WifiPreset{
		ID:       "preset-1",
		Name:     "Office_5G_Renamed",
		SSID:     "Office_5G_v2",
		Security: "WPA3",
		Password: "brandnewpass",
	}
	if err := repo.UpdateWifiPreset(updated2); err != nil {
		t.Fatalf("UpdateWifiPreset (with new password) failed: %v", err)
	}
	fetched, _ = repo.GetWifiPresetByID("preset-1")
	if fetched.Password != "brandnewpass" {
		t.Errorf("expected password to be updated to 'brandnewpass', got %q", fetched.Password)
	}

	// Update on a non-existent id fails.
	if err := repo.UpdateWifiPreset(model.WifiPreset{ID: "no-such-id", Name: "X", SSID: "X", Security: "WPA2"}); err == nil {
		t.Error("expected UpdateWifiPreset on a nonexistent id to fail")
	}

	// Delete
	if err := repo.DeleteWifiPreset("preset-1"); err != nil {
		t.Fatalf("DeleteWifiPreset failed: %v", err)
	}
	fetched, err = repo.GetWifiPresetByID("preset-1")
	if err != nil {
		t.Fatalf("GetWifiPresetByID after delete errored: %v", err)
	}
	if fetched != nil {
		t.Error("expected preset to be gone after delete")
	}

	// Delete on a non-existent id fails.
	if err := repo.DeleteWifiPreset("preset-1"); err == nil {
		t.Error("expected DeleteWifiPreset on an already-deleted id to fail")
	}
}
