# PiGate

**PiGate** (Raspberry Pi Firewall/Gateway Controller) is a high-performance firewall and gateway management system designed to run on the Raspberry Pi 5 (or compatible Raspberry Pi OS distributions). It is engineered to serve as a gateway and firewall for home networks or small offices, featuring an easy-to-use administration interface via a Web UI (React Single-Page Application) and a backend developed in Go (Golang) for high execution speed and stability.

The system focuses on the following key areas:
- **High Performance & Kernel-Level Security:** Direct communication with the Linux Kernel via Netlink Sockets for the Firewall (`nftables`), Routing, and Network Interfaces. It utilizes D-Bus to control various services instead of executing shell commands, thereby completely preventing Command Injection vulnerabilities.
- **Supply Chain Security:** Minimizes external dependencies by utilizing the Go Standard Library and a pure Go driver for SQLite (`modernc.org/sqlite`). This allows for compilation into a secure and easily deployable single binary.
- **SD Card Protection:** Employs an in-memory ring buffer and `/run/` or `/tmp/` directory locations to store large log files, prolonging the lifespan of the Raspberry Pi's MicroSD card.
- **Privilege Separation:** Runs services under a non-privileged user account (`pigate`) and elevates network management permissions using Linux Capabilities (`cap_net_admin, cap_net_raw`), thereby preventing Operating System takeover (OS Takeover).

---

## Disclaimer Warning

This project has been developed primarily utilizing AI assistance, combined with the project owner's fundamental programming knowledge and experience (predominantly in Node.js). The author does not specialize in cybersecurity or the Go programming language.

Consequently, cybersecurity integrity cannot be guaranteed. This software should be used strictly for personal and non-critical purposes, such as in homelabs, testing, research, education, or local systems positioned behind a Network Address Translation (NAT) router.

If deployed in a production environment, the project owner accepts no liability or responsibility for any damages or losses incurred. Users are free to use, modify, and distribute this software under their own discretion and risk.

---

## Layout

The core structure of the project at the root level and the backend directory is organized as follows:

```text
pigate/
├── backend/                         # Go Backend API Server & Kernel Integration
│   ├── cmd/
│   │   └── pigate/
│   │       └── main.go              # Main entrypoint for system boot and configuration
│   ├── internal/
│   │   ├── api/                     # API Interface (Frontend Gateway) & Middleware
│   │   │   ├── handlers.go          # HTTP API handlers for request processing
│   │   │   ├── router.go            # Endpoint routing registration
│   │   │   ├── middleware.go        # CORS, Authentication, and Rate limiting middlewares
│   │   │   └── embed.go             # Embeds the React SPA (dist/) using go:embed
│   │   ├── db/                      # SQLite Database Management Layer
│   │   │   ├── connection.go        # SQLite connection configuration & database migrations
│   │   │   └── repository.go        # CRUD operations for system configurations
│   │   ├── kernel/                  # Linux Operating System Interaction Layer (Low-level OS)
│   │   │   ├── interfaces.go        # Unified interface definitions for OS control
│   │   │   ├── real_network.go      # IP and network interface management using Netlink
│   │   │   ├── real_routing.go      # Routing table management using Netlink
│   │   │   ├── real_firewall.go     # nftables management via google/nftables (Netlink)
│   │   │   ├── real_qos.go          # Traffic Control (tc/HTB/IFB) queuing using Netlink
│   │   │   ├── real_hostname.go     # System hostname control via systemd-hostnamed (D-Bus)
│   │   │   ├── real_timedate.go     # Timezone/NTP configuration via systemd-timedated (D-Bus)
│   │   │   ├── dhcp_server.go       # DHCP server: dnsmasq config generation, validation, and D-Bus lease watcher
│   │   │   ├── dns_server.go        # Local DNS zones (FQDN) via dnsmasq config generation
│   │   │   ├── dhcpcd.go            # DHCP client (dhcpcd@<iface>) lifecycle control via systemd D-Bus
│   │   │   ├── wpa.go               # Wi-Fi management via unix control socket wpa_supplicant
│   │   │   ├── dns.go               # DNS configuration and systemd-resolved control via D-Bus
│   │   │   └── mock.go              # Memory-resident mock implementation for local testing
│   │   ├── service/                 # System Coordination & Business Logic Layer
│   │   │   ├── interface.go         # Network interface status update logic
│   │   │   ├── routing.go           # Routing logic and metric coordination
│   │   │   ├── netlink_monitor.go   # Background service monitoring Kernel events for state reconciliation
│   │   │   ├── firewall.go          # Firewall security policy management logic
│   │   │   ├── dhcp_server.go       # DHCP server coordination (configs, reservations, live leases)
│   │   │   ├── dns_server.go        # Local DNS zone/record coordination
│   │   │   ├── dns.go               # System DNS (systemd-resolved) coordination
│   │   │   ├── dhcpcd.go            # DHCP client (WAN-side) coordination
│   │   │   ├── qos.go               # QoS bandwidth rule coordination
│   │   │   ├── hostname.go          # System hostname coordination
│   │   │   ├── timesync.go          # Timezone / NTP / manual time coordination
│   │   │   ├── user.go              # Multi-user account and role management
│   │   │   └── backup.go            # Typed configuration export/import (backup & restore)
│   │   ├── model/
│   │   │   └── types.go             # Data structure structs and validation tags
│   │   └── logs/
│   │       └── ringbuffer.go        # In-memory ring buffer for temporary RAM-based log storage
│   ├── go.mod                       # Go backend module dependencies
│   └── go.sum                       # Cryptographic checksum hashes for Go dependencies
├── frontend/                        # React 19 Frontend SPA (Vite + Tailwind CSS + shadcn/ui)
├── docs/                            # Design documentation, system requirements, and development guides
├── build.sh                         # Compilation script to bundle Frontend and Backend into a Single Binary
├── install.sh                       # Installation script for automated Linux host deployment
├── note.md                          # Installation, build notes, and test commands
├── docs/ref/readme-ref.md           # Reference template for README.md
└── LICENSE                          # Software license agreement
```

