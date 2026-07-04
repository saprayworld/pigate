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
│   │   │   ├── wpa.go               # Wi-Fi management via unix control socket wpa_supplicant
│   │   │   ├── dns.go               # DNS configuration and systemd-resolved control via D-Bus
│   │   │   └── mock.go              # Memory-resident mock implementation for local testing
│   │   ├── service/                 # System Coordination & Business Logic Layer
│   │   │   ├── interface.go         # Network interface status update logic
│   │   │   ├── routing.go           # Routing logic and metric coordination
│   │   │   ├── netlink_monitor.go   # Background service monitoring Kernel events for state reconciliation
│   │   │   └── firewall.go          # Firewall security policy management logic
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
├── readme-ref.md                    # Reference template for README.md
└── LICENSE                          # Software license agreement
```

---

## Feature Status

The following table summarizes the development status of each feature in the PiGate system:

| Feature | Frontend | Backend | Status / Remarks |
|---|---|---|---|
| **Dashboard** | Mock | Mock | Real-time traffic graphs (Recharts), system status via Server-Sent Events (SSE), and security event logs. |
| **Interface** | Completed | Completed | IP management, Netlink interface handling, `wpa_supplicant` Wi-Fi scanning and state management, and random MAC addresses. |
| **Routing** | Completed | Completed | CRUD operations for static routes, Netlink event monitoring, and automatic routing self-healing. |
| **DNS System** | Completed | Completed | `systemd-resolved` D-Bus integration completed; local DNS server integration is ongoing. |
| **Firewall System** | Completed | Completed | `nftables` management via Netlink, forward chain policy configuration, WAN Network Address Translation (Masquerade), and Docker compatibility. |
| **DHCP Server** | Mock | In Progress | UI and SQLite database model completed; configuration generation for `dnsmasq` or `isc-dhcp-server` is ongoing. |
| **DNS Server** | Mock | In Progress | UI and SQLite database model completed; configuration generation for local DNS resolution/FQDN is ongoing. |
| **QoS Limiting** | Completed | Completed | HTB and IFB traffic shaping via tc Netlink, supporting Source/Destination IP address ranges (CIDR). |
| **Setting (Overall)** | Mock | Mock | Administrator password updates, time settings, and system service lifecycle controls via D-Bus. |
| **Import/Export** | Completed | Completed | Typed JSON backup (schema v2) with SHA-256 integrity, optional user accounts, and optional passphrase encryption (AES-256-GCM + Argon2id); import uses validate → pre-import snapshot → single-transaction wipe & restore → kernel re-apply (startup order). Cross-machine safe (raw routes, interface match-by-name), `super_admin`-only, with actor lock-out guard. Accepts legacy v1 files. |
| **User System** | Completed | Completed | Multi-user management (create/edit/delete/enable-disable) with `super_admin` / `admin_readonly` roles, per-request DB-backed session validation, role-based authorization middleware, session-based auth, login rate limiting, and first-time login password change enforcement. |
| **System Time** | Mock | Mock | Native operating system time synchronization and configuration. |
| **Power Control (Shutdown/Restart)** | Mock | Mock | Remote power actions (system shutdown or reboot) executed via API. |

---

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
2. Configure Access Control Lists (ACLs) for `/etc/wpa_supplicant` and `/etc/systemd/resolved.conf.d`.
3. Create Polkit rules at `/etc/polkit-1/rules.d/10-pigate-wpa.rules` to authorize the `pigate` user to interact with and control `wpa_supplicant` and `systemd-resolved` services via D-Bus.
4. Grant Linux capabilities directly to `/usr/sbin/dhcpcd` (no sudo/root required) and prepare `/var/lib/dhcpcd` for the `pigate` user.
5. Deploy the binary to `/usr/local/bin/pigate` and assign the required Linux capabilities.
6. Configure, register, and launch the Systemd service `pigate.service`.

### Service Management Post-Installation
- **Start Service:** `sudo systemctl start pigate`
- **Stop Service:** `sudo systemctl stop pigate`
- **Check Service Status:** `sudo systemctl status pigate`
- **View Log Output (Journal):** `sudo journalctl -u pigate -f`

---

## Requirements

To ensure proper functionality, the host operating system must satisfy the following hardware, software, and dependency requirements:

### Hardware & Operating System
- **Raspberry Pi 5** single-board computer (or similar x86/ARM mini-PCs running Debian-based Linux distributions, such as Raspberry Pi OS).
- Elevated administrative privileges (`sudo` access) for the initial installation procedure.

### Software Dependencies
- **Linux Kernel** compiled with Netfilter and `nftables` support.
- **NetworkManager** daemon (with active D-Bus configuration).
- **wpa_supplicant** (required for Wi-Fi management capabilities).
- **systemd-resolved** (required for system-wide DNS configuration management).
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
