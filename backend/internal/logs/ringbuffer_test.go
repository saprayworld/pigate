package logs

import (
	"testing"
	"time"

	"pigate/internal/model"
)

func recv(t *testing.T, ch <-chan LogEvent) (LogEvent, bool) {
	t.Helper()
	select {
	case ev := <-ch:
		return ev, true
	case <-time.After(time.Second):
		return LogEvent{}, false
	}
}

// A subscriber receives every Add as a "log" event carrying the entry.
func TestSubscribeReceivesAdds(t *testing.T) {
	rb := NewRingBuffer(10)
	ch, cancel := rb.Subscribe(8)
	defer cancel()

	rb.Add(model.FirewallLog{ID: "a", Action: "PASS"})
	rb.Add(model.FirewallLog{ID: "b", Action: "DROP"})

	ev, ok := recv(t, ch)
	if !ok || ev.Kind != "log" || ev.Entry.ID != "a" {
		t.Fatalf("first event = %+v ok=%v, want log/a", ev, ok)
	}
	ev, ok = recv(t, ch)
	if !ok || ev.Kind != "log" || ev.Entry.ID != "b" {
		t.Fatalf("second event = %+v ok=%v, want log/b", ev, ok)
	}
}

// Clear emits a "clear" event and empties the buffer.
func TestClearEmitsEvent(t *testing.T) {
	rb := NewRingBuffer(10)
	ch, cancel := rb.Subscribe(8)
	defer cancel()

	rb.Add(model.FirewallLog{ID: "a"})
	if _, ok := recv(t, ch); !ok {
		t.Fatal("expected the add event")
	}
	rb.Clear()
	ev, ok := recv(t, ch)
	if !ok || ev.Kind != "clear" {
		t.Fatalf("clear event = %+v ok=%v, want clear", ev, ok)
	}
	if got := rb.GetAll(); len(got) != 0 {
		t.Fatalf("buffer not empty after Clear: %d", len(got))
	}
}

// A slow subscriber (buffer of 1, never drained) must not block Add; events
// beyond its buffer are dropped, and Add still returns promptly.
func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	rb := NewRingBuffer(100)
	_, cancel := rb.Subscribe(1) // never read from this channel
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			rb.Add(model.FirewallLog{ID: "x"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Add blocked on a full slow-subscriber channel")
	}
	// The buffer itself still recorded everything (capacity 100).
	if got := rb.GetAll(); len(got) != 100 {
		t.Fatalf("buffer len = %d, want 100", len(got))
	}
}

// After cancel, the subscriber no longer receives events and is removed from
// the internal set (no leak).
func TestCancelStopsDelivery(t *testing.T) {
	rb := NewRingBuffer(10)
	ch, cancel := rb.Subscribe(8)

	cancel()
	cancel() // idempotent — must not panic

	rb.mu.RLock()
	n := len(rb.subs)
	rb.mu.RUnlock()
	if n != 0 {
		t.Fatalf("subscriber not removed after cancel: %d remain", n)
	}

	rb.Add(model.FirewallLog{ID: "a"})
	select {
	case ev := <-ch:
		t.Fatalf("received %+v after cancel, want nothing", ev)
	case <-time.After(100 * time.Millisecond):
		// expected: no delivery
	}
}

// LastMatchedByRule (docs/ref/todo/firewall-policy-rule-usage-stats-plan.md
// T-02) returns the most recent Time per distinct RuleID, ignoring entries
// with no RuleID, and Size reports the current entry count.
func TestLastMatchedByRule(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Add(model.FirewallLog{ID: "1", RuleID: "rule-a", Time: "2026-01-01T00:00:00Z"})
	rb.Add(model.FirewallLog{ID: "2", RuleID: "", Time: "2026-01-01T00:00:01Z"}) // no RuleID — must be ignored
	rb.Add(model.FirewallLog{ID: "3", RuleID: "rule-b", Time: "2026-01-01T00:00:02Z"})
	rb.Add(model.FirewallLog{ID: "4", RuleID: "rule-a", Time: "2026-01-01T00:00:03Z"}) // newer rule-a hit

	if got := rb.Size(); got != 4 {
		t.Fatalf("expected Size()=4, got %d", got)
	}

	got := rb.LastMatchedByRule()
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct rule ids, got %d: %+v", len(got), got)
	}
	if got["rule-a"] != "2026-01-01T00:00:03Z" {
		t.Errorf("expected rule-a's most recent (newest) time, got %q", got["rule-a"])
	}
	if got["rule-b"] != "2026-01-01T00:00:02Z" {
		t.Errorf("expected rule-b time %q, got %q", "2026-01-01T00:00:02Z", got["rule-b"])
	}
	if _, ok := got[""]; ok {
		t.Errorf("expected empty RuleID entries to be excluded")
	}
}

// LastMatchedByRule on an empty buffer must return an empty (non-nil) map,
// never panic — the "clear ring buffer" path exercised by
// PolicyStatsService.
func TestLastMatchedByRule_Empty(t *testing.T) {
	rb := NewRingBuffer(10)
	got := rb.LastMatchedByRule()
	if got == nil {
		t.Fatalf("expected non-nil empty map")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %+v", got)
	}
	if rb.Size() != 0 {
		t.Fatalf("expected Size()=0")
	}
}

