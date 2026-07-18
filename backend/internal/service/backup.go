package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"pigate/internal/db"
	"pigate/internal/model"
)

// BackupService owns configuration export/import. Export only needs the
// repository; import additionally drives every subsystem service so restored DB
// state is re-applied to the kernel in the same order as startup.
type BackupService struct {
	repo       *db.Repository
	dbPath     string
	appVersion string

	interfaceService  *InterfaceService
	routingService    *RoutingService
	firewallService   *FirewallService
	dnsService        *DNSService
	dnsServerService  *DNSServerService
	qosService        *QosService
	dhcpServerService *DhcpServerService
	dhcpcdService     *DhcpcdService
	hostnameService   *HostnameService
	timeService       *TimeService
	monitor           *NetlinkMonitor
}

func NewBackupService(
	repo *db.Repository,
	dbPath, appVersion string,
	interfaceService *InterfaceService,
	routingService *RoutingService,
	firewallService *FirewallService,
	dnsService *DNSService,
	dnsServerService *DNSServerService,
	qosService *QosService,
	dhcpServerService *DhcpServerService,
	dhcpcdService *DhcpcdService,
	hostnameService *HostnameService,
	timeService *TimeService,
	monitor *NetlinkMonitor,
) *BackupService {
	return &BackupService{
		repo:              repo,
		dbPath:            dbPath,
		appVersion:        appVersion,
		interfaceService:  interfaceService,
		routingService:    routingService,
		firewallService:   firewallService,
		dnsService:        dnsService,
		dnsServerService:  dnsServerService,
		qosService:        qosService,
		dhcpServerService: dhcpServerService,
		dhcpcdService:     dhcpcdService,
		hostnameService:   hostnameService,
		timeService:       timeService,
		monitor:           monitor,
	}
}

// =============================================================================
// EXPORT
// =============================================================================

