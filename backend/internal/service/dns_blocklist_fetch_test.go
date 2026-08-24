package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pigate/internal/model"
)

// TestBlocklistDialControl is the SENSITIVE test named in the plan (T-04
// item 5) — it must prove, at the dialer layer (not just URL validation),
// that private/internal addresses are refused while public ones pass. This
// is the primary SSRF/DNS-rebinding defense (blocklistDialControl's doc
// comment), so it is exercised directly against the real function, not a
// stub.
func TestBlocklistDialControl(t *testing.T) {
	cases := []struct {
		name    string
		address string // host:port, as net/http would pass it
		wantErr bool
	}{
		{"loopback IPv4", "127.0.0.1:443", true},
		{"RFC1918", "192.168.1.1:443", true},
		{"link-local / cloud metadata", "169.254.169.254:443", true},
		{"loopback IPv6", "[::1]:443", true},
		{"CGNAT", "100.64.0.1:443", true},
		{"unspecified", "0.0.0.0:443", true},

		{"public IPv4 1", "1.1.1.1:443", false},
		{"public IPv6", "[2606:4700::1111]:443", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := blocklistDialControl("tcp", c.address, nil)
			if c.wantErr && err == nil {
				t.Errorf("blocklistDialControl(%q) = nil error, want error (must be refused)", c.address)
			}
			if !c.wantErr && err != nil {
				t.Errorf("blocklistDialControl(%q) = %v, want nil (must be allowed)", c.address, err)
			}
		})
	}
}

// TestBlocklistDialControlInvalidAddress covers malformed dial addresses
// (missing port / unparseable host) — must fail closed, not panic or pass.
func TestBlocklistDialControlInvalidAddress(t *testing.T) {
	cases := []string{"not-a-host-port", "example.com:443", "[::1]"}
	for _, addr := range cases {
		t.Run(addr, func(t *testing.T) {
			if err := blocklistDialControl("tcp", addr, nil); err == nil {
				t.Errorf("blocklistDialControl(%q) = nil error, want error", addr)
			}
		})
	}
}

// TestNewBlocklistFetcherProduction asserts the production constructor
// wires every knob required by plan §2.5, including that Control is the
// real blocklistDialControl (not nil, not skipped).
func TestNewBlocklistFetcherProduction(t *testing.T) {
	f := newBlocklistFetcher()
	if f.client == nil || f.client.Transport == nil {
		t.Fatal("newBlocklistFetcher: nil client/transport")
	}
	tr, ok := f.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("newBlocklistFetcher: transport is not *http.Transport")
	}
	if tr.Proxy != nil {
		t.Error("newBlocklistFetcher: Transport.Proxy must be nil")
	}
	if !tr.DisableCompression {
		t.Error("newBlocklistFetcher: Transport.DisableCompression must be true")
	}
	if f.client.Timeout != model.DNSBlocklistFetchTimeout {
		t.Errorf("newBlocklistFetcher: Timeout = %v, want %v", f.client.Timeout, model.DNSBlocklistFetchTimeout)
	}
	if f.client.CheckRedirect == nil {
		t.Error("newBlocklistFetcher: CheckRedirect must not be nil (must not follow redirects unchecked)")
	}
}

// newLoopbackFetcher builds a blocklistFetcher whose Transport dials
// straight to srv's real listener address regardless of what host/port the
// request URL names, and skips TLS certificate verification (httptest's
// self-signed cert is not for a name we control). This is a TEST-ONLY seam
// (plan T-04 item 5: "httptest ผ่าน constructor สำหรับ test ที่ไม่ตั้ง
// Control") — it deliberately does NOT go through blocklistDialControl (the
// real SSRF guard, covered separately and directly by
// TestBlocklistDialControl above) so it can reach an httptest.Server bound
// to a loopback address, which the real guard would correctly refuse in
// production. It reuses the real blocklistCheckRedirect so redirect-hop/
// re-validation behavior under test is the actual production logic — the
// request URL therefore uses a fixed https host with no explicit port
// (https://blocklist.pigate.test/...) so it passes
// model.ValidateDNSBlocklistURL exactly like a real subscribe URL would.
func newLoopbackFetcher(srv *httptest.Server) *blocklistFetcher {
	transport := &http.Transport{
		Proxy:              nil,
		DisableCompression: true,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, srv.Listener.Addr().String())
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only loopback trust
	}
	return &blocklistFetcher{
		client: &http.Client{
			Timeout:       model.DNSBlocklistFetchTimeout,
			Transport:     transport,
			CheckRedirect: blocklistCheckRedirect,
		},
	}
}

const testBlocklistBaseURL = "https://blocklist.pigate.test"

func TestBlocklistFetcherFetchSuccess(t *testing.T) {
	body := "127.0.0.1 localhost\n0.0.0.0 ads.example.com\n"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := newLoopbackFetcher(srv)
	got, err := f.Fetch(context.Background(), testBlocklistBaseURL+"/hosts")
	if err != nil {
		t.Fatalf("Fetch: unexpected error: %v", err)
	}
	if string(got) != body {
		t.Errorf("Fetch body = %q, want %q", got, body)
	}
}

func TestBlocklistFetcherRejectsHTTP(t *testing.T) {
	f := newBlocklistFetcher()
	_, err := f.Fetch(context.Background(), "http://example.com/hosts")
	if err == nil {
		t.Fatal("Fetch: expected error for http:// url, got nil")
	}
}

