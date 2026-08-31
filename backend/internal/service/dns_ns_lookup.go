// Package service: NS-delegation glue auto-lookup (docs/ref/todo/
// dns-ns-delegation-plan.md T-05). Resolves the nameserver name a user
// enters as an NS record's Value into the IP address(es) to use as glue,
// so the "ค้นหา IP อัตโนมัติ" button in the DNS Server UI does not require
// the user to look the address up themselves.
//
// Deliberately NOT a kernel.* capability: this does not touch the OS
// (no Netlink/D-Bus/exec.Command), it only issues an outbound DNS query via
// the Go standard resolver, so it does not need a real/mock kernel pair —
// it behaves identically in -mock=true and real mode.
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"pigate/internal/model"
)

// ErrNSLookupInvalidName/ErrNSLookupNotFound/ErrNSLookupRateLimited are the
// sentinel errors ResolveNameserver returns — the API handler (T-06) maps
// each to a distinct HTTP status.
var (
	ErrNSLookupInvalidName = errors.New("dns ns lookup: invalid nameserver name")
	ErrNSLookupNotFound    = errors.New("dns ns lookup: no addresses found")
	ErrNSLookupRateLimited = errors.New("dns ns lookup: rate limited")
)

// nsLookupTimeout bounds how long a single ResolveNameserver call may block
// on the outbound DNS query, so a slow/unreachable resolver can never hang
// the HTTP request indefinitely.
const nsLookupTimeout = 3 * time.Second

// resolveNSHostIPs is the seam tests replace (precedent:
// kernel/real_firewall.go's lookupIP, stubbed in kernel/policy_chain_test.go).
// Production uses the Go resolver (PreferGo) so lookups follow
// /etc/resolv.conf, i.e. the same resolver the box itself uses
// (dnsmasq/systemd-resolved) — NOT the upstream servers configured in DNS
// Server settings, which would be shadowed by this zone's own `local=`
// directive and could never see a name outside it anyway (owner decision,
// plan §7 item 4).
var resolveNSHostIPs = func(ctx context.Context, name string) ([]net.IP, error) {
	r := &net.Resolver{PreferGo: true}
	addrs, err := r.LookupIPAddr(ctx, name)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

// nsLookupRateLimiter is a minimal single-bucket token bucket (burst 5,
// refill 1/s), modeled after ipInfoRateLimiter (service/ipinfo.go) but
// declared separately here so the two features' quotas never interact.
type nsLookupRateLimiter struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64 // tokens added per second
	last   time.Time
}

func newNSLookupRateLimiter() *nsLookupRateLimiter {
	return &nsLookupRateLimiter{tokens: 5, max: 5, rate: 1, last: time.Now()}
}

func (l *nsLookupRateLimiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.last).Seconds()
	l.last = now
	l.tokens += elapsed * l.rate
	if l.tokens > l.max {
		l.tokens = l.max
	}
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

// nsLookupLimiter is process-wide (one bucket for the whole service, not per
// caller) — its job is to protect against a runaway UI/script hammering this
// endpoint, not to separate individual API callers.
var nsLookupLimiter = newNSLookupRateLimiter()

// ResolveNameserver looks up the IP address(es) of an NS-delegation target
// name, for the "ค้นหา IP อัตโนมัติ" auto-lookup button (DNS Server UI). It
// validates name, rate-limits, resolves via resolveNSHostIPs, then dedupes/
// sorts/caps the result.
//
// name is validated BEFORE anything else — including before it is ever
// logged — via model.EncodeDNSNameHex, reusing the exact same charset/label/
// length rules (and newline rejection) the config generator itself relies
// on, so this function can never accept a name the generator would reject.
func (s *DNSServerService) ResolveNameserver(ctx context.Context, name string) ([]string, error) {
	// Validate first — do not log the raw, unvalidated input anywhere (log
	// injection guard): a name containing a newline must never reach a log
	// line before we know it's safe.
	if _, err := model.EncodeDNSNameHex(name); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNSLookupInvalidName, err)
	}
	// Normalize the same way EncodeDNSNameHex does (trim, drop trailing dot,
	// lowercase) so the name we resolve/log/return is the exact validated
	// form, never the raw user input.
	validated := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))

	if !nsLookupLimiter.allow() {
		return nil, ErrNSLookupRateLimited
	}

	lookupCtx, cancel := context.WithTimeout(ctx, nsLookupTimeout)
	defer cancel()

	// NOTE: deliberately NOT filtered through isGloballyRoutable
	// (service/ipinfo.go) — that guard exists because ipinfo.go/
	// dns_blocklist_fetch.go actually CONNECT to the looked-up address
	// (SSRF risk). Here we only write the resolved IP into a config file as
	// NS-delegation glue; a private/LAN nameserver (e.g. 192.168.1.53) is a
	// legitimate, intended use case (internal delegation), and the user
	// could type that same private IP into the glue field by hand anyway.
	addrs, err := resolveNSHostIPs(lookupCtx, validated)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(addrs))
	var v4, v6 []string
	for _, ip := range addrs {
		canon := ip.String()
		if seen[canon] {
			continue
		}
		seen[canon] = true
		if ip.To4() != nil {
			v4 = append(v4, canon)
		} else {
			v6 = append(v6, canon)
		}
	}
	sort.Strings(v4)
	sort.Strings(v6)

	result := append(v4, v6...)
	if len(result) > model.DNSNSGlueMaxIPs {
		result = result[:model.DNSNSGlueMaxIPs]
	}
	if len(result) == 0 {
		return nil, ErrNSLookupNotFound
	}

	log.Printf("[DNS Server] Resolved NS delegation target %q to %d address(es)", validated, len(result))
	return result, nil
}
