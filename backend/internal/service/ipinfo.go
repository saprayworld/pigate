// Package service: Public IP Info lookup (docs/ref/todo/
// statistics-host-ipinfo-plan.md) — a backend proxy in front of ipinfo.io so
// the Statistics -> Traffic -> Host page can show a "Public IP Info" card for
// public IPs instead of "Top peers" (plan §1/§2). This is deliberately a
// backend proxy, not a direct frontend fetch (plan §3: CSP, privacy, cache/
// rate-limit, "actually enforceable off-by-default"). This file also holds
// isGloballyRoutable, the SENSITIVE guard (plan Caution 3/§4 item 1) that
// decides whether an IP is even eligible to be looked up over the internet —
// every code path that may reach the provider MUST go through it first.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"pigate/internal/model"
)

// ipv4CGNAT/ipv4DocA/ipv4DocB/ipv4DocC/ipv4Linklocal/ipv4TestReserved/
// ipv6ULA/ipv6Doc are the extra non-globally-routable ranges that
// net/netip's own IsPrivate/IsLoopback/IsLinkLocalUnicast classifiers do NOT
// cover (plan T-02 / Caution 3): CGNAT, the TEST-NET blocks, "reserved"
// 240.0.0.0/4, and IPv6 documentation range. IsPrivate() already covers
// fc00::/7 (IPv6 ULA) and RFC1918, so it is not re-declared here — see the
// comment on isGloballyRoutable below.
var (
	ipv4CGNAT        = netip.MustParsePrefix("100.64.0.0/10")
	ipv4ZeroNet      = netip.MustParsePrefix("0.0.0.0/8")
	ipv4LinkLocal    = netip.MustParsePrefix("169.254.0.0/16")
	ipv4TestNet1     = netip.MustParsePrefix("192.0.2.0/24")
	ipv4TestNet2     = netip.MustParsePrefix("198.51.100.0/24")
	ipv4TestNet3     = netip.MustParsePrefix("203.0.113.0/24")
	ipv4Reserved     = netip.MustParsePrefix("240.0.0.0/4")
	ipv6DocumentAddr = netip.MustParsePrefix("2001:db8::/32")
)

// isGloballyRoutable reports whether ip is a public, internet-routable
// address — the ONLY guard allowed to gate an outbound ipinfo.io request
// (plan Caution 3: the existing isPrivateIP in statistics.go is display-only
// and covers too few ranges — e.g. it misses CGNAT, TEST-NET, and multicast —
// so it must never be reused here). An unparseable string is treated as NOT
// globally routable (fail closed).
//
// addr.Unmap() runs BEFORE any classification so an IPv4-mapped IPv6 literal
// like "::ffff:192.168.1.1" is judged by its embedded IPv4 address, not by
// the (globally-routable-looking) IPv6 wrapper — otherwise a private IPv4
// address smuggled in IPv6 form would slip past every check below and reach
// the provider (plan T-02 explicit requirement).
func isGloballyRoutable(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()

	switch {
	case addr.IsPrivate(): // RFC1918 (IPv4) + ULA fc00::/7 (IPv6)
		return false
	case addr.IsLoopback():
		return false
	case addr.IsLinkLocalUnicast():
		return false
	case addr.IsLinkLocalMulticast():
		return false
	case addr.IsMulticast():
		return false
	case addr.IsUnspecified():
		return false
	case addr.IsInterfaceLocalMulticast():
		return false
	}

	if addr.Is4() || addr.Is4In6() {
		a4 := addr
		switch {
		case ipv4CGNAT.Contains(a4):
			return false
		case ipv4ZeroNet.Contains(a4):
			return false
		case ipv4LinkLocal.Contains(a4):
			return false
		case ipv4TestNet1.Contains(a4):
			return false
		case ipv4TestNet2.Contains(a4):
			return false
		case ipv4TestNet3.Contains(a4):
			return false
		case ipv4Reserved.Contains(a4):
			return false
		}
		return true
	}

	// IPv6: ULA (fc00::/7) already excluded by IsPrivate() above; the only
	// additional range this feature needs to exclude is the documentation
	// prefix 2001:db8::/32 (plan T-02).
	if ipv6DocumentAddr.Contains(addr) {
		return false
	}

	return true
}

