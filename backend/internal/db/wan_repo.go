package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"pigate/internal/model"

	"github.com/google/uuid"
)

// wanUplinkColumns is the column list shared by every SELECT below, kept as
// one constant so Get/GetByID never drift from each other (mirrors the
// qos_rules repo pattern in db/qos.go).
const wanUplinkColumns = `id, name, interface, priority, probe_targets, probe_method, probe_tcp_port,
	probe_interval_seconds, probe_count, probe_timeout_ms, loss_threshold_pct, latency_threshold_ms,
	fail_strikes, recover_strikes, status, description`

// scanWanUplink reads one wan_uplinks row (column order matching
// wanUplinkColumns) into a model.WanUplink, splitting the comma-separated
// probe_targets column back into a slice.
func scanWanUplink(scan func(dest ...any) error) (model.WanUplink, error) {
	var u model.WanUplink
	var probeTargets string
	var statusInt int
	err := scan(
		&u.ID, &u.Name, &u.Interface, &u.Priority, &probeTargets, &u.ProbeMethod, &u.ProbeTCPPort,
		&u.ProbeIntervalSeconds, &u.ProbeCount, &u.ProbeTimeoutMs, &u.LossThresholdPct, &u.LatencyThresholdMs,
		&u.FailStrikes, &u.RecoverStrikes, &statusInt, &u.Description,
	)
	if err != nil {
		return model.WanUplink{}, err
	}
	u.Status = statusInt == 1
	if probeTargets != "" {
		u.ProbeTargets = strings.Split(probeTargets, ",")
	} else {
		u.ProbeTargets = []string{}
	}
	return u, nil
}

