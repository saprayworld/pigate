package service

import (
	"context"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// Sampling cadence and RAM-only ring-buffer sizing for the Dashboard
// "Detailed" tab traffic-analytics cards (Protocol Breakdown, Top Talkers,
// Top Rules by Traffic — docs/ref/todo/dashboard-traffic-detail-plan.md).
// Mirrors SystemStatusService's trafficPollInterval/trafficBucketSpan/
// trafficBucketMax pattern (system_status.go) — a separate bucket ring
// because this data has a different shape (per-host/per-category/per-rule
// breakdown, not a single rx/tx pair) and a different source (conntrack +
// nftables counters, not netlink link stats).
const (
	flowPollInterval        = 10 * time.Second
	trafficDetailBucketSpan = 5 * time.Minute
	trafficDetailBucketMax  = 288 // 288 x 5min = 24h
	// maxTrackedFlows bounds the flow-baseline map across polls, mirroring
	// the per-dump cap already applied in the kernel layer (plan Caution 5).
	maxTrackedFlows = 50000
	// flowStaleMisses is how many consecutive polls a previously-seen flow
	// key may be absent from the dump before its baseline entry is pruned
	// (conntrack entry expired/evicted).
	flowStaleMisses = 3
	// serviceObjectsCacheTTL bounds how often the categorizer re-reads
	// Service Objects from the DB (plan §2.2) — refreshed roughly every
	// poll-minute rather than on every 10s tick.
	serviceObjectsCacheTTL = 60 * time.Second
	// topN caps how many rows each of Top Talkers / Top Rules returns.
	topN = 10

	// defaultMaxTrackedHosts/defaultMaxTrackedDests/defaultMaxTrackedConversations
	// are the fallback values used when NewTrafficStatsService is passed a
	// <=0 value (defense-in-depth for direct callers — tests, future call
	// sites — mirroring dns_query_stats.go's defaultMaxTrackedDNSPairs/
	// defaultMaxTrackedDNSClients). The authoritative range validation for
	// the config-file-sourced production values lives in config.Resolve, not
	// here (docs/ref/todo/statistics-traffic-page-plan.md §1.6/T-02). Keep
	// these in sync with config.Defaults()'s TrafficStatsMaxHosts/
	// TrafficStatsMaxDests/TrafficStatsMaxConversations.
	//
	// maxTrackedHosts/maxTrackedDests/maxTrackedConversations bound how many
	// distinct source IPs/destination IPs/conversations a single bucket
	// remembers, so a port-scan/P2P burst can't grow a bucket's map without
	// limit (plan §5 Caution 5, docs/ref/todo/statistics-page-plan.md T-02,
	// plan Caution 5/8). Existing keys already in a bucket keep accumulating
	// past the cap; only brand-new keys are dropped. These used to be package
	// consts; they are now per-service fields (s.maxTrackedHosts etc.) set
	// once at construction from the file-only traffic-stats-max-* config
	// keys, so an operator can raise them without a rebuild.
	defaultMaxTrackedHosts         = 500
	defaultMaxTrackedDests         = 500
	defaultMaxTrackedConversations = 600
	// statsTopN caps how many rows the Statistics page's top-N lists return
	// (kept distinct from topN, which the Dashboard "Detailed" tab cards use
	// and must not be touched by this plan).
	statsTopN = 10

	trafficWindow1h  = "1h"
	trafficWindow24h = "24h"
	// trafficWindow1hBuckets is how many trailing 5-minute buckets make up
	// the "1h" window (12 x 5min = 1h).
	trafficWindow1hBuckets = 12

	// sessionSampleInterval is the cadence of the Active Sessions sampler
	// (docs/ref/todo/dashboard-active-sessions-graph-plan.md) — an
	// independent ticker in run() from flowPollInterval, so total-session
	// freshness (~5s) is decoupled from the conntrack-dump poll (10s).
	sessionSampleInterval = 5 * time.Second
	// sessionRingMax bounds the RAM-only Active Sessions history ring to 30
	// minutes (5s x 360 points); never persisted to SQLite.
	sessionRingMax = 360
)

// flowSampleState is the per-flow-key baseline the poller keeps between
// ticks: the last-seen cumulative byte count (for delta computation) and a
// consecutive-miss counter (for pruning an expired conntrack entry —
// plan Caution 5).
type flowSampleState struct {
	orig, reply uint64
	misses      int
	// lanFlip is set ONCE when this state is first created (from the flow's
	// own SrcIP/DstIP, via lanFlipFor) and never touched again — it decides
	// whether Orig/Reply map to up/down or down/up for the bucket-level
	// LAN-relative Observed total (plan docs/ref/todo/
	// statistics-overview-bandwidth-chart-plan.md §2.2/T-02 item 2). A flow's
	// (srcIP, dstIP) pair never changes for the lifetime of a flowKey (the key
	// is hashed from that same pair — kernel/real_traffic_account.go:112-142),
	// so recomputing this on every poll would be wasted work, not a
	// correctness fix.
	lanFlip bool
}

// trafficDetailBucket is one 5-minute bucket of accumulated deltas across
// all three cards. Kept entirely in RAM — never written to SQLite (plan
// Caution 12, mirrors TrafficBucket in system_status.go).
type trafficDetailBucket struct {
	ts        string // RFC3339, 5-minute-aligned bucket start
	hostBytes map[string]dirBytes
	catBytes  map[string]uint64
	ruleBytes map[string]model.RuleCounter
	// observed is the bucket's LAN-relative total (Orig = up/leaving the LAN,
	// Reply = down/entering the LAN — see lanFlipFor below), unlike
	// hostBytes/dstBytes/convBytes below which stay flow-relative. This is a
	// deliberate, documented exception (plan §2.2): Observed is a whole-network
	// total with no single "row owner" to give Orig/Reply a flow-relative
	// meaning against, so it is flipped once here instead, at write time.
	observed dirBytes

	// dstBytes/convBytes back the Statistics page (Top Destinations / Top
	// Conversations, docs/ref/todo/statistics-page-plan.md T-02) — same
	// delta source (processFlows/onFlowEnd) and merged via addBucket in the
	// same call as hostBytes, so they can never drift out of sync with it
	// (plan Caution 2: never compute a second delta).
	// dstBytes is keyed by destination IP; convBytes is keyed by
	// convKey(srcIP, dstIP, proto, dstPort).
	//
	// hostBytes/dstBytes/convBytes store flow-relative direction (Orig =
	// srcIP -> dstIP, Reply = dstIP -> srcIP) — never up/down; the
	// up/down/flip mapping happens only at the presentation layer
	// (service/statistics.go buildTopHosts, per plan §2.5) so bucket data can
	// never itself go stale relative to that mapping. observed above is the
	// one deliberate exception to this rule — see its own comment.
	dstBytes  map[string]dirBytes
	convBytes map[string]dirBytes
}

// lanFlipFor reports whether Orig/Reply must be swapped to get LAN-relative
// up/down for a flow between srcIP and dstIP (plan docs/ref/todo/
// statistics-overview-bandwidth-chart-plan.md §2.2 — the single source of
// this classification, used by both processFlows and onFlowEnd so the two
// paths can never disagree):
//
//	src / dst                    | up (leaving LAN) | down (entering LAN)
//	private -> public            | Orig              | Reply
//	public -> private            | Reply             | Orig
//	private/private, public/public (unchanged)         | Orig | Reply
func lanFlipFor(srcIP, dstIP string) bool {
	return !isPrivateIP(srcIP) && isPrivateIP(dstIP)
}

// dirBytes is a flow-relative pair of byte counts: Orig is the srcIP->dstIP
// direction, Reply is dstIP->srcIP (see FlowSample.BytesOrig/BytesReply).
// Kept as a computed-total type rather than caching Total as a third field,
// so there is exactly one source of truth for the sum (plan §2.3).
type dirBytes struct{ Orig, Reply uint64 }

// Total returns the combined byte count of both directions.
func (d dirBytes) Total() uint64 { return d.Orig + d.Reply }

// categoryEntry is one Service-Object-derived Protocol Breakdown category
// lookup rule (plan §2.2). isICMP entries have no port range and only match
// non-TCP/UDP flows (ICMP/ICMPv6/other).
type categoryEntry struct {
	name      string
	protocols []uint8 // 6 (TCP), 17 (UDP), or both
	isICMP    bool
	portStart int
	portEnd   int
}

// TrafficStatsService owns the Dashboard "Detailed" tab traffic-analytics
// pipeline: it polls kernel.TrafficAccountingManager on a background
// goroutine, turns cumulative conntrack/nftables counters into deltas, buckets
// them in RAM, and composes the model.TrafficDetail DTO the API serves. All
// state here is RAM-only (plan Caution 12).
type TrafficStatsService struct {
	acct  kernel.TrafficAccountingManager
	repo  *db.Repository
	dhcp  kernel.DhcpManager
	stats kernel.SystemStatsManager

	// maxTrackedHosts/maxTrackedDests/maxTrackedConversations are the
	// effective per-bucket caps, set once at construction from the file-only
	// traffic-stats-max-* config keys (or defaultMaxTracked* when the caller
	// passes <=0 — see NewTrafficStatsService). Read-only after
	// construction, so no mutex guards these (same pattern as
	// dnsQueryStats.maxPairs/maxClients).
	maxTrackedHosts         int
	maxTrackedDests         int
	maxTrackedConversations int

	// Bucket ring — the aggregated deltas GetTrafficDetail reads from.
	mu      sync.RWMutex
	buckets []trafficDetailBucket

	// protoMu/protoCounts holds the per-protocol session counts observed by
	// the most recent processFlows() call (10s cadence, same dump the bucket
	// ring is built from) — separate from the byte-delta state above because
	// this counts *sessions seen*, not bytes transferred.
	protoMu     sync.RWMutex
	protoCounts struct {
		tcp, udp, icmp, other int
		capped                bool
		at                    time.Time
	}

	// sessMu/sessRing/sessCur back the Active Sessions Dashboard card: sessCur
	// is the latest snapshot (Total/Max/Available from /proc, TCP/UDP/ICMP/
	// Other from protoCounts above); sessRing is the RAM-only 30-minute
	// history sampled every sessionSampleInterval by run()'s independent
	// ticker (plan Caution 6 — must not be tied to the metrics broadcaster,
	// which skips ticks when nobody is viewing the dashboard).
	sessMu   sync.RWMutex
	sessRing []model.SessionHistoryPoint
	sessCur  model.SessionCounts

	// Flow-delta baseline (conntrack).
	flowMu     sync.Mutex
	flowSeeded bool
	flowState  map[string]*flowSampleState

	// Rule-counter baseline (nftables), with reset detection (plan Caution 2).
	ruleMu       sync.Mutex
	ruleSeeded   bool
	ruleBaseline map[string]model.RuleCounter

	// Service Objects categorization cache (plan §2.2, Caution 9).
	svcMu       sync.RWMutex
	svcCache    []categoryEntry
	svcCachedAt time.Time

	// eventsActive reports whether the conntrack DESTROY event watcher
	// (StartFlowEndWatcher) is currently subscribed and running. Read by
	// GetTrafficDetail to set TrafficDetail.Accuracy ("near-exact" once
	// events are augmenting the poll, "estimated" while running poll-only —
	// see docs/ref/todo/traffic-accounting-accuracy-phase2-plan.md T-06).
	eventsActive atomic.Bool
}

// NewTrafficStatsService constructs the service. acct/dhcp/stats may be either
// the real or mock kernel implementation (main.go selects per -mock); stats is
// nil-safe (sampleSessions no-ops when nil, e.g. in older tests that don't
// exercise the Active Sessions feature).
//
// maxHosts/maxDests/maxConversations are the per-bucket tracking caps (see
// the field comments on TrafficStatsService). In production they come from
// the bootstrap config keys traffic-stats-max-hosts / -max-dests /
// -max-conversations, resolved by internal/config and wired through by
// cmd/pigate/main.go — this package deliberately does not import
// internal/config itself (same rationale as NewStatisticsService's
// maxDNSPairs/maxDNSClients). A <=0 value is defense-in-depth for direct
// callers (tests, future call sites) and falls back to
// defaultMaxTrackedHosts/defaultMaxTrackedDests/
// defaultMaxTrackedConversations; the authoritative range validation lives
// in config.Resolve, not here.
func NewTrafficStatsService(acct kernel.TrafficAccountingManager, repo *db.Repository, dhcp kernel.DhcpManager, stats kernel.SystemStatsManager, maxHosts, maxDests, maxConversations int) *TrafficStatsService {
	if maxHosts <= 0 {
		maxHosts = defaultMaxTrackedHosts
	}
	if maxDests <= 0 {
		maxDests = defaultMaxTrackedDests
	}
	if maxConversations <= 0 {
		maxConversations = defaultMaxTrackedConversations
	}
	return &TrafficStatsService{
		acct:                    acct,
		repo:                    repo,
		dhcp:                    dhcp,
		stats:                   stats,
		maxTrackedHosts:         maxHosts,
		maxTrackedDests:         maxDests,
		maxTrackedConversations: maxConversations,
		flowState:               make(map[string]*flowSampleState),
		ruleBaseline:            make(map[string]model.RuleCounter),
	}
}

// Start launches the background poller. Stops when ctx is cancelled
// (shutdown) — call once from main.go alongside the other dashboard samplers.
func (s *TrafficStatsService) Start(ctx context.Context) {
	go s.run(ctx)
	log.Printf("[TrafficStats] Started traffic-detail collector (poll every %s)", flowPollInterval)
}

func (s *TrafficStatsService) run(ctx context.Context) {
	t := time.NewTicker(flowPollInterval)
	defer t.Stop()
	// st is the Active Sessions sampler ticker — deliberately independent of
	// t above (and of the metrics broadcaster's own ticker in
	// system_status.go), so history ring samples keep landing every ~5s even
	// when nobody currently has the dashboard open (plan Caution 6).
	st := time.NewTicker(sessionSampleInterval)
	defer st.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.poll()
		case <-st.C:
			s.sampleSessions()
		}
	}
}

