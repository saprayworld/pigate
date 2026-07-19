package service

import (
	"errors"
	"testing"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

func newWifiPresetTestServices(t *testing.T) (*WifiPresetService, *db.Repository) {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to initialize memory database: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })

	repo := db.NewRepository(sqliteDB)
	ifaceService := NewInterfaceService(repo, kernel.NewMockNetwork())
	return NewWifiPresetService(repo, ifaceService), repo
}

func seedWirelessInterface(t *testing.T, repo *db.Repository, id, name string) {
	t.Helper()
	iface := model.NetworkInterface{
		ID:             id,
		Name:           name,
		Alias:          name,
		Role:           "WAN",
		Type:           "wireless",
		AddressingMode: "dhcp",
		MacAddress:     "AA:BB:CC:DD:EE:FF",
		AdminAccess:    []string{"PING"},
		Status:         "up",
		Speed:          "72 Mbps",
	}
	if err := repo.CreateInterfaceForTest(iface); err != nil {
		t.Fatalf("failed to seed wireless interface: %v", err)
	}
}

func seedEthernetInterface(t *testing.T, repo *db.Repository, id, name string) {
	t.Helper()
	iface := model.NetworkInterface{
		ID:             id,
		Name:           name,
		Alias:          name,
		Role:           "LAN",
		Type:           "ethernet",
		AddressingMode: "static",
		IP:             "192.168.1.1",
		Netmask:        "24",
		MacAddress:     "AA:BB:CC:DD:EE:00",
		AdminAccess:    []string{"PING"},
		Status:         "up",
		Speed:          "1000 Mbps",
	}
	if err := repo.CreateInterfaceForTest(iface); err != nil {
		t.Fatalf("failed to seed ethernet interface: %v", err)
	}
}

