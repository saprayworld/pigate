package logs

import (
	"sync"

	"pigate/internal/model"
)

type RingBuffer struct {
	mu       sync.RWMutex
	logs     []model.FirewallLog
	capacity int
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		logs:     make([]model.FirewallLog, 0, capacity),
		capacity: capacity,
	}
}

// Add appends a log entry to the buffer, evicting the oldest if capacity is reached
func (r *RingBuffer) Add(log model.FirewallLog) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If capacity is reached, slice out the oldest
	if len(r.logs) >= r.capacity {
		r.logs = append(r.logs[1:], log)
	} else {
		r.logs = append(r.logs, log)
	}
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

// Clear flushes the circular buffer
func (r *RingBuffer) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = r.logs[:0]
}
