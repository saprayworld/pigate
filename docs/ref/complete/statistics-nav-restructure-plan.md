# Frontend: "Statistics" sidebar group + DNS statistics as its own page (route drill-down)

> Frontend-only restructuring. **No backend change at all** — every API used here already exists
> (`/api/statistics`, `/api/statistics/dns`, `/api/statistics/dns/domain`, `/api/statistics/dns/client`).
>
> Written: 2026-07-31 · Work branch: `feat/frontend-statistics-dns-nav` (already created from latest `main`)
> Reference docs: `docs/rules_of_work.md` (shadcn-only, no hardcoded brand colors, flat design,
> Drawer-for-forms / Dialog-for-confirmation), `docs/ref/todo/dns-query-statistics-drilldown-plan.md`
> (the plan that originally built the tab we are now promoting to a page).

---

## 0. Goal & scope

**Goal:**
1. A new sidebar group **"Statistics"**, positioned directly **below the Dashboard item** (i.e. group index 1,
   before "Network").
2. The existing **Statistics** menu item (currently `/logs/statistics`, page `pages/Statistics.tsx`, traffic
   top-N: Top Sources / Destinations / Conversations / Denied / Queried Domains) is **renamed "Overview"** and
   moved into the new group.
3. A new menu item **"DNS"** in the same group — the functionality currently living in the "สถิติ" tab of the
   DNS Server page (`components/dns/DnsStatisticsTab.tsx`) becomes a **standalone page**. The tab is removed
   from `pages/DnsServer.tsx` entirely (not deprecated-in-place — see §2).
4. **Behaviour change:** clicking a row (Top Domains / Top Source Hosts) no longer opens a `<Dialog>`
   drill-down; it **navigates to a dedicated route/page**.

**Out of scope:**
- Any backend/Go change, any change to the statistics data model or API contract.
- Redesigning the Overview page contents (only its title/route/menu label change).
- Adding new statistics data (e.g. blocked-domain stats) — additive features stay out.
- Changing the Dashboard page.

---

## 1. Current state (verified against the working tree on 2026-07-31)

| Piece | Current state | Reference |
|---|---|---|
| Sidebar groups | 5 groups built from a local `groups: NavGroup[]` array; group 0 is untitled and holds only Dashboard | `frontend/src/components/app-sidebar.tsx:50-96` |
| Sidebar active state | **exact match only**: `location.pathname === item.path` | `app-sidebar.tsx:129` |
| Statistics menu item | `{ path: "/logs/statistics", label: "Statistics", icon: BarChart3 }` inside group "Log & Report" | `app-sidebar.tsx:82` |
| Routes | Nested under `<Route path="/" element={<ProtectedRoute><ShellLayout/></ProtectedRoute>}>`; statistics is `logs/statistics` | `frontend/src/App.tsx:136-189`, item at `:174` |
| Overview page | `pages/Statistics.tsx` — h1 literal "Statistics", window `1h/24h` in local state, 10 s auto-refresh | `frontend/src/pages/Statistics.tsx:360-499` |
| DNS statistics | `components/dns/DnsStatisticsTab.tsx` — 2 tables + `<Dialog>` drill-down, gated by an `active` prop, `onGoToSettings` callback | `frontend/src/components/dns/DnsStatisticsTab.tsx:63-439` |
| Its only consumer | `<TabsContent value="stats">` in the DNS Server page; tabs are controlled via `activeTab` state | `frontend/src/pages/DnsServer.tsx:80, 770-776, 1167-1175` |
| API client | `dnsStatisticsService.getDNSStatistics / getDomainClients / getClientDomains` — already encode their own query params, mock-mode aware | `frontend/src/services/dnsStatisticsService.ts:89-238` |
| Header title | Static exact-path map `TITLES`, fallback "PiGate Controller"; has `/logs/statistics`? **No** (missing today) | `frontend/src/components/site-header.tsx:10-33` |
| Router deps | `react-router-dom` v6-style API in use (`Routes/Route/Navigate/NavLink/useLocation/useNavigate`) | `App.tsx:2`, `app-sidebar.tsx:2` |

**Conclusion:** everything needed already exists client-side; the work is purely (a) moving/renaming nav entries,
(b) converting one tab component into three route pages, (c) replacing dialog state with navigation.

