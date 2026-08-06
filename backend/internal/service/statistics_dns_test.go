package service

import (
	"fmt"
	"math"
	"testing"

	"pigate/internal/model"
)

// approxEqual is a small tolerance float comparison for percent assertions
// (percentOf uses floating point division).
func approxEqual(a, b float64) bool {
	// percentOf rounds to one decimal place, so allow slightly more slack
	// than the rounding step itself.
	return math.Abs(a-b) < 0.1
}

// seedDNSVolumeFixture builds a StatisticsService with a deterministic
// domain/client/IP/traffic scenario shared by the join tests below (plan
// docs/ref/todo/statistics-dns-page-revamp-plan.md §1.1/§1.2, T-04):
//
//	a.example.com  -> 10.0.0.1                     (not shared)
//	b.example.com  -> 10.0.0.2, 10.0.0.3            (shares 10.0.0.3 with c)
//	c.example.com  -> 10.0.0.3                      (shares 10.0.0.3 with b)
//
//	client 192.168.1.10: 4x a.example.com, 2x b.example.com
//	client 192.168.1.11: 1x b.example.com, 1x c.example.com
//
//	Flows (all bytes credited as BytesOrig, i.e. "up"):
//	  192.168.1.10 -> 10.0.0.1  500
//	  192.168.1.10 -> 10.0.0.3  300
//	  192.168.1.11 -> 10.0.0.2  200
//	  192.168.1.11 -> 10.0.0.3  100
//
// giving dstBytes{10.0.0.1:500, 10.0.0.2:200, 10.0.0.3:400},
// hostBytes{.10:800, .11:300}, Observed=1100 (both src/dst are RFC1918, so
// lanFlipFor never flips — Observed.Orig accumulates dOrig unchanged).
func seedDNSVolumeFixture(t *testing.T) *StatisticsService {
	t.Helper()

	seedFlows := []model.FlowSample{
		{Key: "f1", SrcIP: "192.168.1.10", DstIP: "10.0.0.1", Proto: 6, DstPort: 443},
		{Key: "f2", SrcIP: "192.168.1.10", DstIP: "10.0.0.3", Proto: 6, DstPort: 443},
		{Key: "f3", SrcIP: "192.168.1.11", DstIP: "10.0.0.2", Proto: 6, DstPort: 443},
		{Key: "f4", SrcIP: "192.168.1.11", DstIP: "10.0.0.3", Proto: 6, DstPort: 443},
	}
	deltaFlows := []model.FlowSample{
		{Key: "f1", SrcIP: "192.168.1.10", DstIP: "10.0.0.1", Proto: 6, DstPort: 443, BytesOrig: 500},
		{Key: "f2", SrcIP: "192.168.1.10", DstIP: "10.0.0.3", Proto: 6, DstPort: 443, BytesOrig: 300},
		{Key: "f3", SrcIP: "192.168.1.11", DstIP: "10.0.0.2", Proto: 6, DstPort: 443, BytesOrig: 200},
		{Key: "f4", SrcIP: "192.168.1.11", DstIP: "10.0.0.3", Proto: 6, DstPort: 443, BytesOrig: 100},
	}
	acct := &fakeTrafficAccounting{flowResponses: [][]model.FlowSample{seedFlows, deltaFlows}}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll() // seed
	s.traffic.poll() // delta

	s.SetDNSLoggingEnabled(true)

	for i := 0; i < 4; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "a.example.com", QueryType: "A", ClientIP: "192.168.1.10"})
	}
	for i := 0; i < 2; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "b.example.com", QueryType: "A", ClientIP: "192.168.1.10"})
	}
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "b.example.com", QueryType: "A", ClientIP: "192.168.1.11"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "c.example.com", QueryType: "A", ClientIP: "192.168.1.11"})

	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: "a.example.com", AnswerIP: "10.0.0.1"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: "b.example.com", AnswerIP: "10.0.0.2"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: "b.example.com", AnswerIP: "10.0.0.3"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: "c.example.com", AnswerIP: "10.0.0.3"})

	return s
}

