# Statistics → "Traffic" page + per-IP drill-down

> Goal: a new **Statistics → Traffic** page answering "who uses the most bandwidth, and on what",
> with a per-IP drill-down (conversations, protocol/port, bytes per conversation) that works in
> **both directions** (source IP → its destinations, destination IP → who talked to it).
>
> Written: 2026-08-02 · Planned branch (create AFTER owner approval): `feat/statistics-traffic-page`
> Read first: `CLAUDE.md`, `docs/tech_stack_design.md` (§4 firewall / §8 no-SQLite-for-runtime-state),
> `docs/rules_of_work.md` (frontend), `docs/ref/complete/statistics-page-plan.md`,
> `docs/ref/complete/statistics-nav-restructure-plan.md`,
> `docs/ref/complete/dns-query-statistics-drilldown-plan.md` (the drill-down pattern we mirror),
> `docs/ref/complete/dns-stats-tracking-limits-config-plan.md` (the config-key pattern we mirror).

---

## 0. Current state (verified against the working tree, 2026-08-02)

| Piece | State today | Reference |
|---|---|---|
| Data source | conntrack dump via **Netlink** (`vishvananda/netlink` ConntrackTableList) every 10 s + conntrack **DESTROY events** (near-exact accounting), plus nftables rule counters | `backend/internal/kernel/real_traffic_account.go`, `real_conntrack_events.go` |
| Aggregation | RAM-only ring: 288 × 5-minute buckets (24 h). Per bucket: `hostBytes` (by src IP), `dstBytes` (by dst IP), `convBytes` (by `src\|dst\|proto\|dstPort`), `catBytes`, `ruleBytes`, `observed` | `service/traffic_stats.go:99-128` |
| Raw accessor | `GetTrafficBreakdown(window)` already returns the **full, uncapped** `Hosts` / `Dests` / `Convs` maps + `Observed` + `Truncated` + `Accuracy` + `Series` | `service/traffic_stats.go:983` |
| Presentation | `StatisticsService.GetStatistics(window)` cuts every list to `statsTopN = 10`, no filter/sort/drill-down | `service/statistics.go:187-329` |
| API | `GET /api/statistics/traffic?window=1h\|24h` (only this one) | `api/router.go:42`, `api/handlers.go:420` |
| Frontend | `/statistics/overview` shows Top Source Hosts / Top Destinations / Top Conversations as static top-10 cards, no interaction | `pages/StatisticsOverview.tsx` |
| Nav | Sidebar group "Statistics" with `Overview` + `DNS`; `matchPrefix` already supported | `components/app-sidebar.tsx:111-121` |
| Per-bucket caps | `maxTrackedHosts = 500`, `maxTrackedDests = 300`, `maxTrackedConversations = 200` (hardcoded consts) | `service/traffic_stats.go:35-59` |
| Reusable FE pieces | `useStatsWindow` / `StatsWindowSelect` (`components/statistics/DnsStatsShared.tsx`), `HostLabel` / `UpDownLine` (`components/statistics/HostCells.tsx`), `fmtBytes` (`lib/formatBytes.ts`) | — |

**Conclusion: no new kernel capability is needed.** Every number this feature wants is already being
collected in RAM; what is missing is (a) an API that returns more than the top 10 and can be drilled
into by IP, and (b) the two pages. This plan therefore touches **zero** files under `internal/kernel/`.

---

## 1. Design decisions (settled — the developer must not re-litigate these)

### 1.1 "Session" = 4-tuple conversation, not a single TCP connection
`model.FlowSample` carries **no source port** (`types.go:615-627`; src port is hashed into `Key` but
never exposed), so the finest granularity available downstream today is
`(srcIP, dstIP, proto, dstPort)` — which is exactly what `convKey` already stores. The Traffic page
therefore shows **conversations** (e.g. `192.168.1.102 → 173.194.76.94 TCP/443 · 26 MB`), i.e. all
connections between the same pair on the same service are summed into one row.

Exposing true per-connection rows would mean adding `SrcPort` to `FlowSample`, changing `convKey`,
and multiplying conversation cardinality by the number of ephemeral ports — a RAM blow-up on a Pi.
**Out of scope for this plan**; wording in the UI must say "Conversations", never "Sessions",
and a footnote must state that rows are aggregated per destination service.

