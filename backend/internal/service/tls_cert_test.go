package service

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureSelfSignedCert_GeneratesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	ips := []net.IP{net.ParseIP("192.168.1.1")}

	certPath, keyPath, generated, err := EnsureSelfSignedCert(dir, "pigate.local", ips)
	if err != nil {
		t.Fatalf("EnsureSelfSignedCert returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated=true on first call")
	}
	if certPath != filepath.Join(dir, tlsCertFileName) || keyPath != filepath.Join(dir, tlsKeyFileName) {
		t.Fatalf("unexpected paths: cert=%q key=%q", certPath, keyPath)
	}
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("cert file not written: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("key file not written: %v", err)
	}
}

func TestEnsureSelfSignedCert_Idempotent(t *testing.T) {
	dir := t.TempDir()

	_, _, generated1, err := EnsureSelfSignedCert(dir, "pigate.local", nil)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if !generated1 {
		t.Fatalf("first call should generate")
	}

	certBefore, err := os.ReadFile(filepath.Join(dir, tlsCertFileName))
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}

	_, _, generated2, err := EnsureSelfSignedCert(dir, "pigate.local", nil)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if generated2 {
		t.Fatalf("second call should reuse the existing valid cert (generated=false)")
	}

	certAfter, err := os.ReadFile(filepath.Join(dir, tlsCertFileName))
	if err != nil {
		t.Fatalf("read cert after: %v", err)
	}
	if string(certBefore) != string(certAfter) {
		t.Fatalf("cert was regenerated despite being valid")
	}
}

func TestEnsureSelfSignedCert_KeyPerms0600(t *testing.T) {
	dir := t.TempDir()
	_, keyPath, _, err := EnsureSelfSignedCert(dir, "pigate.local", nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected key perms 0600, got %o", perm)
	}
}

func TestEnsureSelfSignedCert_SANsAndValidity(t *testing.T) {
	dir := t.TempDir()
	ip := net.ParseIP("10.20.30.40")
	_, _, _, err := EnsureSelfSignedCert(dir, "myhost", []net.IP{ip})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	cert := parseCert(t, filepath.Join(dir, tlsCertFileName))

	// DNS SANs must include hostname + localhost.
	if !containsStr(cert.DNSNames, "myhost") {
		t.Errorf("DNSNames missing hostname: %v", cert.DNSNames)
	}
	if !containsStr(cert.DNSNames, "localhost") {
		t.Errorf("DNSNames missing localhost: %v", cert.DNSNames)
	}

	// IP SANs must include loopback and the supplied interface IP.
	if !containsIP(cert.IPAddresses, net.IPv4(127, 0, 0, 1)) {
		t.Errorf("IPAddresses missing 127.0.0.1: %v", cert.IPAddresses)
	}
	if !containsIP(cert.IPAddresses, ip) {
		t.Errorf("IPAddresses missing supplied IP: %v", cert.IPAddresses)
	}

	// Validity must be the fixed constant window, not derived from now.
	if !cert.NotBefore.Equal(certNotBefore) {
		t.Errorf("NotBefore = %v, want %v", cert.NotBefore, certNotBefore)
	}
	if !cert.NotAfter.Equal(certNotAfter) {
		t.Errorf("NotAfter = %v, want %v", cert.NotAfter, certNotAfter)
	}
}

func TestEnsureSelfSignedCert_RegeneratesWhenExpired(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, tlsCertFileName)
	keyPath := filepath.Join(dir, tlsKeyFileName)

	// Generate a normal cert first, capture it.
	if _, _, _, err := EnsureSelfSignedCert(dir, "pigate.local", nil); err != nil {
		t.Fatalf("initial gen: %v", err)
	}
	valid := parseCert(t, certPath)
	_ = valid

	// Overwrite the cert file with an already-expired certificate to simulate a
	// stale on-disk cert; the key stays. EnsureSelfSignedCert must regenerate.
	writeExpiredCert(t, certPath, keyPath)

	origMtimeCert, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read expired cert: %v", err)
	}

	_, _, generated, err := EnsureSelfSignedCert(dir, "pigate.local", nil)
	if err != nil {
		t.Fatalf("regen call error: %v", err)
	}
	if !generated {
		t.Fatalf("expected regeneration for expired cert")
	}

	newCertBytes, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read regenerated cert: %v", err)
	}
	if string(newCertBytes) == string(origMtimeCert) {
		t.Fatalf("cert was not regenerated")
	}
	// Regenerated cert must be within the fixed validity window.
	regen := parseCert(t, certPath)
	now := time.Now()
	if now.Before(regen.NotBefore) || now.After(regen.NotAfter) {
		t.Fatalf("regenerated cert not currently valid: NotBefore=%v NotAfter=%v", regen.NotBefore, regen.NotAfter)
	}
}

func TestEnsureSelfSignedCert_RegeneratesWhenUnparseable(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, tlsCertFileName)
	keyPath := filepath.Join(dir, tlsKeyFileName)

	// Write garbage cert + a dummy key.
	if err := os.WriteFile(certPath, []byte("not a pem"), 0644); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("garbage"), 0600); err != nil {
		t.Fatalf("write garbage key: %v", err)
	}

	_, _, generated, err := EnsureSelfSignedCert(dir, "pigate.local", nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !generated {
		t.Fatalf("expected regeneration for unparseable cert")
	}
	// New cert must parse.
	parseCert(t, certPath)
}

// --- helpers ---

func parseCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cert %q: %v", path, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatalf("cert %q is not valid PEM", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert %q: %v", path, err)
	}
	return cert
}

// writeExpiredCert generates a self-signed cert whose validity window is entirely
// in the past and writes it to certPath (reusing a fresh key at keyPath).
func writeExpiredCert(t *testing.T, certPath, keyPath string) {
	t.Helper()
	// Temporarily swap the package validity constants to a past window, generate,
	// then restore. This keeps the expired cert generation using the same code path.
	origBefore, origAfter := certNotBefore, certNotAfter
	certNotBefore = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	certNotAfter = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	defer func() { certNotBefore, certNotAfter = origBefore, origAfter }()

	if err := generateSelfSignedCert(certPath, keyPath, "pigate.local", nil); err != nil {
		t.Fatalf("write expired cert: %v", err)
	}
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func containsIP(list []net.IP, want net.IP) bool {
	for _, ip := range list {
		if ip.Equal(want) {
			return true
		}
	}
	return false
}
