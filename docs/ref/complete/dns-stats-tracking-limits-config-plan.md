# Plan: make the DNS query-stats per-bucket caps configurable from `pigate.conf`

Branch: `feat/dns-stats-tracking-limits-config` (already created from latest `main`).
Scope: backend only. **No frontend changes.**

## 1. Goal

Turn the two hard-coded per-bucket caps of the DNS query-statistics ring into
values that can be tuned from the bootstrap config file:

| Current constant (`backend/internal/service/dns_query_stats.go`) | New config key | New default |
| --- | --- | --- |
| `maxTrackedDNSPairs = 1200` (line 30) | `dns-stats-max-pairs` | **2400** (raised from 1200) |
| `maxTrackedDNSClients = 200` (line 34) | `dns-stats-max-clients` | **200** (unchanged) |

**Deliberate exception to the existing pattern:** every key in `internal/config`
today mirrors a CLI flag in `cmd/pigate/main.go` 1:1. These two keys are
**file-only** — no `flag.Int(...)` is registered for them. This is intentional
(owner's decision: they are RAM-tuning knobs for an appliance, not day-to-day
CLI switches) and must be documented in the package doc comment, otherwise the
next contributor will "fix" the asymmetry.

Consequences of file-only that the implementer must keep in mind:
- `flag.Visit` in `resolveConfig` can never produce these keys, so the
  `explicit` layer will never contain them — precedence collapses to
  *code default < config file*. That is correct and needs no special casing.
- `config.Write` still emits them, so the auto-created
  `/var/lib/pigate/pigate.conf` will show them with their defaults, ready to
  edit. This is the discoverability mechanism — do not skip them in `Write`.

## 2. Design decisions (settled — do not re-litigate during implementation)

1. **Key names:** `dns-stats-max-pairs`, `dns-stats-max-clients` (kebab-case,
   matching the existing key style). Config struct fields: `DNSStatsMaxPairs`,
   `DNSStatsMaxClients` (`int`).
2. **Value validation — split by failure kind:**
   - *Non-integer* (`dns-stats-max-pairs=abc`) → **fail-fast error** from
     `applyKey`, exactly like `port`/`https-port` today. It is a syntax error
     and the existing package contract already fails fast on those.
   - *Out-of-range* (`<= 0`, or absurdly large) → **clamp to the default +
     warning**, NOT a fatal. Rationale: this is a cosmetic statistics knob on a
     firewall appliance; a typo'd `0` must never turn into a boot loop that
     takes the gateway's network off the air. `Resolve` already returns a
     `warnings []string` slice that `main.go` logs, so the mechanism exists —
     do the range check as a post-processing step at the end of `Resolve`
     (after both layers have been applied), not inside `applyKey` (which has no
     way to report a warning).
   - Upper sanity caps (RAM guard, see §3): `dns-stats-max-pairs` > 50000 or
     `dns-stats-max-clients` > 10000 is clamped to the default with the same
     warning path.
3. **Plumbing:** pass the two resolved values as explicit parameters to
   `service.NewStatisticsService(...)` and store them on the `dnsQueryStats`
   struct (set once at construction, never mutated afterwards — so the existing
   reads under `s.dns.mu.RLock()` stay race-free without any new locking).
   No global/package-level mutable state, no setter (a setter would let the cap
   change mid-flight while buckets built under the old cap are still in the
   ring, which would make `Truncated` meaningless).
4. **Defensive clamp in the service too:** `NewStatisticsService` clamps a
   `<= 0` argument to the package default constant. This is defense-in-depth for
   direct callers (tests, future call sites), not the primary validation path.
   The service package must **not** import `internal/config` for this — keep
   the literal defaults in both places with a cross-reference comment in each.

## 3. RAM context (for the doc comments / example file wording)

The ring is 288 buckets (24h at 5-minute granularity). The pair cap is
*per bucket*, so worst case tracked pairs = `dns-stats-max-pairs × 288`
(2400 × 288 ≈ 690k map entries ≈ 70-100 MB of Go map + string overhead in the
absolute worst case, i.e. only under a sustained unique-domain flood; normal
home/office traffic sits orders of magnitude below the cap). This is why the
value is capped at all, and why the example file must warn that raising it
raises the worst-case RAM ceiling roughly linearly.