// TestGetDNSQueryStatistics_VolumeJoin covers plan T-04 acceptance: the
// domain/client byte join, the two different bytesPercent denominators
// (DomainBytes for domains, ObservedBytes for clients), and the sharedIps
// flag.
func TestGetDNSQueryStatistics_VolumeJoin(t *testing.T) {
	s := seedDNSVolumeFixture(t)

	stats := s.GetDNSQueryStatistics("1h")
	if !stats.Enabled {
		t.Fatalf("expected Enabled=true")
	}
	if stats.ObservedBytes != 1100 {
		t.Fatalf("expected ObservedBytes=1100, got %d", stats.ObservedBytes)
	}
	if stats.DomainBytes != 1500 { // 500 (a) + 600 (b) + 400 (c)
		t.Fatalf("expected DomainBytes=1500 (sum across ALL domains, double-counting the shared IP), got %d", stats.DomainBytes)
	}
	if stats.TotalDomains != 3 || stats.TotalClients != 2 {
		t.Fatalf("expected TotalDomains=3/TotalClients=2, got %d/%d", stats.TotalDomains, stats.TotalClients)
	}

	byDomain := map[string]model.DNSDomainStat{}
	for _, d := range stats.TopDomains {
		byDomain[d.Domain] = d
	}
	a, b, c := byDomain["a.example.com"], byDomain["b.example.com"], byDomain["c.example.com"]

	if a.Bytes != 500 || a.IPCount != 1 || a.Clients != 1 || a.SharedIPs {
		t.Fatalf("unexpected a.example.com row: %+v", a)
	}
	if b.Bytes != 600 || b.IPCount != 2 || b.Clients != 2 || !b.SharedIPs {
		t.Fatalf("unexpected b.example.com row: %+v", b)
	}
	if c.Bytes != 400 || c.IPCount != 1 || c.Clients != 1 || !c.SharedIPs {
		t.Fatalf("unexpected c.example.com row: %+v", c)
	}
	// bytesPercent denominator for domain rows is DomainBytes (1500), NOT
	// ObservedBytes (1100).
	if !approxEqual(a.BytesPercent, 500.0/1500.0*100) {
		t.Fatalf("expected a.example.com bytesPercent ~= %v, got %v", 500.0/1500.0*100, a.BytesPercent)
	}
	if !approxEqual(b.BytesPercent, 600.0/1500.0*100) {
		t.Fatalf("expected b.example.com bytesPercent ~= %v, got %v", 600.0/1500.0*100, b.BytesPercent)
	}

	byClient := map[string]model.DNSClientStat{}
	for _, c := range stats.TopClients {
		byClient[c.IP] = c
	}
	c10, c11 := byClient["192.168.1.10"], byClient["192.168.1.11"]
	if c10.Bytes != 800 || c10.Domains != 2 {
		t.Fatalf("unexpected client 192.168.1.10 row: %+v", c10)
	}
	if c11.Bytes != 300 || c11.Domains != 2 {
		t.Fatalf("unexpected client 192.168.1.11 row: %+v", c11)
	}
	// bytesPercent denominator for client rows is ObservedBytes (1100), NOT
	// DomainBytes (1500) — the opposite denominator from the domain rows
	// above.
	if !approxEqual(c10.BytesPercent, 800.0/1100.0*100) {
		t.Fatalf("expected client 192.168.1.10 bytesPercent ~= %v, got %v", 800.0/1100.0*100, c10.BytesPercent)
	}
	if !approxEqual(c11.BytesPercent, 300.0/1100.0*100) {
		t.Fatalf("expected client 192.168.1.11 bytesPercent ~= %v, got %v", 300.0/1100.0*100, c11.BytesPercent)
	}
}

