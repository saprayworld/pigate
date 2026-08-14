package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// newPersistedEndpointsTestServices wires a PolicyCounterStore + a
// PolicyEndpointRecorder into a PolicyStatsService, mirroring cmd/pigate/
// main.go's E-08 wiring, for the persisted-source tests below (docs/ref/todo/
// persisted-rule-endpoints-plan.md E-07, issue #141 follow-up).
func newPersistedEndpointsTestServices(t *testing.T) (*db.Repository, *logs.RingBuffer, *PolicyStatsService, *PolicyCounterStore, *PolicyEndpointRecorder) {
	t.Helper()
	repo, _, _, ringBuffer, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{})
	counterStore := NewPolicyCounterStore(repo, nil, time.Hour)
	if err := counterStore.Load(); err != nil {
		t.Fatalf("PolicyCounterStore.Load: %v", err)
	}
	recorder := NewPolicyEndpointRecorder(true, 1000)
	counterStore.SetEndpointRecorder(recorder, 1000)
	statsSvc.SetEndpointStore(counterStore, recorder, true, 1000)
	return repo, ringBuffer, statsSvc, counterStore, recorder
}

// TestGetRuleEndpoints_PersistedSource_MonitoredRuleReadsFromDB covers the
// core E-D6 contract: a monitored rule with data already flushed to
// policy_rule_endpoints reads from there (source="persisted"), not the ring
// buffer, and the ring buffer's contents are ignored entirely.
func TestGetRuleEndpoints_PersistedSource_MonitoredRuleReadsFromDB(t *testing.T) {
	repo, ringBuffer, statsSvc, counterStore, _ := newPersistedEndpointsTestServices(t)
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true, Log: true, Monitored: true})
	if err := counterStore.Load(); err != nil { // pick up the counter row CreatePolicy seeded
		t.Fatalf("Load: %v", err)
	}

	// Ring buffer has unrelated stale data that must NOT leak into the
	// persisted response (E-D6: never add the two sources together).
	ringBuffer.Add(model.FirewallLog{RuleID: "rule-1", Src: "192.168.9.9", Dest: "-", Proto: "TCP", Port: "22", Time: "2026-01-01T00:00:00Z"})

	if _, err := repo.AddPolicyEndpointDeltas([]model.PersistedEndpoint{
		{RuleID: "rule-1", Direction: model.EndpointDirectionSrc, Key: "10.0.0.5", Count: 7, FirstSeenAt: "2026-01-01T00:00:00Z", LastSeenAt: "2026-01-01T00:05:00Z"},
		{RuleID: "rule-1", Direction: model.EndpointDirectionDst, Key: "8.8.8.8", Count: 4, FirstSeenAt: "2026-01-01T00:00:00Z", LastSeenAt: "2026-01-01T00:05:00Z"},
		{RuleID: "rule-1", Direction: model.EndpointDirectionSvc, Key: "UDP/53", Count: 4, FirstSeenAt: "2026-01-01T00:00:00Z", LastSeenAt: "2026-01-01T00:05:00Z"},
	}, 1000); err != nil {
		t.Fatalf("AddPolicyEndpointDeltas: %v", err)
	}

	got, err := statsSvc.GetRuleEndpoints("rule-1", 10)
	if err != nil {
		t.Fatalf("GetRuleEndpoints: %v", err)
	}
	if got.Source != "persisted" {
		t.Fatalf("expected source=persisted, got %q", got.Source)
	}
	if got.CollectingSince == "" {
		t.Fatal("expected CollectingSince to be set for a persisted response")
	}
	if got.ScannedEntries != 0 || got.BufferOldestAt != "" {
		t.Fatalf("expected ScannedEntries=0/BufferOldestAt=\"\" for persisted source, got %+v", got)
	}
	if got.MaxPerDirection != 1000 {
		t.Fatalf("expected MaxPerDirection=1000, got %d", got.MaxPerDirection)
	}
	if len(got.Sources) != 1 || got.Sources[0].IP != "10.0.0.5" || got.Sources[0].Count != 7 {
		t.Fatalf("expected the persisted source row (not the ring buffer's 192.168.9.9), got %+v", got.Sources)
	}
	if len(got.Destinations) != 1 || got.Destinations[0].IP != "8.8.8.8" || got.Destinations[0].Count != 4 {
		t.Fatalf("unexpected destinations: %+v", got.Destinations)
	}
	if len(got.Services) != 1 || got.Services[0].Proto != "UDP" || got.Services[0].Port != "53" || got.Services[0].Count != 4 {
		t.Fatalf("unexpected services: %+v", got.Services)
	}
}