---

## 4. Tasks

```json
{
  "task_id": "T-01",
  "title": "Add the two file-only keys to internal/config",
  "layer": "backend/config",
  "files": ["backend/internal/config/config.go"],
  "instruction": "In backend/internal/config/config.go: (1) Add fields DNSStatsMaxPairs int and DNSStatsMaxClients int to the Config struct, with a comment noting they are the only keys with NO matching CLI flag (file-only tuning knobs). (2) Defaults(): DNSStatsMaxPairs: 2400, DNSStatsMaxClients: 200 — add a comment 'keep in sync with defaultMaxTrackedDNSPairs/defaultMaxTrackedDNSClients in internal/service/dns_query_stats.go'. (3) Add consts keyDNSStatsMaxPairs = \"dns-stats-max-pairs\" and keyDNSStatsMaxClients = \"dns-stats-max-clients\"; append both to orderedKeys AT THE END (so existing generated files keep a stable diff). (4) applyKey: strconv.Atoi both, returning the same 'invalid int for %q' fail-fast error as keyPort on a parse failure — do NOT range-check here. (5) keyValue: strconv.Itoa both. (6) At the END of Resolve, after both apply() calls succeed, add a range-sanity pass: if cfg.DNSStatsMaxPairs <= 0 || cfg.DNSStatsMaxPairs > maxDNSStatsPairsCap, append a warning like 'dns-stats-max-pairs=%d out of range (1..%d), using default %d' and reset it to defaults.DNSStatsMaxPairs; same for DNSStatsMaxClients with maxDNSStatsClientsCap. Define const maxDNSStatsPairsCap = 50000 and maxDNSStatsClientsCap = 10000 with a comment explaining they are a RAM guard (cap is per 5-minute bucket, ring holds 288 buckets). Use `defaults` (the function argument), not Defaults(), as the fallback source. (7) Update the package doc comment at the top of the file: the line 'Defaults() -> code defaults (1:1 with cmd/pigate/main.go flags)' is no longer universally true — state explicitly that dns-stats-max-pairs/dns-stats-max-clients are intentionally file-only (no CLI flag) and that this asymmetry is by design, not an oversight. Also note the two-tier validation rule (type error = fatal, range error = clamp + warning). Do not touch any other key's behavior.",
  "acceptance": [
    "go build ./... passes",
    "Config has the two new int fields; Defaults() returns 2400/200",
    "orderedKeys/KnownKeys include both new keys, appended last",
    "Resolve clamps <=0 and >cap values to the defaults and appends one warning per clamped key; a non-integer value still returns an error",
    "No CLI flag is registered anywhere for these two keys"
  ],
  "depends_on": []
}
```

```json
{
  "task_id": "T-02",
  "title": "Update/extend config package tests",
  "layer": "backend/config",
  "files": ["backend/internal/config/config_test.go"],
  "instruction": "In backend/internal/config/config_test.go: (1) TestKnownKeys currently asserts len(keys) == 12 — update to 14 and additionally assert that both new keys are present. (2) Add subtests under TestResolve: 'dns stats keys default when absent' (Resolve(Defaults(), nil, nil) yields 2400/200); 'file overrides dns stats keys' (dns-stats-max-pairs=5000, dns-stats-max-clients=500 are honored, no warnings); 'non-integer dns stats value fails fast' (dns-stats-max-pairs=abc returns an error); 'zero/negative clamps to default with a warning' (dns-stats-max-pairs=0 and dns-stats-max-clients=-1 each produce exactly one warning and the default value, and no error); 'absurdly large clamps to default with a warning' (dns-stats-max-pairs=999999). (3) TestWriteParseRoundTrip: set non-default values for both new fields on the cfg under test so the round-trip actually covers Write+Parse+Resolve for them (pick in-range values, e.g. 3000/300). TestWriteParseRoundTripDefaults needs no change but must still pass.",
  "acceptance": [
    "go test ./internal/config/... passes",
    "Tests cover: default, file override, type error, <=0 clamp, over-cap clamp, and Write round-trip of both new keys"
  ],
  "depends_on": ["T-01"]
}
```