// sampleSessions is the Active Sessions sampler tick: reads the live
// conntrack total from kernel.SystemStatsManager plus the latest per-proto
// snapshot processFlows() already computed, and records both the current
// snapshot (sessCur) and a new history-ring point (sessRing) — but only when
// the live read is available, so a host without conntrack gets an empty
// history (and the frontend's CapabilityBanner empty state) instead of a
// misleading flat line of zeros.
func (s *TrafficStatsService) sampleSessions() {
	if s.stats == nil {
		return
	}
	total, max, available := s.stats.GetConntrackCount()
	tcp, udp, icmp, other, capped, at := s.protoSnapshot()

	protoSampledAt := ""
	if !at.IsZero() {
		protoSampledAt = at.UTC().Format(time.RFC3339)
	}
	cur := model.SessionCounts{
		Total:          total,
		TCP:            tcp,
		UDP:            udp,
		ICMP:           icmp,
		Other:          other,
		Max:            max,
		Available:      available,
		ProtoSampledAt: protoSampledAt,
		ProtoCapped:    capped,
	}

	s.sessMu.Lock()
	s.sessCur = cur
	if available {
		point := model.SessionHistoryPoint{
			T:     time.Now().UTC().Format(time.RFC3339),
			Total: total,
			TCP:   tcp,
			UDP:   udp,
			ICMP:  icmp,
			Other: other,
		}
		s.sessRing = append(s.sessRing, point)
		if len(s.sessRing) > sessionRingMax {
			s.sessRing = s.sessRing[len(s.sessRing)-sessionRingMax:]
		}
	}
	s.sessMu.Unlock()
}

