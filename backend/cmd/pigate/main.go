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
	flag.Parse()

	log.Printf("Starting PiGate Backend Server (Go v1.26.4)...")
	log.Printf("Port: %d", *port)
	log.Printf("Database: %s", *dbPath)
	log.Printf("Mock OS Integration: %t", *mockOS)

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

	// 4. Instantiate Kernel managers (Force Mock layer for now)
	var fw kernel.FirewallManager
	var net kernel.NetworkManager
	var rt kernel.RoutingManager
	var dhcp kernel.DhcpManager

	if *mockOS {
		fw = kernel.NewMockFirewall()
		net = kernel.NewMockNetwork()
		rt = kernel.NewMockRouting()
		dhcp = kernel.NewMockDhcp()
	} else {
		// Real integrations will be swapped here for Raspberry Pi 5 production
		fw = kernel.NewMockFirewall()
		net = kernel.NewMockNetwork()
		rt = kernel.NewMockRouting()
		dhcp = kernel.NewMockDhcp()
	}

	// 5. Instantiate Server & Router
	server := api.NewServer(repo, fw, net, rt, dhcp, ringBuffer)
	handler := api.RegisterRoutes(server)

	// 6. Start HTTP API listener
	address := ":" + strconv.Itoa(*port)
	log.Printf("PiGate API Backend is listening at http://localhost%s", address)
	if err := http.ListenAndServe(address, handler); err != nil {
		log.Fatalf("Server listener failed: %v", err)
	}
}