// Usage on an empty buffer reports zero used, the configured capacity, empty
// oldest/newest, and zero evicted (docs/ref/todo/
// firewall-log-buffer-capacity-plan.md T-01, issue #134).
func TestUsage_Empty(t *testing.T) {
	rb := NewRingBuffer(5)
	used, capacity, oldest, newest, evicted := rb.Usage()
	if used != 0 {
		t.Errorf("used = %d, want 0", used)
	}
	if capacity != 5 {
		t.Errorf("capacity = %d, want 5", capacity)
	}
	if oldest != "" || newest != "" {
		t.Errorf("oldest=%q newest=%q, want both empty", oldest, newest)
	}
	if evicted != 0 {
		t.Errorf("evicted = %d, want 0", evicted)
	}
}

// Usage reports oldest as logs[0] (the least-recently-added, still-held
// entry) and newest as the most-recently-added entry, before the buffer has
// filled up (no eviction yet).
func TestUsage_BeforeFull(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Add(model.FirewallLog{ID: "1", Time: "2026-01-01T00:00:00Z"})
	rb.Add(model.FirewallLog{ID: "2", Time: "2026-01-01T00:00:01Z"})
	rb.Add(model.FirewallLog{ID: "3", Time: "2026-01-01T00:00:02Z"})

	used, capacity, oldest, newest, evicted := rb.Usage()
	if used != 3 {
		t.Errorf("used = %d, want 3", used)
	}
	if capacity != 5 {
		t.Errorf("capacity = %d, want 5", capacity)
	}
	if oldest != "2026-01-01T00:00:00Z" {
		t.Errorf("oldest = %q, want the first entry's time", oldest)
	}
	if newest != "2026-01-01T00:00:02Z" {
		t.Errorf("newest = %q, want the last entry's time", newest)
	}
	if evicted != 0 {
		t.Errorf("evicted = %d, want 0 (buffer not yet full)", evicted)
	}
}

// Once the buffer fills up and starts evicting, Usage.evicted counts every
// eviction and oldest/newest track the current head/tail after eviction.
func TestUsage_AfterEviction(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Add(model.FirewallLog{ID: "1", Time: "2026-01-01T00:00:00Z"})
	rb.Add(model.FirewallLog{ID: "2", Time: "2026-01-01T00:00:01Z"})
	rb.Add(model.FirewallLog{ID: "3", Time: "2026-01-01T00:00:02Z"})
	// Buffer is now full (3/3, no eviction yet).
	used, _, _, _, evicted := rb.Usage()
	if used != 3 || evicted != 0 {
		t.Fatalf("after filling: used=%d evicted=%d, want used=3 evicted=0", used, evicted)
	}

	rb.Add(model.FirewallLog{ID: "4", Time: "2026-01-01T00:00:03Z"}) // evicts entry "1"
	rb.Add(model.FirewallLog{ID: "5", Time: "2026-01-01T00:00:04Z"}) // evicts entry "2"

	used, capacity, oldest, newest, evicted := rb.Usage()
	if used != 3 {
		t.Errorf("used = %d, want 3 (still at capacity)", used)
	}
	if capacity != 3 {
		t.Errorf("capacity = %d, want 3", capacity)
	}
	if oldest != "2026-01-01T00:00:02Z" {
		t.Errorf("oldest = %q, want entry 3's time (1 and 2 evicted)", oldest)
	}
	if newest != "2026-01-01T00:00:04Z" {
		t.Errorf("newest = %q, want entry 5's time", newest)
	}
	if evicted != 2 {
		t.Errorf("evicted = %d, want 2", evicted)
	}
}

// Clear resets evicted back to 0 (docs/ref/todo/
// firewall-log-buffer-capacity-plan.md T-01/design decision 4).
func TestClearResetsEvicted(t *testing.T) {
	rb := NewRingBuffer(2)
	rb.Add(model.FirewallLog{ID: "1", Time: "2026-01-01T00:00:00Z"})
	rb.Add(model.FirewallLog{ID: "2", Time: "2026-01-01T00:00:01Z"})
	rb.Add(model.FirewallLog{ID: "3", Time: "2026-01-01T00:00:02Z"}) // evicts "1"

	if _, _, _, _, evicted := rb.Usage(); evicted != 1 {
		t.Fatalf("expected evicted=1 before Clear, got %d", evicted)
	}

	rb.Clear()

	used, capacity, oldest, newest, evicted := rb.Usage()
	if used != 0 {
		t.Errorf("used = %d, want 0 after Clear", used)
	}
	if capacity != 2 {
		t.Errorf("capacity = %d, want 2 (unchanged by Clear)", capacity)
	}
	if oldest != "" || newest != "" {
		t.Errorf("oldest=%q newest=%q, want both empty after Clear", oldest, newest)
	}
	if evicted != 0 {
		t.Errorf("evicted = %d, want 0 after Clear", evicted)
	}
}