// Export reads every configuration table into a typed BackupFile. Errors from
// any table abort the export (a silent partial backup is worse than none). When
// includeUsers is true the users table — including bcrypt hashes — is included.
// When passphrase is non-empty the config section is AES-256-GCM encrypted with
// an Argon2id-derived key; the returned file then carries EncryptedConfig
// instead of Config.
func (s *BackupService) Export(includeUsers bool, passphrase string) (*model.BackupFile, error) {
	cfg := model.BackupConfig{}
	var err error

	if cfg.Interfaces, err = s.repo.GetInterfaces(); err != nil {
		return nil, fmt.Errorf("read interfaces: %w", err)
	}
	// Raw static routes only — never the kernel-merged or gateway-resolved view.
	if cfg.StaticRoutes, err = s.repo.GetRawStaticRoutes(); err != nil {
		return nil, fmt.Errorf("read static routes: %w", err)
	}
	if cfg.Addresses, err = s.repo.GetAddresses(); err != nil {
		return nil, fmt.Errorf("read address objects: %w", err)
	}
	if cfg.ServiceObjects, err = s.repo.GetServices(); err != nil {
		return nil, fmt.Errorf("read service objects: %w", err)
	}
	if cfg.Policies, err = s.repo.GetPolicies(); err != nil {
		return nil, fmt.Errorf("read policies: %w", err)
	}
	if cfg.PortForwards, err = s.repo.GetPortForwards(); err != nil {
		return nil, fmt.Errorf("read port forwards: %w", err)
	}
	if cfg.DhcpConfigs, err = s.repo.GetDHCPConfigs(); err != nil {
		return nil, fmt.Errorf("read dhcp configs: %w", err)
	}
	if cfg.DhcpReservations, err = s.repo.GetDHCPReservations(); err != nil {
		return nil, fmt.Errorf("read dhcp reservations: %w", err)
	}
	if cfg.DnsZones, err = s.repo.GetDNSZones(); err != nil {
		return nil, fmt.Errorf("read dns zones: %w", err)
	}
	dnsIfaces, err := s.repo.GetDNSServerInterfaces()
	if err != nil {
		return nil, fmt.Errorf("read dns server settings: %w", err)
	}
	cfg.DnsServerSettings = model.DNSServerSettings{Interfaces: dnsIfaces}

	dnsCfg, err := s.repo.GetDNSConfig()
	if err != nil {
		return nil, fmt.Errorf("read system dns: %w", err)
	}
	cfg.SystemDns = model.DNSConfigInput{
		Mode:         dnsCfg.Mode,
		PrimaryDNS:   dnsCfg.PrimaryDNS,
		SecondaryDNS: dnsCfg.SecondaryDNS,
		LocalDomain:  dnsCfg.LocalDomain,
	}

	if cfg.QosRules, err = s.repo.GetQosRules(); err != nil {
		return nil, fmt.Errorf("read qos rules: %w", err)
	}

	sysTime, err := s.repo.GetSystemTimeSettings()
	if err != nil {
		return nil, fmt.Errorf("read system time: %w", err)
	}
	sysTime.Status = nil // live-only, never persisted in a backup
	cfg.SystemTime = *sysTime

	sysHostname, err := s.repo.GetHostnameSettings()
	if err != nil {
		return nil, fmt.Errorf("read hostname settings: %w", err)
	}
	cfg.SystemHostname = *sysHostname

	if includeUsers {
		if cfg.Users, err = s.repo.GetBackupUsers(); err != nil {
			return nil, fmt.Errorf("read users: %w", err)
		}
	}

	checksum, err := configChecksum(cfg)
	if err != nil {
		return nil, fmt.Errorf("compute checksum: %w", err)
	}

	file := &model.BackupFile{
		Meta: model.BackupMeta{
			Device:        "PiGate Firewall Gateway",
			Hostname:      sysHostname.Hostname,
			AppVersion:    s.appVersion,
			SchemaVersion: model.CurrentBackupSchemaVersion,
			ExportedAt:    time.Now().Format(time.RFC3339),
			Checksum:      checksum,
			IncludeUsers:  includeUsers,
		},
	}

	if passphrase == "" {
		file.Config = &cfg
		return file, nil
	}

	enc, encParams, err := encryptConfig(cfg, passphrase)
	if err != nil {
		return nil, fmt.Errorf("encrypt config: %w", err)
	}
	file.Meta.Encrypted = true
	file.Meta.Encryption = encParams
	file.EncryptedConfig = enc
	return file, nil
}

// =============================================================================
// IMPORT
// =============================================================================