// IPInfoProvider is the pluggable backend for a single IP lookup — kept as
// an interface (plan §7 risk note) so a future provider swap (paid tier,
// token, or an offline MaxMind GeoLite2 .mmdb) never has to touch
// IPInfoService's caching/rate-limit/guard logic, only this seam.
type IPInfoProvider interface {
	Lookup(ctx context.Context, ip string) (model.IPInfoLookup, error)
}

// ipinfoIOBaseURL is the ONLY allowed base for outbound requests from
// ipinfoIOProvider (plan T-04 item 1) — every request is this constant plus
// an IP that has already been round-tripped through netip.ParseAddr().String()
// plus a fixed "/json" suffix. No raw client-supplied string is ever
// concatenated into the URL.
const ipinfoIOBaseURL = "https://ipinfo.io/"

// ipinfoIOProvider is the real IPInfoProvider — this is the FIRST outbound
// HTTP client in the pigate daemon (plan §4 item 1 / Caution 1), so every
// choice below is deliberately defensive:
//   - a dedicated http.Client with its own Timeout (never the zero-value
//     default client, which never times out);
//   - CheckRedirect refuses to follow any redirect (return
//     http.ErrUseLastResponse) so a hostile/compromised ipinfo.io response
//     can never redirect this request to an internal address
//     (SSRF-via-redirect defense);
//   - the response body is read through io.LimitReader capped at
//     model.IPInfoMaxResponseBytes, regardless of what Content-Length claims.
type ipinfoIOProvider struct {
	client *http.Client
	// token is accepted for forward-compatibility only (plan T-04 item 4) —
	// the constructor is ALWAYS called with "" in this phase; no config key
	// or UI exists to set it. When non-empty, it is sent as a Bearer token;
	// when empty (the only case reachable today), no Authorization header is
	// sent at all.
	token string
}

// newIPInfoIOProvider constructs the real provider. token is currently
// always "" (plan T-04 item 4 — no token support in this phase); the
// parameter exists so a future token can be threaded in without changing
// this constructor's signature or the IPInfoProvider interface.
func NewIPInfoIOProvider(token string) *ipinfoIOProvider {
	return &ipinfoIOProvider{
		client: &http.Client{
			Timeout: model.IPInfoHTTPTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		token: token,
	}
}

// ipinfoIORawResponse mirrors the subset of ipinfo.io's /json response this
// feature cares about. Fields absent from the response are simply the zero
// value ("") — never an error — since the free tier's exact field set can
// change at any time (plan §7).
type ipinfoIORawResponse struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	// Org is ipinfo.io's combined "AS<number> <name>" string (e.g.
	// "AS15169 Google LLC") — split into model.IPInfoLookup's Asn/AsName by
	// splitIPInfoOrg below, while Org itself is also kept verbatim.
	Org      string `json:"org"`
	Timezone string `json:"timezone"`
	Loc      string `json:"loc"`
}

// Lookup performs the real ipinfo.io request. ip is expected to already be a
// validated, canonical address (IPInfoService only calls providers after
// isGloballyRoutable has passed), but this re-parses defensively anyway
// (plan T-04 item 1: only a netip.ParseAddr().String() output may ever reach
// the URL, never a raw caller string).
func (p *ipinfoIOProvider) Lookup(ctx context.Context, ip string) (model.IPInfoLookup, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return model.IPInfoLookup{}, fmt.Errorf("ipinfo: invalid ip %q: %w", ip, err)
	}

	url := ipinfoIOBaseURL + addr.String() + "/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return model.IPInfoLookup{}, fmt.Errorf("ipinfo: build request: %w", err)
	}
	// token is always "" in this phase (plan T-04 item 4) — this branch is
	// unreachable today but kept so a future token doesn't require touching
	// the request-building logic.
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return model.IPInfoLookup{}, fmt.Errorf("ipinfo: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return model.IPInfoLookup{}, fmt.Errorf("ipinfo: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, model.IPInfoMaxResponseBytes))
	if err != nil {
		return model.IPInfoLookup{}, fmt.Errorf("ipinfo: read body: %w", err)
	}

	var raw ipinfoIORawResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return model.IPInfoLookup{}, fmt.Errorf("ipinfo: decode response: %w", err)
	}

	asn, asName := splitIPInfoOrg(raw.Org)

	return model.IPInfoLookup{
		Ip:       addr.String(),
		Hostname: raw.Hostname,
		City:     raw.City,
		Region:   raw.Region,
		Country:  raw.Country,
		Org:      raw.Org,
		Asn:      asn,
		AsName:   asName,
		Timezone: raw.Timezone,
		Loc:      raw.Loc,
	}, nil
}

