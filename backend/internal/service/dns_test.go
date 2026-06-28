package service

import (
	"pigate/internal/db"
	"pigate/internal/model"
	"testing"
)

type trackingDNSManager struct {
	linkDNS     map[string][]string
	globalDNS   []string
	globalDom   string
	revertedLinks []string
	setGlobalCalls int
}

func (t *trackingDNSManager) GetLinkDNS(ifaceName string) ([]string, error) {
	if ifaceName == "eth0" {
		return []string{"1.1.1.1"}, nil
	}
	return t.linkDNS[ifaceName], nil
}

func (t *trackingDNSManager) SetLinkDNS(ifaceName string, servers []string) error {
	if t.linkDNS == nil {
		t.linkDNS = make(map[string][]string)
	}
	t.linkDNS[ifaceName] = servers
	return nil
}

func (t *trackingDNSManager) RevertLinkDNS(ifaceName string) error {
	t.revertedLinks = append(t.revertedLinks, ifaceName)
	if t.linkDNS != nil {
		delete(t.linkDNS, ifaceName)
	}
	return nil
}

func (t *trackingDNSManager) SetGlobalDNS(servers []string, searchDomain string) error {
	t.globalDNS = servers
	t.globalDom = searchDomain
	t.setGlobalCalls++
	return nil
}

func TestDNSGetAndApplyConfig(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	dnsMgr := &trackingDNSManager{
		linkDNS: make(map[string][]string),
	}
	dnsSvc := NewDNSService(repo, dnsMgr)

	// Seed DNS configs (update pre-seeded row)
	_, err = sqliteDB.Exec(`UPDATE system_dns_settings SET mode = 'static', primary_dns = '8.8.8.8', secondary_dns = '8.8.4.4', local_domain = 'pigate.test' WHERE id = 1`)
	if err != nil {
		t.Fatalf("Failed to seed system_dns_settings: %v", err)
	}

	// Update pre-seeded eth0 interface to be WAN DHCP
	_, err = sqliteDB.Exec(`UPDATE network_interfaces SET role = 'WAN', addressing_mode = 'dhcp', status = 'up' WHERE name = 'eth0'`)
	if err != nil {
		t.Fatalf("Failed to seed network_interfaces: %v", err)
	}

	// 1. Get DNS configuration
	cfg, err := dnsSvc.GetDNSConfig()
	if err != nil {
		t.Fatalf("GetDNSConfig failed: %v", err)
	}

	if cfg.Mode != "static" || cfg.PrimaryDNS != "8.8.8.8" || cfg.SecondaryDNS != "8.8.4.4" || cfg.LocalDomain != "pigate.test" {
		t.Errorf("Unexpected DNSConfig values: %+v", cfg)
	}

	// Check dynamic DNS from active WAN interface
	if len(cfg.DynamicDNS) != 1 || cfg.DynamicDNS[0].InterfaceName != "eth0" || cfg.DynamicDNS[0].DNSServers[0] != "1.1.1.1" {
		t.Errorf("Unexpected DynamicDNS populated: %+v", cfg.DynamicDNS)
	}

	// 2. Apply static configurations
	err = dnsSvc.ApplyDNSConfig()
	if err != nil {
		t.Fatalf("ApplyDNSConfig failed: %v", err)
	}

	if len(dnsMgr.globalDNS) != 2 || dnsMgr.globalDNS[0] != "8.8.8.8" || dnsMgr.globalDNS[1] != "8.8.4.4" {
		t.Errorf("ApplyDNSConfig failed to configure global static DNS: %v", dnsMgr.globalDNS)
	}
	if dnsMgr.globalDom != "pigate.test" {
		t.Errorf("ApplyDNSConfig failed to configure global domain: %s", dnsMgr.globalDom)
	}

	if len(dnsMgr.linkDNS["eth0"]) != 2 || dnsMgr.linkDNS["eth0"][0] != "8.8.8.8" {
		t.Errorf("ApplyDNSConfig failed to configure interface static DNS: %v", dnsMgr.linkDNS["eth0"])
	}

	// 3. Update to WAN mode
	input := model.DNSConfigInput{
		Mode:         "wan",
		PrimaryDNS:   "",
		SecondaryDNS: "",
		LocalDomain:  "pigate.dhcp",
	}

	err = dnsSvc.UpdateDNSConfig(input)
	if err != nil {
		t.Fatalf("UpdateDNSConfig failed: %v", err)
	}

	cfg, err = dnsSvc.GetDNSConfig()
	if err != nil {
		t.Fatalf("GetDNSConfig failed after update: %v", err)
	}

	if cfg.Mode != "wan" || cfg.LocalDomain != "pigate.dhcp" {
		t.Errorf("Unexpected updated DNSConfig values: %+v", cfg)
	}

	// In WAN mode, SetGlobalDNS should be called with empty values, and RevertLinkDNS should be triggered for WAN link
	if len(dnsMgr.globalDNS) != 0 {
		t.Errorf("Revert global DNS failed, globalDNS not empty: %v", dnsMgr.globalDNS)
	}
	foundEth0 := false
	foundWlan0 := false
	for _, l := range dnsMgr.revertedLinks {
		if l == "eth0" {
			foundEth0 = true
		}
		if l == "wlan0" {
			foundWlan0 = true
		}
	}
	if !foundEth0 || !foundWlan0 {
		t.Errorf("Expected eth0 and wlan0 link DNS to be reverted, got: %v", dnsMgr.revertedLinks)
	}
}