// SessionCurrent returns the latest Active Sessions snapshot. Safe to call
// concurrently — SessionCounts is scalar-only, so returning it by value under
// RLock is a real copy (no shared references, unlike trafficDetailBucket's
// maps — see the GetTrafficDetail comment below).
func (s *TrafficStatsService) SessionCurrent() model.SessionCounts {
	s.sessMu.RLock()
	defer s.sessMu.RUnlock()
	return s.sessCur
}

// SessionHistory returns a copy of the Active Sessions RAM ring buffer.
// Deliberately makes a fresh slice and copies into it rather than returning
// s.sessRing (or a sub-slice of it) directly: sampleSessions appends to
// s.sessRing from the sampler goroutine on every tick, and a caller holding a
// slice that aliases that backing array would race with those appends —
// same class of bug the trafficDetailBucket race note above (:534-551)
// warns about, just with a slice instead of a map.
func (s *TrafficStatsService) SessionHistory() []model.SessionHistoryPoint {
	s.sessMu.RLock()
	defer s.sessMu.RUnlock()
	out := make([]model.SessionHistoryPoint, len(s.sessRing))
	copy(out, s.sessRing)
	return out
}

// StartFlowEndWatcher launches the conntrack DESTROY event watcher on its own
// background goroutine, separate from run()'s poll ticker
// (docs/ref/todo/traffic-accounting-accuracy-phase2-plan.md T-06/T-08). It is
// safe to call this even when the underlying kernel.TrafficAccountingManager
// cannot actually subscribe (e.g. mock, or a real kernel without
// CONFIG_NF_CONNTRACK_EVENTS / missing cap_net_admin): a failure here only
// keeps eventsActive false, degrading GetTrafficDetail's Accuracy back to
// "estimated" — it must never be treated as fatal to the caller (plan
// Caution 6).
func (s *TrafficStatsService) StartFlowEndWatcher(ctx context.Context) {
	go s.runFlowEndWatcher(ctx)
	log.Printf("[TrafficStats] Starting conntrack flow-end event watcher")
}

// runFlowEndWatcher calls the blocking kernel.WatchFlowEnd for as long as ctx
// is alive, flipping eventsActive on/off around the call so
// GetTrafficDetail's Accuracy label always reflects whether events are
// actually flowing right now. A non-nil error after ctx is NOT cancelled
// means the subscription itself failed or dropped (e.g. transient netlink
// error) — logged once here; the poll loop in run() is entirely unaffected
// and keeps the Dashboard cards working (poll-only).
func (s *TrafficStatsService) runFlowEndWatcher(ctx context.Context) {
	s.eventsActive.Store(true)
	err := s.acct.WatchFlowEnd(ctx, s.onFlowEnd)
	s.eventsActive.Store(false)
	if err != nil && ctx.Err() == nil {
		log.Printf("[TrafficStats] Flow-end event watcher stopped (degrading to poll-only accounting): %v", err)
	}
}

// onFlowEnd is the callback WatchFlowEnd invokes once per conntrack DESTROY
// event. It credits ONLY the byte delta the poll path has not already
// counted, under the same flowMu/flowState baseline poll() itself maintains,
// then deletes the key so a later poll tick can never see it again and
// double-credit it (plan Caution 1 — the single most important invariant of
// this whole phase):
//   - key has a baseline (poll saw this flow at least once): credit
//     f.Bytes - baseline (clamped to 0 — a flow can only grow, so a negative
//     result here would mean a key collision or a kernel counter reset, never
//     a legitimate value) and delete the baseline.
//   - key has no baseline (the flow was born and died entirely between two
//     poll ticks — exactly the gap this phase exists to close): credit
//     f.Bytes in full.
//
// The credited delta lands in whichever 5-minute bucket is open *right now*,
// not the bucket(s) the flow's bytes actually flowed through — a flow that
// carried data 4 minutes ago but is only torn down (and thus only reported)
// now will show up in the current bucket instead. The window total (1h/24h)
// stays correct either way; only the bucket-by-bucket shape shifts slightly.
// This is expected, not a bug (plan Caution 7) — see also the analogous note
// in real_conntrack_events.go about ordering.
func (s *TrafficStatsService) onFlowEnd(f model.FlowSample) {
	s.flowMu.Lock()
	var dOrig, dReply uint64
	if st, ok := s.flowState[f.Key]; ok {
		if f.BytesOrig > st.orig {
			dOrig = f.BytesOrig - st.orig
		}
		if f.BytesReply > st.reply {
			dReply = f.BytesReply - st.reply
		}
		delete(s.flowState, f.Key)
	} else {
		dOrig = f.BytesOrig
		dReply = f.BytesReply
	}
	s.flowMu.Unlock()

	delta := dOrig + dReply
	if delta == 0 {
		return
	}

	hostDeltas := map[string]dirBytes{f.SrcIP: {Orig: dOrig, Reply: dReply}}
	catDeltas := map[string]uint64{s.categorize(f.Proto, f.DstPort): delta}
	dstDeltas := map[string]dirBytes{f.DstIP: {Orig: dOrig, Reply: dReply}}
	convDeltas := map[string]dirBytes{convKey(f): {Orig: dOrig, Reply: dReply}}

	// LAN-relative Observed delta (plan §2.2 T-02 item 6): computed straight
	// from f.SrcIP/f.DstIP, not from flowState (the baseline for this key was
	// just deleted above, and the "no baseline" branch never had one to begin
	// with) — lanFlipFor is pure and keyed off the same IP pair regardless, so
	// this always agrees with what processFlows would have used.
	var observed dirBytes
	if lanFlipFor(f.SrcIP, f.DstIP) {
		observed = dirBytes{Orig: dReply, Reply: dOrig}
	} else {
		observed = dirBytes{Orig: dOrig, Reply: dReply}
	}
	s.addBucket(time.Now(), hostDeltas, catDeltas, nil, dstDeltas, convDeltas, observed)
}

