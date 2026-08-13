package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"pigate/internal/model"
)

// seedTrafficLogEntry pushes a synthetic FirewallLog entry directly into the
// server's ring buffer, bypassing the kernel/NFLOG layer entirely — these
// tests exercise HandleGetTrafficLogs' filter/cursor logic only.
func seedTrafficLogEntry(t *testing.T, server *Server, id string, when time.Time, chain, action, reason string) {
	t.Helper()
	server.logs.Add(model.FirewallLog{
		ID:     id,
		Time:   when.Format(time.RFC3339Nano),
		Action: action,
		Src:    "192.168.1.1",
		Dest:   "8.8.8.8",
		Port:   "443",
		Proto:  "TCP",
		Chain:  chain,
		Reason: reason,
	})
}

func getTrafficLogs(t *testing.T, handler http.Handler, query string) []model.FirewallLog {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/logs/traffic?"+query, nil)
	addSessionCookie(req, "mock_session_id_test_token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("query=%q: expected 200, got %d: %s", query, rec.Code, rec.Body.String())
	}
	var out []model.FirewallLog
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("query=%q: failed to unmarshal response %s: %v", query, rec.Body.String(), err)
	}
	return out
}

// TestHandleGetTrafficLogs_ChainFilter asserts chain=input / chain=local /
// no chain param behave as documented (plan §2.7/§4).
func TestHandleGetTrafficLogs_ChainFilter(t *testing.T) {
	server, _ := buildTestServer(t, false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")

	base := time.Now().Add(-1 * time.Hour)
	seedTrafficLogEntry(t, server, "fwd-1", base.Add(1*time.Second), model.PolicyChainForward, "PASS", "Allowed (forward)")
	seedTrafficLogEntry(t, server, "inp-1", base.Add(2*time.Second), model.PolicyChainInput, "PASS", "Allowed (local-in)")
	seedTrafficLogEntry(t, server, "out-1", base.Add(3*time.Second), model.PolicyChainOutput, "PASS", "Allowed (local-out)")

	all := getTrafficLogs(t, handler, "limit=100")
	if len(all) != 3 {
		t.Fatalf("no chain filter: expected 3 entries, got %d: %+v", len(all), all)
	}

	inputOnly := getTrafficLogs(t, handler, "chain=input&limit=100")
	if len(inputOnly) != 1 || inputOnly[0].ID != "inp-1" {
		t.Fatalf("chain=input: expected [inp-1], got %+v", inputOnly)
	}

	local := getTrafficLogs(t, handler, "chain=local&limit=100")
	if len(local) != 2 {
		t.Fatalf("chain=local: expected 2 entries (input+output), got %d: %+v", len(local), local)
	}
	for _, e := range local {
		if e.Chain != model.PolicyChainInput && e.Chain != model.PolicyChainOutput {
			t.Errorf("chain=local: unexpected chain %q in result", e.Chain)
		}
	}

	forwardOnly := getTrafficLogs(t, handler, "chain=forward&limit=100")
	if len(forwardOnly) != 1 || forwardOnly[0].ID != "fwd-1" {
		t.Fatalf("chain=forward: expected [fwd-1], got %+v", forwardOnly)
	}
}

// TestHandleGetTrafficLogs_ChainBogus asserts an unrecognized chain value is
// rejected with 400, not silently treated as "all chains".
func TestHandleGetTrafficLogs_ChainBogus(t *testing.T) {
	server, _ := buildTestServer(t, false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")

	req := httptest.NewRequest("GET", "/api/logs/traffic?chain=bogus", nil)
	addSessionCookie(req, "mock_session_id_test_token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("chain=bogus: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleGetTrafficLogs_CursorPagination walks a full ring buffer 2 rows
// at a time via beforeId/beforeTime cursors and asserts every row is visited
// exactly once, with the last page shorter than the requested limit.
func TestHandleGetTrafficLogs_CursorPagination(t *testing.T) {
	server, _ := buildTestServer(t, false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")

	const n = 7
	base := time.Now().Add(-1 * time.Hour)
	var ids []string
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("entry-%d", i)
		ids = append(ids, id)
		seedTrafficLogEntry(t, server, id, base.Add(time.Duration(i)*time.Second), model.PolicyChainForward, "PASS", "Allowed (forward)")
	}
	// ring buffer GetAll() is newest-first, so expected visiting order is
	// entry-6, entry-5, ..., entry-0.
	wantOrder := make([]string, n)
	for i := 0; i < n; i++ {
		wantOrder[i] = ids[n-1-i]
	}

	var gotOrder []string
	query := "limit=2"
	for {
		page := getTrafficLogs(t, handler, query)
		if len(page) == 0 {
			break
		}
		for _, e := range page {
			gotOrder = append(gotOrder, e.ID)
		}
		last := page[len(page)-1]
		query = fmt.Sprintf("limit=2&beforeId=%s&beforeTime=%s", url.QueryEscape(last.ID), url.QueryEscape(last.Time))
		if len(page) < 2 {
			break
		}
	}

	if len(gotOrder) != n {
		t.Fatalf("expected to visit %d entries, got %d: %+v", n, len(gotOrder), gotOrder)
	}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Errorf("position %d: got %q, want %q (full: got=%+v want=%+v)", i, gotOrder[i], wantOrder[i], gotOrder, wantOrder)
		}
	}
}

// TestHandleGetTrafficLogs_CursorFallbackOnEvictedID asserts that when
// beforeId is no longer present in the buffer (evicted) but beforeTime is
// present, the handler falls back to a time-based cut rather than erroring
// or restarting from the first page.
func TestHandleGetTrafficLogs_CursorFallbackOnEvictedID(t *testing.T) {
	server, _ := buildTestServer(t, false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")

	base := time.Now().Add(-1 * time.Hour)
	seedTrafficLogEntry(t, server, "old-1", base.Add(1*time.Second), model.PolicyChainForward, "PASS", "Allowed (forward)")
	seedTrafficLogEntry(t, server, "old-2", base.Add(2*time.Second), model.PolicyChainForward, "PASS", "Allowed (forward)")
	seedTrafficLogEntry(t, server, "new-1", base.Add(3*time.Second), model.PolicyChainForward, "PASS", "Allowed (forward)")

	cursorTime := base.Add(3 * time.Second).Format(time.RFC3339Nano)
	page := getTrafficLogs(t, handler, "limit=100&beforeId=evicted-does-not-exist&beforeTime="+url.QueryEscape(cursorTime))
	if len(page) != 2 {
		t.Fatalf("expected fallback to time cut to yield 2 older entries, got %d: %+v", len(page), page)
	}
	for _, e := range page {
		if e.ID == "new-1" {
			t.Errorf("time-fallback should exclude entries at/after beforeTime, but found %q", e.ID)
		}
	}

	// beforeId missing AND beforeTime missing/unparseable -> "no more data".
	empty := getTrafficLogs(t, handler, "limit=100&beforeId=evicted-does-not-exist")
	if len(empty) != 0 {
		t.Fatalf("expected empty array when cursor is entirely unresolvable, got %+v", empty)
	}
}

// TestHandleGetTrafficLogs_FilterBeforeCursor asserts that q/action filtering
// happens against the WHOLE buffer before the cursor/limit cut — a match
// deeper than the first page must still be returned.
func TestHandleGetTrafficLogs_FilterBeforeCursor(t *testing.T) {
	server, _ := buildTestServer(t, false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")

	base := time.Now().Add(-1 * time.Hour)
	// Seed a bunch of PASS noise, then one DROP entry buried deep (oldest),
	// then more PASS noise near the top.
	seedTrafficLogEntry(t, server, "deep-drop", base, model.PolicyChainForward, "DROP", "Blocked (forward)")
	for i := 1; i <= 5; i++ {
		seedTrafficLogEntry(t, server, fmt.Sprintf("noise-%d", i), base.Add(time.Duration(i)*time.Second), model.PolicyChainForward, "PASS", "Allowed (forward)")
	}

	// limit=2 would normally only return the 2 newest (all PASS/noise) if
	// filtering happened after the cut — assert action=DROP still finds the
	// buried entry regardless.
	page := getTrafficLogs(t, handler, "action=DROP&limit=2")
	if len(page) != 1 || page[0].ID != "deep-drop" {
		t.Fatalf("expected filter-before-cursor to surface the buried DROP entry, got %+v", page)
	}
}

// TestHandleGetTrafficLogs_QSearchesRuleNameAndID covers T-12 (docs/ref/todo/
// firewall-rule-matched-endpoints-plan.md): q= matches ruleName and ruleId
// (for the RuleStatsDrawer "ดู log ของกฎนี้" deep-link), while the pre-existing
// q= behavior (IP/port/reason/etc) still finds unrelated rows and never
// leaks a differently-named rule's rows into a rule-name search.
func TestHandleGetTrafficLogs_QSearchesRuleNameAndID(t *testing.T) {
	server, _ := buildTestServer(t, false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")

	now := time.Now()
	server.logs.Add(model.FirewallLog{
		ID: "by-name", Time: now.Add(-3 * time.Second).Format(time.RFC3339Nano),
		Action: "PASS", Src: "192.168.1.50", Dest: "8.8.8.8", Port: "443", Proto: "TCP",
		Chain: model.PolicyChainForward, Reason: "Allowed (forward)",
		RuleID: "rule-abc123", RuleName: "Allow LAN to WAN",
	})
	server.logs.Add(model.FirewallLog{
		ID: "other-rule", Time: now.Add(-2 * time.Second).Format(time.RFC3339Nano),
		Action: "PASS", Src: "192.168.1.51", Dest: "1.1.1.1", Port: "53", Proto: "UDP",
		Chain: model.PolicyChainForward, Reason: "Allowed (forward)",
		RuleID: "rule-def456", RuleName: "Allow DNS",
	})
	server.logs.Add(model.FirewallLog{
		ID: "no-rule", Time: now.Add(-1 * time.Second).Format(time.RFC3339Nano),
		Action: "DROP", Src: "203.0.113.9", Dest: "203.0.113.1", Port: "23", Proto: "TCP",
		Chain: model.PolicyChainForward, Reason: "Blocked (forward)",
	})

	// q=<rule name> returns only that rule's row.
	byName := getTrafficLogs(t, handler, "q="+url.QueryEscape("Allow LAN to WAN"))
	if len(byName) != 1 || byName[0].ID != "by-name" {
		t.Fatalf("expected q=<rule name> to return only by-name, got %+v", byName)
	}

	// q=<rule id> also works.
	byID := getTrafficLogs(t, handler, "q=rule-def456")
	if len(byID) != 1 || byID[0].ID != "other-rule" {
		t.Fatalf("expected q=<rule id> to return only other-rule, got %+v", byID)
	}

	// Pre-existing q= behavior (IP/port/reason) must be unchanged: matching
	// on an IP still finds its row regardless of rule name/id.
	byIP := getTrafficLogs(t, handler, "q=203.0.113.9")
	if len(byIP) != 1 || byIP[0].ID != "no-rule" {
		t.Fatalf("expected q=<ip> to still work, got %+v", byIP)
	}
	byPort := getTrafficLogs(t, handler, "q=53")
	if len(byPort) != 1 || byPort[0].ID != "other-rule" {
		t.Fatalf("expected q=<port> to still work, got %+v", byPort)
	}
	byReason := getTrafficLogs(t, handler, "q="+url.QueryEscape("Blocked (forward)"))
	if len(byReason) != 1 || byReason[0].ID != "no-rule" {
		t.Fatalf("expected q=<reason> to still work, got %+v", byReason)
	}
}

// TestHandleGetTrafficLogs_BackwardCompatible asserts a client that doesn't
// send chain/cursor params (the pre-pagination contract) still works.
func TestHandleGetTrafficLogs_BackwardCompatible(t *testing.T) {
	server, _ := buildTestServer(t, false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")

	base := time.Now().Add(-1 * time.Hour)
	seedTrafficLogEntry(t, server, "a", base.Add(1*time.Second), model.PolicyChainForward, "DROP", "Blocked (forward)")
	seedTrafficLogEntry(t, server, "b", base.Add(2*time.Second), model.PolicyChainForward, "PASS", "Allowed (forward)")

	page := getTrafficLogs(t, handler, "action=DROP&limit=200")
	if len(page) != 1 || page[0].ID != "a" {
		t.Fatalf("expected legacy action+limit query to still work, got %+v", page)
	}
}

// TestHandleGetTrafficLogUsage covers GET /api/logs/traffic/usage (docs/ref/
// todo/firewall-log-buffer-capacity-plan.md T-04/T-06, issue #134): auth
// required, empty-buffer defaults, and a populated buffer's used/capacity/
// oldest/newest/evicted numbers.
func TestHandleGetTrafficLogUsage(t *testing.T) {
	server, _ := buildTestServer(t, false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")

	// Requires auth.
	req := httptest.NewRequest("GET", "/api/logs/traffic/usage", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("without session: expected 401, got %d", rec.Code)
	}

	get := func() model.TrafficLogBufferUsage {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/logs/traffic/usage", nil)
		addSessionCookie(req, "mock_session_id_test_token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var out model.TrafficLogBufferUsage
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("failed to unmarshal response %s: %v", rec.Body.String(), err)
		}
		return out
	}

	// Empty buffer: used=0, capacity from buildTestServer's ring (50), oldest/
	// newest empty, evicted=0.
	usage := get()
	if usage.Used != 0 || usage.Capacity != 50 || usage.OldestEntry != "" || usage.NewestEntry != "" || usage.Evicted != 0 {
		t.Fatalf("empty buffer: unexpected usage %+v", usage)
	}
	if usage.UsedPercent != 0 {
		t.Fatalf("empty buffer: expected usedPercent=0, got %v", usage.UsedPercent)
	}

	base := time.Now().Add(-1 * time.Hour)
	seedTrafficLogEntry(t, server, "a", base.Add(1*time.Second), model.PolicyChainForward, "DROP", "Blocked (forward)")
	seedTrafficLogEntry(t, server, "b", base.Add(2*time.Second), model.PolicyChainInput, "PASS", "Allowed (local-in)")

	usage = get()
	if usage.Used != 2 {
		t.Fatalf("expected used=2 after seeding 2 entries, got %d", usage.Used)
	}
	if usage.Capacity != 50 {
		t.Fatalf("expected capacity=50, got %d", usage.Capacity)
	}
	if usage.OldestEntry != base.Add(1*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("expected oldestEntry to be the first-seeded entry's time, got %q", usage.OldestEntry)
	}
	if usage.NewestEntry != base.Add(2*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("expected newestEntry to be the last-seeded entry's time, got %q", usage.NewestEntry)
	}
	if usage.Evicted != 0 {
		t.Fatalf("expected evicted=0 (buffer nowhere near full), got %d", usage.Evicted)
	}

	// POST /api/dashboard/logs/clear must zero everything out.
	clearReq := httptest.NewRequest("POST", "/api/dashboard/logs/clear", nil)
	addSessionCookie(clearReq, "mock_session_id_test_token")
	clearRec := httptest.NewRecorder()
	handler.ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear: expected 200, got %d", clearRec.Code)
	}

	usage = get()
	if usage.Used != 0 || usage.OldestEntry != "" || usage.NewestEntry != "" || usage.Evicted != 0 {
		t.Fatalf("after clear: expected all-zero usage, got %+v", usage)
	}
}