---

## 2. Technical decisions (fixed — developer must not re-litigate these)

### 2.1 Route map

| Route | Page file | Purpose |
|---|---|---|
| `/statistics` | — | `<Navigate to="/statistics/overview" replace />` |
| `/statistics/overview` | `pages/StatisticsOverview.tsx` (renamed from `pages/Statistics.tsx`) | traffic top-N (unchanged content) |
| `/statistics/dns` | `pages/StatisticsDns.tsx` (new) | Top Domains + Top Source Hosts tables |
| `/statistics/dns/domain/:domain` | `pages/StatisticsDnsDomain.tsx` (new) | hosts that queried `:domain` |
| `/statistics/dns/client/:client` | `pages/StatisticsDnsClient.tsx` (new) | domains queried by `:client` |
| `/logs/statistics` | — | `<Navigate to="/statistics/overview" replace />` (keep, bookmarks/old docs point here) |

All of them stay **inside the existing `ProtectedRoute` + `ShellLayout` block** — no new top-level route.

### 2.2 Time window (`1h` / `24h`) lives in the URL query string

`?window=1h|24h` on all four statistics pages, read/written through `useSearchParams` (default `1h`, any other
value falls back to `1h`). Reason: when a row click becomes a navigation, the drill-down page must inherit the
window the user was looking at, and the Back link must return to the same window — a URL param does this with
no shared state/context. Overview also adopts it for consistency (harmless, makes the window bookmarkable).

### 2.3 Path params carry the raw (decoded) key

- Build links with `encodeURIComponent(...)`: `` to={`/statistics/dns/domain/${encodeURIComponent(d.domain)}?window=${win}`} ``.
- Read with `useParams()`, which returns the **already-decoded** value. Pass that decoded value straight to
  `dnsStatisticsService.getDomainClients/getClientDomains` — **those services encode the value themselves**
  (`dnsStatisticsService.ts:184, 231`). Encoding it again before calling them is a double-encoding bug
  (`www.youtube.com` would be looked up as `www%2Eyoutube.com`).
- `client` may legitimately be the literal string `unknown` (the backend/mock treat it as a real bucket) — do
  not special-case it away; only render it as "ไม่ทราบต้นทาง" like today's code does.
- Empty/missing param → `<Navigate to="/statistics/dns" replace />`.

### 2.4 The "สถิติ" tab is deleted, not deprecated

`DnsServer.tsx` is already ~1,780 lines; leaving a dead tab creates two places for the same feature to drift.
Remove `TabsTrigger value="stats"`, its `TabsContent`, and the import — then delete
`components/dns/DnsStatisticsTab.tsx` (it has exactly one importer, verified). Users reach the feature from
the sidebar. To keep the old empty-state affordance ("go to the Settings tab to enable query logging"),
`DnsServer.tsx` gains **`?tab=` deep-link support** so the new page can link to `/network/dns-server?tab=settings`.

### 2.5 No `<Dialog>` anywhere in this feature

Per `docs/rules_of_work.md` §1.3.1 Dialog is reserved for confirmations; the drill-down was already a borderline
use, and the whole point of this change is route-based drill-down. Nothing here needs `modal={false}` or a Drawer.

### 2.6 Shared presentation goes into one module, not copy-paste

Three pages render the same two table shapes. Extract them once into
`frontend/src/components/statistics/DnsStatsShared.tsx` so the list page and the two detail pages stay visually
identical and a later tweak is a one-file change.

---

## 3. Tasks

