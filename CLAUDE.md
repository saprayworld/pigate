# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

PiGate is a firewall/gateway controller for Raspberry Pi 5 (and similar Debian-based boards). It ships as a **single Go binary**: a Go backend (`net/http`) that embeds a built React SPA via `go:embed`, talks to the Linux kernel directly over Netlink (nftables, routing, interfaces, tc), and controls OS services (Wi-Fi, DNS) over D-Bus instead of shelling out — this is a deliberate security choice to eliminate command-injection risk. See `docs/tech_stack_design.md` for the full architecture rationale before making non-trivial changes; it is the canonical design doc.

The author is not a security or Go specialist and relies heavily on AI assistance (see README "Disclaimer Warning"). Treat security-sensitive code (auth, firewall rule generation, D-Bus/Netlink calls, input validation) with extra scrutiny.

## Commands

### Backend (Go, in `backend/`)
```bash
cd backend
go build -o pigate-backend ./cmd/pigate      # build
go test ./...                                 # run all tests
go test ./internal/service/... -run TestName  # run a single test
sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend   # required for real (non-mock) kernel access

# Run against mock kernel (safe on a dev workstation, does not touch real routing/firewall)
./pigate-backend -port=8081 -db=pigate.db -mock=true
./pigate-backend -port=8081 -db=pigate.db -mock=true -disable-edit=true   # read-only mode
```
Other relevant `main.go` flags: `-mock-from-real` (mock layer seeded from real kernel state), `-allow-edit-system-routes`, `-enable-edit-system-route`, `-prioritize-kernel-routes`, `-docker-compat`.

### Frontend (React 19 + Vite + Tailwind + shadcn/ui, in `frontend/`)
```bash
cd frontend
yarn install
yarn dev        # dev server
yarn build      # tsc -b && vite build
yarn lint
```
- Package management uses **Yarn v1** — always use `yarn add`, not `npm install`.
- Yarn v1 has no `yarn dlx`, so new shadcn components must be added with `npx shadcn@latest add <component>` (run inside `frontend/`), never via yarn.

### Full single-binary build
```bash
bash build.sh   # builds frontend, copies frontend/dist -> backend/internal/api/dist, builds backend, outputs ./pigate
```
After building, `./pigate` needs `sudo setcap cap_net_admin,cap_net_raw+ep ./pigate` to run without root. `install.sh` automates full host installation (creates `pigate` system user, Polkit rules for wpa_supplicant/systemd-resolved D-Bus access, sudoers entries for dhcpcd/dhclient, systemd service).

## Architecture

### Backend layering (`backend/internal/`)
Strict three-layer separation — respect these boundaries when adding features:
- **`api/`** — HTTP handlers, routing, CORS/auth/rate-limit middleware, and `embed.go` (embeds the built frontend `dist/`). Talks only to the service layer.
- **`service/`** — business logic and coordination (e.g. `firewall.go`, `routing.go`, `interface.go`, `dns.go`, `dhcp_server.go`, `qos.go`). Reads/writes via the repository and drives the kernel layer through the interfaces below. `netlink_monitor.go` is a background goroutine that watches kernel Netlink events and reconciles DB/kernel state (e.g. self-healing routes) — anything that mutates routing/interfaces should consider whether the monitor needs to be aware of it.
- **`kernel/`** — the only layer allowed to talk to the OS. Each subsystem is defined as an interface in `interfaces.go` (`FirewallManager`, `NetworkManager`, `RoutingManager`, `DhcpManager`, `DNSManager`, `QosManager`, `DNSServerManager`) with two implementations: `real_*.go` (Netlink/D-Bus/`google/nftables`/`vishvananda/netlink` for production) and `mock.go` (in-memory, safe for local dev). `main.go` selects real vs. mock per the `-mock`/`-mock-from-real` flags. When adding a new kernel capability, add the method to the interface and implement it in both the real and mock backends.
- **`db/`** — SQLite (via `modernc.org/sqlite`, pure Go, no CGO) is the source of truth for configuration; `connection.go` handles connection + migrations, `repository.go` holds CRUD. Kernel/runtime state (e.g. firewall hit counters, live leases) is fetched live from the kernel rather than persisted, to reduce SD card wear.
- **`model/`** — shared structs/DTOs (`types.go`, `dns_server.go`) used across all layers.
- **`logs/ringbuffer.go`** — fixed-size in-memory ring buffer for firewall/event logs; deliberately not persisted to SQLite (SD card write-cycle preservation, see tech_stack_design.md §8).

