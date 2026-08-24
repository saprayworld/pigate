// Package service: blocklist subscribe-URL fetcher (docs/ref/todo/
// dns-blocklist-import-plan.md §2.5, T-04) — SENSITIVE: this is the second
// outbound HTTP client in the pigate daemon (after service/ipinfo.go's
// ipinfoIOProvider) and the first one whose target host is chosen by the
// user (a stored subscribe URL), not a hardcoded constant. Every defensive
// choice below exists specifically to prevent that URL from being used to
// reach an internal/private address (SSRF) — see the plan section for the
// full table this file implements line-by-line. Do not weaken any of these
// checks to "simplify" a future change without re-reading §2.5 first.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"

	"pigate/internal/model"
)

// blocklistDialControl is the SSRF/DNS-rebinding guard (plan §2.5 "IP guard
// ที่ dialer") — net/http calls this from within net.Dialer.DialContext with
// the actual IP address it is about to open a TCP connection to, AFTER DNS
// resolution has already happened. This is deliberately the enforcement
// point instead of (or in addition to) validating the hostname before
// resolving it: checking only the hostname is vulnerable to DNS rebinding
// (the name can resolve to a public IP at validation time and a private one
// at connect time, or vice versa across a redirect chain), and
// http.Client's automatic redirect-following would otherwise let a
// malicious/compromised server redirect us to an internal address without
// this function ever seeing it. Because every dial (including every
// redirect hop, since net/http re-dials per hop through the same
// Transport/DialContext) passes through here, this single guard covers all
// of them automatically — do NOT remove it and do NOT replace it with a
// pre-resolve hostname check alone.
//
// isGloballyRoutable (service/ipinfo.go:57) is reused as-is — it already
// fail-closes on unparsed input and covers RFC1918, loopback, link-local,
// multicast, CGNAT, TEST-NET, the IPv6 documentation range, etc.
func blocklistDialControl(network, address string, _ syscall.RawConn) error {
	_ = network
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("blocklist: invalid dial address %q: %w", address, err)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		// address here is always the numeric IP the dialer is about to
		// connect to (DNS resolution already happened upstream of this
		// callback) — an unparseable host is unexpected for "tcp"/"tcp4"/
		// "tcp6" networks, but fail closed rather than let it through.
		return fmt.Errorf("blocklist: invalid dial ip %q: %w", host, err)
	}
	if !isGloballyRoutable(addr.String()) {
		return errors.New("blocklist: refusing to connect to non-public address")
	}
	return nil
}

// blocklistFetcher performs the outbound GET for a subscribe-URL blocklist
// (plan §2.5/T-04). It owns a dedicated *http.Client — never
// http.DefaultClient, which has no Timeout and would let a hung/malicious
// server block a request indefinitely.
type blocklistFetcher struct {
	client *http.Client
}

// blocklistCheckRedirect enforces plan §2.5's redirect rules: no more than 3
// hops, and every hop's target URL is re-validated with
// model.ValidateDNSBlocklistURL (https-only, port 443-or-implicit, no
// userinfo, etc.) — the initial URL passing validation does not guarantee a
// redirect target does, since the redirect target is chosen by whatever
// server answered the first request.
func blocklistCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return fmt.Errorf("blocklist: too many redirects (max 3)")
	}
	if err := model.ValidateDNSBlocklistURL(req.URL.String()); err != nil {
		return fmt.Errorf("blocklist: redirect target rejected: %w", err)
	}
	return nil
}

// newBlocklistFetcher builds the production fetcher's http.Client per plan
// §2.5:
//   - Timeout: model.DNSBlocklistFetchTimeout bounds the whole
//     request/response round trip (belt-and-suspenders alongside the
//     caller's context deadline).
//   - Transport.Proxy = nil: never honor HTTP_PROXY/HTTPS_PROXY env vars for
//     this outbound request — an operator-set proxy must not be able to
//     silently redirect where blocklist fetches go.
//   - Transport.DisableCompression = true: refuses to auto-decompress a
//     gzip/deflate response, which would otherwise let a small compressed
//     payload inflate past DNSBlocklistMaxFileBytes after the LimitReader in
//     Fetch has already "passed" the wire-size check (gzip bomb defense).
//   - TLSHandshakeTimeout / ResponseHeaderTimeout: bound the two network
//     phases a bare Client.Timeout does not reliably interrupt once
//     in-flight on some code paths.
//   - DialContext uses a net.Dialer whose Control is blocklistDialControl —
//     see that function's doc comment for why this, not a pre-resolve
//     hostname check, is the real SSRF defense.
//   - CheckRedirect refuses to follow more than 3 hops and re-validates
//     scheme/host/port of every hop's target URL, so a redirect chain can
//     never smuggle the request onto http:// or a non-443 port even though
//     the initial URL was validated.
func newBlocklistFetcher() *blocklistFetcher {
	return newBlocklistFetcherWithControl(blocklistDialControl)
}

// newBlocklistFetcherWithControl builds a fetcher whose net.Dialer.Control
// is the given function — production code always calls newBlocklistFetcher
// (control == blocklistDialControl). This seam exists ONLY so tests can
// point the fetcher at an httptest.Server, whose listener is bound to a
// loopback address that blocklistDialControl would otherwise correctly
// refuse (plan T-04 item 5: "httptest ผ่าน constructor สำหรับ test ที่ไม่ตั้ง
// Control"). control == nil means "no dial-time IP check at all" and MUST
// NEVER be reachable from production wiring (cmd/pigate/main.go must always
// call newBlocklistFetcher(), never this function directly).
func newBlocklistFetcherWithControl(control func(network, address string, c syscall.RawConn) error) *blocklistFetcher {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: control,
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DisableCompression:    true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		DialContext:           dialer.DialContext,
	}
	return &blocklistFetcher{
		client: &http.Client{
			Timeout:       model.DNSBlocklistFetchTimeout,
			Transport:     transport,
			CheckRedirect: blocklistCheckRedirect,
		},
	}
}

// Fetch performs the actual GET for rawURL (plan §2.5/T-04 item 4):
// validate → GET with a fixed User-Agent (no cookies/auth headers are ever
// sent) → require HTTP 200 → read the body through io.LimitReader capped at
// model.DNSBlocklistMaxFileBytes+1, and if that many bytes were read, treat
// it as an error (an oversized source is rejected outright, never silently
// truncated and used — truncating would risk chopping a hosts file
// mid-line/mid-entry into something that parses "successfully" into
// garbage). Only the request's host and the response status code are ever
// logged — never the full URL (which could contain a query string/path an
// operator did not intend to have written to a shared log).
func (f *blocklistFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	if err := model.ValidateDNSBlocklistURL(rawURL); err != nil {
		return nil, fmt.Errorf("blocklist: invalid url: %w", err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("blocklist: invalid url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("blocklist: build request: %w", err)
	}
	req.Header.Set("User-Agent", "PiGate")

	resp, err := f.client.Do(req)
	if err != nil {
		log.Printf("blocklist: fetch %s failed: %v", parsed.Host, err)
		return nil, fmt.Errorf("blocklist: request failed: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("blocklist: fetch %s status=%d", parsed.Host, resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("blocklist: unexpected status %d", resp.StatusCode)
	}

	limit := int64(model.DNSBlocklistMaxFileBytes) + 1
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("blocklist: read body: %w", err)
	}
	if int64(len(body)) >= limit {
		return nil, fmt.Errorf("blocklist: response exceeds maximum of %d bytes", model.DNSBlocklistMaxFileBytes)
	}

	return body, nil
}