// Import validates, snapshots, restores, and re-applies a backup file. It never
// leaves the DB partially written: validation and checksum run before any DB
// mutation, the restore itself is a single transaction, and a file-copy snapshot
// is taken beforehand so a catastrophic failure is recoverable. Kernel re-apply
// failures are non-fatal (collected as warnings) because the DB is the source of
// truth and a reboot re-applies it anyway.
func (s *BackupService) Import(raw []byte, opts model.ImportOptions) (*model.ImportResult, error) {
	cfg, schemaVersion, err := decodeBackup(raw, opts.Passphrase)
	if err != nil {
		return nil, err
	}

	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	result := &model.ImportResult{
		SchemaVersion: schemaVersion,
		Counts:        map[string]int{},
		Warnings:      []string{},
	}

	// Normalise legacy timezone display strings so both old and new backups
	// produce a bare IANA name that systemd-timedated accepts.
	cfg.SystemTime.Timezone = db.NormalizeTimezone(cfg.SystemTime.Timezone)
	cfg.SystemTime.Status = nil

	// Resolve backup interfaces against the device: match by name, merge config
	// fields onto the live row, skip (with a warning) any interface absent here.
	mergedIfaces, ifaceWarnings, ifacesChanged, err := s.resolveInterfaces(cfg.Interfaces)
	if err != nil {
		return nil, fmt.Errorf("resolve interfaces: %w", err)
	}
	cfg.Interfaces = mergedIfaces
	result.Warnings = append(result.Warnings, ifaceWarnings...)
	result.InterfacesChanged = ifacesChanged

	// Snapshot the pre-import user set so we can (a) purge sessions of users that
	// disappear and (b) reinstate the actor if the backup would lock them out.
	var preUsers []model.User
	if opts.IncludeUsers {
		if preUsers, err = s.repo.GetUsers(); err != nil {
			return nil, fmt.Errorf("read existing users: %w", err)
		}
	}

	// Bracket the whole mutation with a monitor pause so kernel reconciliation
	// doesn't race the replacement of routes/interfaces.
	if s.monitor != nil {
		s.monitor.Pause()
		defer s.monitor.Resume()
	}

	// Pre-import snapshot (best-effort; checkpoint first so WAL pages are flushed).
	_ = s.repo.Checkpoint()
	if snapPath, snapErr := db.SnapshotDatabase(s.dbPath, "backup-preimport"); snapErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("pre-import snapshot failed: %v", snapErr))
	} else if snapPath != "" {
		log.Printf("[Import] Pre-import snapshot: %s", snapPath)
	}

	// Atomic restore. On any error the original DB is untouched.
	if err := s.repo.RestoreConfig(cfg, opts.IncludeUsers); err != nil {
		return nil, fmt.Errorf("restore failed (no changes applied): %w", err)
	}

	// Guard against the actor locking themselves out, and figure out whose
	// sessions to purge.
	if opts.IncludeUsers {
		result.UsersImported = true
		if warn := s.guardActor(opts, preUsers); warn != "" {
			result.Warnings = append(result.Warnings, warn)
		}
		result.RemovedUsernames = s.removedUsernames(preUsers, opts.ActorUsername)
	}

	result.Counts = configCounts(cfg)

	// Re-apply DB config to the kernel in startup order. Failures are warnings.
	result.Warnings = append(result.Warnings, s.reapply()...)

	return result, nil
}