```json
{
  "task_id": "T-03",
  "title": "Turn the two service constants into per-instance fields",
  "layer": "service",
  "files": ["backend/internal/service/dns_query_stats.go"],
  "instruction": "In backend/internal/service/dns_query_stats.go: (1) In the top const block, replace `maxTrackedDNSPairs = 1200` and `maxTrackedDNSClients = 200` with `defaultMaxTrackedDNSPairs = 2400` and `defaultMaxTrackedDNSClients = 200`, and rewrite their comments: they are now the fallback defaults used when the caller passes a non-positive value, and the effective values come from the bootstrap config keys dns-stats-max-pairs / dns-stats-max-clients (config.Defaults() must stay in sync — say so). Keep the existing explanation of WHY the caps exist (per-bucket flood/OOM guard, both caps must be enforced or the cap is pointless) and add the RAM note: the cap is per 5-minute bucket and the ring holds 288 buckets, so worst-case tracked pairs is cap x 288. (2) Add fields `maxPairs int` and `maxClients int` to the dnsQueryStats struct, documented as set once at construction and never mutated afterwards — which is why the existing reads under mu.RLock() need no extra synchronization. (3) Replace every use of the old constants with the struct fields: line ~157 (b.pairCount >= maxTrackedDNSPairs), ~160 (len(b.clientCount) >= maxTrackedDNSClients), ~246 (domainSnapshot truncated check, both caps), ~320 (GetDNSQueryStatistics truncated check, both caps), ~381 (GetDNSDomainClients), ~446 (GetDNSClientDomains). Grep for maxTrackedDNS to be sure none is left. Do NOT change any other behavior: queries++ stays uncapped, the existing-pair-always-increments rule stays, the Truncated semantics stay identical.",
  "acceptance": [
    "No references to maxTrackedDNSPairs/maxTrackedDNSClients remain in non-test code",
    "All six cap checks read s.dns.maxPairs / s.dns.maxClients",
    "go build ./... passes (test files may still be red at this point — T-06 fixes them)"
  ],
  "depends_on": []
}
```

```json
{
  "task_id": "T-04",
  "title": "Extend NewStatisticsService to accept the two limits",
  "layer": "service",
  "files": ["backend/internal/service/statistics.go"],
  "instruction": "In backend/internal/service/statistics.go, change the signature to NewStatisticsService(traffic *TrafficStatsService, repo *db.Repository, dhcp kernel.DhcpManager, maxDNSPairs, maxDNSClients int) *StatisticsService. Inside, clamp each argument defensively: if the value is <= 0, fall back to defaultMaxTrackedDNSPairs / defaultMaxTrackedDNSClients (defense-in-depth for direct callers; the authoritative validation lives in config.Resolve — say so in the comment). Store them in the constructed &dnsQueryStats{...} as maxPairs/maxClients alongside reverseCache. Update the constructor's doc comment to explain where the values come from (bootstrap config keys dns-stats-max-pairs / dns-stats-max-clients, wired in cmd/pigate/main.go). Do not import internal/config from this package.",
  "acceptance": [
    "Signature updated; dnsQueryStats is constructed with both limits populated and never zero",
    "service package does not import internal/config"
  ],
  "depends_on": ["T-03"]
}
```

```json
{
  "task_id": "T-05",
  "title": "Wire the config values through main.go",
  "layer": "backend/cmd",
  "files": ["backend/cmd/pigate/main.go"],
  "instruction": "In backend/cmd/pigate/main.go: (1) Update the NewStatisticsService call at ~line 223 to pass cfg.DNSStatsMaxPairs and cfg.DNSStatsMaxClients, with a short comment that these come from the file-only bootstrap keys dns-stats-max-pairs / dns-stats-max-clients (no CLI flag by design). (2) Add two log lines next to the existing startup cfg dump (~lines 87-98), e.g. log.Printf(\"[Main] DNS Stats Max Pairs/Clients per bucket: %d / %d\", cfg.DNSStatsMaxPairs, cfg.DNSStatsMaxClients). (3) Do NOT register CLI flags for them, and do NOT add anything to the flag.Visit exclusion list in resolveConfig (they can never appear there). (4) Verify the comment above the flag block ('Their default values here must stay 1:1 with config.Defaults()') is still accurate and, if needed, add half a sentence noting the two file-only keys have no flag counterpart.",
  "acceptance": [
    "go build ./... passes",
    "Running the binary logs the two resolved values at startup",
    "No new flag.Int/flag.String registrations"
  ],
  "depends_on": ["T-01", "T-04"]
}
```

