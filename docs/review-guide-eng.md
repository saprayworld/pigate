# Security Review Guide — PiGate

> This document is a **repeatable methodology** for security-reviewing PiGate.
> The next reviewer should be able to follow it start-to-finish without prior project history.
> Thai version: `docs/review-guide.md`
> Last updated: 2026-07-08 (reflects code state on branch `feat/dialog-to-drawer`)

---

## 1. Purpose and Scope

PiGate is a firewall/gateway controller running on a Raspberry Pi with `cap_net_admin,cap_net_raw`
capabilities, driving the kernel directly via Netlink/D-Bus — **a security failure in this project
means full takeover of the network's gateway**. The review bar must therefore be higher than for a
typical web app.

Review scope:

| Area | Location | Criticality |
|---|---|---|
| Authentication / Session / RBAC | `backend/internal/api/middleware.go`, `handlers.go`, `internal/service/user.go` | Critical |
| Firewall / routing / QoS rule generation | `internal/service/firewall.go`, `routing.go`, `qos.go`, `internal/kernel/real_*.go` | Critical |
| OS config file generation (wpa_supplicant, dnsmasq) | `internal/kernel/wpa.go`, `dhcp_server.go`, `dns_server.go` | Critical |
| OS interaction (Netlink, D-Bus, exec) | all of `internal/kernel/` | Critical |
| Database / SQL | `internal/db/repository.go`, `connection.go` | High |
| Secret handling (Wi-Fi passwords, backups) | `internal/service/backup.go`, `backup_crypto.go`, `handlers.go` | High |
| Frontend (XSS, token storage) | `frontend/src/services/`, `components/` | Medium |
| Installation / system privileges | `install.sh`, `build.sh`, installed Polkit/sudoers rules | High |
| Dependencies | `backend/go.mod`/`go.sum`, `frontend/package.json`/`yarn.lock` | Medium |

**Read first:** `docs/tech_stack_design.md` (full design rationale, especially §4.3 nftables chain
structure) and `docs/wifi_wpa_working_instruction.md` (before touching any Wi-Fi code).

---

## 2. Environment Setup

Review safely on a dev workstation using mock mode (does not touch the real kernel):

```bash
cd backend
go build -o pigate-backend ./cmd/pigate
./pigate-backend -port=8081 -db=/tmp/review.db -mock=true
go test ./...
```

Install analysis tools (one-time):

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
go install honnef.co/go/tools/cmd/staticcheck@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

---

## 3. Threat Model (frames what counts as "risky")

Attackers to consider, in order of likelihood:

1. **A compromised device on the LAN** (malware-infected phone/IoT) — can sniff the admin UI's HTTP
   traffic, call the API directly, and brute-force the login.
2. **An `admin_readonly` user attempting privilege escalation** — prove the middleware closes every
   mutation path.
3. **A logged-in admin submitting dangerous input (malicious or tricked)** — SSIDs, hostnames,
   zone names containing special characters must never become config or command injection.
4. **A leaked backup file** — contains Wi-Fi passwords and account hashes.
5. **XSS in the frontend** — the token lives in `localStorage`; any XSS steals the session instantly.

*Out of scope:* an attacker who already has root on the Pi, physical attacks, WAN-side attacks
(the nftables design drops WAN input by default).

---

## 4. Per-Area Review Checklist (work through in order)

Each area follows: **Where to look → How to check → Risk if missed → Remediation**.

### A. Authentication and Sessions

**Where:** `internal/api/middleware.go`, `internal/api/handlers.go` (AUTHENTICATION HANDLERS
section), `internal/service/user.go`, `internal/db/connection.go` (`seed` function)

**How:**
- Verify tokens come from `crypto/rand` only (never `math/rand`) and are ≥ 16 bytes — see
  `generateRandomToken()` in `handlers.go`.
- Verify the `rand.Read` error is **not ignored** (currently `_, _ = rand.Read(b)` ignores it — an
  entropy failure would yield an all-zero token).