// resolveInterfaces matches each backup interface to a live device interface by
// name, merging the backup's config fields onto the device row while keeping the
// device's hardware/runtime identity (id, name, type, mac addresses, status,
// speed). Interfaces in the backup that don't exist on this device are skipped
// with a warning (§3.5). Returns the merged rows to restore, warnings, and
// whether any interface config changed (used to warn the admin about possible
// disconnection).
func (s *BackupService) resolveInterfaces(backup []model.NetworkInterface) ([]model.NetworkInterface, []string, bool, error) {
	existing, err := s.repo.GetInterfaces()
	if err != nil {
		return nil, nil, false, err
	}
	byName := make(map[string]model.NetworkInterface, len(existing))
	for _, e := range existing {
		byName[e.Name] = e
	}

	var merged []model.NetworkInterface
	var warnings []string
	changed := false
	for _, b := range backup {
		dev, ok := byName[b.Name]
		if !ok {
			// VLAN sub-interfaces are not present in the kernel until they are
			// re-created at reapply time (InitApplyConfigurationAtStartup), so a VLAN
			// row absent from the device must NOT be dropped — keep it verbatim as long
			// as its parent interface exists here. Without this, restoring onto a fresh
			// board would silently lose every VLAN.
			if b.Subtype == "vlan" {
				parentName := ""
				if b.VlanParent != nil {
					parentName = *b.VlanParent
				}
				if _, parentOK := byName[parentName]; parentName == "" || !parentOK {
					warnings = append(warnings, fmt.Sprintf("VLAN %q from backup skipped: parent %q is not present on this device", b.Name, parentName))
					continue
				}
				changed = true
				merged = append(merged, b)
				continue
			}
			warnings = append(warnings, fmt.Sprintf("interface %q from backup is not present on this device — skipped", b.Name))
			continue
		}
		if interfaceConfigDiffers(dev, b) {
			changed = true
		}
		// Start from the live device row (keeps id/name/type/mac/status/speed)
		// then overlay the backup's config-only fields.
		m := dev
		m.Alias = b.Alias
		m.Role = b.Role
		m.AddressingMode = b.AddressingMode
		m.IP = b.IP
		m.Netmask = b.Netmask
		m.Gateway = b.Gateway
		m.Metric = b.Metric
		m.AdminAccess = b.AdminAccess
		m.MacMode = b.MacMode
		m.RandomizedMac = b.RandomizedMac
		m.LaaMacAddress = b.LaaMacAddress
		m.RandomizeOnReconnect = b.RandomizeOnReconnect
		m.WifiSSID = b.WifiSSID
		m.WifiPassword = b.WifiPassword
		m.WifiSecurity = b.WifiSecurity
		m.FailoverEnabled = b.FailoverEnabled
		m.BackupSSID = b.BackupSSID
		m.BackupWifiPassword = b.BackupWifiPassword
		m.BackupWifiSecurity = b.BackupWifiSecurity
		m.IPCheckTimeout = b.IPCheckTimeout
		m.PrimaryMaxRetries = b.PrimaryMaxRetries
		m.FailoverCooldown = b.FailoverCooldown
		merged = append(merged, m)
	}

	// Alias uniqueness guard (issue #25): network_interfaces has a case-insensitive
	// unique index on alias, and RestoreConfig runs in a single transaction — one
	// bad alias in the backup would roll back the whole restore. Normalize (empty ->
	// own name) and de-duplicate against both the other merged rows and the device
	// rows this restore does not touch (restore updates by id, it does not replace
	// the table).
	mergedIDs := make(map[string]bool, len(merged))
	for _, m := range merged {
		mergedIDs[m.ID] = true
	}
	taken := make(map[string]bool)
	names := make(map[string]bool)
	for _, e := range existing {
		names[strings.ToLower(e.Name)] = true
		if !mergedIDs[e.ID] {
			taken[strings.ToLower(e.Alias)] = true
		}
	}
	for _, m := range merged {
		names[strings.ToLower(m.Name)] = true // VLAN rows may be new to this device
	}
	conflicts := func(alias, ownName string) bool {
		lower := strings.ToLower(alias)
		return taken[lower] || (names[lower] && !strings.EqualFold(alias, ownName))
	}
	for i := range merged {
		m := &merged[i]
		alias := strings.TrimSpace(m.Alias)
		if alias == "" {
			alias = m.Name
		}
		if conflicts(alias, m.Name) {
			orig := alias
			alias = m.Name
			for n := 2; conflicts(alias, m.Name); n++ {
				alias = fmt.Sprintf("%s_%d", m.Name, n)
			}
			warnings = append(warnings, fmt.Sprintf("interface %q: alias %q already in use — replaced with %q", m.Name, orig, alias))
		}
		m.Alias = alias
		taken[strings.ToLower(alias)] = true
	}

	return merged, warnings, changed, nil
}

// guardActor ensures the account performing the import is still an active
// super_admin after users were restored. If the backup omitted or demoted/
// disabled them, the actor is reinstated from the pre-import snapshot so an
// import can never lock the operator out. Returns a warning string if it acted.
func (s *BackupService) guardActor(opts model.ImportOptions, preUsers []model.User) string {
	if opts.ActorUsername == "" {
		return ""
	}
	actor, err := s.repo.GetUserByUsername(opts.ActorUsername)
	if err != nil {
		return fmt.Sprintf("could not verify actor account %q after import: %v", opts.ActorUsername, err)
	}
	if actor != nil && actor.Role == "super_admin" && actor.Status == "active" {
		return "" // still fine
	}

	// Reinstate from the pre-import record.
	var pre *model.User
	for i := range preUsers {
		if preUsers[i].Username == opts.ActorUsername {
			pre = &preUsers[i]
			break
		}
	}

	if actor == nil {
		if pre == nil {
			return fmt.Sprintf("actor %q not found before or after import; could not guarantee access", opts.ActorUsername)
		}
		reinstated := *pre
		reinstated.Role = "super_admin"
		reinstated.Status = "active"
		if err := s.repo.CreateUser(reinstated); err != nil {
			return fmt.Sprintf("failed to reinstate actor %q: %v", opts.ActorUsername, err)
		}
		return fmt.Sprintf("imported backup omitted your account %q — it was preserved as an active super_admin to prevent lock-out", opts.ActorUsername)
	}

	// Exists but demoted/disabled → restore role+status.
	_ = s.repo.UpdateUserRole(actor.ID, "super_admin")
	_ = s.repo.SetUserStatus(actor.ID, "active")
	return fmt.Sprintf("imported backup demoted/disabled your account %q — it was kept as an active super_admin to prevent lock-out", opts.ActorUsername)
}

