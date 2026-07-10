package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"pigate/internal/db"
	"pigate/internal/model"
)

// newBackupTestEnv spins up a temp-file, mock-seeded DB and a BackupService with
// no downstream services (re-apply becomes a no-op, monitor is nil), which is all
// the export/import DB logic needs. A real file (not ":memory:") is used so
// pooled connections share one database and the pre-import snapshot path is
// exercised for real.
func newBackupTestEnv(t *testing.T) (*BackupService, *db.Repository) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "pigate-test.db")
	sqlDB, err := db.InitDB(dbPath, true)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	repo := db.NewRepository(sqlDB)
	repo.SetMockMode(true, false)
	bs := NewBackupService(repo, dbPath, "test",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	return bs, repo
}

// seedCustomConfig adds one custom object of each restorable kind on top of the
// mock seed so a backup exercises every section.
func seedCustomConfig(t *testing.T, repo *db.Repository) {
	t.Helper()
	if err := repo.CreateAddress(model.AddressObject{ID: "addr-c1", Name: "LabNet", Type: "subnet", Value: "10.10.0.0/24"}); err != nil {
		t.Fatalf("create address: %v", err)
	}
	if err := repo.CreateService(model.ServiceObject{ID: "svc-c1", Name: "Custom8080", Protocol: "TCP", Port: "8080", Type: "custom"}); err != nil {
		t.Fatalf("create service: %v", err)
	}
	if err := repo.CreatePolicy(model.PolicyRule{
		ID: "pol-1", Name: "AllowLab", InInterface: "eth0", OutInterface: "any",
		Source: []string{"LabNet"}, Destination: []string{"ALL"}, Service: []string{"Custom8080"},
		Action: "ACCEPT", Log: true, Status: true,
	}); err != nil {
		t.Fatalf("create policy: %v", err)
	}
	// Gateway "default" must survive a round-trip verbatim (not resolved to an IP).
	if err := repo.CreateRoute(model.StaticRoute{
		ID: "rt-1", Destination: "10.20.0.0/24", Gateway: "default", Interface: "eth0",
		Metric: 100, Type: "customgateway", Status: true, Scope: "global", Proto: "static",
	}); err != nil {
		t.Fatalf("create route: %v", err)
	}
	if err := repo.CreateDHCPConfig(model.DhcpConfig{
		Interface: "wlan0", Enabled: true, StartIP: "192.168.5.100", EndIP: "192.168.5.200",
		Gateway: "192.168.5.1", Netmask: "255.255.255.0", DNS1: "8.8.8.8", DNS2: "1.1.1.1", LeaseTime: 3600,
	}); err != nil {
		t.Fatalf("create dhcp config: %v", err)
	}
	if err := repo.CreateDNSZone(model.DNSZone{ID: "zone-1", ZoneName: "lab.local", IsAuthoritative: true, Enabled: true}); err != nil {
		t.Fatalf("create dns zone: %v", err)
	}
	if err := repo.CreateDNSRecord(model.DNSRecord{ID: "rec-1", ZoneID: "zone-1", Name: "server", Type: "A", Value: "10.10.0.5", TTL: 300}); err != nil {
		t.Fatalf("create dns record: %v", err)
	}
	if _, err := repo.CreateQosRule(model.QosRuleInput{Name: "CapLab", Interface: "eth0", EgressRateMbps: 50, EgressCeilMbps: 100, Priority: 10, Status: true}); err != nil {
		t.Fatalf("create qos rule: %v", err)
	}
}

func TestExportIncludesAllSections(t *testing.T) {
	bs, repo := newBackupTestEnv(t)
	seedCustomConfig(t, repo)

	file, err := bs.Export(false, "")
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if !strings.HasPrefix(file.Meta.Checksum, "sha256:") {
		t.Errorf("checksum missing/malformed: %q", file.Meta.Checksum)
	}
	if file.Meta.SchemaVersion != model.CurrentBackupSchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", file.Meta.SchemaVersion, model.CurrentBackupSchemaVersion)
	}

	c := file.Config
	if len(c.Policies) != 1 {
		t.Errorf("policies = %d, want 1", len(c.Policies))
	}
	if len(c.QosRules) != 1 {
		t.Errorf("qosRules = %d, want 1", len(c.QosRules))
	}
	if len(c.DnsZones) != 1 || len(c.DnsZones[0].Records) != 1 {
		t.Errorf("dns zones/records not exported correctly: %+v", c.DnsZones)
	}
	// wlan0 custom config + seeded eth0 default => at least 2 DHCP configs.
	if len(c.DhcpConfigs) < 2 {
		t.Errorf("dhcpConfigs = %d, want >=2 (multi-config)", len(c.DhcpConfigs))
	}
	// Raw route must keep the "default" gateway sentinel.
	var found bool
	for _, r := range c.StaticRoutes {
		if r.ID == "rt-1" {
			found = true
			if r.Gateway != "default" {
				t.Errorf("route gateway = %q, want raw \"default\"", r.Gateway)
			}
		}
	}
	if !found {
		t.Errorf("custom route rt-1 not exported")
	}
	if len(c.Users) != 0 {
		t.Errorf("users must be excluded when includeUsers=false, got %d", len(c.Users))
	}

	withUsers, err := bs.Export(true, "")
	if err != nil {
		t.Fatalf("export with users: %v", err)
	}
	if len(withUsers.Config.Users) == 0 {
		t.Errorf("expected users when includeUsers=true")
	}
	if withUsers.Config.Users[0].PasswordHash == "" {
		t.Errorf("exported user must carry password hash for restore")
	}
}

