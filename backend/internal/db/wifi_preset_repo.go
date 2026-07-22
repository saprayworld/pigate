package db

import (
	"database/sql"
	"errors"
	"fmt"

	"pigate/internal/model"
)

// GetWifiPresets returns every saved Wi-Fi preset, including its plaintext
// password. This is the "for service" read path (wifi-presets-plan.md §2.3) —
// masking/omitting the password for the frontend is the handler's job, never
// the repository's, so internal callers (e.g. the /apply flow) can still read
// the real credential.
func (r *Repository) GetWifiPresets() ([]model.WifiPreset, error) {
	rows, err := r.db.Query("SELECT id, name, ssid, security, password, mac_mode FROM wifi_presets ORDER BY name COLLATE NOCASE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []model.WifiPreset{}
	for rows.Next() {
		var p model.WifiPreset
		var macMode sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &p.SSID, &p.Security, &p.Password, &macMode); err != nil {
			return nil, err
		}
		p.MacMode = macMode.String
		p.HasPassword = p.Password != ""
		list = append(list, p)
	}
	return list, rows.Err()
}

// GetWifiPresetByID returns a single preset (with its plaintext password) or
// nil if no row matches — mirrors GetAddressByID's not-found convention.
func (r *Repository) GetWifiPresetByID(id string) (*model.WifiPreset, error) {
	row := r.db.QueryRow("SELECT id, name, ssid, security, password, mac_mode FROM wifi_presets WHERE id = ?", id)
	var p model.WifiPreset
	var macMode sql.NullString
	err := row.Scan(&p.ID, &p.Name, &p.SSID, &p.Security, &p.Password, &macMode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.MacMode = macMode.String
	p.HasPassword = p.Password != ""
	return &p, nil
}

// CreateWifiPreset validates then inserts a new preset. Mirrors CreateAddress's
// validate-then-exec shape (db/repository.go, validateAddressObject).
func (r *Repository) CreateWifiPreset(p model.WifiPreset) error {
	if err := model.ValidateWifiPreset(p); err != nil {
		return err
	}
	_, err := r.db.Exec(`INSERT INTO wifi_presets (id, name, ssid, security, password, mac_mode) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.SSID, p.Security, p.Password, p.MacMode)
	return err
}

// UpdateWifiPreset validates then updates an existing preset. An empty
// incoming Password means "leave the stored credential unchanged" — the
// write-only field never round-trips through the frontend (unlike
// network_interfaces' "••••••••" masked-sentinel convention), so an empty
// string is the only signal a caller can send for "unchanged".
func (r *Repository) UpdateWifiPreset(p model.WifiPreset) error {
	if err := model.ValidateWifiPreset(p); err != nil {
		return err
	}

	if p.Password == "" {
		var existing string
		err := r.db.QueryRow("SELECT password FROM wifi_presets WHERE id = ?", p.ID).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("wifi preset %q not found", p.ID)
		}
		if err != nil {
			return err
		}
		p.Password = existing
	}

	res, err := r.db.Exec(`UPDATE wifi_presets SET name = ?, ssid = ?, security = ?, password = ?, mac_mode = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		p.Name, p.SSID, p.Security, p.Password, p.MacMode, p.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("wifi preset %q not found", p.ID)
	}
	return nil
}

// DeleteWifiPreset removes a preset. Presets have no foreign-key references
// (they are a template, not a live link — wifi-presets-plan.md §0), so unlike
// address_objects there is nothing to reference-count before deleting.
func (r *Repository) DeleteWifiPreset(id string) error {
	res, err := r.db.Exec("DELETE FROM wifi_presets WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("wifi preset %q not found", id)
	}
	return nil
}

// WifiPresetNameExists reports whether a preset with the given name already
// exists. Mirrors AddressNameExists (db/repository.go). Callers updating a
// preset should skip this check when the submitted name is unchanged from
// the existing row, the same convention used elsewhere for UNIQUE-named
// resources.
func (r *Repository) WifiPresetNameExists(name string) (bool, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM wifi_presets WHERE name = ?", name).Scan(&count)
	return count > 0, err
}
