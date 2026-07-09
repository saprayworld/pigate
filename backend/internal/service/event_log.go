package service

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"pigate/internal/db"
	"pigate/internal/model"
)

// EventLogService is the single entry point for the central audit/event log.
// Events are queued in RAM and flushed to the system_events table by one
// background goroutine, either when the queue reaches eventFlushBatchSize or
// every eventFlushInterval — whichever comes first. This keeps SD card writes
// bounded even under a login brute-force (see tech_stack_design.md §8).
//
// Callers that are about to take the process down (reboot/shutdown) must call
// Flush() synchronously after Log(), or the queued event dies with the process.
type EventLogService struct {
	repo *db.Repository

	mu      sync.Mutex
	pending []model.SystemEvent

	kick chan struct{} // signals the writer that the batch threshold was hit

	batchSize     int
	flushInterval time.Duration
	maxRows       int
}

const (
	eventFlushBatchSize = 10
	eventFlushInterval  = 10 * time.Second
	eventMaxRows        = 10000
)

func NewEventLogService(repo *db.Repository) *EventLogService {
	return &EventLogService{
		repo:          repo,
		kick:          make(chan struct{}, 1),
		batchSize:     eventFlushBatchSize,
		flushInterval: eventFlushInterval,
		maxRows:       eventMaxRows,
	}
}

// Log queues one event. Timestamps are always RFC3339 UTC; timezone conversion
// is the frontend's job. Safe to call from any goroutine; never blocks on I/O.
func (s *EventLogService) Log(category, action, severity, actor, target, message string) {
	if actor == "" {
		actor = model.EventActorSystem
	}
	ev := model.SystemEvent{
		Time:     time.Now().UTC().Format(time.RFC3339),
		Category: category,
		Action:   action,
		Severity: severity,
		Actor:    actor,
		Target:   target,
		Message:  message,
	}

	s.mu.Lock()
	s.pending = append(s.pending, ev)
	n := len(s.pending)
	s.mu.Unlock()

	if n >= s.batchSize {
		select {
		case s.kick <- struct{}{}:
		default: // writer already signalled
		}
	}
}

// Start launches the batch writer goroutine. It flushes on the batch-size
// signal, on the interval tick, and one final time when ctx is cancelled.
func (s *EventLogService) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.flushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				if err := s.Flush(); err != nil {
					log.Printf("[EventLog] Final flush on shutdown failed: %v", err)
				}
				return
			case <-s.kick:
			case <-ticker.C:
			}
			if err := s.Flush(); err != nil {
				log.Printf("[EventLog] Batch flush failed: %v", err)
			}
		}
	}()
}

// Flush synchronously writes all queued events in one transaction, then prunes
// the table back to maxRows. Power handlers call this before asking logind to
// reboot/poweroff so the event survives the process being killed.
func (s *EventLogService) Flush() error {
	s.mu.Lock()
	batch := s.pending
	s.pending = nil
	s.mu.Unlock()

	if len(batch) == 0 {
		return nil
	}
	if err := s.repo.InsertSystemEvents(batch); err != nil {
		// Put the batch back at the front so a transient DB error doesn't lose
		// events; they retry on the next flush.
		s.mu.Lock()
		s.pending = append(batch, s.pending...)
		s.mu.Unlock()
		return err
	}
	if err := s.repo.PruneSystemEvents(s.maxRows); err != nil {
		log.Printf("[EventLog] Prune failed: %v", err)
	}
	return nil
}

// Query returns events (newest first) plus the total matching count. Reads hit
// SQLite; on the first page, still-queued events matching the filter are merged
// in front so a just-logged event is visible before the next batch flush.
func (s *EventLogService) Query(category, severity, q string, limit, offset int) ([]model.SystemEvent, int, error) {
	events, err := s.repo.GetSystemEvents(category, severity, q, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountSystemEvents(category, severity, q)
	if err != nil {
		return nil, 0, err
	}

	s.mu.Lock()
	var queued []model.SystemEvent
	for i := len(s.pending) - 1; i >= 0; i-- { // newest first
		ev := s.pending[i]
		if matchesEventFilter(ev, category, severity, q) {
			queued = append(queued, ev)
		}
	}
	s.mu.Unlock()

	total += len(queued)
	if offset == 0 && len(queued) > 0 {
		merged := append(queued, events...)
		if len(merged) > limit {
			merged = merged[:limit]
		}
		events = merged
	}
	return events, total, nil
}

func matchesEventFilter(ev model.SystemEvent, category, severity, q string) bool {
	if category != "" && ev.Category != category {
		return false
	}
	if severity != "" && ev.Severity != severity {
		return false
	}
	if q != "" {
		needle := strings.ToLower(q)
		haystacks := []string{ev.Message, ev.Action, ev.Actor, ev.Target}
		found := false
		for _, h := range haystacks {
			if strings.Contains(strings.ToLower(h), needle) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Clear wipes the event table and immediately logs (and flushes) who did it —
// an empty audit trail must always start with the row that explains why.
func (s *EventLogService) Clear(actor string) error {
	// Drop anything still queued; it belongs to the pre-clear era.
	s.mu.Lock()
	s.pending = nil
	s.mu.Unlock()

	if err := s.repo.ClearSystemEvents(); err != nil {
		return err
	}
	s.Log(model.EventCategoryConfig, "config.logs_cleared", model.EventSeverityWarning,
		actor, "system_events", "Event log cleared by "+actor)
	return s.Flush()
}