Startup sequence in `cmd/pigate/main.go` matters: DB init → kernel manager selection (mock vs real) → service construction → apply each subsystem's DB-configured state to the kernel (interfaces → routes → netlink monitor start → DHCP → DNS → firewall → QoS) → register HTTP routes → listen. New subsystems that need kernel state applied at boot should follow this "apply config at startup" pattern (`InitApplyConfig()` / `InitApplyConfigurationAtStartup()`).

### Key security/design constraints (from `docs/tech_stack_design.md`)
- **No shell execution** for kernel/OS control — use Netlink sockets (`google/nftables`, `vishvananda/netlink`) or D-Bus (`godbus/dbus`) instead of `nmcli`/`iptables`/etc. This is the project's core anti-command-injection defense; don't introduce `exec.Command` for anything that has a Netlink/D-Bus alternative.
- **Wi-Fi client config uses `wpa_supplicant` directly**, not NetworkManager (`nmcli`) — write per-interface config atomically (e.g. `/etc/wpa_supplicant/wpa_supplicant-wlan0.conf`), then send `RECONFIGURE` via the `wpa_supplicant` Unix control socket (`unixgram`), never via subprocess. See `docs/wifi_wpa_working_instruction.md`.
- **Firewall (nftables)** default `input` chain follows a strict 4-section structure: sanity/drop checks → audit log point → dynamic DB-driven accept rules (+ Docker compat for `docker0`/`br-*`) → final drop-and-log. Preserve this ordering when touching firewall rule generation; see the example nftables ruleset in `docs/tech_stack_design.md` §4.3.
- **Linux Capabilities, not root**: the binary runs as an unprivileged `pigate` user with `cap_net_admin,cap_net_raw` — don't add code paths that assume root.
- Dependencies are kept minimal and pinned via `go.sum`; prefer stdlib or well-known Google/`golang.org/x` modules over new third-party deps.

### Frontend structure (`frontend/src/`)
- `pages/` — one file per top-level route/feature (Dashboard, Interfaces, FirewallPolicy, DhcpServer, DnsServer, QoS, StaticRoutes, SettingsMaintenance, etc.).
- `services/` — one API client module per backend resource (e.g. `interfaceService.ts`, `policyService.ts`, `dhcpService.ts`); `mockSync.ts` and `config.ts` handle mock-mode/base URL concerns.
- `components/ui/` — shadcn/ui primitives; **all UI must be built from these**, not ad hoc components (see `docs/rules_of_work.md` §1.1).
- Feature status (README "Feature Status" table) — Interfaces, Routing, DNS (client), Firewall, QoS, DHCP Server (dnsmasq), DNS Server (local zones), Hostname, System Time, User System, Import/Export are Completed on both frontend/backend; Power Control and the System Services panel are still Mock; Dashboard backend is Partial (real leases/Wi-Fi, simulated traffic/CPU/RAM/Temp) — check current code state before assuming a feature is finished, this table can drift.

### Frontend styling rules (`docs/rules_of_work.md`)
- No hardcoded Tailwind color classes for brand/status colors (e.g. `text-emerald-500`) — always go through theme variables (`text-primary`, `bg-primary/10`, etc.) declared in `src/index.css`.
- Flat design only: no `shadow-*` or `backdrop-blur-*` classes anywhere.
- Dialogs/Modals must use `<Dialog modal={false}>` **only when they contain a Combobox input field** (Radix's focus/pointer blocker breaks its dropdown clicks); Dialogs without a Combobox keep the default modal behavior.
- Dark/light mode must both be supported; use the semantic color variables, not raw palette classes.

## Documentation map (`docs/`)
- `tech_stack_design.md` — architecture blueprint, read first for anything touching kernel/security design.
- `rules_of_work.md` — frontend component/styling/Wi-Fi conventions.
- `ref/` — per-subsystem design docs (`dhcp-service-design.md`, `dnsmasq-design.md`, `dns-system-design.md`, `qos-system-design.md`, `hostname-setting-design.md`).
- `openapi.yaml` — API contract, also served at `frontend/public/openapi.yaml` / rendered via the ApiDocs page (swagger-ui-react).
- `wifi_wpa_working_instruction.md` — required reading before touching Wi-Fi/wpa_supplicant code.
- `firewall_default_rule.md`, `project_status.md`, `backend_development_report.md`, `frontend_design_review.md`, `frontend_data_testing_guide.md` — supplementary status/design notes.

## Project conventions
- Do not create git commits unless the user explicitly asks.
- Never read or expose `.env` files, secrets, keys, or credentials; use placeholder values in examples.