// TestGetDNSDomainClients_VolumeJoin covers plan T-04 acceptance: the
// Resolved IPs table (sorted bytes desc / ip asc, shared flag, bytesPercent
// against the domain's own TotalBytes), the conversation-level per-client
// byte join (plan §1.2), and the DestSeries invariant sum==TotalBytes.
func TestGetDNSDomainClients_VolumeJoin(t *testing.T) {
	s := seedDNSVolumeFixture(t)

	drill := s.GetDNSDomainClients("1h", "b.example.com")
	if !drill.Enabled {
		t.Fatalf("expected Enabled=true")
	}
	if drill.TotalBytes != 600 || drill.TotalBytesUp != 600 || drill.TotalBytesDown != 0 {
		t.Fatalf("unexpected domain totals: %+v", drill)
	}
	if !drill.SharedIPs {
		t.Fatalf("expected SharedIPs=true (10.0.0.3 is shared with c.example.com)")
	}
	if len(drill.IPs) != 2 {
		t.Fatalf("expected 2 resolved IPs, got %+v", drill.IPs)
	}
	// Sorted bytes desc: 10.0.0.3 (400) before 10.0.0.2 (200).
	if drill.IPs[0].IP != "10.0.0.3" || drill.IPs[0].Bytes != 400 || !drill.IPs[0].Shared {
		t.Fatalf("unexpected first IP row: %+v", drill.IPs[0])
	}
	if drill.IPs[1].IP != "10.0.0.2" || drill.IPs[1].Bytes != 200 || drill.IPs[1].Shared {
		t.Fatalf("unexpected second IP row: %+v", drill.IPs[1])
	}
	if !approxEqual(drill.IPs[0].BytesPercent, 400.0/600.0*100) {
		t.Fatalf("expected first IP bytesPercent ~= %v, got %v", 400.0/600.0*100, drill.IPs[0].BytesPercent)
	}

	byClient := map[string]model.DNSClientStat{}
	for _, c := range drill.Clients {
		byClient[c.IP] = c
	}
	c10, c11 := byClient["192.168.1.10"], byClient["192.168.1.11"]
	// client10 only talks to 10.0.0.3 (part of b's IP set) -> 300, NOT the
	// 500 bytes it sent to a.example.com's 10.0.0.1 (outside this domain's
	// IP set) — proves the conv-level join, not a blanket Hosts[ip] copy.
	if c10.Bytes != 300 {
		t.Fatalf("expected client 192.168.1.10's domain-scoped bytes=300, got %+v", c10)
	}
	if c11.Bytes != 300 { // 200 (10.0.0.2) + 100 (10.0.0.3)
		t.Fatalf("expected client 192.168.1.11's domain-scoped bytes=300, got %+v", c11)
	}
	if !approxEqual(c10.BytesPercent, 50) || !approxEqual(c11.BytesPercent, 50) {
		t.Fatalf("expected both clients at 50%% of this domain's TotalBytes, got %+v / %+v", c10, c11)
	}
	// T-06 (docs/ref/todo/statistics-dns-review-fixes-plan.md, review fix on
	// PR 127): Domains is system-wide, not scoped to b.example.com — .10
	// queried a.example.com + b.example.com (2), .11 queried b.example.com +
	// c.example.com (2). Neither is 1, proving this isn't the old
	// always-1-in-a-single-domain-context placeholder.
	if c10.Domains != 2 {
		t.Fatalf("expected client 192.168.1.10's system-wide domain count=2, got %+v", c10)
	}
	if c11.Domains != 2 {
		t.Fatalf("expected client 192.168.1.11's system-wide domain count=2, got %+v", c11)
	}

	var seriesSum uint64
	for _, p := range drill.Series {
		seriesSum += p.Bytes
	}
	if seriesSum != drill.TotalBytes {
		t.Fatalf("invariant broken: sum(series[].bytes)=%d != totalBytes=%d", seriesSum, drill.TotalBytes)
	}
}

// TestGetDNSDomainClients_NoKnownIPs covers the fast path: a domain with no
// answer ever observed must skip the Convs scan entirely and still return a
// non-nil, zero-filled Series plus an empty (non-nil) IPs slice.
func TestGetDNSDomainClients_NoKnownIPs(t *testing.T) {
	s := seedDNSVolumeFixture(t)
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "unresolved.example.com", QueryType: "A", ClientIP: "192.168.1.10"})

	drill := s.GetDNSDomainClients("1h", "unresolved.example.com")
	if !drill.Enabled {
		t.Fatalf("expected Enabled=true")
	}
	if drill.IPs == nil || len(drill.IPs) != 0 {
		t.Fatalf("expected empty (non-nil) IPs, got %+v", drill.IPs)
	}
	if drill.Series == nil {
		t.Fatalf("expected non-nil zero-filled Series even with no known IPs")
	}
	if len(drill.Series) != statsWindowBucketCount("1h") {
		t.Fatalf("expected Series length %d, got %d", statsWindowBucketCount("1h"), len(drill.Series))
	}
	for _, p := range drill.Series {
		if p.Bytes != 0 {
			t.Fatalf("expected zero-filled series, got %+v", drill.Series)
		}
	}
	if drill.TotalBytes != 0 {
		t.Fatalf("expected TotalBytes=0, got %d", drill.TotalBytes)
	}
}

