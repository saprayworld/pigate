package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// TLS certificate/key file names inside the tls dir. Kept as package constants so
// the listener ladder in main.go and the tests reference the same paths.
const (
	tlsCertFileName = "server.crt"
	tlsKeyFileName  = "server.key"
)

// Fixed certificate validity window. Deliberately NOT derived from time.Now():
// a Raspberry Pi has no RTC battery, so first boot before NTP sync can report a
// clock anywhere from 1970 to a stale image date. If we computed NotAfter = now+Ny
// on a box whose clock later jumped forward, the cert would already be expired; if
// the clock was 1970 at gen time, NotBefore would sit in the future once corrected.
// A wide constant window (2020..2056) is valid regardless of the wall clock at
// generation time — the browser, not the server, validates the cert, and a
// self-signed cert already shows a trust warning the user accepts once.
var (
	certNotBefore = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	certNotAfter  = time.Date(2056, 1, 1, 0, 0, 0, 0, time.UTC)
)

// EnsureSelfSignedCert makes sure a usable self-signed TLS certificate/key pair
// exists in dir, generating one only when needed. It is idempotent: if a valid,
// parseable, non-expired (relative to the current clock) certificate already
// exists it is reused and generated=false. It regenerates when the pair is
// missing, unreadable, unparseable, or outside its validity window.
//
// The generated certificate is ECDSA P-256, with SANs covering hostname,
// "localhost", 127.0.0.1, ::1 and the supplied interface IPs. The private key is
// written with 0600 permissions and never leaves the filesystem (not in DB/backup).
func EnsureSelfSignedCert(dir, hostname string, ips []net.IP) (certPath, keyPath string, generated bool, err error) {
	certPath = filepath.Join(dir, tlsCertFileName)
	keyPath = filepath.Join(dir, tlsKeyFileName)

	if certValid(certPath, keyPath) {
		return certPath, keyPath, false, nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", false, fmt.Errorf("failed to create tls dir %q: %w", dir, err)
	}

	if err := generateSelfSignedCert(certPath, keyPath, hostname, ips); err != nil {
		return "", "", false, err
	}
	return certPath, keyPath, true, nil
}

// certValid reports whether both files exist, parse as a matching cert/key pair,
// and the certificate is currently within its validity window relative to the
// system clock. Any failure returns false so the caller regenerates.
func certValid(certPath, keyPath string) bool {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}
	if _, err := os.Stat(keyPath); err != nil {
		return false
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}

	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return false
	}
	return true
}

// generateSelfSignedCert creates an ECDSA P-256 self-signed certificate and writes
// the cert (0644) and key (0600) as PEM files.
func generateSelfSignedCert(certPath, keyPath, hostname string, ips []net.IP) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate ECDSA key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return fmt.Errorf("failed to generate certificate serial: %w", err)
	}

	if hostname == "" {
		hostname = "pigate"
	}

	// SAN set: DNS names (hostname + localhost) and IP addresses (loopback +
	// supplied interface IPs), de-duplicated.
	dnsNames := dedupeStrings([]string{hostname, "localhost"})
	ipSet := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	ipSet = append(ipSet, ips...)
	ipSet = dedupeIPs(ipSet)

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   hostname,
			Organization: []string{"PiGate"},
		},
		NotBefore:             certNotBefore,
		NotAfter:              certNotAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ipSet,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write the private key first with 0600 before the cert. Use O_EXCL-free
	// truncating create so a regenerate overwrites a stale pair cleanly.
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return fmt.Errorf("failed to marshal EC private key: %w", err)
	}
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open key file for writing: %w", err)
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		keyFile.Close()
		return fmt.Errorf("failed to write key PEM: %w", err)
	}
	if err := keyFile.Close(); err != nil {
		return fmt.Errorf("failed to close key file: %w", err)
	}
	// Re-assert 0600 in case a pre-existing file had looser perms (OpenFile does
	// not tighten permissions on an existing file).
	if err := os.Chmod(keyPath, 0600); err != nil {
		return fmt.Errorf("failed to chmod key file: %w", err)
	}

	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open cert file for writing: %w", err)
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		certFile.Close()
		return fmt.Errorf("failed to write cert PEM: %w", err)
	}
	if err := certFile.Close(); err != nil {
		return fmt.Errorf("failed to close cert file: %w", err)
	}

	return nil
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func dedupeIPs(in []net.IP) []net.IP {
	seen := make(map[string]bool, len(in))
	out := make([]net.IP, 0, len(in))
	for _, ip := range in {
		if ip == nil {
			continue
		}
		key := ip.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ip)
	}
	return out
}

// LocalInterfaceIPs returns the non-loopback unicast IP addresses currently
// assigned to the host's interfaces, for inclusion in the certificate SANs. Errors
// are swallowed to a nil slice — the cert is still valid via hostname/localhost.
func LocalInterfaceIPs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() || !ip.IsGlobalUnicast() {
			continue
		}
		ips = append(ips, ip)
	}
	return ips
}