// poll is one collection tick: DumpFlows + DumpRuleCounters must never be
// called from an HTTP request handler (plan Caution 6) — this background
// goroutine is the only caller, and GetTrafficDetail only ever reads the
// cached bucket ring.
func (s *TrafficStatsService) poll() {
	now := time.Now()
	hostDeltas := make(map[string]dirBytes)
	catDeltas := make(map[string]uint64)
	dstDeltas := make(map[string]dirBytes)
	convDeltas := make(map[string]dirBytes)
	var observed dirBytes

	if flows, err := s.acct.DumpFlows(); err != nil {
		log.Printf("[TrafficStats] DumpFlows failed (traffic cards will show stale/empty data): %v", err)
	} else {
		observed = s.processFlows(flows, hostDeltas, catDeltas, dstDeltas, convDeltas)
	}

	ruleDeltas := make(map[string]model.RuleCounter)
	if counters, err := s.acct.DumpRuleCounters(); err != nil {
		log.Printf("[TrafficStats] DumpRuleCounters failed (Top Rules will show stale/empty data): %v", err)
	} else {
		s.processRuleCounters(counters, ruleDeltas)
	}

	if observed.Total() == 0 && len(hostDeltas) == 0 && len(catDeltas) == 0 && len(ruleDeltas) == 0 {
		// Either a seed-only first poll (plan Caution 4) or a genuinely quiet
		// tick — nothing to add to the bucket ring.
		return
	}
	s.addBucket(now, hostDeltas, catDeltas, ruleDeltas, dstDeltas, convDeltas, observed)
}

// processFlows folds one DumpFlows snapshot into per-host/per-category
// deltas, seeding (not counting) on the very first call ever made (plan
// Caution 4), clamping negative deltas to 0 (Caution 3), and pruning flow
// keys absent for flowStaleMisses consecutive polls (Caution 5). Returns the
// total observed byte delta this poll, as a LAN-relative dirBytes (Orig = up
// leaving the LAN, Reply = down entering it — plan §2.2/T-02 item 4), derived
// from each flow's own st.lanFlip (set once when its flowSampleState was
// created — see that struct's comment) rather than recomputed here.
func (s *TrafficStatsService) processFlows(flows []model.FlowSample, hostDeltas map[string]dirBytes, catDeltas map[string]uint64, dstDeltas, convDeltas map[string]dirBytes) dirBytes {
	s.flowMu.Lock()
	defer s.flowMu.Unlock()

	firstPoll := !s.flowSeeded
	seen := make(map[string]bool, len(flows))
	var observed dirBytes
	var protoTCP, protoUDP, protoICMP, protoOther int

	for _, f := range flows {
		seen[f.Key] = true

		// Active Sessions per-proto session count (plan §Step 3) — counts
		// every flow this dump saw, including the seed round and delta==0
		// flows (this is a session count, not a byte-delta count), and MUST
		// run before the memory-cap continue below so a capped/dropped flow
		// still contributes to the proto tally.
		switch f.Proto {
		case 6:
			protoTCP++
		case 17:
			protoUDP++
		case 1, 58:
			protoICMP++
		default:
			protoOther++
		}

		st, ok := s.flowState[f.Key]
		if !ok {
			if len(s.flowState) >= maxTrackedFlows {
				continue // memory cap (plan Caution 5) — drop tracking for this flow
			}
			// lanFlip is computed once here from this flow's own SrcIP/DstIP and
			// never touched again (plan §2.2/T-02 item 2/Caution 1).
			st = &flowSampleState{lanFlip: lanFlipFor(f.SrcIP, f.DstIP)}
			s.flowState[f.Key] = st
		}

		// Per-direction delta + clamp (plan §5 Caution 3: this is a deliberate
		// change from Phase 2's single combined clamp — a counter monotonic
		// per direction means clamping per direction is strictly more correct
		// than clamping the sum, which could otherwise let one direction's
		// legitimate growth mask the other direction's counter reset/key
		// collision). st.orig and st.reply are ALWAYS updated together on the
		// same line (plan §5 Caution 2) so neither direction's baseline can
		// silently fall behind and get re-credited every poll.
		dOrigI := int64(f.BytesOrig) - int64(st.orig)
		dReplyI := int64(f.BytesReply) - int64(st.reply)
		st.orig, st.reply, st.misses = f.BytesOrig, f.BytesReply, 0
		if dOrigI < 0 {
			dOrigI = 0
		}
		if dReplyI < 0 {
			dReplyI = 0
		}
		dOrig, dReply := uint64(dOrigI), uint64(dReplyI)

		if firstPoll || dOrig+dReply == 0 {
			continue
		}
		// Single combined delta `d` — the only value that flows into
		// observed/catDeltas; host/dst/conv receive the (dOrig, dReply) pair
		// derived from that same computation, never a second delta (plan §5
		// Caution 1/2, invariant 5 of §2.1).
		d := dOrig + dReply
		// observed accumulates LAN-relative up/down (Orig field = up, Reply
		// field = down), using st.lanFlip fixed once at flowSampleState
		// creation — never recomputed per poll (plan §2.2/§5 Caution 1).
		if st.lanFlip {
			observed.Orig += dReply
			observed.Reply += dOrig
		} else {
			observed.Orig += dOrig
			observed.Reply += dReply
		}
		if _, exists := hostDeltas[f.SrcIP]; exists || len(hostDeltas) < s.maxTrackedHosts {
			cur := hostDeltas[f.SrcIP]
			cur.Orig += dOrig
			cur.Reply += dReply
			hostDeltas[f.SrcIP] = cur
		}
		catDeltas[s.categorize(f.Proto, f.DstPort)] += d
		// Same (dOrig, dReply) feeds Top Destinations/Top Conversations
		// (docs/ref/todo/statistics-page-plan.md T-02 step 4, Caution 2) —
		// never compute a second delta for these, or a stray poll/event race
		// would silently double-count.
		if _, exists := dstDeltas[f.DstIP]; exists || len(dstDeltas) < s.maxTrackedDests {
			cur := dstDeltas[f.DstIP]
			cur.Orig += dOrig
			cur.Reply += dReply
			dstDeltas[f.DstIP] = cur
		}
		ck := convKey(f)
		if _, exists := convDeltas[ck]; exists || len(convDeltas) < s.maxTrackedConversations {
			cur := convDeltas[ck]
			cur.Orig += dOrig
			cur.Reply += dReply
			convDeltas[ck] = cur
		}
	}

	for k, st := range s.flowState {
		if seen[k] {
			continue
		}
		st.misses++
		if st.misses >= flowStaleMisses {
			delete(s.flowState, k)
		}
	}

	s.flowSeeded = true

	s.protoMu.Lock()
	s.protoCounts.tcp = protoTCP
	s.protoCounts.udp = protoUDP
	s.protoCounts.icmp = protoICMP
	s.protoCounts.other = protoOther
	s.protoCounts.capped = len(flows) >= kernel.MaxFlowsPerDump
	s.protoCounts.at = time.Now()
	s.protoMu.Unlock()

	return observed
}