```json
{
  "task_id": "T-06",
  "title": "Update service-layer tests for the new signature/fields",
  "layer": "service",
  "files": [
    "backend/internal/service/statistics_test.go",
    "backend/internal/service/dns_query_stats_test.go"
  ],
  "instruction": "(1) backend/internal/service/statistics_test.go line ~14: the newTestStatisticsService helper must pass the two new arguments. Pass defaultMaxTrackedDNSPairs, defaultMaxTrackedDNSClients so existing tests exercise the production defaults. (2) backend/internal/service/dns_query_stats_test.go TestStatisticsService_DNSPairCap (lines ~129-172) references maxTrackedDNSPairs three times — change those to read s.dns.maxPairs (the instance value) and update the comment that says 'maxTrackedDNSPairs per bucket'. The test floods 5000 unique pairs; the default cap is now 2400, so the 'at least one bucket hit the cap' assertion still holds — verify, do not weaken the assertion. Also check the comment at line ~139 referring to maxTrackedDNSClients. (3) Add ONE new test, TestStatisticsService_DNSPairCap_Configurable: construct a StatisticsService directly via NewStatisticsService(traffic, repo, dhcp, 10, 3) (reuse whatever newTestTrafficStatsService gives you, mirroring newTestStatisticsService), enable DNS logging, flood with unique (domain, client) pairs, and assert the bucket's pairCount never exceeds 10 and len(clientCount) never exceeds 3, while TotalQueries still equals the number of events recorded. (4) statistics_test.go TestStatisticsService_DomainRingCap (line ~188) floods 5000 unique domains from a single 'unknown' client — with the cap at 2400 it must still report DNSTruncated=true; confirm and fix its stale comment mentioning 'maxTrackedDomains'. Do not otherwise change existing test expectations.",
  "acceptance": [
    "go test ./internal/service/... passes, including with -race",
    "The new configurable-cap test proves a non-default cap is actually honored"
  ],
  "depends_on": ["T-04"]
}
```

```json
{
  "task_id": "T-07",
  "title": "Update the api-layer test call sites",
  "layer": "api",
  "files": ["backend/internal/api/handlers_test.go"],
  "instruction": "backend/internal/api/handlers_test.go calls service.NewStatisticsService(trafficStatsService, repo, dhcp) at the end of two long NewServer(...) lines (~line 70 and ~line 561). Add the two new arguments to both, using the production defaults (literal 2400, 200 is fine here since the service package's unexported constants are not visible from package api — add a trailing comment '// dns-stats-max-pairs / dns-stats-max-clients defaults'). No other change to this file.",
  "acceptance": ["go test ./internal/api/... passes"],
  "depends_on": ["T-04"]
}
```