// splitIPInfoOrg splits ipinfo.io's combined "AS<number> <name>" org string
// (e.g. "AS15169 Google LLC") into (asn, asName). When org doesn't start
// with "AS<digits>" it is left entirely in asName (unparsed), asn "".
func splitIPInfoOrg(org string) (asn, asName string) {
	org = strings.TrimSpace(org)
	if org == "" {
		return "", ""
	}
	parts := strings.SplitN(org, " ", 2)
	if len(parts) == 2 && strings.HasPrefix(parts[0], "AS") && isAllDigits(parts[0][2:]) {
		return parts[0], strings.TrimSpace(parts[1])
	}
	return "", org
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// mockIPInfoProvider is a deterministic, in-memory IPInfoProvider used when
// pigate runs with -mock (plan T-04: "สำหรับโหมด -mock") — never performs any
// network I/O. Calls is exported for tests (plan T-05 acceptance: "disabled
// แล้ว provider ไม่ถูกเรียกเลย ... นับด้วย counter ใน mock").
type mockIPInfoProvider struct {
	Calls int
	// FailFor, when non-empty, makes Lookup return an error for that exact
	// IP (test hook for the provider-failure path).
	FailFor map[string]bool
}

func NewMockIPInfoProvider() *mockIPInfoProvider {
	return &mockIPInfoProvider{FailFor: make(map[string]bool)}
}

// mockIPInfoData is a small, deterministic fixture set covering a handful of
// well-known public IPs, mirroring the frontend's mock fixtures (T-08) so
// dev-mode drill-downs into these IPs show plausible data end to end.
var mockIPInfoData = map[string]model.IPInfoLookup{
	"8.8.8.8":              {Ip: "8.8.8.8", Hostname: "dns.google", City: "Mountain View", Region: "California", Country: "US", Org: "AS15169 Google LLC", Asn: "AS15169", AsName: "Google LLC", Timezone: "America/Los_Angeles", Loc: "37.4056,-122.0775"},
	"1.1.1.1":              {Ip: "1.1.1.1", Hostname: "one.one.one.one", City: "", Region: "", Country: "AU", Org: "AS13335 Cloudflare, Inc.", Asn: "AS13335", AsName: "Cloudflare, Inc.", Timezone: "Australia/Sydney", Loc: "-33.8688,151.2093"},
	"2606:4700:4700::1111": {Ip: "2606:4700:4700::1111", Hostname: "one.one.one.one", Country: "AU", Org: "AS13335 Cloudflare, Inc.", Asn: "AS13335", AsName: "Cloudflare, Inc."},
}

func (m *mockIPInfoProvider) Lookup(_ context.Context, ip string) (model.IPInfoLookup, error) {
	m.Calls++
	if m.FailFor[ip] {
		return model.IPInfoLookup{}, fmt.Errorf("mock ipinfo: simulated failure for %s", ip)
	}
	if v, ok := mockIPInfoData[ip]; ok {
		return v, nil
	}
	// Deterministic synthetic fallback for any other public IP so the mock
	// UI has something plausible to render.
	return model.IPInfoLookup{
		Ip:      ip,
		City:    "Mockville",
		Region:  "Mock Region",
		Country: "US",
		Org:     "AS64500 Mock Org",
		Asn:     "AS64500",
		AsName:  "Mock Org",
	}, nil
}

// ErrIPInfoDisabled/ErrIPInfoNotPublic/ErrIPInfoRateLimited/ErrIPInfoProvider
// are the sentinel errors IPInfoService.Lookup returns — the API handler
// (plan T-07) maps each to a distinct HTTP status without ever leaking the
// upstream provider's raw error text to the client.
var (
	ErrIPInfoDisabled    = errors.New("ipinfo: feature disabled")
	ErrIPInfoNotPublic   = errors.New("ipinfo: ip is not globally routable")
	ErrIPInfoRateLimited = errors.New("ipinfo: rate limited")
	ErrIPInfoProvider    = errors.New("ipinfo: provider lookup failed")
)

// ipInfoRateLimiter is a minimal single-bucket token bucket (~1 req/s,
// burst 5), modeled after api.rateLimiter but service-wide (one bucket for
// the whole process, not per-client) — this limiter's job is to protect the
// upstream provider from this gateway's own auto-refreshing UI, not to
// separate individual API callers (plan T-05 item 4).
type ipInfoRateLimiter struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64 // tokens added per second
	last   time.Time
}