// protoSnapshot returns the most recent per-protocol session counts
// processFlows() computed, plus whether that dump hit kernel.MaxFlowsPerDump
// and when it was taken (zero time if processFlows has never run yet).
func (s *TrafficStatsService) protoSnapshot() (tcp, udp, icmp, other int, capped bool, at time.Time) {
	s.protoMu.RLock()
	defer s.protoMu.RUnlock()
	return s.protoCounts.tcp, s.protoCounts.udp, s.protoCounts.icmp, s.protoCounts.other, s.protoCounts.capped, s.protoCounts.at
}

// processRuleCounters folds one DumpRuleCounters snapshot into ruleDeltas,
// seeding (not counting) the first time each rule id is ever seen and the
// very first poll overall (plan Caution 4), and detecting a counter reset
// (cur < baseline, e.g. the operator triggered ApplyRules) by treating the
// new cumulative value itself as the delta instead of underflowing a uint64
// subtraction (plan Caution 2).
func (s *TrafficStatsService) processRuleCounters(counters map[string]model.RuleCounter, ruleDeltas map[string]model.RuleCounter) {
	s.ruleMu.Lock()
	defer s.ruleMu.Unlock()

	firstPoll := !s.ruleSeeded

	for id, cur := range counters {
		base, existed := s.ruleBaseline[id]
		reset := existed && (cur.Bytes < base.Bytes || cur.Packets < base.Packets)

		switch {
		case !existed:
			// First time this DB rule id has ever been observed — seed only.
			s.ruleBaseline[id] = cur
		case reset:
			s.ruleBaseline[id] = cur
			if !firstPoll && (cur.Bytes > 0 || cur.Packets > 0) {
				ruleDeltas[id] = cur
			}
		case firstPoll:
			s.ruleBaseline[id] = cur
		default:
			deltaBytes := cur.Bytes - base.Bytes
			deltaPackets := cur.Packets - base.Packets
			s.ruleBaseline[id] = cur
			if deltaBytes > 0 || deltaPackets > 0 {
				ruleDeltas[id] = model.RuleCounter{Bytes: deltaBytes, Packets: deltaPackets}
			}
		}
	}

	s.ruleSeeded = true
}

// categorize maps one flow's (proto, dstPort) to a Service-Object-defined
// category name, or "Other" when nothing matches (plan §2.2). TCP/UDP flows
// match on the narrowest matching port range (ties broken alphabetically for
// determinism); everything else (ICMP/ICMPv6/other) only matches an explicit
// Service Object with Protocol=="ICMP".
func (s *TrafficStatsService) categorize(proto uint8, dstPort uint16) string {
	entries := s.categoryEntries()

	if proto != 6 && proto != 17 {
		for _, e := range entries {
			if e.isICMP {
				return e.name
			}
		}
		return "Other"
	}

	var best *categoryEntry
	port := int(dstPort)
	for i := range entries {
		e := &entries[i]
		if e.isICMP || !protoMatches(e.protocols, proto) {
			continue
		}
		if port < e.portStart || port > e.portEnd {
			continue
		}
		if best == nil {
			best = e
			continue
		}
		width, bestWidth := e.portEnd-e.portStart, best.portEnd-best.portStart
		if width < bestWidth || (width == bestWidth && e.name < best.name) {
			best = e
		}
	}
	if best != nil {
		return best.name
	}
	return "Other"
}

func protoMatches(protocols []uint8, proto uint8) bool {
	for _, p := range protocols {
		if p == proto {
			return true
		}
	}
	return false
}

// categoryEntries returns the cached Service-Object lookup table, refreshing
// it from the DB when older than serviceObjectsCacheTTL. A refresh failure
// falls back to the previous (possibly stale) cache rather than clearing it
// — losing categorization entirely on a transient DB error would be worse
// than a slightly-stale category set (plan Caution 9).
func (s *TrafficStatsService) categoryEntries() []categoryEntry {
	s.svcMu.RLock()
	fresh := s.svcCache != nil && time.Since(s.svcCachedAt) < serviceObjectsCacheTTL
	cached := s.svcCache
	s.svcMu.RUnlock()
	if fresh {
		return cached
	}

	svcs, err := s.repo.GetServices()
	if err != nil {
		log.Printf("[TrafficStats] Failed to refresh Service Objects cache, using previous snapshot: %v", err)
		s.svcMu.RLock()
		defer s.svcMu.RUnlock()
		return s.svcCache
	}

	entries := buildCategoryEntries(svcs)
	s.svcMu.Lock()
	s.svcCache = entries
	s.svcCachedAt = time.Now()
	s.svcMu.Unlock()
	return entries
}

func buildCategoryEntries(svcs []model.ServiceObject) []categoryEntry {
	out := make([]categoryEntry, 0, len(svcs))
	for _, sv := range svcs {
		proto := strings.ToUpper(strings.TrimSpace(sv.Protocol))
		switch proto {
		case "ICMP":
			out = append(out, categoryEntry{name: sv.Name, isICMP: true})
		case "TCP", "UDP", "TCP/UDP":
			start, end, ok := parseServicePortRange(sv.Port)
			if !ok {
				continue // invalid port spec on this Service Object — skip it
			}
			protocols := []uint8{6, 17}
			if proto == "TCP" {
				protocols = []uint8{6}
			} else if proto == "UDP" {
				protocols = []uint8{17}
			}
			out = append(out, categoryEntry{name: sv.Name, protocols: protocols, portStart: start, portEnd: end})
		default:
			// Unsupported/unrecognized protocol string — not a valid
			// firewall-usable Service Object either, so skip categorization.
		}
	}
	return out
}

// parseServicePortRange normalizes a Service Object's Port field ("", "-",
// "443", "8000-8010") into an inclusive range, treating an empty/"-" spec as
// "all ports" the same way real_firewall.go's dportMatchExprs skip does.
func parseServicePortRange(port string) (start, end int, ok bool) {
	p := strings.TrimSpace(port)
	if p == "" || p == "-" {
		return 1, 65535, true
	}
	s, e, err := model.ParsePortSpec(p)
	if err != nil {
		return 0, 0, false
	}
	return s, e, true
}