// removedUsernames returns usernames that existed before the import but are gone
// or disabled afterwards, excluding the actor. The API layer purges their
// sessions.
func (s *BackupService) removedUsernames(preUsers []model.User, actor string) []string {
	post, err := s.repo.GetUsers()
	if err != nil {
		return nil
	}
	activeNow := make(map[string]bool, len(post))
	for _, u := range post {
		if u.Status == "active" {
			activeNow[u.Username] = true
		}
	}
	var removed []string
	for _, u := range preUsers {
		if u.Username == actor {
			continue
		}
		if !activeNow[u.Username] {
			removed = append(removed, u.Username)
		}
	}
	return removed
}

// reapply pushes the freshly restored DB state to the kernel, in the same order
// as the startup sequence in cmd/pigate/main.go. Each failure is collected as a
// warning rather than aborting — the DB is authoritative and a reboot re-applies
// everything regardless.
func (s *BackupService) reapply() []string {
	var warnings []string
	step := func(name string, fn func() error) {
		if fn == nil {
			return
		}
		if err := fn(); err != nil {
			warnings = append(warnings, fmt.Sprintf("re-apply %s failed: %v", name, err))
		}
	}

	if s.timeService != nil {
		step("time", s.timeService.InitApplyConfig)
	}
	if s.interfaceService != nil {
		step("interfaces", s.interfaceService.InitApplyConfigurationAtStartup)
	}
	if s.routingService != nil {
		step("routes", s.routingService.InitApplyConfig)
	}
	if s.hostnameService != nil {
		step("hostname", s.hostnameService.InitApplyConfig)
	}
	if s.dhcpcdService != nil {
		s.dhcpcdService.SyncActiveInterfaces()
	}
	if s.dhcpServerService != nil {
		step("dhcp", s.dhcpServerService.InitApplyConfig)
	}
	if s.dnsServerService != nil {
		step("dns-server", s.dnsServerService.InitApplyConfig)
	}
	if s.dnsService != nil {
		step("dns", s.dnsService.ApplyDNSConfig)
	}
	if s.firewallService != nil {
		step("firewall", s.firewallService.InitApplyConfig)
	}
	if s.qosService != nil {
		step("qos", s.qosService.InitApplyConfig)
	}
	return warnings
}

// =============================================================================
// Decoding / validation helpers
// =============================================================================