```json
[
  {
    "task_id": "T-01",
    "title": "Create shared DNS-statistics presentation module",
    "layer": "frontend",
    "files": ["frontend/src/components/statistics/DnsStatsShared.tsx"],
    "instruction": "Create a new file frontend/src/components/statistics/DnsStatsShared.tsx holding the pieces the three DNS statistics pages share. Export exactly:\n\n1. `export type StatsWindow = \"1h\" | \"24h\"`.\n2. `export function useStatsWindow(): [StatsWindow, (w: StatsWindow) => void]` — backed by react-router `useSearchParams`; reads the `window` search param, returns \"1h\" for a missing/unrecognised value, and the setter writes the param back with `{ replace: true }` so changing the window does not spam browser history.\n3. `export function StatsWindowSelect({ value, onChange }: { value: StatsWindow; onChange: (w: StatsWindow) => void })` — the exact shadcn Select currently used in DnsStatisticsTab.tsx:172-180 (className `h-9 w-28 text-xs bg-background`, items \"1 ชั่วโมง\" / \"24 ชั่วโมง\").\n4. `export function DomainStatsTable({ rows, emptyLabel, onRowClick }: { rows: TopDomain[]; emptyLabel: string; onRowClick?: (domain: string) => void })` — copy the Domain/Type/Count/% table markup from DnsStatisticsTab.tsx:235-273 verbatim (same column widths, same `Badge variant=\"outline\"` query-type chip using border-primary/20 bg-primary/10 text-primary). When `onRowClick` is provided the row gets `className=\"cursor-pointer hover:bg-muted/50\"`, an onClick calling it with the row's domain, and the existing `title` tooltip text; when it is omitted the row must be non-interactive (`hover:bg-transparent`, no cursor-pointer).\n5. `export function ClientStatsTable({ rows, emptyLabel, onRowClick }: { rows: DNSClientStat[]; emptyLabel: string; onRowClick?: (ip: string, hostname: string) => void })` — same treatment for the Host/Count/% table (DnsStatisticsTab.tsx:282-323), including the `ip === \"unknown\"` -> \"ไม่ทราบต้นทาง\" rendering and the hostname-plus-mono-IP layout.\n6. `export function DnsStatsPrivacyNote()` — the `text-[10px] text-muted-foreground` paragraph from DnsStatisticsTab.tsx:329-332, text unchanged.\n7. `export function DnsStatsTruncatedWarning()` — the warning strip from DnsStatisticsTab.tsx:194-199 (border-warning/20 bg-warning/10 text-warning + TriangleAlert), text unchanged.\n\nImport `TopDomain` / `DNSClientStat` types from `@/services/dnsStatisticsService`. Use only shadcn primitives from `@/components/ui/*`. No shadow-*/backdrop-blur-* classes, no hardcoded emerald/brand colours (theme variables only), and keep both light and dark mode working by using semantic tokens exactly as the source code does. This module must contain no data fetching and no router navigation (callbacks only).",
    "acceptance": [
      "File exists and exports the 7 symbols listed above with those exact names",
      "No `fetch`, no service import other than types, no useNavigate/NavLink inside this file",
      "`yarn build` type-checks this file (it may be unused until T-02)",
      "No shadow-*/backdrop-blur-* class and no hardcoded emerald-* colour class"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-02",
    "title": "New page: DNS statistics list (/statistics/dns)",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsDns.tsx"],
    "instruction": "Create frontend/src/pages/StatisticsDns.tsx — a default-exported page component `StatisticsDns` that replaces components/dns/DnsStatisticsTab.tsx as a full page. Port the data logic from that file 1:1 (load via dnsStatisticsService.getDNSStatistics(window), 10_000 ms auto-refresh interval with the loadRef pattern, background-refresh errors swallowed, error card only when there is no snapshot yet) with these differences:\n\n- Remove the `active` prop and the hasLoaded/lazy gating — a page always loads on mount.\n- Window state comes from `useStatsWindow()` (T-01), not local useState.\n- Delete ALL drill-down dialog code (state, effects, `<Dialog>` markup). Row clicks now navigate: use `useNavigate()` and go to `/statistics/dns/domain/${encodeURIComponent(domain)}?window=${window_}` for a domain row and `/statistics/dns/client/${encodeURIComponent(ip)}?window=${window_}` for a client row.\n- Add a page header in the same visual style as pages/Statistics.tsx:406-434: a size-10 rounded-lg bg-primary/10 text-primary icon tile (BarChart3), h1 \"DNS Statistics\" (text-lg font-bold tracking-tight) plus a one-line Thai description, with StatsWindowSelect + a Refresh Button (variant=\"outline\" size=\"sm\", spinning RefreshCw while loading) on the right. Keep the small \"อัปเดตล่าสุด <time>\" line derived from stats.generatedAt.\n- Render the two tables with `DomainStatsTable` / `ClientStatsTable` from T-01, each wrapped in the existing Card with CardTitle \"Top Domains\" / \"Top Source Hosts\", inside `grid grid-cols-1 gap-4 lg:grid-cols-2`, both with onRowClick provided.\n- Keep the `stats.enabled === false` empty state (same Thai copy), but its action button must now be a link to the DNS Server settings tab: `<Button asChild size=\"sm\" variant=\"outline\"><NavLink to=\"/network/dns-server?tab=settings\">ไปที่หน้า DNS Server > Settings</NavLink></Button>` (the ?tab= support is added in T-08). Remove the onGoToSettings prop entirely.\n- Keep DnsStatsTruncatedWarning and DnsStatsPrivacyNote in the same positions as the old tab.\n\nStyling rules as in T-01 (shadcn only, theme colour variables, flat, dark+light).",
    "acceptance": [
      "Page compiles and default-exports the component",
      "No import of `@/components/ui/dialog` and no Dialog usage anywhere in the file",
      "Row clicks call navigate() with encodeURIComponent'd keys and carry the current window as ?window=",
      "Auto-refresh interval is 10_000 ms and unconditional on mount (no `active` prop)"
    ],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-03",
    "title": "New page: domain drill-down (/statistics/dns/domain/:domain)",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsDnsDomain.tsx"],
    "instruction": "Create frontend/src/pages/StatisticsDnsDomain.tsx, default-exporting `StatisticsDnsDomain`. Behaviour:\n\n- Read `domain` with useParams(). react-router returns it ALREADY DECODED — pass it as-is to dnsStatisticsService.getDomainClients(domain, window); do NOT encodeURIComponent it first (the service encodes internally at dnsStatisticsService.ts:184). If the param is missing or empty after trim, render `<Navigate to=\"/statistics/dns\" replace />`.\n- Window via `useStatsWindow()` (T-01); refetch whenever domain or window changes. Use the same stale-response guard idea as DnsStatisticsTab.tsx:137-144 (an `ignore` flag in the effect cleanup) so a late response from a previous domain/window never overwrites the current one. Keep a 10_000 ms auto-refresh consistent with the list page (background errors swallowed, error shown only when there is no data yet).\n- Header: a Back control first — `<Button asChild variant=\"outline\" size=\"sm\"><NavLink to={`/statistics/dns?window=${window_}`}><ArrowLeft className=\"size-4\" /> กลับไปหน้า DNS Statistics</NavLink></Button>` — then an h1 showing `Source Hosts ที่ค้นหา <span className=\"font-mono\">{domain}</span>` (truncate long domains), with StatsWindowSelect + Refresh on the right, mirroring the T-02 header layout.\n- Body: a Card containing `ClientStatsTable` (T-01) with rows = data.clients, emptyLabel \"ไม่พบเครื่องที่ค้นหาโดเมนนี้ในช่วงเวลานี้\", and onRowClick navigating to `/statistics/dns/client/${encodeURIComponent(ip)}?window=${window_}` (cross drill-down). Show a centered Loader2 while first-loading and a text-destructive message on error, same as the old dialog did.\n- Also render DnsStatsPrivacyNote at the bottom.\n\nStyling rules as in T-01.",
    "acceptance": [
      "Uses useParams + useStatsWindow; never double-encodes the domain before calling the service",
      "Missing/empty param redirects to /statistics/dns",
      "Back link preserves the current ?window value",
      "Client rows navigate to the client drill-down route"
    ],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-04",
    "title": "New page: client drill-down (/statistics/dns/client/:client)",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsDnsClient.tsx"],
    "instruction": "Create frontend/src/pages/StatisticsDnsClient.tsx, default-exporting `StatisticsDnsClient`. Mirror T-03 exactly, but for the client direction:\n\n- `client` param from useParams (already decoded — pass straight to dnsStatisticsService.getClientDomains, which encodes internally at dnsStatisticsService.ts:231). The literal value `unknown` is a valid bucket and must be fetched normally, only its LABEL renders as \"ไม่ทราบต้นทาง\".\n- Title: `โดเมนที่ <hostname> (<ip>) ค้นหา`, using the `hostname` field from the response when non-empty (fall back to the IP alone), and \"ไม่ทราบต้นทาง\" when the key is `unknown` — same wording logic as DnsStatisticsTab.tsx:343-350.\n- Body: Card + `DomainStatsTable` with rows = data.domains, emptyLabel \"ไม่พบโดเมนที่เครื่องนี้ค้นหาในช่วงเวลานี้\", onRowClick navigating to `/statistics/dns/domain/${encodeURIComponent(domain)}?window=${window_}`.\n- Same Back button, window select, refresh, loader/error/privacy-note treatment as T-03.\n\nStyling rules as in T-01.",
    "acceptance": [
      "Compiles; client param passed undecorated to the service",
      "`unknown` client still triggers a fetch and renders the ไม่ทราบต้นทาง label",
      "Domain rows navigate to the domain drill-down route with the window preserved"
    ],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-05",
    "title": "Rename the traffic statistics page to Overview",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsOverview.tsx"],
    "instruction": "Rename frontend/src/pages/Statistics.tsx to frontend/src/pages/StatisticsOverview.tsx using `git mv` (preserve history). Inside it:\n- Rename the default-exported component from `Statistics` to `StatisticsOverview`.\n- Change the h1 text from \"Statistics\" to \"Overview\" (line ~412). Leave the description line, the icon tile, and every card/logic untouched.\n- Adopt the shared window hook for URL-driven window state: replace `const [window_, setWindow] = useState<\"1h\"|\"24h\">(\"1h\")` with `const [window_, setWindow] = useStatsWindow()` from @/components/statistics/DnsStatsShared, leaving the rest of the load/refresh logic exactly as-is. Optionally swap its inline window <Select> for `StatsWindowSelect` — only if the rendered markup stays visually identical.\nDo not change any other behaviour, card, wording or API call on this page. Note: App.tsx still imports the old path and will not compile until T-06 fixes it — that is expected, do not touch App.tsx here.",
    "acceptance": [
      "pages/Statistics.tsx no longer exists; pages/StatisticsOverview.tsx exists with the renamed component",
      "h1 reads \"Overview\"",
      "Window state comes from useStatsWindow(); no other logic/card changed"
    ],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-06",
    "title": "Wire the new routes in App.tsx",
    "layer": "frontend",
    "files": ["frontend/src/App.tsx"],
    "instruction": "Update frontend/src/App.tsx routing, keeping everything inside the existing `<Route path=\"/\" element={<ProtectedRoute><ShellLayout/></ProtectedRoute>}>` block:\n\n1. Replace `import Statistics from \"@/pages/Statistics\"` with `import StatisticsOverview from \"@/pages/StatisticsOverview\"`, and add imports for StatisticsDns, StatisticsDnsDomain, StatisticsDnsClient.\n2. Remove `<Route path=\"statistics\" element={<Statistics />} />` from the `logs` group and put a redirect in its place: `<Route path=\"statistics\" element={<Navigate to=\"/statistics/overview\" replace />} />` (keeps old bookmarks and older docs links working).\n3. Add a new sibling group after the dashboard route:\n```tsx\n<Route path=\"statistics\">\n  <Route index element={<Navigate to=\"/statistics/overview\" replace />} />\n  <Route path=\"overview\" element={<StatisticsOverview />} />\n  <Route path=\"dns\" element={<StatisticsDns />} />\n  <Route path=\"dns/domain/:domain\" element={<StatisticsDnsDomain />} />\n  <Route path=\"dns/client/:client\" element={<StatisticsDnsClient />} />\n</Route>\n```\nDo not touch the login/change-password/api-docs routes, the guards, or the catch-all.",
    "acceptance": [
      "No remaining import of @/pages/Statistics",
      "/logs/statistics redirects to /statistics/overview; /statistics redirects to /statistics/overview",
      "All four statistics pages render under ShellLayout behind ProtectedRoute",
      "`yarn build` compiles"
    ],
    "depends_on": ["T-02", "T-03", "T-04", "T-05"]
  },
  {
    "task_id": "T-07",
    "title": "Sidebar: new Statistics group + prefix-aware active state",
    "layer": "frontend",
    "files": ["frontend/src/components/app-sidebar.tsx"],
    "instruction": "Edit frontend/src/components/app-sidebar.tsx:\n\n1. Add an optional field to the NavItem type: `matchPrefix?: boolean`.\n2. Change the active check at line ~129 to `const isActive = location.pathname === item.path || (item.matchPrefix === true && location.pathname.startsWith(item.path + \"/\"))`. Keep it opt-in so existing sibling paths (e.g. /network/dns vs /network/dns-server) cannot start matching each other.\n3. Insert a NEW group as the SECOND entry of the `groups` array (immediately after the untitled Dashboard group, before \"Network\"):\n```ts\n{\n  title: \"Statistics\",\n  items: [\n    { path: \"/statistics/overview\", label: \"Overview\", icon: BarChart3 },\n    { path: \"/statistics/dns\", label: \"DNS\", icon: <pick an icon>, matchPrefix: true },\n  ],\n},\n```\nFor the DNS icon choose a lucide-react export that is NOT already used in the sidebar (Globe/Server/Network/Activity are taken) — e.g. `ChartPie` or `ChartColumnBig`; verify the name actually exists in the installed lucide-react version before using it, and fall back to `Globe` if not.\n4. Remove `{ path: \"/logs/statistics\", label: \"Statistics\", icon: BarChart3 }` from the \"Log & Report\" group (that group keeps Forward Traffic / Local Traffic / System Events).\n5. Keep the BarChart3 import (still used by Overview) and drop any import that becomes unused.",
    "acceptance": [
      "Sidebar renders 6 groups; \"Statistics\" sits directly under Dashboard and above \"Network\"",
      "\"Log & Report\" no longer contains a Statistics item",
      "The DNS item stays highlighted on /statistics/dns/domain/... and /statistics/dns/client/...",
      "No other menu item's active behaviour changes"
    ],
    "depends_on": ["T-06"]
  },
  {
    "task_id": "T-08",
    "title": "DnsServer page: drop the สถิติ tab, add ?tab= deep-link support",
    "layer": "frontend",
    "files": ["frontend/src/pages/DnsServer.tsx"],
    "instruction": "Edit frontend/src/pages/DnsServer.tsx:\n\n1. Remove the `<TabsTrigger value=\"stats\">สถิติ</TabsTrigger>` (line ~774) and the whole `<TabsContent value=\"stats\">…</TabsContent>` block (lines ~1167-1175), plus the `import { DnsStatisticsTab } from \"@/components/dns/DnsStatisticsTab\"` (line 52). The remaining tabs are zones / blocked / settings, and \"zones\" stays the default.\n2. Add deep-link support so other pages can land on a specific tab: read the `tab` search param via `useSearchParams` and use it as the INITIAL value of the existing `activeTab` state, whitelisted to exactly \"zones\" | \"blocked\" | \"settings\" (anything else, including the now-dead \"stats\", falls back to \"zones\"). Keep `activeTab` as the single source of truth for <Tabs value=...>; when the user switches tabs by clicking, also reflect it into the search param with `{ replace: true }` — or, if that turns out to fight the existing state, at minimum honour the param on first mount. Do not introduce a second state variable that can drift from activeTab.\n3. Do not change anything else on this page (zones, blocked domains, settings, drawers, apply flow all untouched).",
    "acceptance": [
      "No reference to DnsStatisticsTab or the \"stats\" tab value remains in DnsServer.tsx",
      "/network/dns-server?tab=settings opens directly on the Settings tab; /network/dns-server (no param) opens on Zones & Records",
      "An unknown ?tab= value falls back to zones without crashing",
      "Zones/Blocked/Settings functionality is otherwise byte-for-byte unchanged"
    ],
    "depends_on": ["T-02"]
  },
  {
    "task_id": "T-09",
    "title": "Delete the now-orphaned DnsStatisticsTab component",
    "layer": "frontend",
    "files": ["frontend/src/components/dns/DnsStatisticsTab.tsx"],
    "instruction": "Delete frontend/src/components/dns/DnsStatisticsTab.tsx with `git rm`. Before deleting, grep the frontend for `DnsStatisticsTab` and confirm there is no remaining importer (after T-08 there should be none). If frontend/src/components/dns/ becomes empty, leave the directory alone (git will drop it). Do not delete dnsStatisticsService.ts — the new pages use it.",
    "acceptance": [
      "The file is gone and `grep -r DnsStatisticsTab frontend/src` returns nothing",
      "`yarn build` still compiles"
    ],
    "depends_on": ["T-08"]
  },
  {
    "task_id": "T-10",
    "title": "Site header titles for the new routes",
    "layer": "frontend",
    "files": ["frontend/src/components/site-header.tsx"],
    "instruction": "Edit frontend/src/components/site-header.tsx so the top bar shows a sensible title on the new pages instead of the generic \"PiGate Controller\" fallback:\n\n1. Add to the exact-match TITLES map: \"/statistics/overview\": \"Statistics Overview\", \"/statistics/dns\": \"DNS Statistics\".\n2. Add a small ordered prefix fallback used only when the exact map misses — e.g. a `PREFIX_TITLES: [string, string][]` with [\"/statistics/dns/domain/\", \"DNS Statistics — Domain\"] and [\"/statistics/dns/client/\", \"DNS Statistics — Source Host\"], resolved with `.find(([p]) => location.pathname.startsWith(p))` before falling back to \"PiGate Controller\". Keep the exact map as the first lookup so existing pages are unaffected.\n3. Do not add the raw domain/IP to the header title (it can be long and is already the page h1).\nNothing else on this component changes.",
    "acceptance": [
      "Header shows the right title on all four statistics routes",
      "Every pre-existing route's header title is unchanged"
    ],
    "depends_on": ["T-06"]
  },
  {
    "task_id": "T-11",
    "title": "Refresh stale /logs/statistics references in the API docs text",
    "layer": "frontend",
    "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml"],
    "instruction": "In both docs/openapi.yaml and frontend/public/openapi.yaml (they must stay byte-identical), update the descriptive text that says the traffic statistics endpoint \"Backs the standalone Statistics page (`/logs/statistics`)\" (around line 388 in each) to reference `/statistics/overview` instead, and mention that /api/statistics/dns* now back the standalone `/statistics/dns` page rather than a tab on the DNS Server page, if such wording exists there. This is a DESCRIPTION-ONLY edit: do not change any path, schema, parameter, required field or response shape — the API contract is unchanged by this work.",
    "acceptance": [
      "Only description/comment prose changed; `git diff` shows no path/schema/parameter modifications",
      "The two openapi.yaml copies remain identical to each other"
    ],
    "depends_on": []
  }
]
```

---

## 4. Cautions for the developer

1. **Do not touch anything under `backend/`.** If a change seems to need a backend edit, stop and report — the
   plan is wrong, not the backend.
2. **Double-encoding trap** (Caution most likely to bite): `useParams` returns decoded values and
   `dnsStatisticsService` encodes internally. Encode only when *building* a link, never before *calling* the service.
3. **Privacy copy must survive the move.** The RAM-only / personal-data notice and the opt-in empty state are
   deliberate (see `docs/ref/todo/dns-query-statistics-drilldown-plan.md`) — carry both onto the new pages.
4. **No `<Dialog>`** in the new pages, and no `modal={false}` anywhere in this change.
5. **Styling gates** (`docs/rules_of_work.md`): shadcn primitives only, no `shadow-*` / `backdrop-blur-*`,
   no hardcoded `emerald-*` brand colours (use `text-primary`, `bg-primary/10`, `border-primary/20`), and both
   dark and light themes must look right.
6. **Git**: work on `feat/frontend-statistics-dns-nav`, do not push to `main`, and do not create commits unless
   the project owner explicitly asks. Use `git mv` / `git rm` for the rename and the deletion.
7. The build is expected to be temporarily broken between T-05 and T-06 (App.tsx import). That is fine —
   we validate once at the end, not per task.

---

## 5. Final Acceptance (run ONCE, after every task above is complete)

```json
{
  "final_acceptance": [
    "cd frontend && yarn build succeeds (tsc -b + vite build) with no TypeScript errors and no unused-import errors",
    "cd frontend && yarn lint passes with no new errors",
    "Sidebar shows a 'Statistics' group directly below the Dashboard item and above 'Network', containing exactly 'Overview' and 'DNS'; 'Log & Report' no longer has a Statistics item",
    "Clicking Overview goes to /statistics/overview and renders the same traffic cards as before (Top Sources / Destinations / Conversations / Denied / Queried Domains) with the h1 now reading 'Overview'",
    "Visiting the old /logs/statistics redirects to /statistics/overview; visiting /statistics redirects to /statistics/overview",
    "Clicking DNS goes to /statistics/dns and renders Top Domains + Top Source Hosts with real data in -mock=true mode (backend run with -mock=true -allow-dev-cors, or frontend mock mode)",
    "Clicking a Top Domains row NAVIGATES to /statistics/dns/domain/<encoded-domain>?window=... (URL changes, no Dialog appears) and lists the hosts that queried it",
    "Clicking a Top Source Hosts row NAVIGATES to /statistics/dns/client/<encoded-ip>?window=... and lists the domains that host queried",
    "Cross drill-down works: from a domain page clicking a host opens that host's page, and vice versa",
    "Switching the window to 24h on /statistics/dns, then drilling in, keeps 24h on the detail page; the Back button returns to /statistics/dns still on 24h",
    "Browser Back/Forward navigate correctly through list -> detail -> detail, and reloading (F5) any detail URL directly renders that drill-down (no blank page, no crash), including a client key of 'unknown'",
    "The DNS sidebar item stays highlighted while on the domain/client detail routes",
    "The top header bar shows a meaningful title on all four statistics routes (never 'PiGate Controller')",
    "The DNS Server page (/network/dns-server) shows exactly three tabs — Zones & Records, Blocked Domains, Settings — defaults to Zones & Records, and all three still work (add/edit/delete zone, record, blocked domain, save settings, Apply DNS Zones)",
    "/network/dns-server?tab=settings opens directly on the Settings tab; an unknown ?tab= value falls back to Zones & Records",
    "With DNS query logging disabled, /statistics/dns shows the opt-in empty state and its button navigates to /network/dns-server?tab=settings",
    "grep -r 'DnsStatisticsTab' frontend/src returns no results and the file is deleted",
    "No <Dialog> / modal={false} was introduced by this change; no shadow-* or backdrop-blur-* class and no hardcoded emerald-* colour class appears in any file touched by this plan",
    "Both dark and light themes render all four statistics pages correctly (text readable, cards/borders visible)",
    "git diff shows zero changes under backend/ ; the only non-frontend change is description prose in the two openapi.yaml copies"
  ]
}
```

---

## 6. สรุปสำหรับเจ้าของโปรเจกต์ (ภาษาไทย)

- งานนี้เป็น **frontend ล้วน ๆ ไม่แตะ backend เลย** — API สถิติ DNS ทั้ง 3 ตัวมีอยู่ครบแล้ว
- โครงเมนูใหม่: หมวด **Statistics** อยู่ใต้ Dashboard มี 2 เมนูคือ **Overview** (ของเดิมที่ชื่อ Statistics ใน
  หมวด Log & Report ย้ายมา, เปลี่ยนเส้นทางเป็น `/statistics/overview`) และ **DNS** (`/statistics/dns`)
- แท็บ "สถิติ" ในหน้า DNS Server **ถูกลบทิ้ง** และย้ายมาเป็นหน้าเต็มแทน (ไฟล์ `DnsStatisticsTab.tsx` ถูกลบ) —
  เพิ่มการเปิดหน้า DNS Server ตรงแท็บ Settings ด้วย `?tab=settings` เพื่อคง flow "ไปเปิดสวิตช์เก็บสถิติ" ไว้เหมือนเดิม
- คลิกแถวในตารางจะ **เปิดหน้าใหม่ (route ใหม่)** แทน Dialog: `/statistics/dns/domain/:domain` และ
  `/statistics/dns/client/:client` กด Back กลับได้ กด F5 ก็ยังอยู่หน้าเดิม และ bookmark ได้
- ช่วงเวลา 1h/24h ย้ายไปอยู่ใน URL (`?window=`) เพื่อให้ drill-down กับปุ่มย้อนกลับใช้ช่วงเวลาเดียวกันเสมอ
- ลิงก์เก่า `/logs/statistics` ยัง redirect ไปหน้าใหม่ให้ ไม่มี bookmark ใครพัง
- แผนแบ่งเป็น 11 task ให้ ai-developer ทำเรียงตาม `depends_on` **ยังไม่ต้องทดสอบระหว่างทาง** — ทำครบแล้วค่อยส่ง
  ai-qa ทดสอบตาม "Final Acceptance" ในหัวข้อ 5 รอบเดียว
- ทำงานบน branch `feat/frontend-statistics-dns-nav` และเข้า `main` ผ่าน PR เท่านั้น