// TestGetDNSClientDomains_VolumeJoin covers plan T-04 acceptance: the
// conversation-level join (mapping dst IP -> domain via a single
// domainIPs.Snapshot() call), the client's own TotalBytes as the
// bytesPercent denominator, and the HostSeries invariant.
func TestGetDNSClientDomains_VolumeJoin(t *testing.T) {
	s := seedDNSVolumeFixture(t)

	drill := s.GetDNSClientDomains("1h", "192.168.1.10")
	if !drill.Enabled {
		t.Fatalf("expected Enabled=true")
	}
	if drill.TotalBytes != 800 || drill.TotalBytesUp != 800 || drill.TotalBytesDown != 0 {
		t.Fatalf("unexpected client totals: %+v", drill)
	}

	byDomain := map[string]model.DNSDomainStat{}
	for _, d := range drill.Domains {
		byDomain[d.Domain] = d
	}
	a, b := byDomain["a.example.com"], byDomain["b.example.com"]
	if a.Bytes != 500 {
		t.Fatalf("expected a.example.com bytes=500 for this client, got %+v", a)
	}
	// 10.0.0.3 is shared between b.example.com and c.example.com, and
	// domainIPs.Snapshot() (used by the client-side join) resolves a shared
	// IP to a SINGLE domain — whichever answer was most recently observed
	// (here: c.example.com, since its answer was recorded after b's in the
	// fixture). So this client's 300 bytes to 10.0.0.3 land on c.example.com
	// in the inverted snapshot, not b.example.com — b's row here is 0 even
	// though the client did query b.example.com (plan §1.1: the domain->IP
	// mapping is "latest knowledge", not authoritative per-flow truth).
	if b.Bytes != 0 {
		t.Fatalf("expected b.example.com bytes=0 for this client (10.0.0.3 attributed to c.example.com in the snapshot), got %+v", b)
	}
	if !approxEqual(a.BytesPercent, 500.0/800.0*100) {
		t.Fatalf("expected a.example.com bytesPercent ~= %v, got %v", 500.0/800.0*100, a.BytesPercent)
	}

	// R-3 fix (docs/ref/todo/statistics-dns-review-fixes-plan.md T-04, review
	// fix on PR 127): Clients/IPCount/SharedIPs must be filled with
	// system-wide values (identical meaning/value to the overview's
	// TopDomains rows for the same domain), never left at zero.
	// a.example.com: only 192.168.1.10 queried it (1 client), 1 known IP
	// (10.0.0.1), not shared.
	if a.Clients != 1 {
		t.Fatalf("expected a.example.com clients=1 (system-wide), got %+v", a)
	}
	if a.IPCount != 1 || a.SharedIPs {
		t.Fatalf("expected a.example.com ipCount=1 sharedIps=false, got %+v", a)
	}
	// b.example.com: both 192.168.1.10 and 192.168.1.11 queried it (2
	// clients), 2 known IPs (10.0.0.2, 10.0.0.3), and 10.0.0.3 is shared with
	// c.example.com.
	if b.Clients != 2 {
		t.Fatalf("expected b.example.com clients=2 (system-wide), got %+v", b)
	}
	if b.IPCount != 2 || !b.SharedIPs {
		t.Fatalf("expected b.example.com ipCount=2 sharedIps=true, got %+v", b)
	}

	var seriesSum uint64
	for _, p := range drill.Series {
		seriesSum += p.Bytes
	}
	if seriesSum != drill.TotalBytes {
		t.Fatalf("invariant broken: sum(series[].bytes)=%d != totalBytes=%d", seriesSum, drill.TotalBytes)
	}
}