// decodeBackup parses a backup file, transparently accepting the v2 typed
// format (plaintext or passphrase-encrypted) and the legacy v1 layout, and
// verifies the checksum when present. Returns the config plus the detected
// schema version.
func decodeBackup(raw []byte, passphrase string) (model.BackupConfig, int, error) {
	// Probe for a v2 meta block.
	var probe struct {
		Meta *struct {
			SchemaVersion int `json:"schemaVersion"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return model.BackupConfig{}, 0, fmt.Errorf("invalid JSON: %w", err)
	}

	if probe.Meta == nil {
		cfg, err := mapLegacyV1(raw)
		return cfg, 1, err
	}

	var file model.BackupFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return model.BackupConfig{}, 0, fmt.Errorf("invalid backup structure: %w", err)
	}
	if file.Meta.SchemaVersion > model.CurrentBackupSchemaVersion {
		return model.BackupConfig{}, 0, fmt.Errorf("backup schema version %d is newer than supported version %d", file.Meta.SchemaVersion, model.CurrentBackupSchemaVersion)
	}

	var cfg model.BackupConfig
	if file.Meta.Encrypted {
		if passphrase == "" {
			return model.BackupConfig{}, 0, ErrPassphraseRequired
		}
		plaintext, err := decryptConfig(file.EncryptedConfig, passphrase, file.Meta.Encryption)
		if err != nil {
			return model.BackupConfig{}, 0, err
		}
		if err := json.Unmarshal(plaintext, &cfg); err != nil {
			return model.BackupConfig{}, 0, fmt.Errorf("decrypted config is not valid JSON: %w", err)
		}
	} else {
		if file.Config == nil {
			return model.BackupConfig{}, 0, fmt.Errorf("backup file has no config section")
		}
		cfg = *file.Config
	}

	if file.Meta.Checksum != "" {
		want := strings.TrimPrefix(file.Meta.Checksum, "sha256:")
		got, err := configChecksum(cfg)
		if err != nil {
			return model.BackupConfig{}, 0, fmt.Errorf("recompute checksum: %w", err)
		}
		if strings.TrimPrefix(got, "sha256:") != want {
			return model.BackupConfig{}, 0, fmt.Errorf("checksum mismatch: backup file is corrupted or was modified")
		}
	}
	return cfg, file.Meta.SchemaVersion, nil
}

// mapLegacyV1 maps the pre-v2 export shape into a v2 BackupConfig. The old
// format nested settings under systemSettings/hostnameSettings and DHCP under
// config.dhcp.config, and exported the kernel-merged route view — ghost
// "route-sys-*" rows and system/defaultgateway routes are dropped here.
func mapLegacyV1(raw []byte) (model.BackupConfig, error) {
	var v1 struct {
		SystemSettings   *model.SystemTimeSettings     `json:"systemSettings"`
		HostnameSettings *model.SystemHostnameSettings `json:"hostnameSettings"`
		Config           struct {
			Addresses      []model.AddressObject    `json:"addresses"`
			ServiceObjects []model.ServiceObject    `json:"serviceObjects"`
			Policies       []model.PolicyRule       `json:"policies"`
			Routes         []model.StaticRoute      `json:"routes"`
			Interfaces     []model.NetworkInterface `json:"interfaces"`
			DHCP           *struct {
				Config       *model.DhcpConfig       `json:"config"`
				Reservations []model.DhcpReservation `json:"reservations"`
			} `json:"dhcp"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &v1); err != nil {
		return model.BackupConfig{}, fmt.Errorf("invalid v1 backup structure: %w", err)
	}

	cfg := model.BackupConfig{
		Addresses:      v1.Config.Addresses,
		ServiceObjects: v1.Config.ServiceObjects,
		Policies:       v1.Config.Policies,
		Interfaces:     v1.Config.Interfaces,
	}
	for _, rt := range v1.Config.Routes {
		if rt.Type == "system" || rt.Type == "defaultgateway" || strings.HasPrefix(rt.ID, "route-sys-") {
			continue
		}
		cfg.StaticRoutes = append(cfg.StaticRoutes, rt)
	}
	if v1.Config.DHCP != nil {
		if v1.Config.DHCP.Config != nil {
			cfg.DhcpConfigs = []model.DhcpConfig{*v1.Config.DHCP.Config}
		}
		cfg.DhcpReservations = v1.Config.DHCP.Reservations
	}
	if v1.SystemSettings != nil {
		cfg.SystemTime = *v1.SystemSettings
	}
	if v1.HostnameSettings != nil {
		cfg.SystemHostname = *v1.HostnameSettings
	}
	return cfg, nil
}

// validateConfig runs cheap, structural validation before any DB write:
// referential integrity of policies against the objects in the same file, plus
// a few enum sanity checks. Deep value checks are left to SQLite CHECK
// constraints, which roll the restore transaction back if violated.
func validateConfig(cfg model.BackupConfig) error {
	addrNames := make(map[string]bool, len(cfg.Addresses))
	for _, a := range cfg.Addresses {
		addrNames[a.Name] = true
	}
	svcNames := make(map[string]bool, len(cfg.ServiceObjects))
	for _, s := range cfg.ServiceObjects {
		svcNames[s.Name] = true
	}

	for _, p := range cfg.Policies {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("policy has empty name")
		}
		if p.Action != "ACCEPT" && p.Action != "DROP" {
			return fmt.Errorf("policy %q has invalid action %q", p.Name, p.Action)
		}
		if len(p.Source) == 0 || len(p.Destination) == 0 || len(p.Service) == 0 {
			return fmt.Errorf("policy %q must reference at least one source, destination, and service", p.Name)
		}
		for _, n := range append(append([]string{}, p.Source...), p.Destination...) {
			if !addrNames[n] {
				return fmt.Errorf("policy %q references unknown address object %q", p.Name, n)
			}
		}
		for _, n := range p.Service {
			if !svcNames[n] {
				return fmt.Errorf("policy %q references unknown service object %q", p.Name, n)
			}
		}
	}

	for _, pf := range cfg.PortForwards {
		if err := model.ValidatePortForward(pf); err != nil {
			return fmt.Errorf("port forward %q: %w", pf.Name, err)
		}
	}

	if cfg.SystemDns.Mode != "" && cfg.SystemDns.Mode != "wan" && cfg.SystemDns.Mode != "static" {
		return fmt.Errorf("invalid system DNS mode %q", cfg.SystemDns.Mode)
	}

	// The import path writes DNS zones/records and DHCP reservations straight to
	// the DB, bypassing the create/update handlers. Enforce the same whitelist
	// here so a crafted backup can't inject a dnsmasq directive. Fail-closed:
	// one bad entry rejects the whole import (which is atomic) before any write.
	for _, z := range cfg.DnsZones {
		if err := model.ValidateDNSZone(z); err != nil {
			return fmt.Errorf("dns zone %q: %w", z.ZoneName, err)
		}
		for _, rec := range z.Records {
			if err := model.ValidateDNSRecord(rec); err != nil {
				return fmt.Errorf("dns record in zone %q: %w", z.ZoneName, err)
			}
		}
	}
	for _, res := range cfg.DhcpReservations {
		if err := model.ValidateReservation(res); err != nil {
			return fmt.Errorf("dhcp reservation %q: %w", res.DeviceName, err)
		}
	}
	for _, c := range cfg.DhcpConfigs {
		if err := model.ValidateDhcpConfig(c); err != nil {
			return fmt.Errorf("dhcp config %q: %w", c.Interface, err)
		}
	}
	return nil
}