func TestImportRoundTrip(t *testing.T) {
	bs, repo := newBackupTestEnv(t)
	seedCustomConfig(t, repo)

	file, err := bs.Export(false, "")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, _ := json.Marshal(file)

	// Mutate DB to prove import replaces state, not merges: drop the custom
	// policy and address ref, add a stray address that should be gone after.
	_ = repo.DeletePolicy("pol-1")
	_ = repo.CreateAddress(model.AddressObject{ID: "addr-stray", Name: "StrayNet", Type: "subnet", Value: "172.31.0.0/24"})

	res, err := bs.Import(raw, model.ImportOptions{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Counts["policies"] != 1 {
		t.Errorf("imported policies count = %d, want 1", res.Counts["policies"])
	}

	// Round-trip fidelity: re-export and compare canonical checksums.
	file2, err := bs.Export(false, "")
	if err != nil {
		t.Fatalf("re-export: %v", err)
	}
	sum1, _ := configChecksum(*file.Config)
	sum2, _ := configChecksum(*file2.Config)
	if sum1 != sum2 {
		t.Errorf("round-trip changed config:\n before=%s\n after =%s", sum1, sum2)
	}

	// Replace semantics: the stray address must be gone.
	addrs, _ := repo.GetAddresses()
	for _, a := range addrs {
		if a.Name == "StrayNet" {
			t.Errorf("StrayNet survived import — replace semantics violated")
		}
	}
}

func TestImportChecksumMismatchRejected(t *testing.T) {
	bs, repo := newBackupTestEnv(t)
	seedCustomConfig(t, repo)

	file, err := bs.Export(false, "")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	file.Meta.Checksum = "sha256:deadbeef"
	raw, _ := json.Marshal(file)

	before, _ := repo.GetAddresses()

	if _, err := bs.Import(raw, model.ImportOptions{}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum error, got %v", err)
	}

	after, _ := repo.GetAddresses()
	if len(after) != len(before) {
		t.Errorf("DB changed on a rejected import: before=%d after=%d", len(before), len(after))
	}
}

func TestImportConstraintViolationRollsBack(t *testing.T) {
	bs, repo := newBackupTestEnv(t)
	seedCustomConfig(t, repo)

	file, err := bs.Export(false, "")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// Inject a value that passes structural validation but violates the SQLite
	// CHECK on address type, so the failure happens mid-transaction.
	file.Config.Addresses = append(file.Config.Addresses, model.AddressObject{
		ID: "addr-bad", Name: "BadType", Type: "bogus", Value: "x",
	})
	sum, _ := configChecksum(*file.Config)
	file.Meta.Checksum = sum
	raw, _ := json.Marshal(file)

	beforePolicies, _ := repo.GetPolicies()

	if _, err := bs.Import(raw, model.ImportOptions{}); err == nil {
		t.Fatalf("expected restore to fail on bad address type")
	}

	// Transaction must have rolled back: original policy still intact, bad
	// address absent.
	afterPolicies, _ := repo.GetPolicies()
	if len(afterPolicies) != len(beforePolicies) {
		t.Errorf("rollback failed: policies before=%d after=%d", len(beforePolicies), len(afterPolicies))
	}
	addrs, _ := repo.GetAddresses()
	for _, a := range addrs {
		if a.Name == "BadType" {
			t.Errorf("bad address leaked despite rollback")
		}
	}
}

func TestImportLegacyV1(t *testing.T) {
	bs, repo := newBackupTestEnv(t)

	v1 := `{
		"device": "PiGate Firewall Gateway",
		"version": "v1.0.0-Release",
		"exportedAt": "2026-01-01T00:00:00Z",
		"systemSettings": {"timezone": "Asia/Bangkok (GMT+7:00)", "ntpSync": true, "ntpServer": "pool.ntp.org"},
		"hostnameSettings": {"hostname": "old-box", "shareWithDhcp": false},
		"config": {
			"addresses": [{"id":"addr-v1","name":"V1Net","type":"subnet","value":"10.99.0.0/24","system":false}],
			"serviceObjects": [],
			"policies": [],
			"routes": [
				{"id":"route-sys-ghost","destination":"0.0.0.0/0","gateway":"192.168.1.1","interface":"eth0","type":"system","status":true},
				{"id":"rt-v1","destination":"10.50.0.0/24","gateway":"default","interface":"eth0","type":"customgateway","status":true}
			],
			"interfaces": [],
			"dhcp": {"config": {"interface":"eth0","enabled":true,"startIp":"192.168.9.10","endIp":"192.168.9.99","gateway":"192.168.9.1","netmask":"255.255.255.0","dns1":"8.8.8.8","dns2":"1.1.1.1","leaseTime":3600}, "reservations": []}
		}
	}`

	res, err := bs.Import([]byte(v1), model.ImportOptions{})
	if err != nil {
		t.Fatalf("import v1: %v", err)
	}
	if res.SchemaVersion != 1 {
		t.Errorf("schemaVersion = %d, want 1", res.SchemaVersion)
	}

	addrs, _ := repo.GetAddresses()
	var haveV1 bool
	for _, a := range addrs {
		if a.Name == "V1Net" {
			haveV1 = true
		}
	}
	if !haveV1 {
		t.Errorf("v1 custom address not restored")
	}

	// Ghost system route must have been dropped by the mapper.
	routes, _ := repo.GetRawStaticRoutes()
	for _, r := range routes {
		if r.ID == "route-sys-ghost" {
			t.Errorf("v1 ghost/system route should not be restored")
		}
	}

	// Legacy display timezone must be normalized to a bare IANA name.
	tz, _ := repo.GetSystemTimeSettings()
	if tz.Timezone != "Asia/Bangkok" {
		t.Errorf("timezone = %q, want normalized Asia/Bangkok", tz.Timezone)
	}
}

func TestExportImportEncryptedRoundTrip(t *testing.T) {
	bs, repo := newBackupTestEnv(t)
	seedCustomConfig(t, repo)

	const pass = "correct horse battery staple"
	file, err := bs.Export(false, pass)
	if err != nil {
		t.Fatalf("encrypted export: %v", err)
	}
	if !file.Meta.Encrypted || file.EncryptedConfig == "" || file.Config != nil {
		t.Fatalf("expected encrypted file with no plaintext config: encrypted=%v cfgNil=%v", file.Meta.Encrypted, file.Config == nil)
	}
	if file.Meta.Encryption == nil || file.Meta.Encryption.Algorithm != "AES-256-GCM" {
		t.Fatalf("missing/incorrect encryption params: %+v", file.Meta.Encryption)
	}
	raw, _ := json.Marshal(file)

	// The ciphertext must not leak a known plaintext token.
	if strings.Contains(string(raw), "LabNet") {
		t.Errorf("plaintext object name leaked into encrypted file")
	}

	// No passphrase → specific ErrPassphraseRequired.
	if _, err := bs.Import(raw, model.ImportOptions{}); err == nil {
		t.Errorf("expected error importing encrypted file without passphrase")
	}

	// Wrong passphrase → generic failure, DB untouched.
	before, _ := repo.GetAddresses()
	if _, err := bs.Import(raw, model.ImportOptions{Passphrase: "wrong"}); err == nil {
		t.Errorf("expected error with wrong passphrase")
	}
	after, _ := repo.GetAddresses()
	if len(after) != len(before) {
		t.Errorf("DB changed on failed decrypt")
	}

	// Correct passphrase → restores successfully.
	res, err := bs.Import(raw, model.ImportOptions{Passphrase: pass})
	if err != nil {
		t.Fatalf("import with correct passphrase: %v", err)
	}
	if res.Counts["policies"] != 1 {
		t.Errorf("policies restored = %d, want 1", res.Counts["policies"])
	}
}

func TestImportUsersActorGuardAndSessionPurge(t *testing.T) {
	bs, repo := newBackupTestEnv(t)

	// A user that exists now but won't be in the backup — must be reported for
	// session purge after the wipe+restore.
	if err := repo.CreateUser(model.User{ID: "u-ghost", Username: "ghost", PasswordHash: "x", Role: "admin_readonly", Status: "active"}); err != nil {
		t.Fatalf("create ghost: %v", err)
	}

	file, err := bs.Export(true, "")
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Simulate a hostile/careless backup: drop ghost and disable the actor.
	kept := file.Config.Users[:0]
	for _, u := range file.Config.Users {
		if u.Username == "ghost" {
			continue
		}
		if u.Username == "pigate" {
			u.Status = "disabled" // would lock the operator out
		}
		kept = append(kept, u)
	}
	file.Config.Users = kept
	sum, _ := configChecksum(*file.Config)
	file.Meta.Checksum = sum
	raw, _ := json.Marshal(file)

	res, err := bs.Import(raw, model.ImportOptions{IncludeUsers: true, ActorUsername: "pigate", ActorUserID: "user-pigate"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !res.UsersImported {
		t.Errorf("UsersImported should be true")
	}

	// Actor must remain an active super_admin despite the backup disabling them.
	actor, _ := repo.GetUserByUsername("pigate")
	if actor == nil || actor.Status != "active" || actor.Role != "super_admin" {
		t.Errorf("actor not preserved as active super_admin: %+v", actor)
	}

	// ghost was removed by the import → flagged for session purge.
	var ghostPurged bool
	for _, u := range res.RemovedUsernames {
		if u == "ghost" {
			ghostPurged = true
		}
	}
	if !ghostPurged {
		t.Errorf("removed user 'ghost' not reported for session purge: %v", res.RemovedUsernames)
	}
}

func TestImportSkipsUnknownInterface(t *testing.T) {
	bs, repo := newBackupTestEnv(t)
	seedCustomConfig(t, repo)

	file, err := bs.Export(false, "")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// Rename one interface to a name that doesn't exist on this device.
	for i := range file.Config.Interfaces {
		if file.Config.Interfaces[i].Name == "eth0" {
			file.Config.Interfaces[i].Name = "eth99"
		}
	}
	sum, _ := configChecksum(*file.Config)
	file.Meta.Checksum = sum
	raw, _ := json.Marshal(file)

	res, err := bs.Import(raw, model.ImportOptions{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	var warned bool
	for _, w := range res.Warnings {
		if strings.Contains(w, "eth99") && strings.Contains(w, "skipped") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected skip warning for eth99, got warnings: %v", res.Warnings)
	}
	// eth99 must not have been created.
	ifaces, _ := repo.GetInterfaces()
	for _, i := range ifaces {
		if i.Name == "eth99" {
			t.Errorf("phantom interface eth99 was created")
		}
	}
}

// A VLAN row in a backup must survive import even though the VLAN link is not
// present on the device (it only comes back when re-created at reapply). Its parent
// must exist; an orphan VLAN (missing parent) is skipped with a warning. (issue #20)
func TestImportKeepsVlanRow(t *testing.T) {
	bs, repo := newBackupTestEnv(t)
	seedCustomConfig(t, repo)

	file, err := bs.Export(false, "")
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	parent := "eth0" // present in the mock-seeded DB
	vid := 100
	orphanParent := "ethX" // not present
	ovid := 200
	file.Config.Interfaces = append(file.Config.Interfaces,
		model.NetworkInterface{
			ID: "iface-eth0.100", Name: "eth0.100", Alias: "vlan100", Role: "LAN",
			Type: "ethernet", Subtype: "vlan", AddressingMode: "dhcp", IP: "0.0.0.0",
			Netmask: "24", Gateway: "", MacAddress: "aa:bb:cc:dd:ee:ff", Status: "up",
			Speed: "1000 Mbps", AdminAccess: []string{"PING"}, VlanParent: &parent, VlanID: &vid,
		},
		model.NetworkInterface{
			ID: "iface-ethX.200", Name: "ethX.200", Alias: "vlanOrphan", Role: "LAN",
			Type: "ethernet", Subtype: "vlan", AddressingMode: "dhcp", IP: "0.0.0.0",
			Netmask: "24", Gateway: "", MacAddress: "aa:bb:cc:dd:ee:00", Status: "up",
			Speed: "1000 Mbps", AdminAccess: []string{"PING"}, VlanParent: &orphanParent, VlanID: &ovid,
		},
	)
	sum, _ := configChecksum(*file.Config)
	file.Meta.Checksum = sum
	raw, _ := json.Marshal(file)

	res, err := bs.Import(raw, model.ImportOptions{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	ifaces, _ := repo.GetInterfaces()
	byName := map[string]model.NetworkInterface{}
	for _, i := range ifaces {
		byName[i.Name] = i
	}
	kept, ok := byName["eth0.100"]
	if !ok {
		t.Fatalf("VLAN eth0.100 was dropped during import")
	}
	if kept.VlanParent == nil || *kept.VlanParent != "eth0" || kept.VlanID == nil || *kept.VlanID != 100 {
		t.Errorf("VLAN metadata not preserved: %+v", kept)
	}
	if _, present := byName["ethX.200"]; present {
		t.Errorf("orphan VLAN ethX.200 (missing parent) should not have been restored")
	}
	var warned bool
	for _, w := range res.Warnings {
		if strings.Contains(w, "ethX.200") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected a skip warning for orphan VLAN ethX.200, got: %v", res.Warnings)
	}
}
