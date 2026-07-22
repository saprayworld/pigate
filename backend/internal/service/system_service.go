package service

import (
	"errors"
	"fmt"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// ErrSystemServiceNotFound / ErrSystemServiceRestartForbidden let the API
// layer translate SystemServiceService.RestartByID failures into the right
// HTTP status (400 / 403) without string-matching error text, following the
// same sentinel-error convention as service.ErrUserNotFound.
var (
	ErrSystemServiceNotFound         = errors.New("ไม่พบบริการที่ระบุ")
	ErrSystemServiceRestartForbidden = errors.New("ไม่อนุญาตให้รีสตาร์ทบริการนี้")
)

// systemServiceEntry is one row of the catalog: a client-facing slug mapped
// to the real systemd unit name it resolves to. The slug — never the raw unit
// name — is what is allowed to cross the API boundary from the client, so a
// malicious/typo'd {id} can only ever select an entry already in this
// whitelist (see Caution 1 in docs/ref/todo/os-service-status-control-plan.md:
// unit-name injection via a client-supplied RestartUnit argument).
type systemServiceEntry struct {
	Slug           string
	UnitName       string
	DisplayName    string
	RestartAllowed bool
}

// staticServiceCatalog lists the fixed, always-present singleton units PiGate
// depends on or controls (D4 locks in ssh.service). pigate.service itself is
// listed as status-only (D3): RestartAllowed=false, because RestartUnit on
// the very process serving this HTTP request would kill it mid-response
// (Caution 2).
var staticServiceCatalog = []systemServiceEntry{
	{Slug: "dnsmasq", UnitName: "dnsmasq.service", DisplayName: "DHCP/DNS Forwarder (dnsmasq)", RestartAllowed: true},
	{Slug: "resolved", UnitName: "systemd-resolved.service", DisplayName: "DNS Resolver (systemd-resolved)", RestartAllowed: true},
	{Slug: "timesyncd", UnitName: "systemd-timesyncd.service", DisplayName: "Time Sync (systemd-timesyncd)", RestartAllowed: true},
	{Slug: "ssh", UnitName: "ssh.service", DisplayName: "SSH Daemon (ssh)", RestartAllowed: true},
	{Slug: "pigate", UnitName: "pigate.service", DisplayName: "PiGate Controller (pigate)", RestartAllowed: false},
}

// SystemServiceService drives the Settings "Network Services Status" panel:
// it owns the slug->unit whitelist catalog (static singletons above, plus
// per-interface dynamic entries built fresh from the DB on every call) and
// reads/controls those units through kernel.SystemServiceManager.
type SystemServiceService struct {
	mgr  kernel.SystemServiceManager
	repo *db.Repository
}

func NewSystemServiceService(mgr kernel.SystemServiceManager, repo *db.Repository) *SystemServiceService {
	return &SystemServiceService{mgr: mgr, repo: repo}
}

// catalog builds the full slug->unit whitelist for one call: the fixed
// static entries plus one row per WLAN interface (wpa_supplicant@<if>.service)
// and one row per DHCP-client interface (dhcpcd@<if>.service), sourced live
// from the DB rather than persisted separately (no new table — see plan doc
// section 0). These are the exact same units InterfaceService/DhcpcdService
// already start/stop as part of normal interface management (Caution 5) —
// this only adds a read/restart affordance in this settings panel, it is not
// a new control path or a duplicate reconcile loop.
func (s *SystemServiceService) catalog() ([]systemServiceEntry, error) {
	entries := make([]systemServiceEntry, len(staticServiceCatalog))
	copy(entries, staticServiceCatalog)

	ifaces, err := s.repo.GetInterfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if iface.Type == "wireless" {
			entries = append(entries, systemServiceEntry{
				Slug:           "wpa-" + iface.Name,
				UnitName:       "wpa_supplicant@" + iface.Name + ".service",
				DisplayName:    "Wi-Fi Supplicant (" + iface.Name + ")",
				RestartAllowed: true,
			})
		}
		if iface.AddressingMode == "dhcp" {
			entries = append(entries, systemServiceEntry{
				Slug:           "dhcpcd-" + iface.Name,
				UnitName:       "dhcpcd@" + iface.Name + ".service",
				DisplayName:    "DHCP Client (" + iface.Name + ")",
				RestartAllowed: true,
			})
		}
	}
	return entries, nil
}

// mapActiveState turns a raw systemd runtime state into the panel's display
// status. LoadState != loaded (no unit file at all, e.g. ssh not installed)
// takes priority over ActiveState — an unloaded unit's ActiveState is
// meaningless (Caution 3).
func mapActiveState(state model.ServiceRuntimeState) string {
	if !state.Loaded {
		return "unavailable"
	}
	switch state.ActiveState {
	case "active":
		return "running"
	case "failed":
		return "failed"
	default:
		return "stopped"
	}
}

// List returns the live status of every catalog entry. A D-Bus/kernel error
// reading any one unit's status is propagated as-is (the API layer maps it to
// 500) — it is never silently downgraded to a fake "stopped", which would
// mislead an admin diagnosing an outage.
func (s *SystemServiceService) List() ([]model.NetworkServiceStatus, error) {
	entries, err := s.catalog()
	if err != nil {
		return nil, err
	}

	result := make([]model.NetworkServiceStatus, 0, len(entries))
	for _, e := range entries {
		state, err := s.mgr.GetStatus(e.UnitName)
		if err != nil {
			return nil, fmt.Errorf("failed to read status of %s: %w", e.UnitName, err)
		}
		result = append(result, model.NetworkServiceStatus{
			ID:             e.Slug,
			Name:           e.DisplayName,
			ServiceName:    e.UnitName,
			Status:         mapActiveState(state),
			RestartAllowed: e.RestartAllowed,
		})
	}
	return result, nil
}

// RestartByID resolves a client-supplied slug through the catalog whitelist
// and restarts the corresponding unit. It never accepts (or forwards) a raw
// unit name from the caller — see Caution 1. Guarded here at the service
// layer, not just hidden in the UI, so RestartAllowed=false (e.g. pigate) is
// enforced regardless of what the client sends (Caution 2).
func (s *SystemServiceService) RestartByID(slug string) error {
	entries, err := s.catalog()
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.Slug != slug {
			continue
		}
		if !e.RestartAllowed {
			return ErrSystemServiceRestartForbidden
		}
		return s.mgr.Restart(e.UnitName)
	}
	return ErrSystemServiceNotFound
}