- Check session lifetime: does the server-side `activeSessions` map expire entries? Currently
  **no TTL** — a token stays valid until process restart, even though the browser cookie expires
  after 24 h.
- Check password hashing: must be bcrypt (cost ≥ 10) — see `service/user.go` and `HandleLogin`.
- Check the forced first-password-change flow (`is_initial`): the bypass list must be exactly the
  three necessary endpoints (`/api/system/password`, `/api/auth/logout`, `/api/auth/session`).
- Check the default admin password: `connection.go` must generate it from `crypto/rand`
  (`generateRandomPassword` exists); the hardcoded `pigate` password must apply only when the DSN
  is `:memory:` (test mode).
- Check `/api/auth/login` rate limiting (per-IP token bucket) — probe with
  `for i in $(seq 10); do curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8081/api/auth/login -d '{}'; done`
  and expect 429s.

**Risk:** non-expiring sessions mean a leaked token (via XSS/logs/network sniffing) works forever;
weak tokens allow session forgery and gateway takeover.

**Remediation:**
- Add server-side session TTL (store `expiresAt` in the map, check it in `IsSessionValid`, sweep
  with a goroutine), with sliding renewal on activity.
- Make `rand.Read` failures fatal instead of ignored.
- Consider capping sessions per user.

### B. Authorization / RBAC

**Where:** `internal/api/middleware.go` (`RoleReadOnlyMiddleware`, `SuperAdminMiddleware`),
`internal/api/router.go`, `internal/service/user.go` (guard rails)

**How:**
- Confirm the **fail-closed** principle: an unknown/missing role must be treated as read-only
  (currently correct: `if role != model.RoleSuperAdmin`).
- Walk `router.go` line by line — every mutating route must go through `authRoute` or
  `superAdminRoute`; routes returning secrets (config export) or controlling power must be
  `superAdminRoute` even for GET.
- **Hunt for GETs with side effects** — `RoleReadOnlyMiddleware` only blocks
  POST/PUT/DELETE/PATCH, so any GET that changes state (e.g. a Wi-Fi scan that drives the kernel)
  slips past the read-only role — assess `HandleScanWifi` and other GET handlers.
- Check the guard rails in `user.go`: no self-demotion, no self-delete/disable, at least one
  active super_admin must always remain — covered by unit tests (`user_test.go`).
- Verify `AuthMiddleware` queries the DB on every request so disabling/deleting an account takes
  effect immediately (it does — keep this behavior).

**Risk:** a new route missing its middleware wrapper = unauthenticated or read-only users editing
the firewall.

**Remediation:** every new route must use the `authRoute`/`superAdminRoute` helpers — never a bare
`mux.Handle` (except login/logout). Add a test in `handlers_test.go` that hits every route without
a token and expects 401.

### C. Transport Security (the biggest structural weakness today)

**Where:** `cmd/pigate/main.go` (the `http.ListenAndServe` line), `handlers.go`
(`http.SetCookie`), `frontend/src/services/authService.ts`

**How:**
- The server currently serves **plain HTTP on all interfaces** (`":"+port`) and the cookie sets
  `Secure: false`.
- ~~The token is delivered twice: as an HttpOnly cookie **and** in the JSON body, which the frontend
  stores in `localStorage`.~~ → **Fixed (cookie-only-session-auth-plan)**: the token is delivered
  only via `Set-Cookie` (HttpOnly); the frontend no longer stores it in `localStorage`.

**Risk:** anyone on the LAN who can sniff (ARP spoofing, rogue AP) sees passwords in plaintext (fix
with TLS — see finding 1). The "XSS steals the token from `localStorage`" vector is now closed —
the token lives only in the HttpOnly cookie.

**Remediation (by value):**
1. Add TLS: generate a self-signed cert at install time, add `-tls-cert`/`-tls-key` flags, set
   `Secure: true`.
2. Stop returning the token in the response body — use the HttpOnly cookie exclusively (frontend
   stops reading `data.token` and stops using `localStorage`; use `credentials: "include"` on
   fetches). This removes the "XSS steals the token" vector entirely.
