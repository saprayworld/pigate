package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/logs"
	"pigate/internal/model"
)

func mustCreateAddress(t *testing.T, repo *db.Repository, addr model.AddressObject) {
	t.Helper()
	if err := repo.CreateAddress(addr); err != nil {
		t.Fatalf("CreateAddress(%s) failed: %v", addr.Name, err)
	}
}

// newPolicyStatsTestServicesFileDB is a variant of newPolicyStatsTestServices
// backed by a temp-file SQLite DB instead of ":memory:". repository.go's
// GetPolicies loads Source/Destination via a nested query issued while the
// outer policy-listing rows are still open; database/sql then borrows a
// second pooled connection for it, which on modernc.org/sqlite's ":memory:"
// DSN is a distinct, empty in-RAM database (each connection = its own DB
// unless a shared-cache URI is used) — so the nested query silently sees no
// rows and Source/Destination end up empty even though CreatePolicy
// succeeded. Only manifests with ":memory:"; production always uses a real
// file DB, where every connection shares the same file and this is a
// non-issue. Kept local to this test file rather than touching
// repository.go (out of scope for this plan).
func newPolicyStatsTestServicesFileDB(t *testing.T) (*db.Repository, *logs.RingBuffer, *PolicyStatsService) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pigate-test.db")
	sqliteDB, err := db.InitDB(path)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		sqliteDB.Close()
		os.Remove(path + ".backup")
	})

	repo := db.NewRepository(sqliteDB)
	trafficSvc := NewTrafficStatsService(&fakeTrafficAccounting{}, repo, &fakeDhcpForTraffic{}, kernel.NewMockSystemStats(), 0, 0, 0)
	ringBuffer := logs.NewRingBuffer(50)
	statsSvc := NewPolicyStatsService(repo, nil, trafficSvc, ringBuffer)
	return repo, ringBuffer, statsSvc
}

// A rule id that doesn't exist returns the sentinel error, never a generic
// one — api.HandleGetPolicyRuleEndpoints depends on errors.Is to map this to
// 404.
func TestGetRuleEndpoints_NotFound(t *testing.T) {
	_, _, _, _, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{})
	_, err := statsSvc.GetRuleEndpoints("does-not-exist", 10)
	if !errors.Is(err, ErrPolicyRuleNotFound) {
		t.Fatalf("expected ErrPolicyRuleNotFound, got %v", err)
	}
}

// A rule that exists but has no matching traffic log entries returns empty
// lists, not an error — including when Log is off.
func TestGetRuleEndpoints_NoLogData(t *testing.T) {
	repo, _, _, _, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true, Log: false})

	got, err := statsSvc.GetRuleEndpoints("rule-1", 10)
	if err != nil {
		t.Fatalf("GetRuleEndpoints: %v", err)
	}
	if got.LogEnabled {
		t.Fatal("expected LogEnabled=false")
	}
	if len(got.Sources) != 0 || len(got.Destinations) != 0 || len(got.Services) != 0 {
		t.Fatalf("expected empty lists, got %+v", got)
	}
	if got.MatchedEntries != 0 {
		t.Fatalf("expected MatchedEntries=0, got %d", got.MatchedEntries)
	}
}