func TestWifiPresetServiceCRUD(t *testing.T) {
	svc, _ := newWifiPresetTestServices(t)

	p := model.WifiPreset{ID: "preset-1", Name: "Home", SSID: "MyHome", Security: "WPA2", Password: "secretpass"}
	if err := svc.Create(p); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	fetched, err := svc.GetByID("preset-1")
	if err != nil || fetched == nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if fetched.Password != "secretpass" {
		t.Errorf("expected service GetByID to carry plaintext password, got %q", fetched.Password)
	}

	all, err := svc.GetAll()
	if err != nil || len(all) != 1 {
		t.Fatalf("GetAll failed: %v (len=%d)", err, len(all))
	}

	exists, err := svc.NameExists("Home")
	if err != nil || !exists {
		t.Errorf("expected NameExists('Home') = true, got %v, err=%v", exists, err)
	}

	if err := svc.Update(model.WifiPreset{ID: "preset-1", Name: "Home2", SSID: "MyHome2", Security: "WPA3"}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	fetched, _ = svc.GetByID("preset-1")
	if fetched.Name != "Home2" || fetched.Password != "secretpass" {
		t.Errorf("expected update to rename but keep password, got %+v", fetched)
	}

	if err := svc.Delete("preset-1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	fetched, _ = svc.GetByID("preset-1")
	if fetched != nil {
		t.Error("expected preset to be gone after Delete")
	}
}

func TestApplyPresetToInterface_Primary(t *testing.T) {
	svc, repo := newWifiPresetTestServices(t)
	seedWirelessInterface(t, repo, "iface-wifi-1", "wlan9")

	preset := model.WifiPreset{ID: "preset-1", Name: "Home", SSID: "MyHome_5G", Security: "WPA2", Password: "secretpass", MacMode: "randomized"}
	if err := svc.Create(preset); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	iface, err := svc.ApplyPresetToInterface("preset-1", "iface-wifi-1", "primary")
	if err != nil {
		t.Fatalf("ApplyPresetToInterface failed: %v", err)
	}
	if iface.WifiSSID == nil || *iface.WifiSSID != "MyHome_5G" {
		t.Errorf("expected WifiSSID to be set to MyHome_5G, got %+v", iface.WifiSSID)
	}
	if iface.WifiPassword == nil || *iface.WifiPassword != "secretpass" {
		t.Errorf("expected WifiPassword to be applied")
	}
	if iface.WifiSecurity == nil || *iface.WifiSecurity != "WPA2" {
		t.Errorf("expected WifiSecurity to be WPA2, got %+v", iface.WifiSecurity)
	}
	if iface.MacMode == nil || *iface.MacMode != "randomized" {
		t.Errorf("expected MacMode to be randomized, got %+v", iface.MacMode)
	}

	// Verify it was persisted to the DB too (ApplyInterfaceConfig persists).
	persisted, err := repo.GetInterfaceByID("iface-wifi-1")
	if err != nil || persisted == nil {
		t.Fatalf("failed to reload persisted interface: %v", err)
	}
	if persisted.WifiSSID == nil || *persisted.WifiSSID != "MyHome_5G" {
		t.Errorf("expected persisted interface to carry the applied SSID, got %+v", persisted.WifiSSID)
	}
}

func TestApplyPresetToInterface_Backup(t *testing.T) {
	svc, repo := newWifiPresetTestServices(t)
	seedWirelessInterface(t, repo, "iface-wifi-1", "wlan9")

	preset := model.WifiPreset{ID: "preset-1", Name: "Backup", SSID: "Backup_2G", Security: "WPA2-PSK", Password: "backuppass"}
	if err := svc.Create(preset); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	iface, err := svc.ApplyPresetToInterface("preset-1", "iface-wifi-1", "backup")
	if err != nil {
		t.Fatalf("ApplyPresetToInterface failed: %v", err)
	}
	if iface.BackupSSID == nil || *iface.BackupSSID != "Backup_2G" {
		t.Errorf("expected BackupSSID to be set to Backup_2G, got %+v", iface.BackupSSID)
	}
	if iface.BackupWifiPassword == nil || *iface.BackupWifiPassword != "backuppass" {
		t.Errorf("expected BackupWifiPassword to be applied")
	}
	if iface.BackupWifiSecurity == nil || *iface.BackupWifiSecurity != "WPA2-PSK" {
		t.Errorf("expected BackupWifiSecurity to be WPA2-PSK, got %+v", iface.BackupWifiSecurity)
	}
	// Primary slot must be untouched.
	if iface.WifiSSID != nil {
		t.Errorf("expected primary WifiSSID to remain untouched, got %+v", iface.WifiSSID)
	}
}

func TestApplyPresetToInterface_InvalidSlot(t *testing.T) {
	svc, repo := newWifiPresetTestServices(t)
	seedWirelessInterface(t, repo, "iface-wifi-1", "wlan9")
	if err := svc.Create(model.WifiPreset{ID: "preset-1", Name: "Home", SSID: "MyHome", Security: "WPA2"}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err := svc.ApplyPresetToInterface("preset-1", "iface-wifi-1", "tertiary")
	if !errors.Is(err, ErrWifiPresetInvalidSlot) {
		t.Errorf("expected ErrWifiPresetInvalidSlot, got %v", err)
	}
}

func TestApplyPresetToInterface_NotWireless(t *testing.T) {
	svc, repo := newWifiPresetTestServices(t)
	seedEthernetInterface(t, repo, "iface-eth-1", "eth9")
	if err := svc.Create(model.WifiPreset{ID: "preset-1", Name: "Home", SSID: "MyHome", Security: "WPA2"}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err := svc.ApplyPresetToInterface("preset-1", "iface-eth-1", "primary")
	if !errors.Is(err, ErrWifiPresetNotWireless) {
		t.Errorf("expected ErrWifiPresetNotWireless, got %v", err)
	}
}

func TestApplyPresetToInterface_PresetNotFound(t *testing.T) {
	svc, repo := newWifiPresetTestServices(t)
	seedWirelessInterface(t, repo, "iface-wifi-1", "wlan9")

	_, err := svc.ApplyPresetToInterface("no-such-preset", "iface-wifi-1", "primary")
	if !errors.Is(err, ErrWifiPresetNotFound) {
		t.Errorf("expected ErrWifiPresetNotFound, got %v", err)
	}
}

func TestApplyPresetToInterface_InterfaceNotFound(t *testing.T) {
	svc, _ := newWifiPresetTestServices(t)
	if err := svc.Create(model.WifiPreset{ID: "preset-1", Name: "Home", SSID: "MyHome", Security: "WPA2"}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err := svc.ApplyPresetToInterface("preset-1", "no-such-iface", "primary")
	if !errors.Is(err, ErrWifiPresetInterfaceNotFound) {
		t.Errorf("expected ErrWifiPresetInterfaceNotFound, got %v", err)
	}
}