// TestGetRuleEndpoints_PersistedSource_PendingFoldedIn covers the "see it
// immediately, no wait for the next flush" requirement (E-D6): data still
// sitting in the RAM recorder (not yet flushed) must be folded into the
// persisted response.
func TestGetRuleEndpoints_PersistedSource_PendingFoldedIn(t *testing.T) {
	repo, _, statsSvc, counterStore, recorder := newPersistedEndpointsTestServices(t)
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true, Log: true, Monitored: true})
	if err := counterStore.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	recorder.SetMonitoredRules(map[string]bool{"rule-1": true})

	// Flushed row for 10.0.0.5.
	if _, err := repo.AddPolicyEndpointDeltas([]model.PersistedEndpoint{
		{RuleID: "rule-1", Direction: model.EndpointDirectionSrc, Key: "10.0.0.5", Count: 2, FirstSeenAt: "2026-01-01T00:00:00Z", LastSeenAt: "2026-01-01T00:01:00Z"},
	}, 1000); err != nil {
		t.Fatalf("AddPolicyEndpointDeltas: %v", err)
	}
	// Pending (not yet flushed): more hits on the same key, plus a brand-new
	// key never seen by the DB.
	recorder.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.5", Time: "2026-01-01T00:02:00Z"})
	recorder.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.6", Time: "2026-01-01T00:02:00Z"})

	got, err := statsSvc.GetRuleEndpoints("rule-1", 10)
	if err != nil {
		t.Fatalf("GetRuleEndpoints: %v", err)
	}
	byIP := make(map[string]int)
	for _, s := range got.Sources {
		byIP[s.IP] = s.Count
	}
	if byIP["10.0.0.5"] != 3 {
		t.Fatalf("expected 10.0.0.5 to be DB(2)+pending(1)=3, got %+v", got.Sources)
	}
	if byIP["10.0.0.6"] != 1 {
		t.Fatalf("expected pending-only key 10.0.0.6 count=1, got %+v", got.Sources)
	}
}

// TestGetRuleEndpoints_UnmonitoredRuleStaysOnBuffer_EvenWithEndpointStoreWired
// is the Caution 11 regression guard: wiring SetEndpointStore must not
// change behavior for a rule that isn't monitored — must still read from the
// ring buffer.
func TestGetRuleEndpoints_UnmonitoredRuleStaysOnBuffer_EvenWithEndpointStoreWired(t *testing.T) {
	repo, ringBuffer, statsSvc, _, _ := newPersistedEndpointsTestServices(t)
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true, Log: true, Monitored: false})
	ringBuffer.Add(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.1", Dest: "-", Proto: "TCP", Port: "443", Time: "2026-01-01T00:00:00Z"})

	got, err := statsSvc.GetRuleEndpoints("rule-1", 10)
	if err != nil {
		t.Fatalf("GetRuleEndpoints: %v", err)
	}
	if got.Source != "buffer" {
		t.Fatalf("expected source=buffer for an unmonitored rule, got %q", got.Source)
	}
	if len(got.Sources) != 1 || got.Sources[0].IP != "10.0.0.1" {
		t.Fatalf("expected the ring-buffer source, got %+v", got.Sources)
	}
}

