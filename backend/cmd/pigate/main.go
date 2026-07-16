package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	// Embed the IANA timezone database so timezone validation
	// (time.LoadLocation) works even on minimal environments that lack a system
	// tzdata package (dev containers, etc.). ~450KB. On the Pi this simply
	// mirrors the system tzdata.
	_ "time/tzdata"

	"github.com/google/uuid"

	"pigate/internal/api"
	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/logs"
	"pigate/internal/model"
	"pigate/internal/service"
)

// version is the PiGate build version. It is overridable at build time via
// -ldflags "-X main.version=<tag>" (see build.sh); the default applies to plain
// `go build` / `go run` during development.
var version = "v0.1.0-pre"

func main() {
	// 1. Parse CLI flags
	port := flag.Int("port", 2479, "Port to run the API server on")
	dbPath := flag.String("db", "pigate.db", "Path to SQLite database file")
	mockOS := flag.Bool("mock", true, "Use mocked kernel operations (default true on PC)")
	mockFromReal := flag.Bool("mock-from-real", false, "Mock operations but initialize/pull from real kernel data at startup")
	disableEdit := flag.Bool("disable-edit", false, "Disable edit operations (Read-only mode)")
	allowEditSystemRoutes := flag.Bool("allow-edit-system-routes", false, "Allow editing and deleting system predefined static routes")
	enableEditSystemRoute := flag.Bool("enable-edit-system-route", false, "Enable direct kernel management of system/kernel-only routes without database")
	prioritizeKernelRoutes := flag.Bool("prioritize-kernel-routes", false, "Prioritize kernel route information over database if duplicate")
	dockerCompat := flag.Bool("docker-compat", true, "Enable Docker compatibility (bypass docker0 and br-* interfaces)")
	httpsPort := flag.Int("https-port", 0, "HTTPS port (0 = HTTP only; the systemd unit passes 443 to make HTTPS the primary channel)")
	tlsDir := flag.String("tls-dir", "", "Directory for the self-signed TLS cert/key (default: <dir of -db>/tls)")
	allowDevCORS := flag.Bool("allow-dev-cors", false, "Echo CORS headers for frontend dev-server origins (localhost:5173/3000). Off by default; only needed when running `yarn dev` against this backend.")
	printVersion := flag.Bool("v", false, "Print Version")
	flag.Parse()
	if *printVersion {
		log.Printf("PiGate Server version %s", version)
		return
	}
	log.Printf("[Main] Starting PiGate Backend Server (Go v1.26.4)...")
	log.Printf("[Main] Port: %d", *port)
	log.Printf("[Main] Database: %s", *dbPath)
	log.Printf("[Main] Mock OS Integration: %t", *mockOS)
	log.Printf("[Main] Mock From Real Data: %t", *mockFromReal)
	log.Printf("[Main] Disable Edit Mode: %t", *disableEdit)
	log.Printf("[Main] Allow Dev CORS Origins: %t", *allowDevCORS)
	log.Printf("[Main] Allow Edit System Routes: %t", *allowEditSystemRoutes)
	log.Printf("[Main] Enable Edit System Route (Bypass DB): %t", *enableEditSystemRoute)
	log.Printf("[Main] Prioritize Kernel Routes: %t", *prioritizeKernelRoutes)
	log.Printf("[Main] Docker Compatibility: %t", *dockerCompat)
	log.Printf("[Main] HTTPS Port: %d (0 = HTTP only)", *httpsPort)

	// 2. Initialize in-memory forward-traffic logs circular buffer (Ring Buffer).
	// Fed live by the TrafficLogManager watcher below (real NFLOG or mock
	// generator); powers both the Forward Traffic page and the Dashboard Recent
	// Logs widget. RAM-only — never persisted (SD card wear, tech_stack_design.md
	// §8). Capacity 500: a FirewallLog is a handful of short strings, so this is
	// only a few hundred KB while giving the log view a useful window.
	ringBuffer := logs.NewRingBuffer(500)

	// 3. Initialize SQLite DB & run migrations
	sqliteDB, err := db.InitDB(*dbPath, *mockOS)
	if err != nil {
		log.Fatalf("Fatal error initializing SQLite DB: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(*mockOS, *mockFromReal)
	repo.SetAllowEditSystemRoutes(*allowEditSystemRoutes)
	repo.SetPrioritizeKernelRoutes(*prioritizeKernelRoutes)

	// 4. Instantiate Kernel managers (Force Mock layer for now)
	var fw kernel.FirewallManager
	var net kernel.NetworkManager
	var rt kernel.RoutingManager
	var dhcp kernel.DhcpManager
	var qos kernel.QosManager
	var dnsServer kernel.DNSServerManager
	var dhcpcd kernel.DhcpcdManager
	var hostnameMgr kernel.HostnameManager
	var timeMgr kernel.TimeManager
	var sysStats kernel.SystemStatsManager
	var powerMgr kernel.PowerManager
	var trafficLog kernel.TrafficLogManager
	dns := kernel.NewDNSManager(*mockOS)

	if *mockOS || *mockFromReal {
		fw = kernel.NewMockFirewall(*dockerCompat)
		net = kernel.NewMockNetwork()
		rt = kernel.NewMockRouting()
		qos = kernel.NewMockQos()
		mDhcp := kernel.NewMockDhcp()
		mDhcp.MockFromReal = *mockFromReal
		dhcp = mDhcp
		dnsServer = kernel.NewMockDNSServerManager()
		dhcpcd = kernel.NewMockDhcpcdManager()
		hostnameMgr = kernel.NewMockHostnameManager()
		timeMgr = kernel.NewMockTimeManager()
		sysStats = kernel.NewMockSystemStats()
		powerMgr = kernel.NewMockPowerManager()
		trafficLog = kernel.NewMockTrafficLog()
	} else {
		// Real kernel integrations via netlink — used on Raspberry Pi 5 production.
		// Requires: sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend
		fw = kernel.NewRealFirewall(*dockerCompat)
		net = kernel.NewRealNetwork()
		rt = kernel.NewRealRouting(*allowEditSystemRoutes)
		qos = kernel.NewRealQos()
		dhcp = kernel.NewRealDhcpManager()
		dnsServer = kernel.NewRealDNSServerManager()
		dhcpcd = kernel.NewRealDhcpcdManager()
		hostnameMgr = kernel.NewRealHostnameManager()
		timeMgr = kernel.NewRealTimeManager()
		sysStats = kernel.NewRealSystemStats()
		powerMgr = kernel.NewRealPowerManager()
		trafficLog = kernel.NewRealTrafficLog()
	}

	// 5. Instantiate Server & Router
	ifaceService := service.NewInterfaceService(repo, net)
	dhcpcdService := service.NewDhcpcdService(repo, ifaceService, dhcpcd)
	routingService := service.NewRoutingService(repo, rt)
	routingService.SetEnableEditSystemRoute(*enableEditSystemRoute)
	firewallService := service.NewFirewallService(repo, fw, ifaceService)
	dnsService := service.NewDNSService(repo, dns)
	qosService := service.NewQosService(repo, qos)
	dhcpServerService := service.NewDhcpServerService(repo, dhcp)
	dnsServerService := service.NewDNSServerService(repo, dnsServer, dnsService)
	hostnameService := service.NewHostnameService(repo, hostnameMgr, dhcpcd, ifaceService)
	timeService := service.NewTimeService(repo, timeMgr)
	userService := service.NewUserService(repo)
	powerService := service.NewPowerService(powerMgr)
	systemStatusService := service.NewSystemStatusService(sysStats, repo, hostnameService, timeService, version)

	// Central event log: every subsystem funnels audit events through this one
	// service (RAM queue + async batch writer to SQLite; see event_log.go).
	eventLogService := service.NewEventLogService(repo)
	dhcpServerService.SetEventLog(eventLogService)

	// Self-healing internal event bus: NetlinkMonitor translates raw kernel events
	// into semantic NetEvents (InterfaceAdded/Removed, LinkChanged, AddrRouteChanged)
	// and publishes them here; interested services subscribe below. This is what makes
	// an interface that vanished and came back re-apply its config on its own without
	// the user touching the UI (issue #48). Subscriptions are registered before the
	// monitor is started (further down, after all startup applies complete).
	eventBus := service.NewNetEventBus()

	// InterfaceService: only a genuinely new/returned interface (InterfaceAdded)
	// re-applies its DB config — a mere flag flap (LinkChanged) must not, or a
	// blinking link would trigger a re-apply storm. Debounced + scoped by name.
	eventBus.Subscribe("interface", []service.NetEventKind{service.InterfaceAdded}, service.Debounced,
		func(e service.NetEvent) {
			log.Printf("[Self-heal] Interface %q returned; re-applying its configuration", e.Name)
			ifaceService.ReapplyInterfaceByName(e.Name)
		})

	// dhcpcd: must observe every link transition in order (Wi-Fi waits for RUNNING),
	// so Immediate mode across Added/Changed/Removed.
	eventBus.Subscribe("dhcpcd",
		[]service.NetEventKind{service.InterfaceAdded, service.LinkChanged, service.InterfaceRemoved},
		service.Immediate,
		func(e service.NetEvent) {
			dhcpcdService.HandleLinkEvent(e.Name, e.Up, e.Running)
		})

	// Routing reconciles on any address/route change or link flag change — routes
	// genuinely can shift when a link flaps, so it must observe those.
	// Debounced: coalesce a burst into a single full reconcile (idempotent).
	eventBus.Subscribe("routing",
		[]service.NetEventKind{service.AddrRouteChanged, service.LinkChanged},
		service.Debounced,
		func(e service.NetEvent) {
			if err := routingService.ReconcileKernelRoutingTable(); err != nil {
				log.Printf("[Self-heal] Error reconciling routing table: %v", err)
			}
		})

	// DNS client only reacts to a genuinely new/returned interface (InterfaceAdded),
	// NOT to LinkChanged. The global DNS config is a system-wide resolved drop-in
	// that does not depend on any single link's up/running state, so a Wi-Fi
	// scan/reconnect flap must not trigger a re-apply (which would restart
	// systemd-resolved and drop DNS). ApplyDNSConfig is idempotent-guarded, so even
	// this InterfaceAdded path is a no-op when the config is unchanged (issue #57).
	eventBus.Subscribe("dns",
		[]service.NetEventKind{service.InterfaceAdded},
		service.Debounced,
		func(e service.NetEvent) {
			if err := dnsService.ApplyDNSConfig(); err != nil {
				log.Printf("[Self-heal] Error applying DNS configuration: %v", err)
			}
		})

	// DHCP server: when an interface returns, re-run the full config so its dhcp-range
	// (which was skipped while the interface was gone) is restored.
	eventBus.Subscribe("dhcp-server", []service.NetEventKind{service.InterfaceAdded}, service.Debounced,
		func(e service.NetEvent) {
			if err := dhcpServerService.ApplyAll(); err != nil {
				log.Printf("[Self-heal] Error re-applying DHCP server config: %v", err)
			}
		})

	// QoS: re-attach qdiscs/classes to an interface that came back.
	eventBus.Subscribe("qos", []service.NetEventKind{service.InterfaceAdded}, service.Debounced,
		func(e service.NetEvent) {
			if err := qosService.SyncToKernel(); err != nil {
				log.Printf("[Self-heal] Error re-syncing QoS to kernel: %v", err)
			}
		})

	// Event log: surface interface come-and-go to the user (self-healing must be
	// observable). Immediate so the log ordering matches reality.
	eventBus.Subscribe("event-log",
		[]service.NetEventKind{service.InterfaceAdded, service.InterfaceRemoved},
		service.Immediate,
		func(e service.NetEvent) {
			switch e.Kind {
			case service.InterfaceAdded:
				eventLogService.Log(model.EventCategoryNetwork, "network.interface.up", model.EventSeverityInfo,
					model.EventActorSystem, e.Name, "Interface "+e.Name+" appeared; re-applying configuration")
			case service.InterfaceRemoved:
				eventLogService.Log(model.EventCategoryNetwork, "network.interface.down", model.EventSeverityWarning,
					model.EventActorSystem, e.Name, "Interface "+e.Name+" removed from kernel")
			}
		})

	// Netlink monitor is created here (but started later, after startup config is
	// applied) so it can be injected into the BackupService, which pauses it (and
	// hence the whole bus) around a config import.
	netlinkMonitor := service.NewNetlinkMonitor(repo, eventBus)

	backupService := service.NewBackupService(
		repo, *dbPath, version,
		ifaceService, routingService, firewallService, dnsService, dnsServerService,
		qosService, dhcpServerService, dhcpcdService, hostnameService, timeService,
		netlinkMonitor,
	)

	server := api.NewServer(repo, fw, net, rt, dhcp, ringBuffer, *disableEdit, *allowDevCORS, ifaceService, dhcpcdService, routingService, firewallService, dnsService, qosService, dhcpServerService, dnsServerService, hostnameService, timeService, userService, backupService, systemStatusService, powerService, eventLogService)

	// Apply config form database to kernel

	// 6.0 Apply Time (timezone / NTP) configuration first. Correct time makes
	// log timestamps and any TLS validation in the following steps sane. This
	// applies only timezone + NTP config — never the wall clock itself (see
	// TimeService.InitApplyConfig).
	log.Printf("[Main] Applying database-configured time/NTP settings to kernel at startup...")
	if err := timeService.InitApplyConfig(); err != nil {
		log.Printf("[Main] Warning: Failed to apply time/NTP settings at startup: %v", err)
	}

	// 6.1 Apply Network Interfaces configuration at startup
	log.Printf("[Main] Applying database-configured network interfaces to kernel at startup...")
	if err := ifaceService.InitApplyConfigurationAtStartup(); err != nil {
		log.Printf("[Main] Warning: Failed to apply network interfaces to kernel at startup: %v", err)
	}

	// 6.2 Apply Static Routes configuration at startup
	log.Printf("[Main] Applying database-configured static routes to kernel at startup...")
	if err := routingService.InitApplyConfig(); err != nil {
		log.Printf("[Main] Warning: Failed to apply static routes to kernel at startup: %v", err)
	}

	// The netlink monitor is started later (after every subsystem's startup apply has
	// completed) so boot-time link events don't race the startup path — but its
	// context is created here because the watchers/samplers below share it.
	monitorCtx, cancelMonitor := context.WithCancel(context.Background())
	defer cancelMonitor()

	// Start the forward-traffic log watcher. It feeds the shared ring buffer that
	// backs the Forward Traffic page and Dashboard Recent Logs. Each event is
	// stamped with an RFC3339 UTC timestamp (the frontend formats it for display)
	// and a unique id. Real mode reads NFLOG group 100; mock mode synthesizes
	// events. If the watcher errors (e.g. NFLOG unavailable), the buffer simply
	// stays empty — packets are unaffected.
	log.Printf("[Main] Starting forward-traffic log watcher...")
	go func() {
		err := trafficLog.WatchForwardTraffic(monitorCtx, func(entry model.FirewallLog) {
			entry.Time = time.Now().UTC().Format(time.RFC3339)
			entry.ID = uuid.NewString()
			ringBuffer.Add(entry)
		})
		if err != nil && monitorCtx.Err() == nil {
			log.Printf("[Main] Warning: forward-traffic log watcher stopped: %v", err)
		}
	}()

	// Start the event log batch writer (flushes queued events to SQLite in
	// batches to preserve the SD card).
	eventLogService.Start(monitorCtx)

	// Start the session sweeper: reaps in-memory sessions past their idle deadline
	// so abandoned tokens don't linger in the map until restart.
	api.StartSessionSweeper(monitorCtx)

	// Start the rate-limiter sweeper: reaps idle per-IP token buckets so the
	// limiter map stays bounded (backstopped by a hard cap during bursts).
	api.StartLimiterSweeper(monitorCtx)

	// Start the dashboard telemetry sampler (CPU usage + WAN traffic history).
	// Shares the monitor context so it stops on shutdown.
	log.Printf("[Main] Starting system status telemetry sampler...")
	systemStatusService.Start(monitorCtx)

	log.Printf("[Main] Applying database-configured hostname settings to kernel at startup...")
	if err := hostnameService.InitApplyConfig(); err != nil {
		log.Printf("[Main] Warning: Failed to apply hostname settings at startup: %v", err)
	}

	log.Printf("[Main] Synchronizing active DHCP interfaces status...")
	dhcpcdService.SyncActiveInterfaces()

	log.Printf("[Main] Applying database-configured DHCP settings to kernel at startup...")
	if err := dhcpServerService.InitApplyConfig(); err != nil {
		log.Printf("[Main] Warning: Failed to apply DHCP configurations at startup: %v", err)
	}

	// Start D-Bus lease watcher in production mode (non-mock)
	if !*mockOS {
		go func() {
			if err := dhcpServerService.StartLeaseWatcher(monitorCtx); err != nil {
				log.Printf("[Main] Warning: DHCP lease watcher encountered error: %v", err)
			}
		}()
	}

	log.Printf("[Main] Applying database-configured DNS local zones to kernel at startup...")
	if err := dnsServerService.InitApplyConfig(); err != nil {
		log.Printf("[Main] Warning: Failed to apply DNS local zones at startup: %v", err)
	}

	log.Printf("[Main] Applying database-configured DNS settings to kernel at startup...")
	if err := dnsService.ApplyDNSConfig(); err != nil {
		log.Printf("[Main] Warning: Failed to apply DNS configurations to kernel at startup: %v", err)
	}

	// 6.3 Apply Firewall Rules at startup
	log.Printf("[Main] Applying database-configured firewall rules to kernel at startup...")
	if err := firewallService.InitApplyConfig(); err != nil {
		log.Printf("[Main] Warning: Failed to apply firewall rules to kernel at startup: %v", err)
	}

	// 6.4 Apply QoS Rules at startup
	log.Printf("[Main] Applying database-configured QoS rules to kernel at startup...")
	if err := qosService.InitApplyConfig(); err != nil {
		log.Printf("[Main] Warning: Failed to apply QoS rules to kernel at startup: %v", err)
	}

	// 6.5 Start the Netlink event monitor LAST, once every subsystem's startup apply
	// has completed. Starting it earlier would let the flurry of boot-time link events
	// (dhcpcd bringing links up) fire self-heal re-applies that race the startup path
	// above (issue #48). A brief drift window between the applies and Start is
	// acceptable — the startup applies just ran.
	log.Printf("[Main] Starting Netlink event monitor (self-healing event bus)...")
	netlinkMonitor.Start(monitorCtx)

	handler := api.RegisterRoutes(server)

	// Record the boot event — the persisted counterpart of system.reboot /
	// system.shutdown, proving the box came back up.
	eventLogService.Log(model.EventCategorySystem, "system.boot", model.EventSeverityInfo,
		model.EventActorSystem, "host", "PiGate backend started (version "+version+")")

	// 7. Start HTTP/HTTPS API listeners.
	// See docs/ref/todo/https-server-foundation-plan.md for the full rationale.
	// Ladder:
	//   httpsPort > 0 (systemd unit passes 443 → HTTPS is the primary channel):
	//     (1) cert OK + :443 binds → HTTPS serves the real handler (TLS 1.2+),
	//         HTTP :<port> (and bonus :80 when httpsPort==443) 308-redirect to HTTPS.
	//     (2) cert fails OR :443 won't bind → warn loudly + event log, then HTTP
	//         :<port> serves the real handler (last-resort fallback; admin must be
	//         able to reach the box no matter what).
	//   httpsPort == 0 (dev/mock, no flag): HTTP :<port> serves the real handler —
	//     identical to the legacy behavior.
	httpAddr := ":" + strconv.Itoa(*port)

	if *httpsPort > 0 {
		tlsDirResolved := *tlsDir
		if tlsDirResolved == "" {
			tlsDirResolved = filepath.Join(filepath.Dir(*dbPath), "tls")
		}

		hostname := "pigate"
		if hs, hErr := hostnameService.Get(); hErr == nil && hs.Hostname != "" {
			hostname = hs.Hostname
		}

		certPath, keyPath, tlsErr := setupTLS(tlsDirResolved, hostname, eventLogService)
		if tlsErr == nil {
			httpsAddr := ":" + strconv.Itoa(*httpsPort)
			// Probe-bind :443 up front: if it fails we fall through to the HTTP
			// fallback instead of dying after HTTP has already become a redirect.
			// (bindTCP wraps net.Listen; the local kernel manager variable named
			// "net" shadows the net package inside main.)
			ln, bindErr := bindTCP(httpsAddr)
			if bindErr == nil {
				redirect := newHTTPSRedirectHandler(*httpsPort)
				startRedirectListener(httpAddr, redirect)
				if *httpsPort == 443 {
					// Bonus: catch users who type the bare http://<ip> (port 80).
					startRedirectListener(":80", redirect)
				}

				httpsServer := &http.Server{
					Handler:           handler,
					TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
					ReadHeaderTimeout: 10 * time.Second,
					ReadTimeout:       30 * time.Second,
					WriteTimeout:      60 * time.Second,
					IdleTimeout:       120 * time.Second,
				}
				log.Printf("[Main] ===== PiGate API Backend is listening at https://localhost%s (HTTP %s → 308 redirect) =====", httpsAddr, httpAddr)
				if err := httpsServer.ServeTLS(ln, certPath, keyPath); err != nil {
					log.Fatalf("HTTPS server listener failed: %v", err)
				}
				return
			}
			log.Printf("[Main] Warning: could not bind HTTPS port %s: %v", httpsAddr, bindErr)
		} else {
			log.Printf("[Main] Warning: could not set up TLS certificate: %v", tlsErr)
		}

		// Fallthrough — TLS could not be started. Serve full HTTP so the admin can
		// still reach the box, but make the degradation impossible to miss.
		log.Printf("[Main] ***** WARNING: HTTPS unavailable — serving PLAIN HTTP on %s. Re-run install.sh to restore HTTPS. *****", httpAddr)
		eventLogService.Log(model.EventCategorySystem, "system.https_fallback", model.EventSeverityWarning,
			model.EventActorSystem, "host", "HTTPS could not start; serving plain HTTP as a fallback (re-run install.sh)")
	}

	// Plain HTTP: dev/mock (no -https-port) or the last-resort fallback above.
	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("[Main] ===== PiGate API Backend is listening at http://localhost%s =====", httpAddr)
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("Server listener failed: %v", err)
	}
}

// bindTCP opens a TCP listener on addr. It exists so main() can bind a socket
// without referencing the net package directly — the local kernel.NetworkManager
// variable named "net" shadows the package inside main().
func bindTCP(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// newHTTPSRedirectHandler returns a handler that 308-redirects any request to the
// same host+path over HTTPS on httpsPort (the port is omitted from the target when
// it is the standard 443). 308 (Permanent Redirect) preserves the method/body,
// unlike 301/302, which matters for API clients that POST to /api over HTTP.
func newHTTPSRedirectHandler(httpsPort int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		target := "https://" + host
		if httpsPort != 443 {
			target += ":" + strconv.Itoa(httpsPort)
		}
		target += r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
}

// startRedirectListener starts an HTTP server on addr in a background goroutine
// serving the redirect handler. Bind/serve failures are logged, never fatal — a
// failed :80 bonus listener (or a :<port> already in use) must not take the whole
// process, including the primary HTTPS listener, down with it.
func startRedirectListener(addr string, h http.Handler) {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("[Main] HTTP redirect listener starting on %s (308 → HTTPS)", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Main] Warning: HTTP redirect listener on %s stopped: %v", addr, err)
		}
	}()
}

// setupTLS ensures a self-signed certificate/key pair exists under tlsDir and
// returns their paths. A newly generated cert is recorded in the event log.
func setupTLS(tlsDir, hostname string, eventLog *service.EventLogService) (certPath, keyPath string, err error) {
	certPath, keyPath, generated, err := service.EnsureSelfSignedCert(tlsDir, hostname, service.LocalInterfaceIPs())
	if err != nil {
		return "", "", err
	}
	if generated {
		log.Printf("[Main] Generated self-signed TLS certificate in %s", tlsDir)
		if eventLog != nil {
			eventLog.Log(model.EventCategorySystem, "system.tls_cert_generated", model.EventSeverityInfo,
				model.EventActorSystem, "host", "Generated self-signed TLS certificate for HTTPS")
		}
	}
	return certPath, keyPath, nil
}