```json
{
  "task_id": "T-08",
  "title": "Write pigate.conf.example from scratch",
  "layer": "docs/ops",
  "files": ["pigate.conf.example"],
  "instruction": "The repo-root file pigate.conf.example is currently empty and untracked. Write it in full. Requirements: (a) header comment block explaining what the file is (a documented sample of /var/lib/pigate/pigate.conf, the bootstrap key=value config read via -config), the syntax (one key=value per line, '#' comments, no quoting, value may contain '='), and the precedence rule 'built-in default < config file < CLI flag explicitly passed' plus the real-world gotcha that install.sh's ExecStart still passes -mock=false -db=... -https-port=443, so editing those three keys in the file has no effect while they remain in the unit. (b) Every key currently in config.orderedKeys, IN THAT ORDER, each preceded by a one-or-two-line comment describing what it does, and shown with its code default value: port=2479, db=pigate.db, mock=true, mock-from-real=false, disable-edit=false, allow-edit-system-routes=false, enable-edit-system-route=false, prioritize-kernel-routes=false, docker-compat=false, https-port=0, tls-dir= (empty = <dir of db>/tls), allow-dev-cors=false. Take each description from the flag help strings in backend/cmd/pigate/main.go so the wording matches. Note next to mock/db/https-port that install.sh writes production values (mock=false, db=/var/lib/pigate/pigate.db, https-port=443). (c) Then the two new keys, dns-stats-max-pairs=2400 and dns-stats-max-clients=200, under a small section header noting these are FILE-ONLY (no CLI flag). Their comments must explain: what a bucket is (5-minute bucket, ring = 288 buckets = 24h), that the cap is per bucket, that it bounds tracked (domain, client) pairs / distinct clients to stop a unique-domain flood from growing RAM without limit, that the total query count is NOT affected by the cap (only the per-domain/per-client breakdown is, and the UI flags it as Truncated), that raising it raises worst-case RAM roughly linearly (cap x 288 entries), and that an out-of-range value (<=0 or above the internal sanity cap) falls back to the default with a warning in the journal while a non-numeric value is a fatal startup error. (d) Verify against backend/internal/config/config.go that no key is missing or misspelled — a wrong key name here would only produce an 'unknown config key' warning at runtime, which is exactly the kind of silent breakage this file is supposed to prevent. Do NOT change install.sh (its create-if-missing block intentionally writes only the production overrides; unspecified keys fall back to code defaults).",
  "acceptance": [
    "pigate.conf.example contains all 14 keys, in orderedKeys order, each with a default value and an explanatory comment",
    "Every key name matches a const in backend/internal/config/config.go exactly",
    "The file's own content, if parsed by config.Parse+Resolve, would produce zero warnings and zero errors"
  ],
  "depends_on": ["T-01"]
}
```

```json
{
  "task_id": "T-09",
  "title": "Sync the affected docs",
  "layer": "docs",
  "files": [
    "README.md",
    "docs/ref/complete/dns-system-design.md",
    "backend/internal/model/statistics.go"
  ],
  "instruction": "(1) README.md 'Configuration File' section (~lines 182-185): the sentence 'Keys are the CLI flag names without the leading -' is now inaccurate. Amend it to say that most keys are CLI flag names without the leading '-', plus two file-only tuning keys (dns-stats-max-pairs, dns-stats-max-clients) that have no flag counterpart, and point readers at pigate.conf.example in the repo root for the fully commented sample. Keep the existing precedence paragraph as is. (2) docs/ref/complete/dns-system-design.md line ~367 states 'New caps per bucket: maxTrackedDNSPairs = 1200 and maxTrackedDNSClients = 200' — update it to the new default 2400/200 and note they are now configurable via dns-stats-max-pairs / dns-stats-max-clients in pigate.conf. (3) backend/internal/model/statistics.go line ~154 has a comment referencing maxTrackedDNSPairs/maxTrackedDNSClients by their old constant names — reword it to refer to the configurable per-bucket caps so the reference does not dangle. Do not edit docs/ref/todo/dns-query-statistics-drilldown-plan.md (it is a historical plan record).",
  "acceptance": [
    "No doc still claims every config key mirrors a CLI flag",
    "No stale reference to the 1200 default or to the removed constant names outside historical plan docs"
  ],
  "depends_on": ["T-03"]
}
```

---

## 5. Cautions for the implementer

1. **Do not add CLI flags** for the two new keys. If a flag is added, `flag.Visit`
   will start injecting them into the `explicit` layer and the "file-only"
   contract silently disappears.
2. `Resolve`'s range check must run **after both layers are applied**, and must
   use the `defaults` parameter as the fallback (not a package-level literal),
   so a caller passing custom defaults still behaves sanely.
3. The `dnsQueryStats.maxPairs/maxClients` fields must be **write-once at
   construction**. Do not add a setter or an API endpoint; the caps are compared
   against buckets that were filled under them, so changing them at runtime
   would corrupt the `Truncated` semantics.
4. `queries++` must remain outside every cap check (window totals stay accurate
   even for a truncated bucket) — this is an existing invariant with a
   dedicated test; do not "tidy" it while editing the surrounding lines.
5. Existing `Truncated` behavior must not change for the default configuration
   other than the threshold moving 1200 → 2400.