// TestGetRuleEndpoints_KillSwitchForcesBufferEvenWhenMonitored covers the
// monitored-endpoints-enabled kill switch (E-D9): even a monitored rule with
// persisted rows must fall back to the buffer path when disabled.
func TestGetRuleEndpoints_KillSwitchForcesBufferEvenWhenMonitored(t *testing.T) {
	repo, ringBuffer, statsSvc, counterStore, _ := newPersistedEndpointsTestServices(t)
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true, Log: true, Monitored: true})
	if err := counterStore.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := repo.AddPolicyEndpointDeltas([]model.PersistedEndpoint{
		{RuleID: "rule-1", Direction: model.EndpointDirectionSrc, Key: "10.0.0.5", Count: 1, FirstSeenAt: "2026-01-01T00:00:00Z", LastSeenAt: "2026-01-01T00:00:00Z"},
	}, 1000); err != nil {
		t.Fatalf("AddPolicyEndpointDeltas: %v", err)
	}
	ringBuffer.Add(model.FirewallLog{RuleID: "rule-1", Src: "192.168.1.1", Dest: "-", Proto: "TCP", Port: "443", Time: "2026-01-01T00:00:00Z"})

	// Re-wire with the kill switch off (recorder untouched).
	statsSvc.SetEndpointStore(counterStore, nil, false, 1000)

	got, err := statsSvc.GetRuleEndpoints("rule-1", 10)
	if err != nil {
		t.Fatalf("GetRuleEndpoints: %v", err)
	}
	if got.Source != "buffer" {
		t.Fatalf("expected source=buffer with the kill switch off, got %q", got.Source)
	}
	if len(got.Sources) != 1 || got.Sources[0].IP != "192.168.1.1" {
		t.Fatalf("expected the ring-buffer source when kill switch is off, got %+v", got.Sources)
	}
}

// TestGetRuleEndpoints_CappedAndEvictedSurfaced covers Capped/Evicted/
// MaxPerDirection in the response.
func TestGetRuleEndpoints_CappedAndEvictedSurfaced(t *testing.T) {
	repo, _, statsSvc, counterStore, recorder := newPersistedEndpointsTestServices(t)
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true, Log: true, Monitored: true})
	if err := counterStore.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Re-wire with a tiny cap so this test runs fast.
	counterStore.SetEndpointRecorder(recorder, 2)
	statsSvc.SetEndpointStore(counterStore, recorder, true, 2)

	// 3 distinct src keys through a cap of 2 -> 1 must be evicted.
	if _, err := repo.AddPolicyEndpointDeltas([]model.PersistedEndpoint{
		{RuleID: "rule-1", Direction: model.EndpointDirectionSrc, Key: "10.0.0.1", Count: 1, FirstSeenAt: "2026-01-01T00:00:01Z", LastSeenAt: "2026-01-01T00:00:01Z"},
		{RuleID: "rule-1", Direction: model.EndpointDirectionSrc, Key: "10.0.0.2", Count: 1, FirstSeenAt: "2026-01-01T00:00:02Z", LastSeenAt: "2026-01-01T00:00:02Z"},
		{RuleID: "rule-1", Direction: model.EndpointDirectionSrc, Key: "10.0.0.3", Count: 1, FirstSeenAt: "2026-01-01T00:00:03Z", LastSeenAt: "2026-01-01T00:00:03Z"},
	}, 2); err != nil {
		t.Fatalf("AddPolicyEndpointDeltas: %v", err)
	}
	if err := counterStore.Load(); err != nil { // refresh cache so EndpointsEvictedFor sees the eviction credit written above
		t.Fatalf("Load: %v", err)
	}

	got, err := statsSvc.GetRuleEndpoints("rule-1", 10)
	if err != nil {
		t.Fatalf("GetRuleEndpoints: %v", err)
	}
	if got.MaxPerDirection != 2 {
		t.Fatalf("expected MaxPerDirection=2, got %d", got.MaxPerDirection)
	}
	if !got.Capped {
		t.Fatalf("expected Capped=true (src direction at cap), got %+v", got)
	}
	if got.Evicted != 1 {
		t.Fatalf("expected Evicted=1, got %d", got.Evicted)
	}
}
