package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"pigate/internal/model"
)

// =========================================================================
// Multi-WAN Failover Handlers (docs/ref/todo/multi-wan-failover-plan.md)
//
// Uplink CRUD + read-only status/metrics (Task 9) are authRoute (same
// sensitivity as Static Routes/QoS). The kill switch (PUT /api/wan/failover)
// and manual override (POST /api/wan/failover/override) are Task 16,
// Phase 2, and are superAdminRoute (D-8): both can force LAN traffic onto a
// different physical uplink, which is exactly the class of "can take the
// whole site offline" action Static Routes' enableEditSystemRoute knob and
// the reboot/shutdown endpoints already reserve for super_admin only.
// GET /api/wan/failover (the read-only settings view) stays authRoute, same
// as everything else in this file.
// =========================================================================

// HandleGetWanUplinks returns every configured WAN uplink.
func (s *Server) HandleGetWanUplinks(w http.ResponseWriter, r *http.Request) {
	uplinks, err := s.repo.GetWanUplinks()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to retrieve WAN uplinks")
		return
	}
	s.writeJSON(w, http.StatusOK, uplinks)
}

// HandleCreateWanUplink validates then creates a new WAN uplink.
// model.ValidateWanUplink is called explicitly (not just relied on inside
// the repository) so a validation failure is always distinguishable from a
// database-layer failure, and always maps to 400.
func (s *Server) HandleCreateWanUplink(w http.ResponseWriter, r *http.Request) {
	var input model.WanUplinkInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := model.ValidateWanUplink(input); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	uplink, err := s.repo.CreateWanUplink(input)
	if err != nil {
		// Anything past our own explicit validation above is still a
		// request-data problem in practice (e.g. the UNIQUE(interface)
		// constraint rejecting a second uplink on the same interface), so it
		// is reported as 400 rather than 500.
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryNetwork, "wan.uplink_created", model.EventSeverityInfo,
		uplink.Name, "WAN uplink \""+uplink.Name+"\" created on "+uplink.Interface)
	s.writeJSON(w, http.StatusCreated, uplink)
}

// HandleUpdateWanUplink validates then updates an existing WAN uplink.
func (s *Server) HandleUpdateWanUplink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.repo.GetWanUplinkByID(id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to look up WAN uplink")
		return
	}
	if existing == nil {
		s.writeError(w, http.StatusNotFound, "WAN uplink not found")
		return
	}

	var input model.WanUplinkInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := model.ValidateWanUplink(input); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated, err := s.repo.UpdateWanUplink(id, input)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryNetwork, "wan.uplink_updated", model.EventSeverityInfo,
		updated.Name, "WAN uplink \""+updated.Name+"\" updated")
	s.writeJSON(w, http.StatusOK, updated)
}

// HandleDeleteWanUplink removes a WAN uplink.
func (s *Server) HandleDeleteWanUplink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.repo.GetWanUplinkByID(id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to look up WAN uplink")
		return
	}
	if existing == nil {
		s.writeError(w, http.StatusNotFound, "WAN uplink not found")
		return
	}

	if err := s.repo.DeleteWanUplink(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to delete WAN uplink")
		return
	}
	s.logEvent(r, model.EventCategoryNetwork, "wan.uplink_deleted", model.EventSeverityWarning,
		existing.Name, "WAN uplink \""+existing.Name+"\" deleted")
	s.writeJSON(w, http.StatusOK, map[string]string{"message": "WAN uplink deleted"})
}