3. At minimum: bind to the LAN management interface's IP instead of `0.0.0.0`.

**C.1 CSRF (always review together with cookies)**
- After the cookie-only auth change, auth arrives via the cookie **only** (the `Authorization:
  Bearer` path was removed), so CSRF is now guarded by a single mechanism: the cookie is
  `SameSite=Strict` (browsers block all cross-site sends).
- **What to watch (now more critical):** `SameSite=Strict` is the sole CSRF barrier — each review
  round, confirm the login/logout cookies still set `SameSite=Strict` (never downgraded to
  `Lax`/`None`), since there is no Bearer-header backstop anymore.
- Defense-in-depth option: check the `Origin`/`Sec-Fetch-Site` headers on mutating requests and
  reject cross-site ones.

### D. Input Validation and Config-File Injection

The most detail-critical area, because user input is written into real OS config files.

**D.1 wpa_supplicant (`internal/kernel/wpa.go`)**
- `SanitizeWpaInput` strips `\n`, `\r`, `"` — verify that **every value** written to the config
  passes through it (ssid, psk) and that sanitized values remain quoted in the file.
- Ask every review round: has any new field (e.g. `country`, EAP identity) been added to
  `GenerateWpaConfig` without sanitization?
- Verify the file is written atomically (temp + rename) with permissions no wider than 0600 —
  it contains the PSK.
- Check wpa_supplicant's own constraints: a quoted psk must be 8–63 chars — validate at the
  service layer so an un-applicable config is rejected early.

**D.2 dnsmasq (`internal/kernel/dhcp_server.go`, `dns_server.go`)**
- Values written to the file: interface names, IP ranges, MACs, reservation hostnames, zone names,
  record names/values.
- **Key check:** any of these strings containing `\n` would become a new dnsmasq directive (config
  injection) — trace handler → service → kernel and confirm every field is validated (regex /
  `net.ParseIP` / `net.ParseMAC`) before reaching the `fmt.Sprintf` calls that build the file.
- An existing safety net: `dnsmasq --test` validates syntax before apply (`validateDnsmasqConfig`)
  — but **never use it as a substitute for validation**, since injected config can be
  syntactically valid.
- If a gap is found: add per-field whitelist regexes at the service layer, e.g. hostname
  `^[a-zA-Z0-9-]{1,63}$`, zone `^[a-z0-9.-]+$`, and reject rather than silently strip.

**D.3 Firewall / Routing / QoS (`internal/service/firewall.go`, `routing.go`, `qos.go`)**
- Every address must pass `net.ParseIP`/`net.ParseCIDR`, ports must be 1–65535, interface names
  must match an actually existing interface.
- Verify nftables rule ordering preserves the 4-section structure from
  `docs/tech_stack_design.md` §4.3 (sanity drops → audit log → dynamic accepts → final drop) —
  reordering is a security bug even without any "injection".
- Test in mock mode: create policies with hostile values (malformed CIDR, port 0, fake interface
  names) directly via `curl` — expect 400, never 500 or a successful apply.

**D.4 Remaining exec calls**
- Run this every review round and assess each hit:
  ```bash
  grep -rn "exec.Command\|execCommand(" backend/internal backend/cmd --include='*.go' | grep -v _test.go
  ```
- Currently acceptable (fixed arguments, no user input in the executable position):
  `dnsmasq --test --conf-file=<tempfile we created>`, `modprobe ifb`, and dhcpcd via the tightly
  scoped sudoers entries in `install.sh`.
- **Every new exec = default reject**, unless it's proven there is no Netlink/D-Bus alternative
  and no argument derives from user input.

### E. SQL / Database

**Where:** `internal/db/repository.go`, `connection.go`

**How:**
- Grep for string-assembled SQL:
  ```bash
  grep -n "Sprintf" backend/internal/db/*.go
  ```
- Currently 3 hits (around lines 388, 398, 1150 of `repository.go`) using `fmt.Sprintf` to build
  `IN (%s)` — verify `%s` is a **placeholder string** (`?,?,?`) generated from the element count,
  not actual values, with the values passed as separate args → if so, it's safe. Re-confirm every
  round.
