package service

import (
	"testing"

	"pigate/internal/db"
	"pigate/internal/model"
)

type trackingHostnameManager struct {
	hostname string
	setCalls []string
}

func (t *trackingHostnameManager) GetHostname() (string, error) {
	return t.hostname, nil
}

func (t *trackingHostnameManager) SetHostname(name string) error {
	t.hostname = name
	t.setCalls = append(t.setCalls, name)
	return nil
}

type trackingDhcpcdManager struct {
	shareCalls []bool
	restarted  []string
	startCalls []string
	stopCalls  []string
}

func (t *trackingDhcpcdManager) StartDhcpcd(ifaceName string) error {
	t.startCalls = append(t.startCalls, ifaceName)
	return nil
}

func (t *trackingDhcpcdManager) StopDhcpcd(ifaceName string) error {
	t.stopCalls = append(t.stopCalls, ifaceName)
	return nil
}

func (t *trackingDhcpcdManager) SetShareHostname(share bool) error {
	t.shareCalls = append(t.shareCalls, share)
	return nil
}

func (t *trackingDhcpcdManager) RestartDhcpcd(ifaceName string) error {
	t.restarted = append(t.restarted, ifaceName)
	return nil
}

func newTestHostnameService(t *testing.T) (*HostnameService, *trackingHostnameManager, *trackingDhcpcdManager) {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(true, false)

	mockNet := &trackingNetworkManager{}
	ifaceService := NewInterfaceService(repo, mockNet)

	hostnameMgr := &trackingHostnameManager{hostname: "pigate-test"}
	dhcpcdMgr := &trackingDhcpcdManager{}

	return NewHostnameService(repo, hostnameMgr, dhcpcdMgr, ifaceService), hostnameMgr, dhcpcdMgr
}

func TestHostnameService_ValidationRejectsBadNames(t *testing.T) {
	svc, _, _ := newTestHostnameService(t)

	cases := []string{
		"",
		"-badstart",
		"badend-",
		"bad_underscore",
		"has space",
		string(make([]byte, 64)), // too long (also fails char check, but length matters)
	}

	for _, name := range cases {
		err := svc.Update(model.SystemHostnameSettings{Hostname: name, ShareWithDhcp: false})
		if err == nil {
			t.Errorf("expected validation error for hostname %q, got nil", name)
		}
	}
}

func TestHostnameService_UpdateAppliesHostnameViaKernel(t *testing.T) {
	svc, hostnameMgr, dhcpcdMgr := newTestHostnameService(t)

	if err := svc.Update(model.SystemHostnameSettings{Hostname: "my-gateway", ShareWithDhcp: false}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if hostnameMgr.hostname != "my-gateway" {
		t.Errorf("expected kernel hostname to be set to my-gateway, got %s", hostnameMgr.hostname)
	}

	got, err := svc.Get()
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Hostname != "my-gateway" {
		t.Errorf("expected persisted hostname my-gateway, got %s", got.Hostname)
	}

	// share unchanged (false -> false), so dhcpcd config should not be rewritten
	if len(dhcpcdMgr.shareCalls) != 0 {
		t.Errorf("expected no SetShareHostname calls when share unchanged, got %v", dhcpcdMgr.shareCalls)
	}
}

func TestHostnameService_ShareToggleRewritesDhcpcdConfigAndRestarts(t *testing.T) {
	svc, _, dhcpcdMgr := newTestHostnameService(t)

	// The mock kernel interface list (GetKernelInterfaces, mock mode) always reports
	// eth0 as static/up, and wlan0 + eth1 as dhcp/up. Restart should only happen on
	// the dhcp-mode interfaces.
	if err := svc.Update(model.SystemHostnameSettings{Hostname: "pigate-test", ShareWithDhcp: true}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if len(dhcpcdMgr.shareCalls) != 1 || dhcpcdMgr.shareCalls[0] != true {
		t.Errorf("expected exactly one SetShareHostname(true) call, got %v", dhcpcdMgr.shareCalls)
	}

	restarted := map[string]bool{}
	for _, name := range dhcpcdMgr.restarted {
		restarted[name] = true
	}
	if !restarted["wlan0"] || !restarted["eth1"] {
		t.Errorf("expected dhcpcd restart on dhcp-mode interfaces wlan0 and eth1, got %v", dhcpcdMgr.restarted)
	}
	if restarted["eth0"] {
		t.Errorf("did not expect dhcpcd restart on static-mode interface eth0, got %v", dhcpcdMgr.restarted)
	}
}

func TestHostnameService_InitApplyConfig(t *testing.T) {
	svc, hostnameMgr, dhcpcdMgr := newTestHostnameService(t)

	if err := svc.InitApplyConfig(); err != nil {
		t.Fatalf("InitApplyConfig failed: %v", err)
	}

	if hostnameMgr.hostname == "" {
		t.Errorf("expected hostname to be applied from DB seed value")
	}
	if len(dhcpcdMgr.shareCalls) != 1 {
		t.Errorf("expected SetShareHostname to be called once at startup, got %v", dhcpcdMgr.shareCalls)
	}
	if len(dhcpcdMgr.restarted) != 0 {
		t.Errorf("expected no dhcpcd restarts at startup, got %v", dhcpcdMgr.restarted)
	}
}