---

## Feature Status

The following table summarizes the development status of each feature in the PiGate system:

| Feature | Frontend | Backend | Status / Remarks |
|---|---|---|---|
| **Dashboard** | Completed | Completed | System status is real: CPU (usage/model/cores/freq), Memory, Temperature, Storage, Uptime/OS/kernel/board, and WAN bandwidth history are read from `/proc`, `/sys`, `statfs`, and netlink counters (no shell exec). Temperature/CPU-freq/board-model degrade to `available:false`/omitted on hosts that lack the sysfs node (e.g. WSL/x86). Traffic history is a RAM ring buffer (24h of 5-min buckets, resets on reboot). Recent Logs are now real forward-chain PASS/DROP events fed from the kernel via NFLOG (see **Forward Traffic Log**). The Detailed tab's traffic-analytics cards (Protocol Breakdown, Top Talkers, Top Rules by Traffic) are also real: Protocol Breakdown/Top Talkers come from a hybrid conntrack-polling + conntrack-DESTROY-event collector (poll every 10s, plus a live event listener crediting each flow's final byte count at teardown so short-lived flows between two polls aren't lost — requires `net.netfilter.nf_conntrack_acct=1` and `net.netfilter.nf_conntrack_events=1`, both set by `install.sh`), categorized against user-defined Service Objects; the card's accuracy badge reads `"near-exact"` while the event listener is active and degrades to `"estimated"` (poll-only) when it isn't (e.g. missing `cap_net_admin` or an older kernel). Top Rules reads nftables' own exact per-rule byte/packet counter. The Detailed tab also has an **Active Sessions** graph: a live conntrack session-count line chart (`nf_conntrack_count`/`nf_conntrack_max`, ~5s via the existing SSE metrics stream) with TCP/UDP/ICMP/Other badges (~10s, from the same conntrack-dump poller), seeded from a 30-minute RAM-only history ring — no new endpoints. |
| **Interface** | Completed | Completed | IP management, Netlink interface handling, `wpa_supplicant` Wi-Fi scanning and state management, random MAC addresses, per-interface route metric for multi-WAN failover, and WAN-side DHCP client via `dhcpcd@<iface>` (systemd D-Bus). Also supports creating/deleting **802.1Q VLAN** sub-interfaces (`<parent>.<id>`) directly from the UI via Netlink; VLANs are persisted in the DB and re-created automatically on every boot (and restored by Import/Export). Interfaces that have a saved configuration but no live kernel link (a VLAN whose parent vanished, an unplugged USB NIC) are surfaced as **offline** rows behind a "show offline" toggle so their config can be deleted from the UI (config is never removed automatically). |
| **Wi-Fi Presets (Saved Networks)** | Completed | Completed | A saved-network library (name + SSID + security + optional MAC mode + password) reusable across wireless interfaces. Passwords are **write-only** (stored plaintext like interface Wi-Fi, but never returned to the browser — GET responses carry only a `hasPassword` flag). Applying a preset to an interface's primary/backup slot is **server-side only** (`POST /api/wifi-presets/{id}/apply`): the backend reads the password from the DB and drives the existing `ConfigureWifi` flow, so credentials never travel through the frontend. All endpoints are `super_admin`-only; input is validated against the same wpa_supplicant-injection sanitizer as interfaces. Included in backup/restore (fail-closed validation). |
| **Routing** | Completed | Completed | CRUD operations for static routes, Netlink event monitoring, and automatic routing self-healing. |
| **DNS System** | Completed | Completed | `systemd-resolved` per-link DNS configuration via D-Bus. |
| **Firewall System** | Completed | Completed | `nftables` management via Netlink; policy configuration across all three base chains — Firewall Policy (`forward`, traffic passing through the box), Local-In Policy (`input`, traffic to the box itself), Local-Out Policy (`output`, traffic from the box itself, default-allow) — plus WAN Network Address Translation (Masquerade) and Docker compatibility. |
| **Port Forwarding (DNAT)** | Completed | Completed | FortiGate-VIP-style port forwarding: a prerouting DNAT chain in `pigate_nat` (guarded by `fib daddr type local`) rewrites external `interface:port` → internal LAN `IP:port`, with an auto-generated forward-accept rule (placed before user policy rules) so the DNAT'd packet is never dropped by the filter chain. Supports single-port translation and keep-port ranges (1:1 range translation is out of scope); conntrack un-DNATs the return path. Included in backup/restore. Out of scope in v1: hairpin/NAT-loopback, IPv6, source-restricted DNAT. |
| **DHCP Server** | Completed | Completed | `dnsmasq` config generation (`/etc/dnsmasq.d/pigate-dhcp.conf`) with syntax validation (`dnsmasq --test`), service restart via systemd D-Bus, per-interface pools and reservations, and a live lease watcher via dnsmasq D-Bus signals. |
| **DNS Server** | Completed | Completed | Local DNS zones/FQDN records via `dnsmasq` config generation (`/etc/dnsmasq.d/pigate-dns.conf`), authoritative zone support, listen-interface selection decoupled from the DHCP server, and a per-domain (with subdomains) deny-list ("Blocked Domains" tab) with NXDOMAIN or sinkhole (0.0.0.0/::) response modes. |
| **QoS Limiting** | Completed | Completed | HTB and IFB traffic shaping via tc Netlink, supporting Source/Destination IP address ranges (CIDR). |
| **System Hostname** | Completed | Completed | Hostname management via `systemd-hostnamed` D-Bus (static + transient), applied at startup, with dependent DHCP client restart. |
| **System Time** | Completed | Completed | Timezone, NTP toggle/server, and manual time setting via `systemd-timedated` D-Bus; timezone/NTP config re-applied at startup. |
| **Setting (Overall)** | Completed | Completed | Password change, time, hostname, export/import, and the system services list/restart panel are all fully functional. Services panel reads live `ActiveState`/`LoadState` for a whitelisted catalog of systemd units (static singletons + per-interface `wpa_supplicant@`/`dhcpcd@`) via `systemd1` D-Bus and restarts them the same way; `{id}` is always resolved through the server-side catalog, never used as a raw unit name, and `pigate.service` itself is status-only (no restart). |
| **Import/Export** | Completed | Completed | Typed JSON backup (schema v2) with SHA-256 integrity, optional user accounts, and optional passphrase encryption (AES-256-GCM + Argon2id); import uses validate → pre-import snapshot → single-transaction wipe & restore → kernel re-apply (startup order). Cross-machine safe (raw routes, interface match-by-name), `super_admin`-only, with actor lock-out guard. Accepts legacy v1 files. |
| **User System** | Completed | Completed | Multi-user management (create/edit/delete/enable-disable) with `super_admin` / `admin_readonly` roles, per-request DB-backed session validation, role-based authorization middleware, session-based auth, login rate limiting, and first-time login password change enforcement. |
| **Power Control (Shutdown/Restart)** | Completed | Completed | Real board reboot/power-off via `systemd-logind` (`org.freedesktop.login1`) D-Bus — no shell exec. `super_admin`-only, authorized by a Polkit rule (see `install.sh`); the command is delayed ~1s so the HTTP 200 flushes before logind stops `pigate.service`, and shutdown is graceful (SQLite closes cleanly). Safe under `-mock=true` (no-op mock manager). |
| **Event Log** | Completed | Completed | Central audit/event log (`EventLogService`): security events (login success/failed, password change, user CRUD), network/firewall/route/DHCP/DNS changes, DHCP lease add/remove, config export/import, and reboot/shutdown/boot. Persisted in SQLite across reboots via an SD-card-friendly async batch writer (RAM queue, flush every 10 events / 10 s, table capped at 10,000 rows, synchronous flush before power actions). Viewer UI at Log & Report › System Events with category/severity/text filters and pagination; clearing the log is `super_admin`-only and always leaves a `logs_cleared` audit row. |
| **Forward Traffic Log** | Completed | Completed | Live PASS/DROP events for packets forwarded through the firewall (LAN↔WAN forward chain). The forward-chain log statements log to an NFLOG group (group 100) instead of printk/dmesg; a pure-Go NFLOG listener (`github.com/florianl/go-nflog`, no CGO) parses the IPv4/IPv6 + TCP/UDP header of each event into a `FirewallLog` and pushes it to a RAM ring buffer (capacity 10,000, shared with input/output — see **Local Traffic Log**). Deliberately **not** persisted (packet-rate + SD-card wear); the listener callback is non-blocking and drops on burst overflow. Viewer UI at Log & Report › Forward Traffic (verdict filter, text search, cursor-based infinite scroll — 500 rows/page — real-time SSE push, pause/resume, clear). Mock mode synthesizes events (no netlink socket). Established connections are accepted without a per-packet log by design, so the view is a recent sample, not a complete record. |
| **Local Traffic Log** | Completed | Completed | Live PASS/DROP events for packets whose destination or source is the board itself: `input` chain ("Local-In", e.g. an admin reaching the web UI/SSH, or an unsolicited WAN scan) and `output` chain ("Local-Out", e.g. the board's own DNS/NTP lookups). These chains' log statements were moved off printk/journald onto a second NFLOG group (group 101, separate socket/queue from the forward chain's group 100 so high-volume forward traffic can never starve/drop the lower-volume local events) feeding the same RAM ring buffer as Forward Traffic, tagged with a `chain` field. The one exception is the input chain's AUDIT rule (an unconditional pre-verdict tap for kernel-level debugging, not a user-facing event) which stays on printk but is now rate-limited (3/min) rather than unlimited. Viewer UI at Log & Report › Local Traffic (All local / Local-In only / Local-Out only filter, text search, cursor-based infinite scroll, real-time SSE, pause/resume, clear — shares its component/hook with Forward Traffic, not a copy). |
| **HTTPS / TLS** | Completed | Completed | Real devices serve the admin UI over **HTTPS on 443 by default** using a self-signed certificate generated once on first boot (ECDSA P-256, stdlib `crypto/*`, no external CA/ACME). The key lives on disk under `<db-dir>/tls/` (0600, never in DB/backup). HTTP on 2479/80 issues a `308` redirect to HTTPS; if TLS cannot start (cert unwritable, port 443 unavailable) the server falls back to serving full HTTP so the admin is never locked out, with a loud warning + event log. The session cookie's `Secure` flag is set per-request from the connection scheme. Cert validity uses a fixed window (not derived from the clock) so a Pi with no RTC battery still gets a usable cert before NTP sync. Dev/mock mode (no `-https-port`) is HTTP-only and unchanged. |
| **Statistics → Traffic** | Completed | Completed | Statistics ▸ Traffic page: full (not top-10-cut) Top Source Hosts / Top Destinations tables with client-side filter/sort, plus a per-IP drill-down (`/statistics/traffic/host/:ip`) showing that IP's conversations in **both directions** — "ในฐานะต้นทาง" (as source) and "ในฐานะปลายทาง" (as destination, i.e. who talked to it) — with cross drill-down between peers. Built entirely on the existing RAM-only conntrack bucket ring (no new kernel capability); rows are 4-tuple conversations (src, dst, proto, dst port), not individual TCP connections. The Statistics Overview page's Top Hosts/Conversations rows link directly into this drill-down. |
| **Statistics → DNS** | Completed | Completed | Statistics ▸ DNS page: domain-centric query statistics with sortable/filterable Top Domains and Top Source Hosts tables (every column sortable, both directions), each with a Volume column (Down/Up/Total) alongside the existing query-count percent. A forward `domain → resolved IP` index (RAM-only, capped, TTL-based, built from the same dnsmasq answer log already tailed for the reverse cache) is joined against the existing traffic bucket ring to show, per domain, which resolved IPs it actually talked to and how many bytes each moved — surfaced in a dedicated "Resolved IPs" table on the domain drill-down page (`/statistics/dns/domain/:domain`), ranked by bandwidth, with a `shared` flag on IPs reused by more than one domain (e.g. CDNs) and a matching `sharedIps` flag on the domain row. Client drill-down (`/statistics/dns/client/:client`) gains the same per-domain volume breakdown and a bandwidth time series; the `unknown` client bucket intentionally has no volume (no IP to join against). Volume figures are an approximation — an info popover on both the overview and drill-down pages explains that shared IPs get double-counted across domains, non-DNS traffic to an IP is never attributed to a domain, and the domain→IP mapping has a TTL and is never used for firewall/policy/routing/QoS decisions. Built on the existing RAM-only conntrack bucket ring plus the new RAM-only forward index — no new kernel capability, and `/api/statistics/traffic` and the Overview page's Top Queried Domains card are unchanged. |

---

> **⚠ Self-signed HTTPS:** the first visit to `https://<device-ip>/` shows a browser certificate warning — this is expected for a LAN gateway with no public domain; accept it once to proceed. Public-CA / Let's Encrypt and in-UI certificate upload are planned for a later phase.
>
> **⚠ Upgrading an existing install:** you **must re-run `install.sh`** after updating the binary. HTTPS requires the `CAP_NET_BIND_SERVICE` capability and the `-https-port=443` flag, both added to the systemd unit by the installer. Replacing only `/usr/local/bin/pigate` leaves the old unit in place, so the server safely falls back to HTTP (no lockout) but will not serve HTTPS until the installer is re-run.

## How to Build

The project can be built into a single self-contained binary using the provided `build.sh` script, or manually by executing the individual compilation steps below.

### Quick Build via Script (Recommended)
```bash
bash build.sh
```

### Manual Compilation Steps
1. **Build the Frontend Interface:**
   ```bash
   cd frontend
   yarn install
   yarn build
   cd ..
   ```
2. **Copy the Production Build to the Backend Embed Location:**
   ```bash
   rm -rf backend/internal/api/dist
   mkdir -p backend/internal/api/dist
   cp -r frontend/dist/* backend/internal/api/dist/
   echo "# Placeholder" > backend/internal/api/dist/.gitkeep
   ```
3. **Build the Go Backend:**
   ```bash
   cd backend
   go build -o pigate-backend ./cmd/pigate
   cd ..
   mv ./backend/pigate-backend pigate
   ```
4. **Grant Linux Capabilities to the Executable (Required to run without Root privileges):**
   ```bash
   sudo setcap cap_net_admin,cap_net_raw+ep ./pigate
   ```

---

## Installation

The project includes an installation script that automates the setup of users, groups, directory permissions, Polkit configurations, and a Systemd service to ensure the application executes securely.

### Automated Installation
After successfully building the `pigate` executable, run the following installation command:
```bash
sudo bash install.sh
```

The script will perform the following actions:
1. Create a system user named `pigate` and append it to the `netdev` system group.
2. Configure Access Control Lists (ACLs) for `/etc/wpa_supplicant`, `/etc/dnsmasq.d`, `/etc/systemd/resolved.conf.d`, and the `systemd-timesyncd` drop-in directory (installing `dnsmasq` if it is missing).
3. Create a systemd template service `dhcpcd@.service` so the WAN-side DHCP client runs as its own root-owned unit, which PiGate starts/stops per interface via systemd D-Bus (no sudo required).
4. Create Polkit rules to authorize the `pigate` user to control `wpa_supplicant`, `systemd-resolved`, `dnsmasq`, `systemd-timesyncd`, and `dhcpcd@*` services via D-Bus.
5. Deploy the binary to `/usr/local/bin/pigate` and assign the required Linux capabilities.
6. Configure, register, and launch the Systemd service `pigate.service`.

### Service Management Post-Installation
- **Start Service:** `sudo systemctl start pigate`
- **Stop Service:** `sudo systemctl stop pigate`
- **Check Service Status:** `sudo systemctl status pigate`
- **View Log Output (Journal):** `sudo journalctl -u pigate -f`

### Configuration File
Runtime settings can be changed without editing the systemd unit. `install.sh` creates `/var/lib/pigate/pigate.conf` (a simple `key=value` file, `#` for comments) with production defaults on first install. Most keys are the CLI flag names without the leading `-`; seven keys (`dns-stats-max-pairs`, `dns-stats-max-clients`, `dns-stats-max-domains`, `dns-stats-max-ips-per-domain`, `traffic-stats-max-hosts`, `traffic-stats-max-dests`, `traffic-stats-max-conversations`) are intentionally file-only tuning knobs with no CLI flag counterpart — the three `traffic-stats-max-*` keys bound the Statistics ▸ Traffic page's RAM-only bucket ring (5-minute buckets, 288 deep = 24h); each raised value costs roughly `value × 288 × ~110 bytes` worst case (see `pigate.conf.example` for the exact numbers). `dns-stats-max-domains` (default `1000`, accepted range 100–20000) and `dns-stats-max-ips-per-domain` (default `16`, accepted range 2–64) bound the Statistics ▸ DNS page's `domain → resolved IP` forward index used for the domain volume/drill-down feature — unlike the ring-based keys above, this index is **not** a ring (it holds current knowledge, not per-bucket history), so its worst-case RAM is just `maxDomains × maxIPsPerDomain` entries (≈1 MB at the defaults), with no bucket-count multiplier. As with the other five keys, an out-of-range value is clamped back to its default with a startup warning rather than failing to boot. See [`pigate.conf.example`](pigate.conf.example) in the repo root for a fully commented sample of every key, its default, and what it does.

Precedence is **built-in default → config file → CLI flag** — a flag explicitly passed on the command line always wins over the file. Because the installed `ExecStart` still passes `-mock=false -db=… -https-port=443` as a safety net, editing those three keys in the file has no effect until they are removed from the unit; other keys (e.g. `docker-compat`) take effect from the file directly. Point at a different file with `-config=/path/to/file` (a missing explicit file is a hard error).

---

## Requirements

To ensure proper functionality, the host operating system must satisfy the following hardware, software, and dependency requirements:

### Hardware & Operating System
- **Raspberry Pi 5** single-board computer (or similar x86/ARM mini-PCs running Debian-based Linux distributions, such as Raspberry Pi OS).
- Elevated administrative privileges (`sudo` access) for the initial installation procedure.

### Software Dependencies
- **Linux Kernel** compiled with Netfilter and `nftables` support.
- **systemd** with D-Bus (used to control services, hostname via `systemd-hostnamed`, and time via `systemd-timedated`).
- **wpa_supplicant** (required for Wi-Fi management capabilities).
- **systemd-resolved** (required for system-wide DNS configuration management).
- **dnsmasq** (required for the DHCP Server and local DNS Server features; installed automatically by `install.sh`).
- **dhcpcd** (required for WAN-side DHCP client operation).
- **Yarn** package manager and **Node.js** runtime environment (required for building the frontend).
- **Go 1.26.4+** compiler (required for compiling the backend).
- **acl** command-line utility (required for file access control list configurations).

### Security Configurations
- For safety during development and testing on a personal workstation, it is highly recommended to run the system in **Mock Mode**. This prevents the application from modifying the host computer's actual routing tables:
  ```bash
  # Launch the mock environment on Port 8081
  ./pigate -port=8081 -db=pigate.db -mock=true
  
  # Launch the mock environment in Read-only Mode
  ./pigate -port=8081 -db=pigate.db -mock=true -disable-edit=true
  ```
  *Default login credentials: Username `pigate` | Password `Printed to console on first run`*
