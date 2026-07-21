package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pigate/internal/db"
	"pigate/internal/model"
)

func newEventLogTestService(t *testing.T) *EventLogService {
	t.Helper()
	sqliteDB, err := db.InitDB(filepath.Join(t.TempDir(), "eventlog.db"))
	if err != nil {
		t.Fatalf("failed to init memory db: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })
	return NewEventLogService(db.NewRepository(sqliteDB))
}

func logN(s *EventLogService, n int) {
	for i := 0; i < n; i++ {
		s.Log(model.EventCategorySystem, "test.event", model.EventSeverityInfo, "tester", "target", "test message")
	}
}

func waitForTotal(t *testing.T, s *EventLogService, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		total, err := s.repo.CountSystemEvents("", "", "")
		if err != nil {
			t.Fatalf("count failed: %v", err)
		}
		if total >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	total, _ := s.repo.CountSystemEvents("", "", "")
	t.Fatalf("expected at least %d persisted events, got %d", want, total)
}

func TestEventLogFlushOnBatchSize(t *testing.T) {
	s := newEventLogTestService(t)
	s.flushInterval = time.Hour // interval must not be the trigger here

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	logN(s, s.batchSize)
	waitForTotal(t, s, s.batchSize)
}

func TestEventLogFlushOnInterval(t *testing.T) {
	s := newEventLogTestService(t)
	s.flushInterval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	logN(s, 3) // below batch size — only the ticker can flush this
	waitForTotal(t, s, 3)
}

func TestEventLogSynchronousFlush(t *testing.T) {
	s := newEventLogTestService(t)

	logN(s, 2)
	if err := s.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	total, err := s.repo.CountSystemEvents("", "", "")
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 persisted events, got %d", total)
	}
}

func TestEventLogPrune(t *testing.T) {
	s := newEventLogTestService(t)
	s.maxRows = 5

	logN(s, 8)
	if err := s.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	total, err := s.repo.CountSystemEvents("", "", "")
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if total != s.maxRows {
		t.Fatalf("expected prune to keep %d rows, got %d", s.maxRows, total)
	}

	// The survivors must be the newest rows (highest ids).
	events, err := s.repo.GetSystemEvents("", "", "", 100, 0)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if events[len(events)-1].ID != 4 {
		t.Fatalf("expected oldest surviving id 4, got %d", events[len(events)-1].ID)
	}
}

func TestEventLogQueryFiltersAndPendingMerge(t *testing.T) {
	s := newEventLogTestService(t)

	s.Log(model.EventCategoryAuth, "login.failed", model.EventSeverityWarning, "alice", "alice", "Login failed for alice")
	s.Log(model.EventCategoryFirewall, "firewall.applied", model.EventSeverityInfo, "bob", "nftables", "Firewall policies applied")
	if err := s.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	// This one stays queued — Query must still surface it on the first page.
	s.Log(model.EventCategoryAuth, "login.success", model.EventSeverityInfo, "alice", "alice", "User alice logged in")

	events, total, err := s.Query(model.EventCategoryAuth, "", "", 50, 0)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2 auth events, got %d", total)
	}
	if len(events) != 2 || events[0].Action != "login.success" {
		t.Fatalf("expected queued login.success first, got %+v", events)
	}

	// Severity + text filters
	_, total, err = s.Query("", model.EventSeverityWarning, "alice", 50, 0)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 warning event for alice, got %d", total)
	}
}

func TestEventLogClearLeavesAuditTrail(t *testing.T) {
	s := newEventLogTestService(t)

	logN(s, 4)
	if err := s.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	if err := s.Clear("admin"); err != nil {
		t.Fatalf("clear failed: %v", err)
	}

	events, err := s.repo.GetSystemEvents("", "", "", 10, 0)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event after clear (the audit row), got %d", len(events))
	}
	if events[0].Action != "config.logs_cleared" || events[0].Actor != "admin" {
		t.Fatalf("expected config.logs_cleared by admin, got %+v", events[0])
	}
}