### 1.2 Filter + sort happen client-side; the API just returns more rows
The API gains a `limit` (default 100, hard max 500 for lists / 300 for a drill-down's conversations),
whitelisted server-side. The client filters (free-text over IP / hostname / domain / proto / port)
and sorts (click a column header) in the browser. Rationale: payloads stay ≤ ~100 KB, no
client-supplied string ever reaches backend aggregation logic (only a whitelisted enum + a
`netip.ParseAddr`-validated IP + a clamped integer), and the code stays in the presentation layer
where it belongs.

### 1.3 Drill-down returns BOTH directions in one response
`GET /api/statistics/traffic/host?ip=…` returns two independent lists:
- `asSource` — conversations where the IP is the flow's SrcIP ("this IP went to these destinations"),
- `asDestination` — conversations where the IP is the flow's DstIP ("these hosts came to this IP").

No `role` guessing on the backend. The page renders two tabs and auto-selects the non-empty one
(preferring `asSource` when both have data); the selected tab is reflected in `?role=src|dst` so the
view is bookmarkable/back-button-safe, following the `?window=` precedent from the DNS pages.

Peer aggregation ("which destinations did this IP talk to, summed across ports") is computed
**client-side** from the returned conversation rows — no extra backend code, and it can never
disagree with the table it is drawn from.

### 1.4 Percent denominators
- Top-lists page: percent of `observedBytes` (unchanged semantics from Overview).
- Drill-down rows: percent of **that IP's own total in the window** (`totals.bytes`), same rule as
  `DNSDomainDrilldown`/`DNSClientDrilldown`. This must be documented in the DTO comment and shown in
  the UI so the two figures are never confused.

### 1.5 Direction convention stays exactly as it is
`TopHost.bytesUp/bytesDown` and `TopConversation.bytesUp/bytesDown` stay **flow-relative**
(`Orig` = src→dst = up, `Reply` = dst→src = down) — the convention fixed by commit `10d53ae`
("stop flipping up/down direction for Top Destinations"). **Do not re-flip anything anywhere.**
In the drill-down, each row additionally gets a `direction` field (`"outbound"`/`"inbound"`)
describing the row *relative to the drilled IP*, so the UI can label columns correctly without
touching the byte numbers.

### 1.6 Tracking caps become config keys (file-only), like the DNS ones
The drill-down is only as complete as the conversation map allows, and today's
`maxTrackedConversations = 200` per 5-minute bucket is low for this feature. Convert the three
consts into file-only `pigate.conf` keys (no CLI flag), mirroring
`dns-stats-max-pairs`/`dns-stats-max-clients` exactly (fail-fast on non-integer, clamp+warn on
out-of-range, appended at the end of `KnownKeys`):

| Const today | New key | New default | Sanity cap (clamp+warn) |
|---|---|---|---|
| `maxTrackedHosts = 500` | `traffic-stats-max-hosts` | 500 (unchanged) | 20000 |
| `maxTrackedDests = 300` | `traffic-stats-max-dests` | 500 (raised) | 20000 |
| `maxTrackedConversations = 200` | `traffic-stats-max-conversations` | 600 (raised) | 20000 |

Worst-case RAM (all buckets saturated, 24 h ring): a conversation entry is ~110 B
(≈45-char key + 16 B `dirBytes` + map overhead) → `600 × 288 ≈ 19 MB`; dests ≈ 10 MB; hosts ≈ 12 MB.
That is the *pathological* ceiling (a saturating scan/P2P burst for a full day), not the normal
footprint of a home LAN. **These numbers must be written into `pigate.conf.example` next to the keys**
so an operator raising them understands the cost. Still RAM-only, still never written to SQLite
(tech_stack_design §8 / SD-card wear).

### 1.7 Nothing existing changes shape
`/api/statistics/traffic`, `/api/dashboard/traffic-detail`, the Overview page's cards, and the
`statsTopN = 10` cut for those responses stay **byte-for-byte identical**. The new endpoints are
purely additive. The only permitted change to an existing page is T-12 (making Overview's host rows
link into the new drill-down), which adds a link and touches no data.

---

## 2. Route / endpoint map

| Method + path | Purpose |
|---|---|
| `GET /api/statistics/traffic/hosts?window=&limit=` | full Top Source Hosts + Top Destinations lists (new) |
| `GET /api/statistics/traffic/host?ip=&window=&limit=` | per-IP drill-down, both directions (new) |
| `/statistics/traffic` | page 1 — two sortable/filterable tables |
| `/statistics/traffic/host/:ip?window=&role=` | page 2 — drill-down |

---

## 3. Tasks

```json
[
  {
    "task_id": "T-01",
    "title": "Backend: DTOs for the Traffic page + drill-down",
    "layer": "model",
    "files": ["backend/internal/model/statistics.go"],
    "instruction": "Add new DTOs to backend/internal/model/statistics.go (do NOT modify TopHost, TopConversation, or TrafficStatistics — they back existing endpoints and must stay byte-for-byte compatible):\n\n1. `TrafficTopHosts` — the GET /api/statistics/traffic/hosts response: Window string `json:\"window\"`, ObservedBytes uint64 `json:\"observedBytes\"`, Accuracy string `json:\"accuracy\"`, Truncated bool `json:\"truncated\"`, Limit int `json:\"limit\"`, Sources []TopHost `json:\"sources\"`, Destinations []TopHost `json:\"destinations\"`, GeneratedAt string `json:\"generatedAt\"`.\n2. `TrafficHostConversation` — one drill-down row: embed the same fields as TopConversation (SrcIP, SrcHostname, DstIP, DstHostname, Proto, DstPort, Bytes, Percent, BytesUp, BytesDown, DstDomain) and ADD two fields: `Direction string `json:\"direction\"`` (\"outbound\" when the drilled IP is the row's SrcIP, \"inbound\" when it is the DstIP) and `PeerDomain string `json:\"peerDomain\"`` (the reverse-DNS-cache domain of the OTHER side of the conversation relative to the drilled IP — for an inbound row that is the SrcIP's domain, which DstDomain cannot express). Prefer composing by embedding `TopConversation` if that keeps the JSON flat and unchanged; otherwise declare the fields explicitly.\n3. `TrafficHostDetail` — the GET /api/statistics/traffic/host response: IP, Hostname, MAC, Domain string; Private bool; Window string; Accuracy string; Truncated bool; Limit int; Found bool `json:\"found\"` (false when the IP appears nowhere in the window's buckets); TotalBytes/TotalBytesUp/TotalBytesDown uint64 (this IP's totals across BOTH directions, i.e. as source plus as destination); PercentOfObserved float64; ObservedBytes uint64 (window total, so the UI can show both denominators); AsSource []TrafficHostConversation `json:\"asSource\"`; AsDestination []TrafficHostConversation `json:\"asDestination\"`; GeneratedAt string.\n\nDocument in comments: (a) Percent on a drill-down row is relative to TotalBytes of the drilled IP, NOT ObservedBytes (plan §1.4); (b) BytesUp/BytesDown stay flow-relative (orig/reply) exactly like TopConversation — Direction is the only IP-relative field (plan §1.5); (c) a row is a 4-tuple conversation, not a single TCP connection (plan §1.1); (d) all of it is RAM-only, never persisted.",
    "acceptance": [
      "backend compiles (`cd backend && go build ./...`)",
      "No existing DTO field was renamed, removed, or re-typed",
      "Each new struct/field carries the doc comments listed above"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-02",
    "title": "Backend: make the three tracking caps configurable (file-only keys)",
    "layer": "service",
    "files": [
      "backend/internal/config/config.go",
      "backend/internal/service/traffic_stats.go",
      "backend/cmd/pigate/main.go",
      "pigate.conf.example"
    ],
    "instruction": "Mirror docs/ref/complete/dns-stats-tracking-limits-config-plan.md exactly for three new FILE-ONLY keys (no CLI flag counterpart): traffic-stats-max-hosts (default 500), traffic-stats-max-dests (default 500), traffic-stats-max-conversations (default 600).\n\n1. internal/config: add Config fields TrafficStatsMaxHosts/TrafficStatsMaxDests/TrafficStatsMaxConversations (int) next to DNSStatsMaxPairs, with the same 'file-only key' comment; add key constants; APPEND the keys at the END of the KnownKeys slice (stable diffs for existing generated files); handle them in applyKey (fail-fast on a non-integer value, exactly like dns-stats-max-pairs) and in the value getter used by Write; add a clamp+warning pass in Resolve for <=0 or > 20000 (declare a maxTrafficStatsCap const with the RAM rationale in its comment).\n2. service/traffic_stats.go: turn maxTrackedHosts/maxTrackedDests/maxTrackedConversations from package consts into per-service fields set from constructor parameters (defaultMaxTrackedHosts/Dests/Conversations consts keep the same names/values for the <=0 fallback, mirroring dns_query_stats.go). Update NewTrafficStatsService's signature to take the three ints, and every use site inside the file (processFlows, addBucket/mergeDirMap calls, and the Truncated check in GetTrafficBreakdown) to read the fields instead of the consts. Do NOT touch maxTrackedFlows or the topN/statsTopN constants.\n3. cmd/pigate/main.go: pass cfg.TrafficStatsMaxHosts/Dests/Conversations into NewTrafficStatsService, with the same comment style used for DNSStatsMaxPairs at main.go:227-229.\n4. pigate.conf.example: document the three keys with their defaults AND the worst-case RAM note from plan §1.6 (e.g. 'each conversation slot costs ~110 bytes and the ring holds 288 five-minute buckets, so 600 => ~19 MB worst case').\n\nAlso update any existing test that constructs NewTrafficStatsService so it still compiles (pass the defaults).",
    "acceptance": [
      "`cd backend && go build ./... && go vet ./...` pass",
      "`cd backend && go test ./internal/config/... ./internal/service/...` pass (existing tests updated only for the new constructor args)",
      "A config file with traffic-stats-max-conversations=abc fails fast; =0 or =999999 clamps to the default with a warning",
      "pigate.conf.example documents all three keys with the RAM note"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-03",
    "title": "Backend: service methods GetTrafficTopHosts + GetTrafficHostDetail",
    "layer": "service",
    "files": ["backend/internal/service/statistics_traffic.go", "backend/internal/service/statistics.go"],
    "instruction": "Create a NEW file backend/internal/service/statistics_traffic.go (keep statistics.go from growing) with two methods on *StatisticsService, both built ONLY on the existing s.traffic.GetTrafficBreakdown(window) + s.traffic.hostLookup() + s.dns.reverseCache — never call the kernel, never add a goroutine, never add a lock of your own:\n\n1. `func (s *StatisticsService) GetTrafficTopHosts(window string, limit int) model.TrafficTopHosts` — re-validate window ({\"1h\",\"24h\"}, default 1h) and clamp limit to 1..500 (default 100 when <=0). Reuse the existing buildTopHosts logic but with a caller-supplied limit: refactor buildTopHosts in statistics.go to take an extra `limit int` parameter and have the two existing call sites pass statsTopN, so there is exactly ONE ranking/sorting implementation (same deterministic sort: bytes desc, then IP asc). Fill Sources from breakdown.Hosts, Destinations from breakdown.Dests, ObservedBytes/Accuracy/Truncated from breakdown, GeneratedAt as UTC RFC3339 like GetStatistics does.\n2. `func (s *StatisticsService) GetTrafficHostDetail(window, ip string, limit int) model.TrafficHostDetail` — window/limit validated the same way (limit clamped 1..300, default 100, applied to EACH of the two lists independently). Iterate breakdown.Convs once, splitting each key with strings.SplitN(key, \"|\", 4) (skip malformed keys defensively, exactly like buildTopConversations does), and bucket the row into asSource when srcIP == ip, into asDestination when dstIP == ip (a same-IP-both-sides row, e.g. loopback, goes into both). Accumulate TotalBytes/Up/Down over the union of both lists BEFORE truncation to `limit`, so the header total never contradicts a truncated table; set Found=false (and leave the slices empty, not nil — use make(...,0)) when the IP appears in neither list AND in neither breakdown.Hosts nor breakdown.Dests. Percent per row = percentOf(row bytes, TotalBytes). Sort each list with the same deterministic tie-breaking as buildTopConversations (bytes desc, then peer IP asc, then dstPort asc), then cut to limit. Resolve hostnames via hostnameFor(leaseByIP,resByIP) and domains via s.dns.reverseCache.LookupMany over the set of IPs actually used (one lookup call for the whole request, like GetStatistics does at statistics.go:205-212) — set DstDomain for the row's DstIP and PeerDomain for the other side relative to the drilled IP. Direction is \"outbound\" for asSource rows, \"inbound\" for asDestination rows.\n\nBoth methods must be pure read-only aggregation, safe to call from an HTTP handler (GetTrafficBreakdown already does its own locking; do not hold anything across it).",
    "acceptance": [
      "`cd backend && go build ./... && go vet ./...` pass",
      "buildTopHosts has exactly one definition, now taking a limit; the two pre-existing callers pass statsTopN so /api/statistics/traffic output is unchanged",
      "Neither new method calls kernel APIs, spawns goroutines, or writes to the DB",
      "Malformed conversation keys are skipped, never panic"
    ],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-04",
    "title": "Backend: HTTP handlers + routes (input validation — sensitive, review carefully)",
    "layer": "api",
    "files": ["backend/internal/api/handlers.go", "backend/internal/api/router.go"],
    "instruction": "🔒 SENSITIVE (client-supplied input reaches the service layer) — mirror the validation style of HandleGetDNSClientDomains (handlers.go:474-499) precisely.\n\n1. handlers.go: add `HandleGetTrafficTopHosts` — window via the existing whitelist helper (reuse dnsStatsWindow, or a shared statsWindow helper if you'd rather rename it; do NOT invent a second whitelist implementation), and `limit` parsed with strconv.Atoi: a missing/empty/unparseable value silently falls back to the default (100) rather than 400ing; a value outside 1..500 is CLAMPED, never rejected. Respond with s.statistics.GetTrafficTopHosts(window, limit).\n2. handlers.go: add `HandleGetTrafficHostDetail` — same window/limit handling (limit clamp 1..300), plus `ip`: REQUIRED, must parse via netip.ParseAddr; on failure return 400 \"invalid ip\" and do not call the service. Normalize with addr.String() before passing down (so 2001:DB8::1 and 2001:db8::1 hit the same bucket keys — note this matches how conntrack samples are stringified). No other query params are read.\n3. router.go: register both under authRoute, next to the existing statistics routes: `GET /api/statistics/traffic/hosts` and `GET /api/statistics/traffic/host`. Note Go 1.22 ServeMux pattern specificity — verify locally that the existing `GET /api/statistics/traffic` route still resolves for the exact path and is not shadowed by the new ones (they are distinct paths, but confirm with a quick curl in mock mode).\n4. Add doc comments in the same style as the neighbouring handlers, referencing this plan file.",
    "acceptance": [
      "`cd backend && go build ./... && go vet ./...` pass",
      "GET /api/statistics/traffic (existing) returns exactly the same JSON as before",
      "?ip=notanip returns 400 and never reaches the service; a missing ip returns 400",
      "?limit=abc / missing limit falls back to the default; ?limit=99999 clamps; no 500s on any input",
      "Both routes require auth (registered via authRoute)"
    ],
    "depends_on": ["T-03"]
  },
  {
    "task_id": "T-05",
    "title": "Backend: unit tests for the two new service methods",
    "layer": "service",
    "files": ["backend/internal/service/statistics_traffic_test.go"],
    "instruction": "Add table-driven tests in the style of the existing statistics_test.go / traffic_stats_test.go (construct a TrafficStatsService with the mock/no kernel, feed synthetic buckets the same way those tests do, wrap in a StatisticsService):\n\n1. GetTrafficTopHosts: returns more than 10 rows when the data has more (proving the statsTopN cut is gone for this endpoint); respects and clamps limit; ranking is deterministic under repeated calls (map-iteration-order safety); Truncated propagates from the breakdown.\n2. GetTrafficHostDetail: an IP that is only a source gets asSource rows and an empty asDestination; an IP that is only a destination gets the mirror image (this is the reverse-lookup requirement); TotalBytes equals the sum over BOTH lists computed before truncation; row Percent sums to ~100% of TotalBytes; Found=false for an IP absent from the window; a malformed conv key in a bucket is skipped without panicking; Direction is outbound/inbound respectively.\n3. A regression test asserting GetStatistics still returns at most statsTopN rows after the buildTopHosts refactor.\n\nNo sleeps, no real kernel, no DB file — follow whatever fixtures the existing tests in this package already use.",
    "acceptance": [
      "`cd backend && go test ./internal/service/...` passes, including `-race`",
      "Tests cover: >10 rows, limit clamp, both drill-down directions, Found=false, malformed key, statsTopN regression"
    ],
    "depends_on": ["T-03"]
  },
  {
    "task_id": "T-06",
    "title": "Docs: OpenAPI contract for the two new endpoints",
    "layer": "api",
    "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml"],
    "instruction": "Document GET /api/statistics/traffic/hosts and GET /api/statistics/traffic/host in BOTH copies (they must stay byte-identical), following the existing /api/statistics/dns* entries as the template: parameters (window enum 1h|24h default 1h; limit integer with its default/min/max and the note that out-of-range values are clamped, not rejected; ip required string for the drill-down, 400 on an unparseable address), response schemas mirroring the T-01 DTOs field-for-field, and prose stating that (a) rows are 4-tuple conversations, not individual TCP connections, (b) drill-down percentages are relative to that IP's own total, (c) all data is RAM-only and lost on restart. Do not modify any existing path or schema.",
    "acceptance": [
      "Both copies are byte-identical and valid YAML",
      "`git diff` shows only additions (no change to existing paths/schemas)"
    ],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-07",
    "title": "Frontend: API client + mock data for the Traffic page",
    "layer": "frontend",
    "files": ["frontend/src/services/trafficStatisticsService.ts"],
    "instruction": "Create frontend/src/services/trafficStatisticsService.ts following the exact structure of services/statisticsService.ts (IS_MOCK_MODE branch first, then fetch against API_BASE_URL, throw on !response.ok).\n\n1. Export TS interfaces mirroring the T-01 Go DTOs field-for-field: TrafficTopHosts, TrafficHostConversation, TrafficHostDetail. Re-use the existing `TopHost` type by importing it from ./statisticsService (do not redeclare it) so the two clients can never drift.\n2. Export `trafficStatisticsService.getTopHosts(window, limit?)` and `.getHostDetail(ip, window, limit?)`. getHostDetail must encodeURIComponent(ip) when building the query string (it is the only place encoding happens; the page passes the raw decoded param).\n3. Mock mode: reuse/extend the mockHosts/mockDests/mockIpDomains data already in statisticsService.ts (export what you need from there rather than copy-pasting, keeping mock LAN IPs consistent with kernel/mock.go: 192.168.1.101/102/105). Generate ~25-40 host rows and ~40 conversation rows so filtering/sorting/pagination are actually exercisable in `yarn dev`, including at least one IPv6 row, one UDP/443 row, one ICMP row, and at least one destination IP (e.g. 173.194.76.94) reachable from TWO different LAN sources so the reverse drill-down has something to show. getHostDetail in mock mode must derive its rows from that same mock conversation list, so drilling into a host shown on page 1 always yields matching data (and an unknown IP yields found=false).",
    "acceptance": [
      "`cd frontend && yarn build` type-checks the file",
      "No duplicate TopHost declaration; mock data reused from statisticsService rather than copied",
      "Mock mode returns >10 host rows and a consistent drill-down for any IP present on page 1, and found=false for an unknown IP"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-08",
    "title": "Frontend: shared filter/sort table primitives for the Traffic pages",
    "layer": "frontend",
    "files": ["frontend/src/components/statistics/TrafficStatsShared.tsx"],
    "instruction": "Create frontend/src/components/statistics/TrafficStatsShared.tsx with the presentation pieces both Traffic pages share. Data fetching and routing stay in the pages; this file exports pure components/hooks only:\n\n1. `useSortableRows<T>(rows: T[], initial: { key: keyof T & string; dir: 'asc'|'desc' })` — returns { rows: sorted, sort, toggle(key) }. Sorting is stable and type-aware (numbers numerically, strings with localeCompare); toggling the active column flips direction, a new column starts at 'desc' for numeric columns and 'asc' for text.\n2. `SortableHead({ label, sortKey, sort, onToggle, align })` — a shadcn <TableHead> rendering the label plus a lucide ArrowUp/ArrowDown/ChevronsUpDown indicator for the active/inactive state; the whole head is a button (keyboard focusable).\n3. `TrafficFilterInput({ value, onChange, placeholder })` — a shadcn <Input> with a lucide Search icon and a clear (X) button when non-empty. Debouncing is NOT needed (filtering is local), do not add a timer.\n4. `useTextFilter<T>(rows, query, fields: ((row: T) => string | number | undefined)[])` — case-insensitive substring match across the supplied accessors, returns the filtered rows; an empty/whitespace query returns rows unchanged.\n5. `TrafficEmptyState({ label })` and `TruncatedWarning()` — copy the visual treatment of StatisticsOverview.tsx's EmptyState and its truncated warning strip (border-warning/20 bg-warning/10 text-warning + TriangleAlert), Thai copy unchanged in spirit.\n\nStyling gates (docs/rules_of_work.md): shadcn primitives from @/components/ui/* only, semantic theme colours (no emerald-*/raw palette), no shadow-* / backdrop-blur-*, dark + light mode both correct.",
    "acceptance": [
      "`cd frontend && yarn build` and `yarn lint` pass for this file",
      "No fetch/router import inside the file",
      "Sorting a numeric column orders numerically (not lexicographically) and is stable for equal values"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-09",
    "title": "Frontend: page 1 — /statistics/traffic (Top Source Hosts + Top Destinations)",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsTraffic.tsx"],
    "instruction": "Create frontend/src/pages/StatisticsTraffic.tsx, default-exporting `StatisticsTraffic`.\n\n- Data: trafficStatisticsService.getTopHosts(window, 100) on mount and whenever window changes; 10_000 ms auto-refresh using the loadRef pattern from StatisticsOverview.tsx:340-366 (background errors swallowed; the error card only when there is no snapshot yet).\n- Window: `useStatsWindow()` + `StatsWindowSelect` from @/components/statistics/DnsStatsShared (URL-driven ?window=), same as the other statistics pages.\n- Header: same visual pattern as StatisticsOverview.tsx:380-400 — size-10 rounded-lg bg-primary/10 text-primary icon tile (use a lucide icon not already in the sidebar, e.g. ArrowLeftRight or Waypoints), h1 \"Traffic\", one-line Thai description ('ดูว่าเครื่องไหนใช้เน็ตมากที่สุด และวิ่งไปหาปลายทางไหน'), and on the right: StatsWindowSelect, the accuracy badge (copy AccuracyBadge from StatisticsOverview — extract it into TrafficStatsShared and use it from BOTH pages rather than duplicating it), and a Refresh button.\n- Body: two Cards side by side (grid grid-cols-1 gap-4 xl:grid-cols-2), 'Top Source Hosts' and 'Top Destinations'. Each holds its OWN TrafficFilterInput (independent filter state) and a Table with sortable columns: Host (hostname/domain + mono IP, reuse HostLabel from @/components/statistics/HostCells), Down, Up, Total, % . Default sort: Total desc. Show at most 50 rows after filtering with a 'แสดง N จาก M รายการ' line under the table (no pagination component — keep it simple).\n- Row click navigates to `/statistics/traffic/host/${encodeURIComponent(h.ip)}?window=${window_}&role=src` for a source row and `...&role=dst` for a destination row. Rows must be keyboard accessible (role=button + tabIndex + onKeyDown Enter/Space, or wrap the host cell in a real <button>).\n- Show TruncatedWarning when the response's truncated flag is set, and a footer line 'Auto-refresh ทุก 10 วินาที · Observed: <fmtBytes(observedBytes)>' like Overview has.\n- Styling gates as in T-08.",
    "acceptance": [
      "`yarn build` + `yarn lint` pass",
      "Both tables filter and sort independently and correctly",
      "Row click navigates with the encoded IP, current window and the right role",
      "No Dialog, no shadow-*/backdrop-blur-*, no hardcoded palette colour"
    ],
    "depends_on": ["T-07", "T-08"]
  },
  {
    "task_id": "T-10",
    "title": "Frontend: page 2 — /statistics/traffic/host/:ip drill-down",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsTrafficHost.tsx"],
    "instruction": "Create frontend/src/pages/StatisticsTrafficHost.tsx, default-exporting `StatisticsTrafficHost`.\n\n- Params: `ip` from useParams() — react-router returns it ALREADY DECODED; pass it raw to trafficStatisticsService.getHostDetail (the service encodes it). Do not double-encode (this is the exact bug documented in docs/ref/complete/statistics-nav-restructure-plan.md Caution 2). Missing/empty param -> <Navigate to=\"/statistics/traffic\" replace />.\n- Window via useStatsWindow(); `role` (\"src\"|\"dst\") via useSearchParams with { replace: true }, defaulting to whichever list is non-empty (prefer 'src' when both are) on first load. Refetch on ip/window change with the `ignore`-flag stale-response guard, plus the 10_000 ms auto-refresh.\n- Header: a Back button (<Button asChild variant=\"outline\" size=\"sm\"> with NavLink to `/statistics/traffic?window=${window_}`, ArrowLeft icon, label 'กลับไปหน้า Traffic'), then an h1 showing the hostname (fallback IP) with the mono IP, the domain badge when data.domain is set, and a LAN/WAN badge from data.private. Right side: StatsWindowSelect + accuracy badge + Refresh.\n- Summary row: 3-4 small Cards — Total (fmtBytes(totalBytes)), Down (totalBytesDown), Up (totalBytesUp), and '% ของทราฟฟิกทั้งหมด' (percentOfObserved) — plus a one-line note that percentages inside the table below are relative to THIS host's total, not the window total (plan §1.4).\n- Main card: shadcn <Tabs> with two triggers — 'ในฐานะต้นทาง (ออกไปหา)' (asSource) and 'ในฐานะปลายทาง (ใครเข้ามาหา)' (asDestination) — each showing its row count in the label, and the active tab written back to ?role=. Each tab body: a TrafficFilterInput + a sortable Table with columns: Peer (the other side: domain/hostname + mono IP, using peerDomain when present), Proto, Port, Down, Up, Total, %. Default sort Total desc. Clicking a peer row navigates to that peer's own drill-down (`/statistics/traffic/host/<encoded peer ip>?window=…&role=` src for an inbound peer / dst for an outbound peer) — this is the cross drill-down.\n- Also render a small 'Top peers' summary above the table: aggregate the currently-visible rows by peer IP client-side (sum bytes) and show the top 5 as HostBar-style rows (reuse the bar markup from StatisticsOverview's HostBar via TrafficStatsShared if you extract it; otherwise a simple div bar with bg-primary).\n- found === false -> a Card with 'ไม่พบข้อมูลของ IP นี้ในช่วงเวลาที่เลือก' plus a hint to switch the window to 24h. Loading -> Skeletons like the other pages. Error with no data -> destructive text card.\n- Styling gates as in T-08.",
    "acceptance": [
      "`yarn build` + `yarn lint` pass",
      "Reloading (F5) a drill-down URL directly renders it, including an IPv6 IP and a ?role=dst deep link",
      "Both tabs filter/sort independently; the reverse direction (ใครเข้ามาหา) is populated for a destination IP",
      "The IP is never double-encoded; cross drill-down between peers works both ways"
    ],
    "depends_on": ["T-07", "T-08"]
  },
  {
    "task_id": "T-11",
    "title": "Frontend: routes, sidebar entry, header title",
    "layer": "frontend",
    "files": [
      "frontend/src/App.tsx",
      "frontend/src/components/app-sidebar.tsx",
      "frontend/src/components/site-header.tsx"
    ],
    "instruction": "1. App.tsx: import the two new pages and add them inside the EXISTING `<Route path=\"statistics\">` group (which already lives under ProtectedRoute + ShellLayout): `<Route path=\"traffic\" element={<StatisticsTraffic />} />` and `<Route path=\"traffic/host/:ip\" element={<StatisticsTrafficHost />} />`. Touch nothing else (no guard, no catch-all, no existing redirect).\n2. app-sidebar.tsx: add `{ path: \"/statistics/traffic\", label: \"Traffic\", icon: <a lucide icon not already used in this file — verify the export exists in the installed lucide-react before using it>, matchPrefix: true }` to the existing 'Statistics' group, positioned between 'Overview' and 'DNS'. Change nothing else about the sidebar.\n3. site-header.tsx: add \"/statistics/traffic\": \"Traffic Statistics\" to the exact-match TITLES map and a PREFIX_TITLES entry [\"/statistics/traffic/host/\", \"Traffic Statistics — Host\"] so the drill-down never falls back to 'PiGate Controller'. Do not put the raw IP in the header title.",
    "acceptance": [
      "`yarn build` + `yarn lint` pass",
      "Sidebar shows Overview / Traffic / DNS in the Statistics group and Traffic stays highlighted on the drill-down route",
      "Header shows a meaningful title on both new routes",
      "No other route, guard or menu item changed"
    ],
    "depends_on": ["T-09", "T-10"]
  },
  {
    "task_id": "T-12",
    "title": "Frontend: link Overview's host rows into the new drill-down",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsOverview.tsx", "frontend/src/components/statistics/HostCells.tsx"],
    "instruction": "Small additive change so the existing Overview page becomes an entry point to the drill-down (and the feature is discoverable):\n\n- In StatisticsOverview.tsx's TopHostsCard, make each host row's label clickable, navigating to `/statistics/traffic/host/${encodeURIComponent(h.ip)}?window=${window_}&role=src` for the Top Source Hosts card and `...&role=dst` for the Top Destinations card (pass the role via a new prop on TopHostsCard). Same for each Top Conversations row: clicking the Source cell opens that IP with role=src, clicking the Destination cell opens it with role=dst.\n- Implement the click affordance the same way the Top Queried Domains card already does it (a real <button> with cursor-pointer + hover:text-primary hover:underline + a Thai title tooltip) — prefer adding an OPTIONAL onClick prop to HostLabel in HostCells.tsx over restructuring the card markup, so a HostLabel without the prop stays exactly as it renders today.\n- Change NOTHING else on the Overview page: no data, no card order, no wording, no API call.",
    "acceptance": [
      "`yarn build` + `yarn lint` pass",
      "Overview renders identically except the host/conversation labels are now links",
      "Clicking a Top Destinations row lands on that IP's drill-down already showing the 'ใครเข้ามาหา' tab",
      "HostLabel without onClick renders byte-for-byte as before (Dashboard/other users unaffected)"
    ],
    "depends_on": ["T-10"]
  },
  {
    "task_id": "T-13",
    "title": "Docs: README + config docs touch-up",
    "layer": "db",
    "files": ["README.md", "docs/ref/todo/statistics-traffic-page-plan.md"],
    "instruction": "Update README: (a) the Feature Status table gains the Statistics → Traffic page (frontend + backend Completed once this plan lands), (b) the Configuration File section mentions the three new file-only keys (traffic-stats-max-hosts / -max-dests / -max-conversations) with a one-line 'RAM-only ring, worst-case cost' note pointing at pigate.conf.example. Do not restructure the README. Finally, once QA passes, move this plan file from docs/ref/todo/ to docs/ref/complete/ (git mv) — that move is the LAST step of the whole plan, done only after Final Acceptance is green.",
    "acceptance": [
      "README documents the new page and the three keys",
      "Plan file moved to docs/ref/complete/ only after final acceptance"
    ],
    "depends_on": ["T-02", "T-11"]
  }
]
```

---

## 4. Cautions for the developer

1. **No kernel/`exec.Command` work at all.** If a task seems to need a new kernel call, stop and
   report — the plan is wrong, not the backend. Everything comes from the existing conntrack ring.
2. **Never persist any of this to SQLite.** RAM-only ring, exactly like today (tech_stack_design §8).
3. **Do not flip up/down anywhere** (§1.5). `bytesUp/bytesDown` stay flow-relative; `direction` is the
   only IP-relative field. This is a bug the project already fixed once (commit `10d53ae`).
4. **Existing responses must not change.** `/api/statistics/traffic` and
   `/api/dashboard/traffic-detail` are covered by tests and by the Overview/Dashboard pages — the
   `buildTopHosts` refactor in T-03 is the only shared-code edit and must keep `statsTopN` behaviour
   for those callers.
5. **T-04 is security-sensitive** (the only client input path): whitelist the window, clamp the limit,
   `netip.ParseAddr` the IP, and never interpolate a client string into a map key without that
   validation. Ask for an extra review pass on this task.
6. **Double-encoding trap** (T-10): `useParams` returns a decoded value; the service encodes. Encode
   only when *building* a link.
7. **Truncation honesty**: when `truncated` is set, the UI must say so — this data is a bounded
   sample, and silently showing an incomplete "who used the most" ranking is worse than a warning.
8. **Privacy**: this page shows per-device browsing behaviour. Keep the RAM-only/"lost on restart"
   note visible on both pages, in the spirit of the DNS statistics privacy note.
9. **Git**: work on `feat/statistics-traffic-page` (created only after the owner approves this plan),
   merge into `main` via PR, and do not create commits unless the owner asks.
10. **No testing between tasks.** Finish T-01..T-13, then run Final Acceptance once.

---

## 5. Final Acceptance (run ONCE, after every task above is complete)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... && go vet ./... && go test ./... -race all pass",
    "cd frontend && yarn build (tsc -b + vite build) and yarn lint pass with no new errors",
    "GET /api/statistics/traffic?window=1h returns exactly the same JSON shape/limits as before this change (Overview page visually unchanged apart from the new links)",
    "GET /api/statistics/traffic/hosts?window=1h&limit=100 returns >10 rows when the device has that many talkers, with sources+destinations, observedBytes, accuracy, truncated, generatedAt",
    "GET /api/statistics/traffic/host?ip=<a LAN IP>&window=1h returns asSource rows with proto/port/bytesUp/bytesDown, found=true, and totals that equal the sum over both lists",
    "GET /api/statistics/traffic/host?ip=<a WAN destination IP> returns asDestination rows listing the LAN hosts that talked to it (the reverse view the owner asked for)",
    "Invalid input handled safely: ?ip=notanip -> 400, missing ip -> 400, ?limit=abc -> default, ?limit=99999 -> clamped, ?window=bogus -> 1h; no 500 and no panic for any of them",
    "Both new endpoints reject unauthenticated requests (401), like every other /api/statistics route",
    "pigate.conf: setting traffic-stats-max-conversations=50 visibly lowers the tracked conversation count and logs no warning; =abc fails startup fast; =0 and =999999 clamp to the default with a warning line",
    "Sidebar: Statistics group shows Overview / Traffic / DNS; Traffic stays highlighted on /statistics/traffic/host/...; header title is meaningful on both new routes",
    "/statistics/traffic renders Top Source Hosts and Top Destinations; typing in each card's filter narrows only that table; clicking each column header sorts asc/desc correctly (numeric columns numerically)",
    "Clicking a source row opens its drill-down on the 'ในฐานะต้นทาง' tab; clicking a destination row opens the drill-down on the 'ในฐานะปลายทาง' tab",
    "The drill-down shows, per row: peer (domain/hostname + IP), protocol, port, down/up/total bytes formatted in KB/MB/GB, and a % of that host's own total; the header states that this % is host-relative",
    "Cross drill-down works both ways (host -> peer -> back to a host) and browser Back/Forward behave; F5 on any drill-down URL (including an IPv6 IP and ?role=dst) renders correctly",
    "Switching the window to 24h on page 1 carries 24h into the drill-down, and the Back button returns to page 1 still on 24h",
    "An IP with no data in the window shows the found=false empty state, not a crash or an empty table with no explanation",
    "Auto-refresh (10 s) updates both pages without resetting the user's filter text, sort column, or active tab",
    "truncated=true (reproducible by setting traffic-stats-max-conversations very low) shows the warning strip on the affected page",
    "Both pages work in -mock=true mode with >10 rows and a working drill-down, and in frontend mock mode (yarn dev)",
    "Dark and light themes both render both new pages correctly; no <Dialog>, no shadow-*/backdrop-blur-*, no hardcoded palette colour was introduced",
    "git diff shows zero changes under backend/internal/kernel/ and no new exec.Command anywhere",
    "docs/openapi.yaml and frontend/public/openapi.yaml are identical and document both new endpoints; README and pigate.conf.example document the three new keys"
  ]
}
```

---

## 6. สรุปสำหรับเจ้าของโปรเจกต์ (ภาษาไทย)

- **ไม่ต้องเก็บข้อมูลใหม่เลย** — ข้อมูลที่หน้านี้ต้องใช้ (ใครใช้เยอะ / วิ่งไปไหน / โปรโตคอล-พอร์ต / กี่ MB)
  ระบบเก็บอยู่แล้วใน ring buffer ใน RAM (conntrack ผ่าน Netlink ทุก 10 วิ + event ตอน flow จบ, 288 ช่วง ×
  5 นาที = 24 ชม.) แผนนี้จึง **ไม่แตะ `internal/kernel/` เลย** และไม่เขียนอะไรลง SQLite เพิ่ม (กัน SD card สึก)
- ของที่ขาดคือ API ที่คืนได้มากกว่า Top 10 และ API เจาะราย IP → เพิ่ม 2 endpoint ใหม่
  (`/api/statistics/traffic/hosts`, `/api/statistics/traffic/host?ip=`) + หน้าใหม่ 2 หน้า
  (`/statistics/traffic` และ `/statistics/traffic/host/:ip`) เมนู "Traffic" อยู่ในหมวด Statistics
  ระหว่าง Overview กับ DNS
- **filter/sort ทำฝั่งเบราว์เซอร์** (backend ส่งมา 100 แถว) — ปลอดภัยกว่าเพราะไม่มี string จากผู้ใช้
  วิ่งเข้าไปในตรรกะ backend เลย มีแค่ window (whitelist), limit (clamp), ip (ตรวจด้วย netip.ParseAddr)
- **Drill-down ได้ 2 ทางในหน้าเดียว**: แท็บ "ในฐานะต้นทาง (ออกไปหา)" และ "ในฐานะปลายทาง (ใครเข้ามาหา)"
  ตรงตามที่ขอ — เลือก IP ปลายทางก็เห็นว่าใครวิ่งไปหามันบ้าง คลิกต่อจาก peer ไป peer ได้เรื่อย ๆ
- **ข้อจำกัดที่ต้องรับทราบ 1 ข้อ**: หนึ่งแถว = "คู่สนทนา" `(ต้นทาง, ปลายทาง, โปรโตคอล, พอร์ตปลายทาง)`
  ไม่ใช่การเชื่อมต่อ TCP รายเส้น เพราะ `FlowSample` ไม่มี source port (ถ้าอยากได้ราย session จริง ๆ ต้องแก้
  โครงสร้างข้อมูลระดับ kernel layer และจำนวนแถวจะระเบิดหลายเท่า — ผมแนะนำไม่ทำในรอบนี้ แต่ถ้าต้องการจริง
  บอกได้ครับ จะแยกเป็นแผนต่างหาก)
- **มีเรื่องต้องตัดสินใจ**: ค่าเพดานการติดตามต่อ 5 นาที ตอนนี้ conversations = 200 ซึ่งน้อยไปสำหรับหน้านี้
  แผนเสนอย้ายไปเป็นค่าใน `pigate.conf` (แบบเดียวกับ `dns-stats-max-pairs`) และขยับ default เป็น
  hosts 500 / dests 500 / conversations 600 → กรณีเลวร้ายสุด (โดนสแกนถล่มเต็ม 24 ชม.) กิน RAM รวม
  ~40 MB ปกติจะน้อยกว่านั้นมาก ถ้าอยากประหยัดกว่านี้ตั้ง 300 ก็ได้ แต่ตารางจะไม่ครบบ่อยขึ้น
- แบ่งเป็น **13 tasks** (backend 6 / frontend 6 / docs 1) ให้ ai-developer ทำเรียงตาม `depends_on`
  **ยังไม่ทดสอบระหว่างทาง** ทำครบแล้วค่อยส่ง ai-qa ทดสอบตาม Final Acceptance รอบเดียว
- ทำบน branch `feat/statistics-traffic-page` (สร้างหลังอนุมัติ) เข้า `main` ผ่าน PR เท่านั้น
