package service

import (
	"errors"
	"fmt"

	"pigate/internal/db"
	"pigate/internal/model"
)

// Sentinel errors so the API layer (T-05) can map ApplyPresetToInterface
// failures to the right HTTP status: not-found -> 404, invalid slot / not
// wireless -> 400. Mirrors the ErrVlan*/ErrAlias* convention in interface.go.
var (
	ErrWifiPresetNotFound          = errors.New("wifi preset not found")
	ErrWifiPresetInterfaceNotFound = errors.New("interface not found")
	ErrWifiPresetNotWireless       = errors.New("interface is not a wireless interface")
	ErrWifiPresetInvalidSlot       = errors.New(`slot must be "primary" or "backup"`)
)

// WifiPresetService owns CRUD for the saved Wi-Fi network library and the
// server-side "apply preset to an interface slot" flow. Apply is
// intentionally the only place a preset's password is ever read: it goes
// straight from the DB into ifaceService.ApplyInterfaceConfig, never through
// an HTTP response body (wifi-presets-plan.md §0/§2.3).
type WifiPresetService struct {
	repo         *db.Repository
	ifaceService *InterfaceService
}

func NewWifiPresetService(repo *db.Repository, ifaceService *InterfaceService) *WifiPresetService {
	return &WifiPresetService{
		repo:         repo,
		ifaceService: ifaceService,
	}
}

// GetAll returns every saved preset (with plaintext password — callers in the
// api layer must sanitize before returning it to the frontend).
func (s *WifiPresetService) GetAll() ([]model.WifiPreset, error) {
	return s.repo.GetWifiPresets()
}

// GetByID returns a single preset, or nil if not found.
func (s *WifiPresetService) GetByID(id string) (*model.WifiPreset, error) {
	return s.repo.GetWifiPresetByID(id)
}

// Create validates and persists a new preset.
func (s *WifiPresetService) Create(p model.WifiPreset) error {
	return s.repo.CreateWifiPreset(p)
}

// Update validates and persists changes to an existing preset. An empty
// Password means "keep the currently stored credential" (see
// db.Repository.UpdateWifiPreset).
func (s *WifiPresetService) Update(p model.WifiPreset) error {
	return s.repo.UpdateWifiPreset(p)
}

// Delete removes a preset. Deleting a preset never touches interfaces that
// previously applied it — a preset is a template, not a live link
// (wifi-presets-plan.md §0).
func (s *WifiPresetService) Delete(id string) error {
	return s.repo.DeleteWifiPreset(id)
}

// NameExists reports whether another preset already uses the given name.
func (s *WifiPresetService) NameExists(name string) (bool, error) {
	return s.repo.WifiPresetNameExists(name)
}

// ApplyPresetToInterface fills the primary or backup Wi-Fi slot of the given
// interface with the given preset's SSID/password/security(/macMode), then
// applies it through the existing InterfaceService.ApplyInterfaceConfig path
// (persist to DB + ConfigureWifi into wpa_supplicant). It never adds a new
// kernel capability — it only prepares the same NetworkInterface struct
// PUT /api/interfaces/{id} already accepts (wifi-presets-plan.md §2.3).
//
// Returns the mutated interface (still carrying the plaintext password in
// memory) so the caller can log/return it — callers MUST mask its password
// fields (e.g. maskInterfacePasswords) before this ever reaches an HTTP
// response.
func (s *WifiPresetService) ApplyPresetToInterface(presetID, interfaceID, slot string) (*model.NetworkInterface, error) {
	if slot != "primary" && slot != "backup" {
		return nil, ErrWifiPresetInvalidSlot
	}

	preset, err := s.repo.GetWifiPresetByID(presetID)
	if err != nil {
		return nil, fmt.Errorf("failed to load wifi preset: %w", err)
	}
	if preset == nil {
		return nil, ErrWifiPresetNotFound
	}

	iface, err := s.repo.GetInterfaceByID(interfaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load interface: %w", err)
	}
	if iface == nil {
		return nil, ErrWifiPresetInterfaceNotFound
	}
	if iface.Type != "wireless" {
		return nil, ErrWifiPresetNotWireless
	}

	ssid := preset.SSID
	password := preset.Password
	security := preset.Security

	switch slot {
	case "primary":
		iface.WifiSSID = &ssid
		iface.WifiPassword = &password
		iface.WifiSecurity = &security
		if preset.MacMode != "" {
			macMode := preset.MacMode
			iface.MacMode = &macMode
		}
	case "backup":
		iface.BackupSSID = &ssid
		iface.BackupWifiPassword = &password
		iface.BackupWifiSecurity = &security
	}

	if err := s.ifaceService.ApplyInterfaceConfig(*iface); err != nil {
		return nil, fmt.Errorf("failed to apply wifi preset to interface: %w", err)
	}

	return iface, nil
}