// addBucket merges this poll's deltas into the current (or a freshly rolled)
// 5-minute bucket, evicting the oldest bucket past trafficDetailBucketMax —
// mirrors SystemStatusService.addToBucketLocked.
func (s *TrafficStatsService) addBucket(now time.Time, hostDeltas map[string]dirBytes, catDeltas map[string]uint64, ruleDeltas map[string]model.RuleCounter, dstDeltas, convDeltas map[string]dirBytes, observed dirBytes) {
	ts := now.Truncate(trafficDetailBucketSpan).Format(time.RFC3339)

	s.mu.Lock()
	defer s.mu.Unlock()

	if n := len(s.buckets); n > 0 && s.buckets[n-1].ts == ts {
		b := &s.buckets[n-1]
		mergeDirMap(b.hostBytes, hostDeltas, s.maxTrackedHosts)
		mergeUint64Map(b.catBytes, catDeltas, 0)
		mergeRuleMap(b.ruleBytes, ruleDeltas)
		mergeDirMap(b.dstBytes, dstDeltas, s.maxTrackedDests)
		mergeDirMap(b.convBytes, convDeltas, s.maxTrackedConversations)
		b.observed.Orig += observed.Orig
		b.observed.Reply += observed.Reply
		return
	}

	b := trafficDetailBucket{
		ts:        ts,
		hostBytes: make(map[string]dirBytes, len(hostDeltas)),
		catBytes:  make(map[string]uint64, len(catDeltas)),
		ruleBytes: make(map[string]model.RuleCounter, len(ruleDeltas)),
		dstBytes:  make(map[string]dirBytes, len(dstDeltas)),
		convBytes: make(map[string]dirBytes, len(convDeltas)),
		observed:  observed,
	}
	mergeDirMap(b.hostBytes, hostDeltas, s.maxTrackedHosts)
	mergeUint64Map(b.catBytes, catDeltas, 0)
	mergeRuleMap(b.ruleBytes, ruleDeltas)
	mergeDirMap(b.dstBytes, dstDeltas, s.maxTrackedDests)
	mergeDirMap(b.convBytes, convDeltas, s.maxTrackedConversations)

	s.buckets = append(s.buckets, b)
	if len(s.buckets) > trafficDetailBucketMax {
		s.buckets = s.buckets[len(s.buckets)-trafficDetailBucketMax:]
	}
}

// convKey builds the Statistics page's 4-tuple conversation key (src IP, dst
// IP, proto, dst port — no src port, plan §0: model.FlowSample has none).
// Deliberately a plain delimited string (not a hash) so GetTrafficBreakdown
// can split it back into its parts for display, unlike flowState's keys
// (plan §1 item 3).
func convKey(f model.FlowSample) string {
	return f.SrcIP + "|" + f.DstIP + "|" + protoLabel(f.Proto) + "|" + strconv.FormatUint(uint64(f.DstPort), 10)
}

// protoLabel renders an IP protocol number the way the Statistics page wants
// to display it: "TCP"/"UDP"/"ICMP" for the common cases, "IP:<n>" otherwise.
func protoLabel(proto uint8) string {
	switch proto {
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 1, 58:
		return "ICMP"
	default:
		return "IP:" + strconv.Itoa(int(proto))
	}
}

// mergeUint64Map adds src's deltas into dst. When capAt > 0, a brand-new key
// is dropped once dst already holds capAt entries (existing keys keep
// accumulating) — bounds a single bucket's host map under a scan/P2P burst
// (plan Caution 5). capAt == 0 means uncapped (categories are bounded by the
// Service Object count already).
func mergeUint64Map(dst, src map[string]uint64, capAt int) {
	for k, v := range src {
		if _, exists := dst[k]; !exists && capAt > 0 && len(dst) >= capAt {
			continue
		}
		dst[k] += v
	}
}

// mergeDirMap is mergeUint64Map's counterpart for dirBytes-valued maps —
// same cap semantics exactly (a brand-new key is dropped once dst already
// holds capAt entries, existing keys keep accumulating in both directions).
func mergeDirMap(dst, src map[string]dirBytes, capAt int) {
	for k, v := range src {
		if _, exists := dst[k]; !exists && capAt > 0 && len(dst) >= capAt {
			continue
		}
		cur := dst[k]
		cur.Orig += v.Orig
		cur.Reply += v.Reply
		dst[k] = cur
	}
}

func mergeRuleMap(dst, src map[string]model.RuleCounter) {
	for k, v := range src {
		cur := dst[k]
		cur.Bytes += v.Bytes
		cur.Packets += v.Packets
		dst[k] = cur
	}
}

// GetTrafficDetail composes the /api/dashboard/traffic-detail response for
// the given window ("1h" default, or "24h"). It only ever reads the cached
// bucket ring — never calls the kernel directly (plan Caution 6).
//
// The per-bucket aggregation below MUST run while s.mu is still held: unlike
// system_status.go's TrafficBucket (scalar fields only, so slicing/copying
// the struct is a real deep copy), trafficDetailBucket carries three map
// fields (hostBytes/catBytes/ruleBytes) — maps are reference types, so
// copying the struct only copies the map header, not its contents. Reading
// those maps after releasing the lock would race with poll()'s background
// goroutine, which keeps mutating the newest (still-open) bucket's maps via
// addBucket/mergeUint64Map every flowPollInterval tick. A previous version
// of this method copied s.buckets into a local slice and released the lock
// before ranging into each bucket's maps — that is exactly the concurrent
// map read/write Go's race detector (and, in production, the runtime's
// built-in map-corruption fatal error) will catch; fixed by keeping the
// whole read+aggregate under one RLock instead (see traffic_stats_test.go
// TestTrafficStats_GetTrafficDetailNoRaceWithPoll, run with `go test -race`).
func (s *TrafficStatsService) GetTrafficDetail(window string) model.TrafficDetail {
	if window != trafficWindow24h {
		window = trafficWindow1h
	}

	hostTotals := make(map[string]uint64)
	catTotals := make(map[string]uint64)
	ruleTotals := make(map[string]model.RuleCounter)
	var observed uint64

	s.mu.RLock()
	var windowBuckets []trafficDetailBucket
	if window == trafficWindow1h {
		n := trafficWindow1hBuckets
		if len(s.buckets) < n {
			n = len(s.buckets)
		}
		windowBuckets = s.buckets[len(s.buckets)-n:]
	} else {
		windowBuckets = s.buckets
	}
	for _, b := range windowBuckets {
		// Summed via .Total() — GetTrafficDetail's response (Dashboard
		// "Detailed" tab) MUST NOT change out of this plan (plan §5 Caution
		// 6): it stays a single combined byte figure, identical to what the
		// pre-split map[string]uint64 would have produced.
		for k, v := range b.hostBytes {
			hostTotals[k] += v.Total()
		}
		for k, v := range b.catBytes {
			catTotals[k] += v
		}
		for k, v := range b.ruleBytes {
			cur := ruleTotals[k]
			cur.Bytes += v.Bytes
			cur.Packets += v.Packets
			ruleTotals[k] = cur
		}
		observed += b.observed.Total()
	}
	s.mu.RUnlock()

	leaseByIP, resByIP := s.hostLookup()

	accuracy := "estimated"
	if s.eventsActive.Load() {
		accuracy = "near-exact"
	}

	return model.TrafficDetail{
		Window:         window,
		ObservedBytes:  observed,
		Estimated:      true,
		Accuracy:       accuracy,
		Categories:     buildCategorySlices(catTotals, observed),
		TopTalkers:     buildTopTalkers(hostTotals, observed, leaseByIP, resByIP),
		TopRules:       buildTopRules(ruleTotals, s.policyLookup()),
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		Sessions:       s.SessionCurrent(),
		SessionHistory: s.SessionHistory(),
	}
}