6. This change touches no auth, firewall-rule generation, D-Bus or Netlink path,
   so it is not in the sensitive tier — but the config parser is user-input
   handling: keep the fail-fast-on-garbage behavior and never let an untrusted
   value reach `make(map, n)`-style allocation sizing (there is none today;
   do not introduce one).

---

## 6. Final Acceptance (run once, after ALL tasks are done)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... succeeds",
    "cd backend && go vet ./... reports nothing new",
    "cd backend && go test ./... passes fully (in particular internal/config, internal/service, internal/api)",
    "cd backend && go test ./internal/service/... -race passes",
    "grep -r 'maxTrackedDNSPairs\\|maxTrackedDNSClients' backend/ returns no hits (constants fully removed/renamed)",
    "Fresh-boot default: run ./pigate-backend -mock=true -db=<tmp>/pigate.db -config=<tmp>/pigate.conf on a non-existent config path => it is a hard error (explicit -config must exist); with no -config in a sandbox where the default path is writable, the generated file contains dns-stats-max-pairs=2400 and dns-stats-max-clients=200",
    "Startup log shows the resolved DNS stats caps, and with dns-stats-max-pairs=5000 in the config file the logged value is 5000 (proves file-only key takes effect without any CLI flag)",
    "With dns-stats-max-pairs=0 in the config file, the process still starts, logs a warning, and uses 2400",
    "With dns-stats-max-pairs=abc in the config file, the process exits with a clear 'invalid int' fatal error",
    "With dns-stats-max-pairs=999999, the process starts, warns about the out-of-range value, and uses 2400",
    "An unknown key such as dns-stats-max-pair=100 still only produces an 'unknown config key' warning (no crash)",
    "Functional check in mock mode: enable DNS query logging, generate DNS activity, and confirm /api/statistics/dns still returns TopDomains/TopClients with TotalQueries unaffected by the caps and Truncated only set when a bucket actually hits a cap",
    "pigate.conf.example parses cleanly: every key in it is recognized by config.KnownKeys() (no typos)",
    "git diff shows zero changes under frontend/ and zero changes to install.sh"
  ]
}
```

---

## 7. สรุปสำหรับเจ้าของโปรเจกต์ (ไทย)

- เพิ่มค่าปรับได้ 2 ตัวในไฟล์ `pigate.conf`: **`dns-stats-max-pairs` (default ใหม่ 2400 จากเดิม 1200)** และ **`dns-stats-max-clients` (default 200 เท่าเดิม)** — ทั้งคู่ **ไม่มี CLI flag คู่กัน** ตามที่ต้องการ (เป็นข้อยกเว้นที่ตั้งใจ และจะเขียนกำกับไว้ในคอมเมนต์ของ config package ให้ชัด)
- ค่าที่ใส่ผิดประเภท (เช่น `abc`) → โปรแกรมตายพร้อม error ชัดเจนเหมือน key อื่น; แต่ค่าที่ **นอกช่วง (`<=0` หรือใหญ่เกินเพดานกัน RAM)** → **ไม่ตาย** แค่เตือนใน log แล้วใช้ค่า default แทน เพราะไม่อยากให้พิมพ์เลขผิดตัวเดียวแล้วเกตเวย์บูตไม่ขึ้น
- ค่าเหล่านี้ถูกส่งผ่าน `NewStatisticsService(...)` ลงไปเก็บเป็น field ของ ring (ตั้งครั้งเดียวตอนสร้าง ไม่แก้ระหว่างรัน เพื่อไม่ให้ค่า Truncated เพี้ยน)
- เขียนไฟล์ `pigate.conf.example` ใหม่ทั้งไฟล์ ครบทุก key (14 ตัว) พร้อมคำอธิบายและค่า default รวมถึงอธิบายผลต่อ RAM (cap เป็นค่าต่อ bucket 5 นาที × 288 buckets = 24 ชม.)
- ไม่แตะ `install.sh` และไม่แตะ frontend เลย; ปรับ README กับ doc ที่อ้างค่า 1200 เดิมให้ตรง
- งานแบ่งเป็น 9 tasks — ให้ ai-developer ทำให้ครบทุกข้อก่อน แล้วค่อยส่ง ai-qa ทดสอบรวมทีเดียวตามหัวข้อ Final Acceptance