// TestGetDNSClientDomains_UnknownBucket covers plan §4 item 9 / final
// acceptance: the reserved "unknown" client bucket must never attempt a
// volume join (no IP to join against) — TotalBytes/Up/Down stay 0, every
// domain row's Bytes stays 0, and Series is empty (not the full zero-filled
// window length used for a real client).
func TestGetDNSClientDomains_UnknownBucket(t *testing.T) {
	s := seedDNSVolumeFixture(t)
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "anonymous.example.com", QueryType: "A", ClientIP: ""})

	drill := s.GetDNSClientDomains("1h", "unknown")
	if !drill.Enabled {
		t.Fatalf("expected Enabled=true")
	}
	if drill.TotalBytes != 0 || drill.TotalBytesUp != 0 || drill.TotalBytesDown != 0 {
		t.Fatalf("expected zero totals for the unknown bucket, got %+v", drill)
	}
	if len(drill.Series) != 0 {
		t.Fatalf("expected empty series for the unknown bucket, got %+v", drill.Series)
	}
	if len(drill.Domains) != 1 || drill.Domains[0].Domain != "anonymous.example.com" {
		t.Fatalf("unexpected domains for unknown bucket: %+v", drill.Domains)
	}
	if drill.Domains[0].Bytes != 0 {
		t.Fatalf("expected zero bytes for the unknown bucket's domain row, got %+v", drill.Domains[0])
	}
	// Even for the reserved "unknown" client bucket, Clients/IPCount/
	// SharedIPs must still be filled in (plan T-04 item 3 — they never need
	// conntrack, only the ring + forward index): 1 client ("unknown" itself)
	// queried anonymous.example.com, and it has no known IP (no answer was
	// ever recorded for it).
	if drill.Domains[0].Clients != 1 {
		t.Fatalf("expected anonymous.example.com clients=1 even in the unknown bucket, got %+v", drill.Domains[0])
	}
	if drill.Domains[0].IPCount != 0 || drill.Domains[0].SharedIPs {
		t.Fatalf("expected anonymous.example.com ipCount=0 sharedIps=false (no answer ever recorded), got %+v", drill.Domains[0])
	}
}

// TestDNSStatistics_DisabledReturnsEmptyNonNilSlices covers plan T-04
// mandatory rule: with query logging disabled, all 3 endpoints return
// enabled=false and every slice field is a non-nil empty slice.
func TestDNSStatistics_DisabledReturnsEmptyNonNilSlices(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})

	stats := s.GetDNSQueryStatistics("1h")
	if stats.Enabled {
		t.Fatalf("expected Enabled=false")
	}
	if stats.TopDomains == nil || len(stats.TopDomains) != 0 {
		t.Fatalf("expected non-nil empty TopDomains, got %#v", stats.TopDomains)
	}
	if stats.TopClients == nil || len(stats.TopClients) != 0 {
		t.Fatalf("expected non-nil empty TopClients, got %#v", stats.TopClients)
	}

	domainDrill := s.GetDNSDomainClients("1h", "example.com")
	if domainDrill.Enabled {
		t.Fatalf("expected Enabled=false")
	}
	if domainDrill.Clients == nil || len(domainDrill.Clients) != 0 {
		t.Fatalf("expected non-nil empty Clients, got %#v", domainDrill.Clients)
	}
	if domainDrill.IPs == nil || len(domainDrill.IPs) != 0 {
		t.Fatalf("expected non-nil empty IPs, got %#v", domainDrill.IPs)
	}
	if domainDrill.Series == nil || len(domainDrill.Series) != 0 {
		t.Fatalf("expected non-nil empty Series, got %#v", domainDrill.Series)
	}

	clientDrill := s.GetDNSClientDomains("1h", "192.168.1.10")
	if clientDrill.Enabled {
		t.Fatalf("expected Enabled=false")
	}
	if clientDrill.Domains == nil || len(clientDrill.Domains) != 0 {
		t.Fatalf("expected non-nil empty Domains, got %#v", clientDrill.Domains)
	}
	if clientDrill.Series == nil || len(clientDrill.Series) != 0 {
		t.Fatalf("expected non-nil empty Series, got %#v", clientDrill.Series)
	}
}