// TrafficBreakdown is the read-only accessor the Statistics page's
// StatisticsService composes its response from (docs/ref/todo/statistics-page-plan.md
// T-02 step 6). Hosts/Dests/Convs are raw byte totals for the requested
// window, keyed the same way the bucket ring stores them (Convs by
// convKey); StatisticsService is responsible for turning those keys back
// into display rows (hostname lookup, percent, sorting).
type TrafficBreakdown struct {
	Hosts, Dests, Convs map[string]dirBytes
	Observed            uint64
	Truncated           bool
	Accuracy            string
	// Series is the bandwidth-over-time chart data (plan docs/ref/todo/
	// statistics-overview-bandwidth-chart-plan.md T-03) — computed in the SAME
	// RLock/loop as Observed above (not a separate method/lock) so
	// sum(Series[].Bytes) == Observed holds by construction, not by luck (plan
	// §2.5). Not itself serialized — StatisticsService.GetStatistics copies it
	// straight into model.TrafficStatistics.Series.
	Series []model.BandwidthPoint
	// HostSeries is nil whenever getTrafficBreakdown is called without a
	// focus IP (i.e. every call through GetTrafficBreakdown) — only
	// GetTrafficBreakdownForIP populates it (docs/ref/todo/
	// statistics-traffic-bandwidth-chart-plan.md §2.3/T-02). Unlike Series
	// above (network-wide, LAN-relative), HostSeries is the focus IP's own
	// traffic only, flow-relative (Orig = up, Reply = down), computed from
	// the SAME b.convBytes map Convs is summed from in the same RLock/loop —
	// so sum(HostSeries[].Bytes) == sum(Convs[...].Total()) for that IP holds
	// by construction (plan §2.2 decision 1).
	HostSeries []model.BandwidthPoint
}

// GetTrafficBreakdown is GetTrafficDetail's sibling for the Statistics page:
// same bucket-ring read pattern (whole aggregation under one RLock — see the
// long comment on GetTrafficDetail above for why), but also returns the raw
// dst/conv totals GetTrafficDetail's response shape has no room for.
// Truncated reports whether any bucket in the window hit one of its
// tracking caps (maxTrackedHosts/maxTrackedDests/maxTrackedConversations) —
// a signal that the ranking below may be missing entries.
//
// GetTrafficBreakdown is unchanged behavior-wise from before plan
// statistics-traffic-bandwidth-chart-plan.md T-02 — it is now a thin wrapper
// around getTrafficBreakdown with an empty focusIP, which always leaves
// HostSeries nil (see the TrafficBreakdown.HostSeries comment above).
func (s *TrafficStatsService) GetTrafficBreakdown(window string) TrafficBreakdown {
	return s.getTrafficBreakdown(window, "")
}

// GetTrafficBreakdownForIP is GetTrafficBreakdown's per-IP sibling (plan
// §2.3/T-02) — same snapshot/RLock as GetTrafficBreakdown (Hosts/Dests/Convs/
// Observed/Series are identical for a given window regardless of focusIP),
// PLUS HostSeries populated for the given IP. Deliberately not a second
// method that re-locks and re-scans the bucket ring: that would read a
// different snapshot than a plain GetTrafficBreakdown call made moments
// apart, breaking the sum(HostSeries)==TotalBytes invariant the caller
// (GetTrafficHostDetail) relies on (plan §2.3 rationale).
func (s *TrafficStatsService) GetTrafficBreakdownForIP(window, ip string) TrafficBreakdown {
	return s.getTrafficBreakdown(window, ip)
}

// getTrafficBreakdown is the shared implementation behind
// GetTrafficBreakdown/GetTrafficBreakdownForIP. When focusIP is "", HostSeries
// is left nil and the per-bucket convBytes scan below is skipped entirely
// (zero added cost for every existing caller — GetTrafficBreakdown itself,
// GetTrafficTopHosts, GetStatistics).
func (s *TrafficStatsService) getTrafficBreakdown(window, focusIP string) TrafficBreakdown {
	if window != trafficWindow24h {
		window = trafficWindow1h
	}

	hostTotals := make(map[string]dirBytes)
	dstTotals := make(map[string]dirBytes)
	convTotals := make(map[string]dirBytes)
	var observed uint64
	var truncated bool

	// series axis (plan §2.5/T-03 step 2): n points, oldest -> newest, ending
	// at the current (possibly still-open) bucket. Computed BEFORE taking
	// s.mu.RLock and reused unchanged inside the loop below, so every bucket
	// in windowBuckets is aggregated into both Observed and Series under the
	// exact same lock acquisition — the race the plan's revision 2 fixed
	// (§2.5 item 1) is structurally impossible here.
	n := trafficWindow1hBuckets
	if window == trafficWindow24h {
		n = trafficDetailBucketMax
	}
	axisEnd := time.Now().Truncate(trafficDetailBucketSpan)
	axisStart := axisEnd.Add(-time.Duration(n-1) * trafficDetailBucketSpan)
	series := make([]model.BandwidthPoint, n)
	for i := range series {
		series[i].Ts = axisStart.Add(time.Duration(i) * trafficDetailBucketSpan).Format(time.RFC3339)
	}
	// hostSeries is allocated (and Ts-filled) whenever focusIP is set, even if
	// that IP turns out to have no traffic at all — so callers always get a
	// fixed-length, zero-filled array rather than nil (plan T-02 step 5).
	var hostSeries []model.BandwidthPoint
	if focusIP != "" {
		hostSeries = make([]model.BandwidthPoint, n)
		for i := range hostSeries {
			hostSeries[i].Ts = series[i].Ts
		}
	}

	// focusIPSrcPrefix/focusIPDstNeedle are the fast-path substring checks the
	// convBytes scan below uses to skip parseConvKey's SplitN+ParseUint
	// allocation for the vast majority of keys that plainly can't match
	// focusIP (plan §5 item 8 — a bucket's convBytes map can hold up to
	// maxTrackedConversations entries, scanned on every 10s-polled HTTP
	// request). A convKey is "src|dst|proto|port" (convKey above), so
	// focusIPSrcPrefix matches the src position and focusIPDstNeedle (with
	// pipes on both sides) can only match the dst position — IP addresses
	// never contain '|', so neither check can false-positive against proto/
	// port.
	var focusIPSrcPrefix, focusIPDstNeedle string
	if focusIP != "" {
		focusIPSrcPrefix = focusIP + "|"
		focusIPDstNeedle = "|" + focusIP + "|"
	}

	s.mu.RLock()
	var windowBuckets []trafficDetailBucket
	if window == trafficWindow1h {
		n := trafficWindow1hBuckets
		if len(s.buckets) < n {
			n = len(s.buckets)
		}
		windowBuckets = s.buckets[len(s.buckets)-n:]
	} else {
		windowBuckets = s.buckets
	}
	for _, b := range windowBuckets {
		for k, v := range b.hostBytes {
			cur := hostTotals[k]
			cur.Orig += v.Orig
			cur.Reply += v.Reply
			hostTotals[k] = cur
		}
		for k, v := range b.dstBytes {
			cur := dstTotals[k]
			cur.Orig += v.Orig
			cur.Reply += v.Reply
			dstTotals[k] = cur
		}
		for k, v := range b.convBytes {
			cur := convTotals[k]
			cur.Orig += v.Orig
			cur.Reply += v.Reply
			convTotals[k] = cur
		}
		observed += b.observed.Total()

		// Plot this bucket into series (plan §2.5 item 2 / T-03 step 4): index
		// by elapsed time from axisStart, carried into the nearest edge point
		// when the ring covers a wider span than the window (older than the
		// axis -> index 0, newer -> index n-1, e.g. after an NTP step back) or
		// when b.ts fails to parse (should never happen — addBucket is the
		// only writer) — this carry rule is what keeps
		// sum(Series[].Bytes) == Observed true even when the window is
		// bucket-count-based, not time-based (§7 item 6 / §2.5 item 2).
		idx := 0
		if bt, err := time.Parse(time.RFC3339, b.ts); err == nil {
			idx = int(bt.Sub(axisStart) / trafficDetailBucketSpan)
			if idx < 0 {
				idx = 0
			} else if idx >= n {
				idx = n - 1
			}
		}
		series[idx].BytesUp += b.observed.Orig
		series[idx].BytesDown += b.observed.Reply
		series[idx].Bytes += b.observed.Total()

		// hostSeries (per-IP, flow-relative — plan §2.2 decision 2/T-02 step
		// 2) is built from the SAME b.convBytes this bucket already merged
		// into convTotals above, at the SAME idx series uses — never a second
		// lock/scan, so sum(HostSeries[].Bytes) == sum of this IP's
		// convTotals rows holds by construction.
		if focusIP != "" {
			for k, v := range b.convBytes {
				if !strings.HasPrefix(k, focusIPSrcPrefix) && !strings.Contains(k, focusIPDstNeedle) {
					continue // fast path: neither side of this key can be focusIP
				}
				src, dst, _, _, ok := parseConvKey(k)
				if !ok {
					continue
				}
				if src == focusIP {
					hostSeries[idx].BytesUp += v.Orig
					hostSeries[idx].BytesDown += v.Reply
					hostSeries[idx].Bytes += v.Total()
				}
				// Not an else-if: a same-IP-both-sides row (e.g. loopback)
				// must be counted twice, matching how TotalBytes is summed in
				// GetTrafficHostDetail (plan T-02 step 2).
				if dst == focusIP {
					hostSeries[idx].BytesUp += v.Orig
					hostSeries[idx].BytesDown += v.Reply
					hostSeries[idx].Bytes += v.Total()
				}
			}
		}

		if len(b.hostBytes) >= s.maxTrackedHosts || len(b.dstBytes) >= s.maxTrackedDests || len(b.convBytes) >= s.maxTrackedConversations {
			truncated = true
		}
	}
	s.mu.RUnlock()

	accuracy := "estimated"
	if s.eventsActive.Load() {
		accuracy = "near-exact"
	}

	return TrafficBreakdown{
		Hosts:      hostTotals,
		Dests:      dstTotals,
		Convs:      convTotals,
		Observed:   observed,
		Truncated:  truncated,
		Accuracy:   accuracy,
		Series:     series,
		HostSeries: hostSeries,
	}
}