// GetWanUplinks returns every configured WAN uplink, ordered by priority
// ascending (lower priority value = tried first, same convention as
// qos_rules).
func (r *Repository) GetWanUplinks() ([]model.WanUplink, error) {
	rows, err := r.db.Query(`SELECT ` + wanUplinkColumns + ` FROM wan_uplinks ORDER BY priority ASC, name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("query wan_uplinks: %w", err)
	}
	defer rows.Close()

	uplinks := []model.WanUplink{}
	for rows.Next() {
		u, err := scanWanUplink(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan wan_uplink: %w", err)
		}
		uplinks = append(uplinks, u)
	}
	return uplinks, rows.Err()
}

// GetWanUplinkByID returns a single uplink, or nil (no error) if id doesn't
// match any row — mirrors GetAddressByID's not-found convention.
func (r *Repository) GetWanUplinkByID(id string) (*model.WanUplink, error) {
	row := r.db.QueryRow(`SELECT `+wanUplinkColumns+` FROM wan_uplinks WHERE id = ?`, id)
	u, err := scanWanUplink(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get wan_uplink %q: %w", id, err)
	}
	return &u, nil
}

// wanUplinkPriorityInUse reports whether another wan_uplinks row (any row
// other than excludeID) already uses priority — Decision F (docs/ref/todo/
// multi-wan-failover-plan.md, T-24) requires Priority to be unique across
// every uplink so the failover controller's active/standby metric bands stay
// collision-free. model.ValidateWanUplink can only check the 1..16 range in
// isolation (it is a DB-free pure function, see its doc comment), so the
// cross-row uniqueness check has to live here, at the repo layer, where
// every other row is visible. excludeID is empty on create (nothing to
// exclude) and the uplink's own id on update (so keeping its own unchanged
// priority is never rejected as "in use by itself"). The UNIQUE index on
// wan_uplinks(priority) (db/connection.go) is the actual, unconditional
// backstop — this check exists purely to give the caller a clear,
// field-specific error message instead of a raw sqlite constraint failure.
func (r *Repository) wanUplinkPriorityInUse(priority int, excludeID string) (bool, error) {
	row := r.db.QueryRow(`SELECT COUNT(1) FROM wan_uplinks WHERE priority = ? AND id != ?`, priority, excludeID)
	var n int
	if err := row.Scan(&n); err != nil {
		return false, fmt.Errorf("check wan_uplink priority uniqueness: %w", err)
	}
	return n > 0, nil
}

// wanUplinkCount returns the total number of wan_uplinks rows — used by
// CreateWanUplink to enforce model.MaxWanUplinks (QA round-1 fix, Finding 3:
// see that constant's doc comment for why 16 is a hard ceiling, not just a
// per-row range check).
func (r *Repository) wanUplinkCount() (int, error) {
	row := r.db.QueryRow(`SELECT COUNT(1) FROM wan_uplinks`)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("count wan_uplinks: %w", err)
	}
	return n, nil
}

// CreateWanUplink validates then inserts a new uplink, returning the created
// record. interface uniqueness is enforced by the UNIQUE constraint on
// wan_uplinks.interface — a duplicate is returned as a plain wrapped sqlite
// error, which the api layer maps to 400 like any other validation failure.
// Priority uniqueness (Decision F, T-24) is checked explicitly below for a
// clearer error message, backstopped by the UNIQUE index on
// wan_uplinks(priority). The total-row-count cap (model.MaxWanUplinks) is
// checked here too, so "there are never more than 16 wan_uplinks rows" is an
// actual invariant the duplicate-priority migration
// (db/connection.go ensureUniqueWanUplinkPriorityIndex) can rely on, rather
// than an unenforced assumption.
func (r *Repository) CreateWanUplink(input model.WanUplinkInput) (*model.WanUplink, error) {
	if err := model.ValidateWanUplink(input); err != nil {
		return nil, err
	}
	if count, err := r.wanUplinkCount(); err != nil {
		return nil, err
	} else if count >= model.MaxWanUplinks {
		return nil, fmt.Errorf("cannot create a new WAN uplink: at most %d WAN uplinks are allowed (this cap keeps every possible priority's active/standby default-route metric inside Decision F's reserved bands)", model.MaxWanUplinks)
	}
	if inUse, err := r.wanUplinkPriorityInUse(input.Priority, ""); err != nil {
		return nil, err
	} else if inUse {
		return nil, fmt.Errorf("priority %d is already in use by another WAN uplink — priorities must be unique", input.Priority)
	}

	id := "wan-" + uuid.New().String()
	statusInt := 0
	if input.Status {
		statusInt = 1
	}

	_, err := r.db.Exec(`INSERT INTO wan_uplinks (
			id, name, interface, priority, probe_targets, probe_method, probe_tcp_port,
			probe_interval_seconds, probe_count, probe_timeout_ms, loss_threshold_pct, latency_threshold_ms,
			fail_strikes, recover_strikes, status, description
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, input.Name, input.Interface, input.Priority, strings.Join(input.ProbeTargets, ","), input.ProbeMethod, input.ProbeTCPPort,
		input.ProbeIntervalSeconds, input.ProbeCount, input.ProbeTimeoutMs, input.LossThresholdPct, input.LatencyThresholdMs,
		input.FailStrikes, input.RecoverStrikes, statusInt, input.Description,
	)
	if err != nil {
		return nil, fmt.Errorf("insert wan_uplink: %w", err)
	}
	return r.GetWanUplinkByID(id)
}

// UpdateWanUplink validates then updates an existing uplink, returning the
// updated record. Priority uniqueness (Decision F, T-24) excludes id itself
// so leaving the priority unchanged is never rejected as "in use by itself".
func (r *Repository) UpdateWanUplink(id string, input model.WanUplinkInput) (*model.WanUplink, error) {
	if err := model.ValidateWanUplink(input); err != nil {
		return nil, err
	}
	if inUse, err := r.wanUplinkPriorityInUse(input.Priority, id); err != nil {
		return nil, err
	} else if inUse {
		return nil, fmt.Errorf("priority %d is already in use by another WAN uplink — priorities must be unique", input.Priority)
	}

	statusInt := 0
	if input.Status {
		statusInt = 1
	}

	res, err := r.db.Exec(`UPDATE wan_uplinks SET
			name = ?, interface = ?, priority = ?, probe_targets = ?, probe_method = ?, probe_tcp_port = ?,
			probe_interval_seconds = ?, probe_count = ?, probe_timeout_ms = ?, loss_threshold_pct = ?, latency_threshold_ms = ?,
			fail_strikes = ?, recover_strikes = ?, status = ?, description = ?
		WHERE id = ?`,
		input.Name, input.Interface, input.Priority, strings.Join(input.ProbeTargets, ","), input.ProbeMethod, input.ProbeTCPPort,
		input.ProbeIntervalSeconds, input.ProbeCount, input.ProbeTimeoutMs, input.LossThresholdPct, input.LatencyThresholdMs,
		input.FailStrikes, input.RecoverStrikes, statusInt, input.Description,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("update wan_uplink %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("wan uplink %q not found", id)
	}
	return r.GetWanUplinkByID(id)
}

// DeleteWanUplink removes an uplink by ID.
func (r *Repository) DeleteWanUplink(id string) error {
	res, err := r.db.Exec(`DELETE FROM wan_uplinks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete wan_uplink %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("wan uplink %q not found", id)
	}
	return nil
}

// GetWanFailoverSettings returns the single-row global failover settings
// (id=1), mirroring GetDhcpHealthSettings's shape.
func (r *Repository) GetWanFailoverSettings() (*model.WanFailoverSettings, error) {
	row := r.db.QueryRow(`SELECT enabled, mode, manual_uplink_id, min_hold_seconds, revert_delay_seconds FROM wan_failover_settings WHERE id = 1`)
	var s model.WanFailoverSettings
	var enabledInt int
	if err := row.Scan(&enabledInt, &s.Mode, &s.ManualUplinkID, &s.MinHoldSeconds, &s.RevertDelaySeconds); err != nil {
		return nil, err
	}
	s.Enabled = enabledInt == 1
	return &s, nil
}

// UpdateWanFailoverSettings validates then persists the global failover
// settings. Validation happens here (not only at the api layer) so any
// future direct caller (e.g. backup import) gets the same fail-closed
// guarantee as WifiPreset/QoS updates.
func (r *Repository) UpdateWanFailoverSettings(s model.WanFailoverSettings) error {
	if err := model.ValidateWanFailoverSettings(s); err != nil {
		return err
	}
	enabledInt := 0
	if s.Enabled {
		enabledInt = 1
	}
	_, err := r.db.Exec(`UPDATE wan_failover_settings SET enabled = ?, mode = ?, manual_uplink_id = ?, min_hold_seconds = ?, revert_delay_seconds = ? WHERE id = 1`,
		enabledInt, s.Mode, s.ManualUplinkID, s.MinHoldSeconds, s.RevertDelaySeconds)
	return err
}
