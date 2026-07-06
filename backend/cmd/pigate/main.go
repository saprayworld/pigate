package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"strconv"

	// Embed the IANA timezone database so timezone validation
	// (time.LoadLocation) works even on minimal environments that lack a system
	// tzdata package (dev containers, etc.). ~450KB. On the Pi this simply
	// mirrors the system tzdata.
	_ "time/tzdata"

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
	log.Printf("[Main] Allow Edit System Routes: %t", *allowEditSystemRoutes)
	log.Printf("[Main] Enable Edit System Route (Bypass DB): %t", *enableEditSystemRoute)
	log.Printf("[Main] Prioritize Kernel Routes: %t", *prioritizeKernelRoutes)
	log.Printf("[Main] Docker Compatibility: %t", *dockerCompat)

	// 2. Initialize in-memory logs circular buffer (Ring Buffer)
	ringBuffer := logs.NewRingBuffer(50)

	// Seed some initial firewall log mock entries for visual display on dashboard
	ringBuffer.Add(model.FirewallLog{ID: "log-init-1", Time: "14:31:02", Action: "DROP", Src: "185.220.101.4", Dest: "10.0.0.45", Port: "445", Proto: "TCP", Reason: "Blocked Port (SMB)"})
	ringBuffer.Add(model.FirewallLog{ID: "log-init-2", Time: "14:31:15", Action: "PASS", Src: "192.168.1.105", Dest: "8.8.8.8", Port: "53", Proto: "UDP", Reason: "DNS request"})

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
	systemStatusService := service.NewSystemStatusService(sysStats, repo, hostnameService, timeService, version)

	// Netlink monitor is created here (but started later, after startup config is
	// applied) so it can be injected into the BackupService, which pauses it
	// around a config import.
	netlinkMonitor := service.NewNetlinkMonitor(repo, routingService, dnsService, dhcpcdService)

	backupService := service.NewBackupService(
		repo, *dbPath, version,
		ifaceService, routingService, firewallService, dnsService, dnsServerService,
		qosService, dhcpServerService, dhcpcdService, hostnameService, timeService,
		netlinkMonitor,
	)

	server := api.NewServer(repo, fw, net, rt, dhcp, ringBuffer, *disableEdit, ifaceService, routingService, firewallService, dnsService, qosService, dhcpServerService, dnsServerService, hostnameService, timeService, userService, backupService, systemStatusService)

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

	// 6.2.1 Start Netlink Monitor to dynamically handle network and routing events
	log.Printf("[Main] Initializing Netlink event monitor...")
	monitorCtx, cancelMonitor := context.WithCancel(context.Background())
	defer cancelMonitor()
	netlinkMonitor.Start(monitorCtx)

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

	handler := api.RegisterRoutes(server)

	// 7. Start HTTP API listener
	address := ":" + strconv.Itoa(*port)
	log.Printf("[Main] ===== PiGate API Backend is listening at http://localhost%s =====", address)
	if err := http.ListenAndServe(address, handler); err != nil {
		log.Fatalf("Server listener failed: %v", err)
	}
}