// HandleGetWanStatus returns the live status of every configured uplink,
// merging repo-configured uplinks (always present) with the monitor's
// RAM-only health state (present only once an uplink has been probed at
// least once). An uplink with no matching state yet is reported as
// state=unknown rather than being omitted — a caller must never need to
// cross-reference GET /api/wan/uplinks separately just to know an uplink
// exists (plan Task 9 acceptance).
func (s *Server) HandleGetWanStatus(w http.ResponseWriter, r *http.Request) {
	uplinks, err := s.repo.GetWanUplinks()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to retrieve WAN uplinks")
		return
	}

	statesByID := make(map[string]model.WanUplinkState, len(uplinks))
	if s.wanMonitor != nil {
		for _, st := range s.wanMonitor.GetStates() {
			statesByID[st.UplinkID] = st
		}
	}

	resp := model.WanStatusResponse{}
	// Phase 2 fields (Task 16): populated from the failover controller when
	// it has been wired in (SetWanFailover) — if it is nil (Phase-1-only
	// callers/tests, or the controller simply hasn't been wired yet), every
	// field stays at its zero value exactly like before Task 16 existed, so
	// this never breaks a caller that only knows about Phase 1. Computed
	// BEFORE building entries below so each entry's own Active field (was
	// always false in Phase 1 — WanMonitor never sets it) can be derived
	// from resp.ActiveUplinkID.
	if s.wanFailover != nil {
		status := s.wanFailover.Status()
		resp.ActiveUplinkID = status.ActiveUplinkID
		resp.LastSwitchAt = status.LastSwitchAt
		resp.LastSwitchReason = status.LastSwitchReason
		for _, iface := range status.Bypassed {
			if iface == "" {
				continue
			}
			// BypassedByStaticRoute is a single flag on the response, not a
			// per-interface list (see WanStatusResponse's doc comment) — true
			// as soon as ANY interface is bypassed. Per-interface detail is
			// available via GET /api/wan/failover's Bypassed list instead.
			resp.BypassedByStaticRoute = true
			break
		}
		// EnforceFailed mirrors BypassedByStaticRoute's shape (T-23): a single
		// flag, true as soon as ANY interface currently has a failed
		// enforcement episode.
		if len(status.EnforceFailedInterfaces) > 0 {
			resp.EnforceFailed = true
		}
	}

	entries := make([]model.WanStatusEntry, 0, len(uplinks))
	for _, u := range uplinks {
		st, ok := statesByID[u.ID]
		if !ok {
			st = model.WanUplinkState{UplinkID: u.ID, Interface: u.Interface, State: model.WanStateUnknown}
		}
		st.Active = resp.ActiveUplinkID != "" && u.ID == resp.ActiveUplinkID
		entries = append(entries, model.WanStatusEntry{WanUplinkState: st, Name: u.Name, Priority: u.Priority})
	}
	resp.Uplinks = entries

	s.writeJSON(w, http.StatusOK, resp)
}

// WanFailoverSettingsResponse is GET /api/wan/failover's response: the raw
// model.WanFailoverSettings DB row plus (QA round-1 fix, Finding 2) the same
// two per-interface diagnostic fields service.WanFailoverController.Status()
// already exposes as model.WanFailoverStatus.Bypassed/
// EnforceFailedInterfaces. Before this fix those two aggregate-only booleans
// (bypassedByStaticRoute/enforceFailed on WanStatusResponse, GET
// /api/wan/status) were the only thing reachable from any endpoint — an
// operator could see "something is stuck" but never which interface, even
// though this endpoint's own doc comments (and the two design docs) claimed
// the per-interface list was available here. Mirrors WanStatusEntry's
// embed-then-extend shape.
type WanFailoverSettingsResponse struct {
	model.WanFailoverSettings
	// Bypassed mirrors model.WanFailoverStatus.Bypassed: interface names that
	// have an active WAN failover metric override currently overridden by an
	// even-higher-precedence active DB static 0.0.0.0/0 route. Empty
	// (omitted) whenever the failover controller has never been wired/
	// enabled or nothing is currently bypassed.
	Bypassed []string `json:"bypassed,omitempty"`
	// EnforceFailedInterfaces mirrors model.WanFailoverStatus.
	// EnforceFailedInterfaces: interface names whose most recent WAN
	// failover metric override/restore enforcement failed at the kernel
	// level (see docs/ref/wan-failover-findings.md, T-23).
	EnforceFailedInterfaces []string `json:"enforceFailedInterfaces,omitempty"`
}