func newIPInfoRateLimiter() *ipInfoRateLimiter {
	return &ipInfoRateLimiter{tokens: 5, max: 5, rate: 1, last: time.Now()}
}

func (l *ipInfoRateLimiter) allow() bool {
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

// IPInfoService wires the guard (isGloballyRoutable), the RAM cache, a
// service-wide rate limiter, and a pluggable IPInfoProvider into a single
// Lookup call (plan T-05). enabled is read ONLY from the constructor
// (plan T-05: "ห้ามอ่าน env/ไฟล์เองในนี้") — it comes from the
// ipinfo-enabled config key, wired once at startup (plan T-06).
type IPInfoService struct {
	enabled  bool
	provider IPInfoProvider
	cache    *ipInfoCache
	limiter  *ipInfoRateLimiter
}

// NewIPInfoService constructs the service. provider is typically
// NewIPInfoIOProvider("") in real mode or a mockIPInfoProvider in -mock mode
// (selected by the caller in cmd/pigate/main.go, plan T-06).
func NewIPInfoService(enabled bool, provider IPInfoProvider) *IPInfoService {
	return &IPInfoService{
		enabled:  enabled,
		provider: provider,
		cache:    newIPInfoCache(),
		limiter:  newIPInfoRateLimiter(),
	}
}

// Lookup resolves ip to a model.IPInfoLookup, following the ordered decision
// path from plan T-05:
//  1. !enabled                    -> ErrIPInfoDisabled
//  2. !isGloballyRoutable(ip)     -> ErrIPInfoNotPublic (never reaches the provider)
//  3. cache hit                   -> Source == "cache"
//  4. rate limiter exhausted      -> ErrIPInfoRateLimited
//  5. single-flight provider call -> cached, Source == "live"
func (s *IPInfoService) Lookup(ctx context.Context, ip string) (model.IPInfoLookup, error) {
	if !s.enabled {
		return model.IPInfoLookup{}, ErrIPInfoDisabled
	}

	if !isGloballyRoutable(ip) {
		return model.IPInfoLookup{}, ErrIPInfoNotPublic
	}

	if v, ok := s.cache.get(ip); ok {
		v.Source = "cache"
		return v, nil
	}

	if s.cache.isNegativelyCached(ip) {
		return model.IPInfoLookup{}, ErrIPInfoProvider
	}

	if !s.limiter.allow() {
		return model.IPInfoLookup{}, ErrIPInfoRateLimited
	}

	result, err, ran := s.cache.singleflightDo(ip, func() (model.IPInfoLookup, error) {
		v, err := s.provider.Lookup(ctx, ip)
		if err != nil {
			s.cache.putNegative(ip)
			return model.IPInfoLookup{}, err
		}
		storedAt := s.cache.put(ip, v)
		v.CachedAt = storedAt
		return v, nil
	})

	if !ran {
		// This goroutine did not run fn itself — singleflightDo already
		// resolved the definitive outcome atomically (either this call's own
		// cache-hit check, or another leader's completed run observed after
		// waiting on its channel), so result/err can be trusted as-is without
		// a second, separately-locked cache re-check (that re-check used to
		// be exactly the TOCTOU gap that let a duplicate provider call slip
		// through — see singleflightDo's doc comment).
		if err != nil {
			return model.IPInfoLookup{}, err
		}
		result.Source = "cache"
		return result, nil
	}

	if err != nil {
		return model.IPInfoLookup{}, fmt.Errorf("%w: %v", ErrIPInfoProvider, err)
	}

	result.Source = "live"
	return result, nil
}