- All ids must go through parameter binding (`?`), always.

**Risk:** SQLite injection → read/modify the users table → full system takeover.

**Remediation:** hard rule — user-derived values must never appear in a SQL format string;
`Sprintf` is allowed only for placeholder generation.

### F. Secret Handling

**Where:** `internal/api/handlers.go` (`maskInterfacePasswords`), `internal/service/backup.go`,
`backup_crypto.go`, `internal/api/router.go` (export/import routes)

**How:**
- Wi-Fi passwords are stored plaintext in the DB (necessary — they feed wpa config generation) —
  verify **every endpoint returning interface data calls `maskInterfacePasswords` first**; grep
  all handler uses of `WifiPassword`.
- Verify config export is a `superAdminRoute` (it is) and that secret-bearing backups support
  encryption.
- `backup_crypto.go`: AES-256-GCM + Argon2id (time=1, mem=64MiB, threads=4), salt/nonce from
  `crypto/rand`, generic decrypt errors (anti-oracle) — this design is sound; verify the
  parameters are never downgraded and that importing old unencrypted backups warns the user.
- Verify logs (`log.Printf`) never print passwords — grep logging statements for `password`;
  `wpa.go` correctly logs only `HasPassword=%t`, but **`SendWpaCommand` logs the full command** —
  if a future command embeds a PSK (e.g. `SET_NETWORK ... psk`), it leaks straight into the
  journal. Redact before logging.
- Never commit `.env` files or keys, per `CLAUDE.md`.

**Remediation:** add a test asserting that no interface endpoint's JSON response contains a real
password; add redaction in `SendWpaCommand` before logging.

### G. DoS / Resource Limits

**Where:** `internal/api/middleware.go` (rate limiter), `handlers.go` (every JSON decode),
`internal/logs/ringbuffer.go`, `cmd/pigate/main.go`

**How:**
- The rate limiter stores a limiter per IP in a map **with no eviction** — LAN spoofing is
  limited, but IPv6 privacy addresses let the map grow unboundedly → add a cleanup goroutine or
  LRU.
- Request body size limits: the config-import endpoint is already capped with
  `http.MaxBytesReader` at 10 MB (good), but the other JSON endpoints are not → apply a small cap
  (e.g. 1 MB) in a shared middleware across all endpoints.
- The server is started via bare `http.ListenAndServe` = **no ReadTimeout/WriteTimeout/
  IdleTimeout** → slowloris can pin connections → construct an `http.Server{}` with timeouts.
- The log ring buffer is fixed-size (good — bounds both memory and SD-card wear).

### H. Frontend

**Where:** `frontend/src/services/*.ts`, `frontend/src/components/`, `frontend/src/pages/`

**How:**
- XSS sinks: `grep -rn "dangerouslySetInnerHTML" frontend/src` — currently a single hit in
  `components/ui/chart.tsx` (stock shadcn code injecting only CSS built from internal config, not
  user input) — re-check every round that no new sinks or `eval`/`new Function` appear.
- Token storage: see area C — the long-term goal is dropping `localStorage`.
- Client-side role (`pigate_role`) is only a UI hint — confirm **enforcement lives exclusively in
  the backend** (it does); the frontend must never be the only gate.
- Mock mode: `IS_MOCK_MODE` must resolve to false in production builds — check
  `services/config.ts` that the condition is bound to the build env, not a user-toggleable runtime
  switch.

### I. Installation and OS Hardening

**Where:** `install.sh`, `build.sh`, the systemd unit / Polkit rules / sudoers entries the script
creates

