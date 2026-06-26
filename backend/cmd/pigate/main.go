package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"strconv"

	"pigate/internal/api"
	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/logs"
	"pigate/internal/model"
	"pigate/internal/service"
)

func main() {
	// 1. Parse CLI flags
	port := flag.Int("port", 2479, "Port to run the API server on")
	dbPath := flag.String("db", "pigate.db", "Path to SQLite database file")
	mockOS := flag.Bool("mock", true, "Use mocked kernel operations (default true on PC)")
	mockFromReal := flag.Bool("mock-from-real", false, "Mock operations but initialize/pull from real kernel data at startup")
	disableEdit := flag.Bool("disable-edit", false, "Disable edit operations (Read-only mode)")
	allowEditSystemRoutes := flag.Bool("allow-edit-system-routes", false, "Allow editing and deleting system predefined static routes")
	prioritizeKernelRoutes := flag.Bool("prioritize-kernel-routes", false, "Prioritize kernel route information over database if duplicate")
	dockerCompat := flag.Bool("docker-compat", true, "Enable Docker compatibility (bypass docker0 and br-* interfaces)")
	version := flag.Bool("v", false, "Print Version")
	flag.Parse()
	if *version {
		log.Printf("PiGate Server version %s", "v0.1.0-pre")
		return
	}
	log.Printf("[Main] Starting PiGate Backend Server (Go v1.26.4)...")
	log.Printf("[Main] Port: %d", *port)
	log.Printf("[Main] Database: %s", *dbPath)
	log.Printf("[Main] Mock OS Integration: %t", *mockOS)
	log.Printf("[Main] Mock From Real Data: %t", *mockFromReal)
	log.Printf("[Main] Disable Edit Mode: %t", *disableEdit)
	log.Printf("[Main] Allow Edit System Routes: %t", *allowEditSystemRoutes)
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

	// Perform initial synchronization of interfaces, routing table, and DNS if real mode or mock-from-real is enabled
	// if !*mockOS || *mockFromReal {
	// 	log.Printf("[Main] Initializing and syncing interfaces, routes, and DNS from OS kernel...")
	// 	if err := repo.SyncInterfacesFromOS(); err != nil {
	// 		log.Printf("[Main] Warning: Failed to sync network interfaces from OS: %v", err)
	// 	}
	// 	if err := repo.SyncRoutesFromOS(); err != nil {
	// 		log.Printf("[Main] Warning: Failed to sync static routes from OS: %v", err)
	// 	}
	// 	if err := repo.SyncDNSFromOS(); err != nil {
	// 		log.Printf("[Main] Warning: Failed to sync DNS settings from OS: %v", err)
	// 	}
	// }

	// 4. Instantiate Kernel managers (Force Mock layer for now)
	var fw kernel.FirewallManager
	var net kernel.NetworkManager
	var rt kernel.RoutingManager
	var dhcp kernel.DhcpManager

	if *mockOS || *mockFromReal {
		fw = kernel.NewMockFirewall(*dockerCompat)
		net = kernel.NewMockNetwork()
		rt = kernel.NewMockRouting()
		mDhcp := kernel.NewMockDhcp()
		mDhcp.MockFromReal = *mockFromReal
		dhcp = mDhcp
	} else {
		// Real kernel integrations via netlink — used on Raspberry Pi 5 production.
		// Requires: sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend
		fw = kernel.NewRealFirewall(*dockerCompat)
		net = kernel.NewRealNetwork()
		rt = kernel.NewRealRouting(*allowEditSystemRoutes)
		mDhcp := kernel.NewMockDhcp()
		mDhcp.MockFromReal = false
		dhcp = mDhcp
	}

	// 5. Instantiate Server & Router
	ifaceService := service.NewInterfaceService(repo, net)
	routingService := service.NewRoutingService(repo, rt)
	server := api.NewServer(repo, fw, net, rt, dhcp, ringBuffer, *disableEdit, ifaceService, routingService)

	// Apply config form database to kernel

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
	netlinkMonitor := service.NewNetlinkMonitor(repo, routingService)
	monitorCtx, cancelMonitor := context.WithCancel(context.Background())
	defer cancelMonitor()
	netlinkMonitor.Start(monitorCtx)

	log.Printf("[Main] [Not Implemented] Applying database-configured DHCP settings to kernel at startup...")

	log.Printf("[Main] [Not Implemented] Applying database-configured DNS settings to kernel at startup...")

	// 6.3 Apply Firewall Rules at startup
	log.Printf("[Main] [Not Implemented] Applying database-configured firewall rules to kernel at startup...")
	// rules, err := repo.GetPolicies()
	// if err != nil {
	// 	log.Printf("[Main] Warning: Failed to load policies from DB for startup apply: %v", err)
	// } else {
	// 	ifaces, err := repo.GetInterfaces()
	// 	if err != nil {
	// 		log.Printf("[Main] Warning: Failed to load interfaces from DB for startup apply: %v", err)
	// 	} else {
	// 		addrs, err := repo.GetAddresses()
	// 		if err != nil {
	// 			log.Printf("[Main] Warning: Failed to load address objects from DB for startup apply: %v", err)
	// 		} else {
	// 			svcs, err := repo.GetServices()
	// 			if err != nil {
	// 				log.Printf("[Main] Warning: Failed to load service objects from DB for startup apply: %v", err)
	// 			} else {
	// 				if err := fw.ApplyRules(rules, ifaces, addrs, svcs); err != nil {
	// 					log.Printf("[Main] Warning: Failed to apply firewall rules to kernel at startup: %v", err)
	// 				} else {
	// 					log.Printf("[Main] Successfully applied firewall rules at startup.")
	// 				}
	// 			}
	// 		}
	// 	}
	// }

	handler := api.RegisterRoutes(server)

	// 7. Start HTTP API listener
	address := ":" + strconv.Itoa(*port)
	log.Printf("[Main] ===== PiGate API Backend is listening at http://localhost%s =====", address)
	if err := http.ListenAndServe(address, handler); err != nil {
		log.Fatalf("Server listener failed: %v", err)
	}
}