func buildCategorySlices(catTotals map[string]uint64, observed uint64) []model.TrafficCategorySlice {
	out := make([]model.TrafficCategorySlice, 0, len(catTotals))
	for name, bytes := range catTotals {
		if bytes == 0 {
			continue
		}
		out = append(out, model.TrafficCategorySlice{Name: name, Bytes: bytes, Percent: percentOf(bytes, observed)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func buildTopTalkers(hostTotals map[string]uint64, observed uint64, leaseByIP map[string]model.ActiveDhcpLease, resByIP map[string]model.DhcpReservation) []model.TopTalker {
	out := make([]model.TopTalker, 0, len(hostTotals))
	for ip, bytes := range hostTotals {
		if bytes == 0 {
			continue
		}
		hostname, mac := hostnameFor(ip, leaseByIP, resByIP)
		out = append(out, model.TopTalker{IP: ip, Hostname: hostname, MAC: mac, Bytes: bytes, Percent: percentOf(bytes, observed)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].IP < out[j].IP
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

// buildTopRules ranks rule-counter deltas. Percent is computed against the
// sum of ALL rule deltas in the window (not ObservedBytes) — the two figures
// come from different accounting sources (nftables' own exact counters vs.
// conntrack-derived estimates), so mixing them would misrepresent an exact
// figure as a rough one (plan §2.3: this card is exact, unlike the other two).
func buildTopRules(ruleTotals map[string]model.RuleCounter, rulesByID map[string]model.PolicyRule) []model.TopRule {
	out := make([]model.TopRule, 0, len(ruleTotals))
	if len(ruleTotals) == 0 {
		return out
	}

	var totalBytes uint64
	for _, c := range ruleTotals {
		totalBytes += c.Bytes
	}

	for id, c := range ruleTotals {
		if c.Bytes == 0 && c.Packets == 0 {
			continue
		}
		name, chain, action := id, "", ""
		if r, ok := rulesByID[id]; ok {
			name, chain, action = r.Name, r.Chain, r.Action
		}
		out = append(out, model.TopRule{
			RuleID: id, Name: name, Chain: chain, Action: action,
			Bytes: c.Bytes, Packets: c.Packets, Percent: percentOf(c.Bytes, totalBytes),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].RuleID < out[j].RuleID
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

func percentOf(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(part)/float64(total)*1000) / 10 // one decimal place
}

// hostLookup snapshots DHCP leases + reservations once per GetTrafficDetail
// call, used to enrich IPs with a hostname (plan §2.1: "ชื่อมาจาก DHCP lease
// hostname (fallback = IP)"). Read failures degrade to an empty map (IP-only
// display) rather than failing the whole response.
func (s *TrafficStatsService) hostLookup() (map[string]model.ActiveDhcpLease, map[string]model.DhcpReservation) {
	leaseByIP := make(map[string]model.ActiveDhcpLease)
	if s.dhcp != nil {
		if leases, err := s.dhcp.GetActiveLeases(); err == nil {
			for _, l := range leases {
				leaseByIP[l.IPAddress] = l
			}
		}
	}
	resByIP := make(map[string]model.DhcpReservation)
	if s.repo != nil {
		if reservations, err := s.repo.GetDHCPReservations(); err == nil {
			for _, r := range reservations {
				resByIP[r.IPAddress] = r
			}
		}
	}
	return leaseByIP, resByIP
}

func hostnameFor(ip string, leaseByIP map[string]model.ActiveDhcpLease, resByIP map[string]model.DhcpReservation) (hostname, mac string) {
	if l, ok := leaseByIP[ip]; ok {
		mac = l.MacAddress
		if l.Hostname != "" && l.Hostname != "*" && !strings.EqualFold(l.Hostname, "unknown") {
			return l.Hostname, mac
		}
	}
	if r, ok := resByIP[ip]; ok && r.DeviceName != "" {
		return r.DeviceName, r.MacAddress
	}
	return ip, mac
}

// policyLookup snapshots the current DB policy rules once per
// GetTrafficDetail call, used to enrich a Top Rules row's Name/Chain/Action
// from its rule id. A read failure degrades to an empty map — unresolved
// rows still show their id as Name (see buildTopRules).
func (s *TrafficStatsService) policyLookup() map[string]model.PolicyRule {
	out := make(map[string]model.PolicyRule)
	if s.repo == nil {
		return out
	}
	rules, err := s.repo.GetPolicies()
	if err != nil {
		return out
	}
	for _, r := range rules {
		out[r.ID] = r
	}
	return out
}