// HandleGetWanFailoverSettings returns the global Phase 2 failover
// kill-switch/mode/dampening configuration, plus (Finding 2 fix) the
// controller's current per-interface bypassed/enforce-failed diagnostics —
// the same data GET /api/wan/status already folds into its two aggregate
// booleans, now also reachable per-interface from this endpoint. authRoute
// (read-only, same sensitivity as the rest of this file) — the mutating
// counterpart below is superAdminRoute.
func (s *Server) HandleGetWanFailoverSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.repo.GetWanFailoverSettings()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to retrieve WAN failover settings")
		return
	}
	resp := WanFailoverSettingsResponse{WanFailoverSettings: *settings}
	if s.wanFailover != nil {
		status := s.wanFailover.Status()
		resp.Bypassed = status.Bypassed
		resp.EnforceFailedInterfaces = status.EnforceFailedInterfaces
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// HandleUpdateWanFailoverSettings updates the kill switch/mode/dampening
// settings. superAdminRoute (D-8) — this can move LAN traffic onto a
// different physical uplink. In addition to model.ValidateWanFailoverSettings
// (structural validation), Mode=="manual" requires ManualUplinkID to refer to
// an uplink that actually exists in the DB (a DB-aware check
// ValidateWanFailoverSettings deliberately cannot do itself — see its doc
// comment) — a typo'd/stale ID would otherwise silently mean "no uplink is
// ever forced active".
func (s *Server) HandleUpdateWanFailoverSettings(w http.ResponseWriter, r *http.Request) {
	var settings model.WanFailoverSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := model.ValidateWanFailoverSettings(settings); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if settings.Mode == model.WanFailoverModeManual {
		uplink, err := s.repo.GetWanUplinkByID(settings.ManualUplinkID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to look up manualUplinkId")
			return
		}
		if uplink == nil {
			s.writeError(w, http.StatusBadRequest, "manualUplinkId does not refer to a configured WAN uplink")
			return
		}
	}

	if err := s.repo.UpdateWanFailoverSettings(settings); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryNetwork, "wan.failover_settings_updated", model.EventSeverityWarning,
		"", fmt.Sprintf("WAN failover settings updated: enabled=%v mode=%q minHoldSeconds=%d revertDelaySeconds=%d", settings.Enabled, settings.Mode, settings.MinHoldSeconds, settings.RevertDelaySeconds))

	if s.wanFailover != nil {
		s.wanFailover.Kick()
	}
	s.writeJSON(w, http.StatusOK, settings)
}

// wanFailoverOverrideRequest is POST /api/wan/failover/override's body.
type wanFailoverOverrideRequest struct {
	UplinkID string `json:"uplinkId"`
}

// HandleSetWanFailoverManualOverride is a convenience shortcut over
// HandleUpdateWanFailoverSettings: it forces Mode="manual" with the given
// uplink, preserving the existing MinHoldSeconds/RevertDelaySeconds (only
// Enabled+Mode+ManualUplinkID change) — a UI "force this uplink active"
// button doesn't need to also resend the dampening settings it never
// touched. superAdminRoute (D-8), same rationale as the settings endpoint
// above.
func (s *Server) HandleSetWanFailoverManualOverride(w http.ResponseWriter, r *http.Request) {
	var body wanFailoverOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(body.UplinkID) == "" {
		s.writeError(w, http.StatusBadRequest, "uplinkId must not be empty")
		return
	}
	uplink, err := s.repo.GetWanUplinkByID(body.UplinkID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to look up uplinkId")
		return
	}
	if uplink == nil {
		s.writeError(w, http.StatusBadRequest, "uplinkId does not refer to a configured WAN uplink")
		return
	}

	current, err := s.repo.GetWanFailoverSettings()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to read current WAN failover settings")
		return
	}
	settings := *current
	settings.Enabled = true
	settings.Mode = model.WanFailoverModeManual
	settings.ManualUplinkID = body.UplinkID

	if err := model.ValidateWanFailoverSettings(settings); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.repo.UpdateWanFailoverSettings(settings); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryNetwork, "wan.failover_manual_override", model.EventSeverityWarning,
		uplink.Name, fmt.Sprintf("WAN failover manual override: forced uplink %q (%s) active", uplink.Name, uplink.Interface))

	if s.wanFailover != nil {
		s.wanFailover.Kick()
	}
	s.writeJSON(w, http.StatusOK, settings)
}

// HandleGetWanMetrics returns the metrics-ring time series for one uplink
// (?uplink=<id>&window=<1h|24h|...>). An unknown/never-probed uplink id
// simply reads back an all-zero series (service.WanUplinkMetricsRing.Series
// never errors on a missing key) rather than 404 — the graph just renders
// empty, which is the correct degrade for "not probed yet" too.
func (s *Server) HandleGetWanMetrics(w http.ResponseWriter, r *http.Request) {
	uplinkID := r.URL.Query().Get("uplink")
	if uplinkID == "" {
		s.writeError(w, http.StatusBadRequest, "uplink query parameter is required")
		return
	}
	window := r.URL.Query().Get("window")

	if s.wanMonitor == nil {
		s.writeJSON(w, http.StatusOK, []model.WanMetricPoint{})
		return
	}
	s.writeJSON(w, http.StatusOK, s.wanMonitor.GetMetrics(uplinkID, window))
}