// TestDNSClientStat_DomainFromReverseCache covers plan
// docs/ref/todo/statistics-dns-host-domain-label-plan.md T-02/T-03: a
// DNSClientStat row's Domain is populated from the SAME dnsReverseCache
// TopHost.Domain uses (answer event with an AnswerIP pointing back at a LAN
// client IP), stays empty for clients with no matching answer (never falls
// back to the IP), and the same behaviour holds for both
// GetDNSQueryStatistics.TopClients and GetDNSDomainClients.Clients. Uses its
// own StatisticsService (not seedDNSVolumeFixture's shared instance) so this
// extra answer event can't perturb that fixture's existing assertions (plan
// T-03 instruction).
func TestDNSClientStat_DomainFromReverseCache(t *testing.T) {
	s := seedDNSVolumeFixture(t)

	// nas.home.lan resolves back to 192.168.1.10 (a LAN client IP, not one of
	// the a/b/c.example.com destination IPs) — the "local zone" scenario the
	// plan describes as the only realistic source of a non-empty Domain for
	// rows in this table.
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: "nas.home.lan", AnswerIP: "192.168.1.10"})

	stats := s.GetDNSQueryStatistics("1h")
	byIP := map[string]model.DNSClientStat{}
	for _, c := range stats.TopClients {
		byIP[c.IP] = c
	}
	if got := byIP["192.168.1.10"].Domain; got != "nas.home.lan" {
		t.Fatalf("expected TopClients[192.168.1.10].Domain=nas.home.lan, got %q", got)
	}
	if got := byIP["192.168.1.11"].Domain; got != "" {
		t.Fatalf("expected TopClients[192.168.1.11].Domain to be empty (no reverse entry), got %q", got)
	}

	domainDrill := s.GetDNSDomainClients("1h", "b.example.com")
	byIP = map[string]model.DNSClientStat{}
	for _, c := range domainDrill.Clients {
		byIP[c.IP] = c
	}
	if got := byIP["192.168.1.10"].Domain; got != "nas.home.lan" {
		t.Fatalf("expected domain drilldown Clients[192.168.1.10].Domain=nas.home.lan, got %q", got)
	}
	if got := byIP["192.168.1.11"].Domain; got != "" {
		t.Fatalf("expected domain drilldown Clients[192.168.1.11].Domain to be empty, got %q", got)
	}
}

// TestGetDNSQueryStatistics_ManyDomainsNotTruncated is a regression test for
// a bug where Truncated was set whenever len(domainTotals)/len(clientTotals)
// exceeded dnsStatsTopN (the top-50 table row cap) — conflating "more unique
// domains/clients exist than fit in the table" (normal, nothing lost) with
// "a per-bucket RAM cap actually dropped data" (real data loss). A single
// client querying 60 distinct domains (well under maxPairs=2400/
// maxClients=200) is fully and accurately tracked and must NOT trip
// Truncated, exactly like TrafficBreakdown.Truncated never fires just
// because more than statsTopN hosts exist.
func TestGetDNSQueryStatistics_ManyDomainsNotTruncated(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	const domainCount = 60 // > dnsStatsTopN (50), far under any tracking cap
	for i := 0; i < domainCount; i++ {
		domain := fmt.Sprintf("d%02d.example.com", i)
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: domain, QueryType: "A", ClientIP: "192.168.1.50"})
	}

	stats := s.GetDNSQueryStatistics("1h")
	if stats.Truncated {
		t.Fatalf("expected Truncated=false for %d fully-tracked domains under every cap, got true", domainCount)
	}
	if stats.TotalDomains != domainCount {
		t.Fatalf("expected TotalDomains=%d, got %d", domainCount, stats.TotalDomains)
	}
	if len(stats.TopDomains) != dnsStatsTopN {
		t.Fatalf("expected TopDomains capped at dnsStatsTopN=%d, got %d", dnsStatsTopN, len(stats.TopDomains))
	}

	domainDrill := s.GetDNSDomainClients("1h", "d00.example.com")
	if domainDrill.Truncated {
		t.Fatalf("expected domain drilldown Truncated=false, got true")
	}

	clientDrill := s.GetDNSClientDomains("1h", "192.168.1.50")
	if clientDrill.Truncated {
		t.Fatalf("expected client drilldown Truncated=false, got true")
	}
	if clientDrill.TotalQueries != domainCount {
		t.Fatalf("expected client drilldown TotalQueries=%d, got %d", domainCount, clientDrill.TotalQueries)
	}
}
