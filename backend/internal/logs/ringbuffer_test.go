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