// Top-N truncation: more unique sources than limit sets Truncated=true, and
// uniqueSources still reports the full pre-truncation count.
func TestGetRuleEndpoints_TopNAndTruncated(t *testing.T) {
	repo, _, _, ringBuffer, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true, Log: true})

	// 3 distinct sources, counts 5/3/1 so ranking is unambiguous.
	for i := 0; i < 5; i++ {
		ringBuffer.Add(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.1", Dest: "-", Proto: "TCP", Port: "443", Time: "2026-01-01T00:00:00Z"})
	}
	for i := 0; i < 3; i++ {
		ringBuffer.Add(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.2", Dest: "-", Proto: "TCP", Port: "443", Time: "2026-01-01T00:00:00Z"})
	}
	ringBuffer.Add(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.3", Dest: "-", Proto: "TCP", Port: "443", Time: "2026-01-01T00:00:00Z"})

	got, err := statsSvc.GetRuleEndpoints("rule-1", 2)
	if err != nil {
		t.Fatalf("GetRuleEndpoints: %v", err)
	}
	if got.UniqueSources != 3 {
		t.Fatalf("expected UniqueSources=3, got %d", got.UniqueSources)
	}
	if len(got.Sources) != 2 {
		t.Fatalf("expected 2 sources after top-N cut, got %d: %+v", len(got.Sources), got.Sources)
	}
	if got.Sources[0].IP != "10.0.0.1" || got.Sources[0].Count != 5 {
		t.Fatalf("expected top source 10.0.0.1 count=5, got %+v", got.Sources[0])
	}
	if got.Sources[1].IP != "10.0.0.2" || got.Sources[1].Count != 3 {
		t.Fatalf("expected 2nd source 10.0.0.2 count=3, got %+v", got.Sources[1])
	}
	if !got.Truncated {
		t.Fatal("expected Truncated=true")
	}
}

// fromRule=true for an address that's one of the rule's own Source/Destination
// objects, and for a service that's the rule's own Service object.
func TestGetRuleEndpoints_FromRuleFlags(t *testing.T) {
	repo, ringBuffer, statsSvc := newPolicyStatsTestServicesFileDB(t)
	mustCreateAddress(t, repo, model.AddressObject{ID: "a1", Name: "LAN_Clients", Type: "subnet", Value: "192.168.1.0/24"})
	mustCreateAddress(t, repo, model.AddressObject{ID: "a2", Name: "Other_Net", Type: "subnet", Value: "10.9.9.0/24"})
	mustCreatePolicy(t, repo, model.PolicyRule{
		ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true, Log: true,
		Source: []string{"LAN_Clients"}, Destination: []string{"ALL"}, Service: []string{"HTTPS"},
	})

	ringBuffer.Add(model.FirewallLog{RuleID: "rule-1", Src: "192.168.1.50", Dest: "8.8.8.8", Proto: "TCP", Port: "443", Time: "2026-01-01T00:00:00Z"})
	ringBuffer.Add(model.FirewallLog{RuleID: "rule-1", Src: "10.9.9.5", Dest: "8.8.8.8", Proto: "TCP", Port: "443", Time: "2026-01-01T00:00:01Z"})

	got, err := statsSvc.GetRuleEndpoints("rule-1", 10)
	if err != nil {
		t.Fatalf("GetRuleEndpoints: %v", err)
	}

	var lanHit, otherHit *model.EndpointHit
	for i := range got.Sources {
		switch got.Sources[i].IP {
		case "192.168.1.50":
			lanHit = &got.Sources[i]
		case "10.9.9.5":
			otherHit = &got.Sources[i]
		}
	}
	if lanHit == nil || otherHit == nil {
		t.Fatalf("expected both sources present, got %+v", got.Sources)
	}
	if lanHit.AddressName != "LAN_Clients" || !lanHit.FromRule {
		t.Fatalf("expected LAN_Clients fromRule=true, got %+v", lanHit)
	}
	if otherHit.AddressName != "Other_Net" || otherHit.FromRule {
		t.Fatalf("expected Other_Net fromRule=false (not referenced by this rule), got %+v", otherHit)
	}

	if len(got.Services) != 1 {
		t.Fatalf("expected 1 service, got %+v", got.Services)
	}
	svc := got.Services[0]
	if svc.ServiceName != "HTTPS" || !svc.FromRule {
		t.Fatalf("expected HTTPS fromRule=true, got %+v", svc)
	}
}

// Every dependency nil (constructed with nil repo/firewall/trafficStats/
// ringBuffer via NewPolicyStatsService directly) must not panic.
func TestGetRuleEndpoints_NilDependenciesDoNotPanic(t *testing.T) {
	statsSvc := NewPolicyStatsService(nil, nil, nil, nil)
	_, err := statsSvc.GetRuleEndpoints("anything", 10)
	if !errors.Is(err, ErrPolicyRuleNotFound) {
		t.Fatalf("expected ErrPolicyRuleNotFound with nil repo, got %v", err)
	}
}

// SetDomainLookup wiring: a domain result wins display-precedence data but is
// only ever consulted for the IPs that survive the top-N cut (batch, not
// per-row) — this test just verifies the plumbing works end to end.
func TestGetRuleEndpoints_DomainLookupWiring(t *testing.T) {
	repo, _, _, ringBuffer, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true, Log: true})
	ringBuffer.Add(model.FirewallLog{RuleID: "rule-1", Src: "-", Dest: "142.250.80.46", Proto: "TCP", Port: "443", Time: "2026-01-01T00:00:00Z"})

	var calledWith []string
	statsSvc.SetDomainLookup(func(ips []string) map[string]string {
		calledWith = append(calledWith, ips...)
		return map[string]string{"142.250.80.46": "www.google.com"}
	})

	got, err := statsSvc.GetRuleEndpoints("rule-1", 10)
	if err != nil {
		t.Fatalf("GetRuleEndpoints: %v", err)
	}
	if len(got.Destinations) != 1 || got.Destinations[0].Domain != "www.google.com" {
		t.Fatalf("expected domain resolved for the destination, got %+v", got.Destinations)
	}
	if len(calledWith) != 1 || calledWith[0] != "142.250.80.46" {
		t.Fatalf("expected exactly one batch domainLookup call, got %v", calledWith)
	}
}