**How:**
- The binary must run as the `pigate` user + capabilities, not root — check no new code assumes
  root (e.g. writing to paths `pigate` can't reach, with odd fallbacks).
- sudoers entries must be **scoped per command and per argument** (dhcpcd/dhclient only), with no
  broadened wildcards.
- Polkit rules must be limited to the required wpa_supplicant/systemd-resolved actions only.
- Consider systemd unit hardening: `NoNewPrivileges=yes`, `ProtectSystem=strict` with minimal
  `ReadWritePaths=`, `ProtectHome=yes`, `PrivateTmp=yes`.
- The DB file (`pigate.db`) must be mode 0600, owned by `pigate`.

### J. Dependencies / Supply Chain

Run every review round:

```bash
cd backend && govulncheck ./... && staticcheck ./... && gosec ./...
cd ../frontend && yarn audit
```

- Go deps must stay pinned via `go.sum`; any new dependency needs justification (project policy:
  stdlib / golang.org/x / well-known modules only).
- Diff `go.mod`/`yarn.lock` against the previous review round.

---

## 5. Standard-Topic Scorecard (re-grade every review round)

This table summarizes the project's state across 12 standard security topics. The next reviewer
must **re-grade every round**, using the referenced checklist areas as the how-to
(A = excellent, F = absent).

| Topic | Grade | Status summary | How to check (area) |
|---|---|---|---|
| Authentication | B+ | bcrypt, generic errors (no account enumeration), forced initial password change, random default password; deductions: ignored `rand.Read` error, no per-account lockout | A |
| Session Management | C | Tokens **never expire server-side** until restart; good: per-request DB check, session purge on account delete/config import | A |
| Authorization | A- | Fail-closed RBAC, complete guard rails with tests; watch GETs with side effects slipping past the read-only role | B |
| Password Hashing | A- | bcrypt cost 10, used consistently across create/login/reset | A |
| CSRF | B | After cookie-only auth: guarded by `SameSite=Strict` alone (Bearer removed) — add an `Origin` check as defense-in-depth since there is no backstop | C.1 |
| Cookie Security | B | `HttpOnly` + `SameSite=Strict` correct, and the token is no longer duplicated into `localStorage` (HttpOnly benefit fully restored); the remaining item is `Secure`, tied to HTTPS (`r.TLS`) | C |
| CORS | B+ | Exact-match whitelist, no wildcard; dev origins still active in production | B, C |
| Rate Limiting | B- | Present at login (the right place); limiter map has no eviction, expensive endpoints (scan/apply) unlimited | A, G |
| File Upload | A- | Single path: config import — super_admin only, 10 MB cap, single transaction + snapshot, session purge after import; no uploaded files ever written to the filesystem | F |
| Secrets | B+ | Passwords masked in every response, export super_admin only, textbook backup crypto; future risk: `SendWpaCommand` logs full commands | F |
| TLS/HTTPS | **F** | **Absent — the number-one weakness**; passwords/tokens travel plaintext on the LAN | C |
| Input Validation | B | Parameterized SQL, wpa sanitization with tests, `ParseIP`/`ParseMAC`; pending: complete the dnsmasq path audit (zone/record/hostname) | D, E |

**Fix priority:** TLS (F) → ~~drop the dual token delivery in favor of HttpOnly-cookie-only auth~~
(**done** — cookie-only-session-auth-plan; Cookie/CSRF grades already raised) → session TTL →
finish the dnsmasq path audit.

## 6. What Is Already Done Well (as of this review — preserve, don't regress)

1. **No shelling out for kernel work** — direct `google/nftables`, `vishvananda/netlink`, D-Bus,
   and unix sockets; the remaining exec calls all have fixed arguments → command injection is
   eliminated structurally.
2. **Fail-closed RBAC** — unknown role = read-only; secret/power endpoints are super_admin only,
   even for GET.
3. **User-system guard rails** — no self-demotion/delete/disable, at least one active super_admin
   enforced, forced initial password change, per-request DB check makes account disabling
   immediate.
4. **bcrypt for passwords + random initial password from `crypto/rand`** (hardcoded only for the
   in-memory test mode).
5. **Login rate limiting** as baseline brute-force protection.
6. **Textbook backup encryption** — AES-256-GCM + Argon2id, parameters stored in meta, generic
   errors against oracles, export/import restricted to super_admin.
7. **Parameterized SQL throughout the repository** (the `Sprintf` hits only build IN-clause
   placeholders).
8. **`SanitizeWpaInput` blocks newline/quote injection** in wpa configs, with tests.
9. **Capabilities instead of root** + dedicated user + tightly scoped Polkit/sudoers.
10. **dnsmasq `--test` before apply** as an extra safety net.
11. **Frontend nearly free of XSS sinks** — the single `dangerouslySetInnerHTML` is shadcn's chart
    component, which takes no user input.

## 7. Findings — What Needs Improvement (by severity)

| # | Severity | Issue | Location | Remediation |
|---|---|---|---|---|
| 1 | High | No TLS — passwords/tokens travel plaintext on the LAN; cookie `Secure:false` | `cmd/pigate/main.go`, `handlers.go` | Self-signed cert at install + TLS flags + `Secure:true` |
| 2 | ~~High~~ **Fixed** | ~~Token stored in `localStorage` and returned in the JSON body despite the HttpOnly cookie — any XSS steals the session~~ → cookie-only auth: `LoginResponse` drops `token`, the frontend keeps only a non-secret `pigate_logged_in` flag, `AuthMiddleware`/logout read the cookie only | `authService.ts`, `HandleLogin`, `middleware.go` | **Done** (cookie-only-session-auth-plan) — cookie is the single channel, `credentials: "include"` |
| 3 | Med–High | Sessions never expire server-side until restart | `middleware.go` (`activeSessions`) | Add TTL + sweeper |
| 4 | Medium | No server timeouts (slowloris); body size limit exists only on import (10 MB), not other endpoints | `main.go`, handlers | Explicit `http.Server` with timeouts; `http.MaxBytesReader` on all endpoints |
| 5 | Medium | Rate-limiter map grows unboundedly (no eviction) | `middleware.go` | LRU or cleanup goroutine |
| 6 | Medium | No security headers (`Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`) on the SPA | `embed.go`/middleware | Add a headers middleware; CSP also mitigates finding #2 |
| 7 | Low–Med | `SendWpaCommand` logs the full command — could leak a PSK into the journal if future commands embed one | `kernel/wpa.go` | Redact before logging |
| 8 | Low | `_, _ = rand.Read(b)` ignores the error during token generation | `handlers.go:117` | Handle the error |
| 9 | Low | CORS allows dev origins (`localhost:5173`) even in the production binary | `middleware.go` | Enable only behind a dev flag |
| 10 | Low | No audit log of who changed which config when (only the firewall event ring buffer) | service layer | Add a separate audit ring buffer recording username+action |

> When a finding is fixed, move it to section 6 with the commit reference and adjust the grade in
> section 5 — this is a living document.

## 8. Repeatable Procedure (summary for the next reviewer)

1. Read `docs/tech_stack_design.md` + this document (especially section 7 — are old findings
   fixed yet?).
2. Build + `go test ./...` + run in mock mode.
3. Run the section-J toolchain (govulncheck / staticcheck / gosec / yarn audit) — attach output to
   the report.
4. Run the standing greps:
   ```bash
   cd backend
   grep -rn "exec.Command\|execCommand(" internal cmd --include='*.go' | grep -v _test.go
   grep -n  "Sprintf" internal/db/*.go
   grep -rn "math/rand" internal cmd --include='*.go' | grep -v _test.go
   grep -rn "dangerouslySetInnerHTML\|eval(" ../frontend/src
   ```
5. Work through checklist areas A–J, focusing on **the diff since the last review**
   (`git log --stat <last-review-tag>..HEAD`) — new files in `kernel/` and new routes in
   `router.go` are the top risk spots.
6. Behavioral tests against the live API (mock mode): login rate limit; mutating endpoints with a
   read-only role must return 403; requests without a token must return 401; malformed input
   (CIDR/port/hostname) must return 400.
7. Update sections 5–7 of this document (scorecard grades, done-well list, findings) + the date
   in the header, then write a short report (new findings, closed findings, residual risk).