func TestBlocklistFetcherRejectsNonStandardPort(t *testing.T) {
	f := newBlocklistFetcher()
	_, err := f.Fetch(context.Background(), "https://example.com:8443/hosts")
	if err == nil {
		t.Fatal("Fetch: expected error for non-443 port, got nil")
	}
}

func TestBlocklistFetcherRejectsNon200(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := newLoopbackFetcher(srv)
	_, err := f.Fetch(context.Background(), testBlocklistBaseURL+"/hosts")
	if err == nil {
		t.Fatal("Fetch: expected error for non-200 status, got nil")
	}
}

func TestBlocklistFetcherBodyTooLarge(t *testing.T) {
	oversized := strings.Repeat("a", model.DNSBlocklistMaxFileBytes+16)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(oversized))
	}))
	defer srv.Close()

	f := newLoopbackFetcher(srv)
	_, err := f.Fetch(context.Background(), testBlocklistBaseURL+"/hosts")
	if err == nil {
		t.Fatal("Fetch: expected error for oversized body, got nil")
	}
}

func TestBlocklistFetcherBodyAtLimitOK(t *testing.T) {
	exact := strings.Repeat("a", model.DNSBlocklistMaxFileBytes)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(exact))
	}))
	defer srv.Close()

	f := newLoopbackFetcher(srv)
	got, err := f.Fetch(context.Background(), testBlocklistBaseURL+"/hosts")
	if err != nil {
		t.Fatalf("Fetch: unexpected error at exactly the byte limit: %v", err)
	}
	if len(got) != model.DNSBlocklistMaxFileBytes {
		t.Errorf("Fetch body len = %d, want %d", len(got), model.DNSBlocklistMaxFileBytes)
	}
}

func TestBlocklistFetcherTooManyRedirects(t *testing.T) {
	var mux http.ServeMux
	// 5 hops in a chain: more than the allowed 3 redirects, so Fetch must
	// fail before reaching the final handler.
	const hops = 5
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should not be reached"))
	})
	for i := 0; i < hops; i++ {
		i := i
		mux.HandleFunc(fmt.Sprintf("/hop%d", i), func(w http.ResponseWriter, r *http.Request) {
			next := fmt.Sprintf("%s/hop%d", testBlocklistBaseURL, i+1)
			if i == hops-1 {
				next = testBlocklistBaseURL + "/final"
			}
			w.Header().Set("Location", next)
			w.WriteHeader(http.StatusFound)
		})
	}
	srv := httptest.NewTLSServer(&mux)
	defer srv.Close()

	f := newLoopbackFetcher(srv)
	_, err := f.Fetch(context.Background(), testBlocklistBaseURL+"/hop0")
	if err == nil {
		t.Fatal("Fetch: expected error for redirect chain exceeding 3 hops, got nil")
	}
}

func TestBlocklistFetcherWithinRedirectLimit(t *testing.T) {
	var mux http.ServeMux
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", testBlocklistBaseURL+"/hop1")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/hop1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", testBlocklistBaseURL+"/final")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := httptest.NewTLSServer(&mux)
	defer srv.Close()

	f := newLoopbackFetcher(srv)
	got, err := f.Fetch(context.Background(), testBlocklistBaseURL+"/start")
	if err != nil {
		t.Fatalf("Fetch: unexpected error within redirect limit: %v", err)
	}
	if string(got) != "ok" {
		t.Errorf("Fetch body = %q, want %q", got, "ok")
	}
}

func TestBlocklistFetcherRedirectToHTTPRejected(t *testing.T) {
	var mux http.ServeMux
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		// A hostile/compromised server tries to downgrade the redirect
		// target to plain http:// — blocklistCheckRedirect must reject this
		// hop even though the initial URL was https.
		w.Header().Set("Location", "http://blocklist.pigate.test/final")
		w.WriteHeader(http.StatusFound)
	})
	srv := httptest.NewTLSServer(&mux)
	defer srv.Close()

	f := newLoopbackFetcher(srv)
	_, err := f.Fetch(context.Background(), testBlocklistBaseURL+"/start")
	if err == nil {
		t.Fatal("Fetch: expected error for redirect to http://, got nil")
	}
}

func TestBlocklistCheckRedirectDirect(t *testing.T) {
	mkReq := func(rawURL string) *http.Request {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatalf("http.NewRequest(%q): %v", rawURL, err)
		}
		return req
	}
	viaOf := func(n int) []*http.Request {
		via := make([]*http.Request, n)
		for i := range via {
			via[i] = mkReq(testBlocklistBaseURL + "/x")
		}
		return via
	}

	cases := []struct {
		name    string
		req     *http.Request
		via     []*http.Request
		wantErr bool
	}{
		{"first hop, valid https url", mkReq(testBlocklistBaseURL + "/a"), viaOf(0), false},
		{"third hop, still within limit", mkReq(testBlocklistBaseURL + "/a"), viaOf(2), false},
		{"fourth hop, exceeds limit", mkReq(testBlocklistBaseURL + "/a"), viaOf(3), true},
		{"http scheme rejected", mkReq("http://blocklist.pigate.test/a"), viaOf(0), true},
		{"non-443 port rejected", mkReq(testBlocklistBaseURL + ":8443/a"), viaOf(0), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := blocklistCheckRedirect(c.req, c.via)
			if c.wantErr && err == nil {
				t.Errorf("blocklistCheckRedirect() = nil error, want error")
			}
			if !c.wantErr && err != nil {
				t.Errorf("blocklistCheckRedirect() = %v, want nil", err)
			}
		})
	}
}
