package main

import (
	"flag"
	"log"
	"net/http"
	"strconv"

	"pigate/internal/api"
	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/logs"
	"pigate/internal/model"
)

func main() {
	// 1. Parse CLI flags
	port := flag.Int("port", 8080, "Port to run the API server on")
	dbPath := flag.String("db", "pigate.db", "Path to SQLite database file")
	mockOS := flag.Bool("mock", true, "Use mocked kernel operations (default true on PC)")
	mockFromReal := flag.Bool("mock-from-real", false, "Mock operations but initialize/pull from real kernel data at startup")
	disableEdit := flag.Bool("disable-edit", false, "Disable edit operations (Read-only mode)")
	allowEditSystemRoutes := flag.Bool("allow-edit-system-routes", false, "Allow editing and deleting system predefined static routes")
	prioritizeKernelRoutes := flag.Bool("prioritize-kernel-routes", false, "Prioritize kernel route information over database if duplicate")
	flag.Parse()

	log.Printf("Starting PiGate Backend Server (Go v1.26.4)...")
	log.Printf("Port: %d", *port)
	log.Printf("Database: %s", *dbPath)
	log.Printf("Mock OS Integration: %t", *mockOS)
	log.Printf("Mock From Real Data: %t", *mockFromReal)
	log.Printf("Disable Edit Mode: %t", *disableEdit)
	log.Printf("Allow Edit System Routes: %t", *allowEditSystemRoutes)
	log.Printf("Prioritize Kernel Routes: %t", *prioritizeKernelRoutes)

	// 2. Initialize in-memory logs circular buffer (Ring Buffer)
	ringBuffer := logs.NewRingBuffer(50)

	// Seed some initial firewall log mock entries for visual display on dashboard
	ringBuffer.Add(model.FirewallLog{ID: "log-init-1", Time: "14:31:02", Action: "DROP", Src: "185.220.101.4", Dest: "10.0.0.45", Port: "445", Proto: "TCP", Reason: "Blocked Port (SMB)"})
	ringBuffer.Add(model.FirewallLog{ID: "log-init-2", Time: "14:31:15", Action: "PASS", Src: "192.168.1.105", Dest: "8.8.8.8", Port: "53", Proto: "UDP", Reason: "DNS request"})

	// 3. Initialize SQLite DB & run migrations
	sqliteDB, err := db.InitDB(*dbPath)
	if err != nil {
		log.Fatalf("Fatal error initializing SQLite DB: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(*mockOS, *mockFromReal)
	repo.SetAllowEditSystemRoutes(*allowEditSystemRoutes)
	repo.SetPrioritizeKernelRoutes(*prioritizeKernelRoutes)

	// Perform initial synchronization of interfaces, routing table, and DNS if real mode or mock-from-real is enabled
	if !*mockOS || *mockFromReal {
		log.Printf("Initializing and syncing interfaces, routes, and DNS from OS kernel...")
		if err := repo.ClearInterfaces(); err != nil {
			log.Printf("Warning: Failed to clear old network interfaces: %v", err)
		}
		if err := repo.SyncInterfacesFromOS(); err != nil {
			log.Printf("Warning: Failed to sync network interfaces from OS: %v", err)
		}
		if err := repo.SyncRoutesFromOS(); err != nil {
			log.Printf("Warning: Failed to sync static routes from OS: %v", err)
		}
		if err := repo.SyncDNSFromOS(); err != nil {
			log.Printf("Warning: Failed to sync DNS settings from OS: %v", err)
		}
	}

	// 4. Instantiate Kernel managers (Force Mock layer for now)
	var fw kernel.FirewallManager
	var net kernel.NetworkManager
	var rt kernel.RoutingManager
	var dhcp kernel.DhcpManager

	if *mockOS || *mockFromReal {
		fw = kernel.NewMockFirewall()
		net = kernel.NewMockNetwork()
		rt = kernel.NewMockRouting()
		mDhcp := kernel.NewMockDhcp()
		mDhcp.MockFromReal = *mockFromReal
		dhcp = mDhcp
	} else {
		// Real kernel integrations via netlink — used on Raspberry Pi 5 production.
		// Requires: sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend
		fw = kernel.NewMockFirewall() // nftables real impl: TODO (google/nftables)
		net = kernel.NewRealNetwork()
		rt = kernel.NewRealRouting(*allowEditSystemRoutes)
		mDhcp := kernel.NewMockDhcp()
		mDhcp.MockFromReal = false
		dhcp = mDhcp
	}

	// 5. Instantiate Server & Router
	server := api.NewServer(repo, fw, net, rt, dhcp, ringBuffer, *disableEdit)
	handler := api.RegisterRoutes(server)

	// 6. Start HTTP API listener
	address := ":" + strconv.Itoa(*port)
	log.Printf("PiGate API Backend is listening at http://localhost%s", address)
	if err := http.ListenAndServe(address, handler); err != nil {
		log.Fatalf("Server listener failed: %v", err)
	}
}
