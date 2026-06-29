package service

import (
	"net"
	"testing"

	"pigate/internal/db"
	"pigate/internal/model"

	"github.com/vishvananda/netlink"
)

func TestDhcpcdService_HandleLinkUpdate(t *testing.T) {
	// Initialize a memory database
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	// Enable mock mode so we don't call real kernel/exec commands
	repo.SetMockMode(true, false)

	// Create InterfaceService with mock network manager
	mockNet := &trackingNetworkManager{}
	ifaceService := NewInterfaceService(repo, mockNet)

	// Clear default seeded interfaces to start fresh
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear DB interfaces: %v", err)
	}

	// Seed eth0 (Ethernet) in DB as DHCP
	err = repo.CreateInterfaceForTest(model.NetworkInterface{
		ID:             "iface-eth0",
		Name:           "eth0",
		Alias:          "eth0",
		Role:           "LAN",
		Type:           "ethernet",
		AddressingMode: "dhcp",
		Status:         "up",
	})
	if err != nil {
		t.Fatalf("Failed to seed eth0: %v", err)
	}

	// Seed wlan0 (Wi-Fi) in DB as DHCP
	err = repo.CreateInterfaceForTest(model.NetworkInterface{
		ID:             "iface-wlan0",
		Name:           "wlan0",
		Alias:          "wlan0",
		Role:           "WAN",
		Type:           "wireless",
		AddressingMode: "dhcp",
		Status:         "up",
	})
	if err != nil {
		t.Fatalf("Failed to seed wlan0: %v", err)
	}

	dhcpcdService := NewDhcpcdService(repo, ifaceService)

	// Test 1: Ethernet interface up
	updateEth0Up := netlink.LinkUpdate{
		Link: &netlink.Device{
			LinkAttrs: netlink.LinkAttrs{
				Name:  "eth0",
				Flags: net.FlagUp,
			},
		},
	}
	dhcpcdService.HandleLinkUpdate(updateEth0Up)

	// Test 2: Ethernet interface down
	updateEth0Down := netlink.LinkUpdate{
		Link: &netlink.Device{
			LinkAttrs: netlink.LinkAttrs{
				Name:  "eth0",
				Flags: 0, // not up
			},
		},
	}
	dhcpcdService.HandleLinkUpdate(updateEth0Down)

	// Test 3: Wi-Fi interface up but not running
	updateWlan0UpNotRunning := netlink.LinkUpdate{
		Link: &netlink.Device{
			LinkAttrs: netlink.LinkAttrs{
				Name:  "wlan0",
				Flags: net.FlagUp,
			},
		},
	}
	dhcpcdService.HandleLinkUpdate(updateWlan0UpNotRunning)

	// Test 4: Wi-Fi interface up and running
	updateWlan0UpRunning := netlink.LinkUpdate{
		Link: &netlink.Device{
			LinkAttrs: netlink.LinkAttrs{
				Name:  "wlan0",
				Flags: net.FlagUp | net.FlagRunning,
			},
		},
	}
	dhcpcdService.HandleLinkUpdate(updateWlan0UpRunning)

	// Test 5: Wi-Fi interface down
	updateWlan0Down := netlink.LinkUpdate{
		Link: &netlink.Device{
			LinkAttrs: netlink.LinkAttrs{
				Name:  "wlan0",
				Flags: 0,
			},
		},
	}
	dhcpcdService.HandleLinkUpdate(updateWlan0Down)

	// Test 6: SyncActiveInterfaces in mock mode
	dhcpcdService.SyncActiveInterfaces()
}