func configCounts(cfg model.BackupConfig) map[string]int {
	records := 0
	for _, z := range cfg.DnsZones {
		records += len(z.Records)
	}
	return map[string]int{
		"interfaces":       len(cfg.Interfaces),
		"staticRoutes":     len(cfg.StaticRoutes),
		"addresses":        len(cfg.Addresses),
		"serviceObjects":   len(cfg.ServiceObjects),
		"policies":         len(cfg.Policies),
		"portForwards":     len(cfg.PortForwards),
		"dhcpConfigs":      len(cfg.DhcpConfigs),
		"dhcpReservations": len(cfg.DhcpReservations),
		"dnsZones":         len(cfg.DnsZones),
		"dnsRecords":       records,
		"qosRules":         len(cfg.QosRules),
		"users":            len(cfg.Users),
	}
}

// configChecksum returns "sha256:<hex>" over the canonical JSON marshalling of a
// BackupConfig. Because Go marshals struct fields in declaration order, the same
// config always produces the same bytes, so a reformatted (pretty-printed) file
// re-normalises through the typed struct and still verifies.
func configChecksum(cfg model.BackupConfig) (string, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// interfaceConfigDiffers reports whether the backup's config-only fields differ
// from the device row, used to flag a possible admin disconnection.
func interfaceConfigDiffers(dev, b model.NetworkInterface) bool {
	return dev.IP != b.IP ||
		dev.Netmask != b.Netmask ||
		dev.Gateway != b.Gateway ||
		dev.AddressingMode != b.AddressingMode ||
		dev.Role != b.Role ||
		strings.Join(dev.AdminAccess, ",") != strings.Join(b.AdminAccess, ",")
}