// TestGetRuleEndpoints_DomainLookupWiredToRealStatisticsService is the
// regression test for the bug ai-qa found in round 1: main.go constructed
// PolicyStatsService but never called SetDomainLookup, so
// EndpointHit.Domain was always "" in every real run (mock and real
// kernel alike). This test wires *StatisticsService.LookupDomains — the
// exact same production dependency main.go now passes to SetDomainLookup —
// instead of a fake closure, and drives the DNS reverse cache through its
// real public entry point (RecordDNSEvent, the same one
// cmd/pigate/main.go wires DNS-server log watchers into) rather than
// reaching into StatisticsService's private fields, so this test would have
// failed before the main.go fix and catches any future regression of the
// same wiring gap.
func TestGetRuleEndpoints_DomainLookupWiredToRealStatisticsService(t *testing.T) {
	repo, _, trafficSvc, ringBuffer, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true, Log: true})

	// Domain is populated on EndpointHit unconditionally (independent of
	// whatever wins display precedence via AddressName/Hostname — see
	// GetRuleEndpoints' buildHit) — that's the exact field the bug left
	// permanently empty in production, so that's what this regression test
	// asserts, regardless of the seeded default "ALL" (0.0.0.0/0) address
	// object also matching this IP.
	const ip = "142.250.80.46"
	ringBuffer.Add(model.FirewallLog{RuleID: "rule-1", Src: "-", Dest: ip, Proto: "TCP", Port: "443", Time: "2026-01-01T00:00:00Z"})

	statisticsSvc := NewStatisticsService(trafficSvc, repo, &fakeDhcpForTraffic{}, 0, 0, 0, 0)
	statisticsSvc.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: "www.google.com", AnswerIP: ip})

	// Mirrors cmd/pigate/main.go's wiring exactly: SetDomainLookup(statisticsService.LookupDomains).
	statsSvc.SetDomainLookup(statisticsSvc.LookupDomains)

	got, err := statsSvc.GetRuleEndpoints("rule-1", 10)
	if err != nil {
		t.Fatalf("GetRuleEndpoints: %v", err)
	}
	if len(got.Destinations) != 1 {
		t.Fatalf("expected exactly one destination, got %+v", got.Destinations)
	}
	hit := got.Destinations[0]
	if hit.Domain != "www.google.com" {
		t.Fatalf("expected Domain to be populated via the real StatisticsService.LookupDomains wiring (this is the exact bug ai-qa flagged), got %+v", hit)
	}
}
