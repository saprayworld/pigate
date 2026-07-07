package service

import (
	"testing"
	"time"

	"pigate/internal/db"
	"pigate/internal/model"
)

// fakeStats is a programmable SystemStatsManager for driving the sampler and
// collector deterministically in tests.
type fakeStats struct {
	snaps    []*model.CPUSnapshot // consumed one per GetCPUSnapshot call
	snapIdx  int
	counters map[string]model.NetCounters
}

func (f *fakeStats) GetCPUSnapshot() (*model.CPUSnapshot, error) {
	if f.snapIdx >= len(f.snaps) {
		return f.snaps[len(f.snaps)-1], nil
	}
	s := f.snaps[f.snapIdx]
	f.snapIdx++
	return s, nil
}
func (f *fakeStats) GetCPUInfo() (*model.CPUInfo, error) {
	return &model.CPUInfo{Cores: 4, ModelName: "Fake"}, nil
}
func (f *fakeStats) GetMemoryInfo() (*model.MemoryInfo, error) {
	return &model.MemoryInfo{UsedBytes: 4 << 30, TotalBytes: 8 << 30, Percent: 50}, nil
}
func (f *fakeStats) GetTemperature() (*model.TemperatureInfo, error) {
	return &model.TemperatureInfo{Available: false, ThrottleCelsius: 80}, nil
}
func (f *fakeStats) GetDiskUsage(path string) (*model.DiskUsage, error) {
	return &model.DiskUsage{Path: path, UsedBytes: 40 << 30, TotalBytes: 128 << 30, Percent: 31.3}, nil
}
func (f *fakeStats) GetHostInfo() (*model.HostInfo, error) {
	return &model.HostInfo{OSName: "FakeOS", UptimeSeconds: 100}, nil
}
func (f *fakeStats) GetNetCounters() (map[string]model.NetCounters, error) {
	return f.counters, nil
}

func newTestService(t *testing.T, stats *fakeStats) *SystemStatusService {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })
	repo := db.NewRepository(sqliteDB)
	// No WAN interface configured → collector falls back to all non-loopback,
	// which is the path we want to exercise here.
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("clear interfaces: %v", err)
	}
	return NewSystemStatusService(stats, repo, nil, nil, "test-version")
}

func TestSampleCPUDelta(t *testing.T) {
	// Between the two snapshots: total advances 1000, idle advances 250 →
	// busy 750/1000 = 75%.
	stats := &fakeStats{snaps: []*model.CPUSnapshot{
		{Idle: 1000, Total: 4000},
		{Idle: 1250, Total: 5000},
	}}
	s := newTestService(t, stats)

	s.sampleCPU() // primes lastCPUSnap (first snapshot), no usage yet
	if got := s.cpuUsagePercent(); got != 0 {
		t.Errorf("usage after first sample = %v, want 0", got)
	}
	s.sampleCPU() // computes delta
	if got := s.cpuUsagePercent(); got != 75.0 {
		t.Errorf("usage after second sample = %v, want 75.0", got)
	}
}

func TestCollectTrafficDeltaAndTotals(t *testing.T) {
	stats := &fakeStats{counters: map[string]model.NetCounters{
		"eth0": {RxBytes: 1000, TxBytes: 500},
		"lo":   {RxBytes: 9999, TxBytes: 9999}, // must be ignored
	}}
	s := newTestService(t, stats)

	// Prime baseline at current counters → first collect yields zero delta.
	s.collectTraffic()
	if in, out := s.GetTrafficTotals(); in != 0 || out != 0 {
		t.Fatalf("totals after prime = (%d,%d), want (0,0)", in, out)
	}

	// Advance eth0 by rx+2000, tx+800; lo jumps too but is excluded.
	stats.counters = map[string]model.NetCounters{
		"eth0": {RxBytes: 3000, TxBytes: 1300},
		"lo":   {RxBytes: 50000, TxBytes: 50000},
	}
	s.collectTraffic()
	in, out := s.GetTrafficTotals()
	if in != 2000 || out != 800 {
		t.Errorf("totals = (%d,%d), want (2000,800)", in, out)
	}

	hist := s.GetTrafficHistory()
	if len(hist.Buckets) == 0 {
		t.Fatalf("expected at least one bucket")
	}
	var rx, tx uint64
	for _, b := range hist.Buckets {
		rx += b.RxBytes
		tx += b.TxBytes
	}
	if rx != 2000 || tx != 800 {
		t.Errorf("bucket sum = (%d,%d), want (2000,800)", rx, tx)
	}
}

func TestCollectTrafficCounterReset(t *testing.T) {
	stats := &fakeStats{counters: map[string]model.NetCounters{
		"eth0": {RxBytes: 100000, TxBytes: 50000},
	}}
	s := newTestService(t, stats)
	s.collectTraffic() // prime

	// Interface re-created: counters drop below the baseline → delta clamped to 0.
	stats.counters = map[string]model.NetCounters{
		"eth0": {RxBytes: 10, TxBytes: 5},
	}
	s.collectTraffic()
	if in, out := s.GetTrafficTotals(); in != 0 || out != 0 {
		t.Errorf("totals after reset = (%d,%d), want (0,0)", in, out)
	}
}

func TestBucketRolloverAndCap(t *testing.T) {
	s := NewSystemStatusService(&fakeStats{}, nil, nil, nil, "v")

	base := time.Date(2026, 7, 6, 9, 0, 0, 0, time.UTC)

	// Two deltas inside the same 5-min bucket accumulate into one entry.
	s.addToBucketLocked(base, 100, 10)
	s.addToBucketLocked(base.Add(2*time.Minute), 50, 5)
	if len(s.buckets) != 1 {
		t.Fatalf("same-bucket: len = %d, want 1", len(s.buckets))
	}
	if s.buckets[0].RxBytes != 150 || s.buckets[0].TxBytes != 15 {
		t.Errorf("same-bucket sum = (%d,%d), want (150,15)", s.buckets[0].RxBytes, s.buckets[0].TxBytes)
	}

	// Crossing the boundary starts a new bucket.
	s.addToBucketLocked(base.Add(6*time.Minute), 7, 3)
	if len(s.buckets) != 2 {
		t.Fatalf("rollover: len = %d, want 2", len(s.buckets))
	}

	// Fill well past the cap and confirm eviction keeps only the newest.
	for i := 0; i < trafficBucketMax+50; i++ {
		s.addToBucketLocked(base.Add(time.Duration(10+i*5)*time.Minute), uint64(i), 0)
	}
	if len(s.buckets) != trafficBucketMax {
		t.Errorf("cap: len = %d, want %d", len(s.buckets), trafficBucketMax)
	}
}
