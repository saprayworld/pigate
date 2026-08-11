package logs

import (
	"sync"

	"pigate/internal/model"
)

// LogEvent is what a Subscribe() channel delivers. Kind distinguishes a new log
// entry (Entry is valid) from a buffer wipe (Entry is zero). SSE consumers turn
// these into their respective wire events.
type LogEvent struct {
	Kind  string            // "log" | "clear"
	Entry model.FirewallLog // valid only when Kind == "log"
}

type subscriber struct {
	ch chan LogEvent
}

type RingBuffer struct {
	mu       sync.RWMutex
	logs     []model.FirewallLog
	capacity int
	subs     map[*subscriber]struct{} // live SSE listeners; notified non-blocking
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		logs:     make([]model.FirewallLog, 0, capacity),
		capacity: capacity,
		subs:     make(map[*subscriber]struct{}),
	}
}

// Add appends a log entry to the buffer, evicting the oldest if capacity is
// reached, then fans it out to every live subscriber.
func (r *RingBuffer) Add(log model.FirewallLog) {
	r.mu.Lock()
	// If capacity is reached, slice out the oldest
	if len(r.logs) >= r.capacity {
		r.logs = append(r.logs[1:], log)
	} else {
		r.logs = append(r.logs, log)
	}
	r.notifyLocked(LogEvent{Kind: "log", Entry: log})
	r.mu.Unlock()
}

// GetAll returns a copy of all current logs in reverse order (newest first)
func (r *RingBuffer) GetAll() []model.FirewallLog {
	r.mu.RLock()
	defer r.mu.RUnlock()

	n := len(r.logs)
	copyLogs := make([]model.FirewallLog, n)
	for i := 0; i < n; i++ {
		copyLogs[i] = r.logs[n-1-i]
	}
	return copyLogs
}

// Size returns the current number of entries held (<= Capacity), so callers
// don't need to call GetAll (which copies the whole buffer) just to check how
// much data exists.
func (r *RingBuffer) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.logs)
}

// LastMatchedByRule scans the buffer once, newest entry first, and returns
// the most recent Time (as-stored string, RFC3339 or RFC3339Nano — see
// FirewallLog.Time) for every distinct non-empty RuleID seen. Unlike GetAll,
// it does not copy every entry — only allocates the resulting small map — so
// it is cheap enough to call from an HTTP handler (see
// docs/ref/todo/firewall-policy-rule-usage-stats-plan.md T-02). Because the
// scan is newest-first and only the first occurrence of each RuleID is kept,
// the returned times are already the most recent one.
func (r *RingBuffer) LastMatchedByRule() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]string)
	n := len(r.logs)
	for i := n - 1; i >= 0; i-- {
		entry := r.logs[i]
		if entry.RuleID == "" {
			continue
		}
		if _, ok := out[entry.RuleID]; ok {
			continue
		}
		out[entry.RuleID] = entry.Time
	}
	return out
}

// Capacity returns the buffer's fixed maximum size, so callers (e.g. the API
// layer capping a query's limit param) don't need to hardcode the number
// configured in main.go.
func (r *RingBuffer) Capacity() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capacity
}

// Clear flushes the circular buffer and tells every subscriber to reset.
func (r *RingBuffer) Clear() {
	r.mu.Lock()
	r.logs = r.logs[:0]
	r.notifyLocked(LogEvent{Kind: "clear"})
	r.mu.Unlock()
}

// Subscribe registers a listener and returns its receive channel plus a cancel
// func that unregisters it (idempotent). buf is the channel's buffer depth; the
// producer never blocks on a full channel — the event is dropped instead (a
// slow/stalled SSE client must not stall the NFLOG watcher loop, see plan
// Caution 3), and the client recovers full state from its next snapshot fetch.
func (r *RingBuffer) Subscribe(buf int) (ch <-chan LogEvent, cancel func()) {
	if buf < 1 {
		buf = 1
	}
	sub := &subscriber{ch: make(chan LogEvent, buf)}

	r.mu.Lock()
	r.subs[sub] = struct{}{}
	r.mu.Unlock()

	var once sync.Once
	cancel = func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.subs, sub)
			r.mu.Unlock()
		})
	}
	return sub.ch, cancel
}

// notifyLocked fans an event out to all subscribers without blocking. Must hold
// the write-lock (called from Add/Clear).
func (r *RingBuffer) notifyLocked(ev LogEvent) {
	for sub := range r.subs {
		select {
		case sub.ch <- ev:
		default:
			// Subscriber's buffer is full — drop rather than stall the producer.
		}
	}
}
