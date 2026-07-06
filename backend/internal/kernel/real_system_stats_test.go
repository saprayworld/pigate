//go:build linux

package kernel

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFixture writes content to root/rel, creating parent dirs.
func writeFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// newFixtureStats builds a RealSystemStats rooted entirely inside dir, with
// proc/sys/etc subdirectories the caller can populate.
func newFixtureStats(dir string) *RealSystemStats {
	return &RealSystemStats{
		procRoot: filepath.Join(dir, "proc"),
		sysRoot:  filepath.Join(dir, "sys"),
		etcRoot:  filepath.Join(dir, "etc"),
		warnOnce: make(map[string]bool),
	}
}

func TestGetCPUSnapshot(t *testing.T) {
	dir := t.TempDir()
	// user nice system idle iowait irq softirq steal ...
	writeFixture(t, dir, "proc/stat", "cpu  100 20 30 800 40 0 10 0 0 0\ncpu0 50 10 15 400 20 0 5 0 0 0\nintr 12345\n")
	s := newFixtureStats(dir)

	snap, err := s.GetCPUSnapshot()
	if err != nil {
		t.Fatalf("GetCPUSnapshot: %v", err)
	}
	// total = 100+20+30+800+40+0+10+0+0+0 = 1000; idle = idle(800)+iowait(40) = 840
	if snap.Total != 1000 {
		t.Errorf("Total = %d, want 1000", snap.Total)
	}
	if snap.Idle != 840 {
		t.Errorf("Idle = %d, want 840", snap.Idle)
	}
}

func TestGetCPUInfo(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "proc/cpuinfo",
		"processor\t: 0\nmodel name\t: Test CPU X\n\nprocessor\t: 1\nmodel name\t: Test CPU X\n")
	s := newFixtureStats(dir)

	info, err := s.GetCPUInfo()
	if err != nil {
		t.Fatalf("GetCPUInfo: %v", err)
	}
	if info.Cores != 2 {
		t.Errorf("Cores = %d, want 2", info.Cores)
	}
	if info.ModelName != "Test CPU X" {
		t.Errorf("ModelName = %q, want %q", info.ModelName, "Test CPU X")
	}
	// No cpufreq fixture written → must degrade, not error.
	if info.FreqAvailable {
		t.Errorf("FreqAvailable = true, want false (no cpufreq fixture)")
	}
}

func TestGetCPUInfoWithFreq(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "proc/cpuinfo", "processor\t: 0\nmodel name\t: Test CPU\n")
	writeFixture(t, dir, "sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq", "2400000\n")
	s := newFixtureStats(dir)

	info, err := s.GetCPUInfo()
	if err != nil {
		t.Fatalf("GetCPUInfo: %v", err)
	}
	if !info.FreqAvailable {
		t.Fatalf("FreqAvailable = false, want true")
	}
	if info.FreqMHz != 2400 {
		t.Errorf("FreqMHz = %v, want 2400", info.FreqMHz)
	}
}

func TestGetMemoryInfo(t *testing.T) {
	dir := t.TempDir()
	// 8 GiB total, 2 GiB available → 6 GiB used, 75%.
	writeFixture(t, dir, "proc/meminfo",
		"MemTotal:        8388608 kB\nMemFree:          500000 kB\nMemAvailable:    2097152 kB\nBuffers:           10000 kB\n")
	s := newFixtureStats(dir)

	mem, err := s.GetMemoryInfo()
	if err != nil {
		t.Fatalf("GetMemoryInfo: %v", err)
	}
	wantTotal := uint64(8388608) * 1024
	if mem.TotalBytes != wantTotal {
		t.Errorf("TotalBytes = %d, want %d", mem.TotalBytes, wantTotal)
	}
	wantUsed := uint64(8388608-2097152) * 1024
	if mem.UsedBytes != wantUsed {
		t.Errorf("UsedBytes = %d, want %d", mem.UsedBytes, wantUsed)
	}
	if mem.Percent != 75.0 {
		t.Errorf("Percent = %v, want 75.0", mem.Percent)
	}
}

func TestGetTemperatureAvailable(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "sys/class/thermal/thermal_zone0/temp", "58123\n")
	s := newFixtureStats(dir)

	temp, err := s.GetTemperature()
	if err != nil {
		t.Fatalf("GetTemperature: %v", err)
	}
	if !temp.Available {
		t.Fatalf("Available = false, want true")
	}
	if temp.Celsius != 58.1 {
		t.Errorf("Celsius = %v, want 58.1", temp.Celsius)
	}
}

func TestGetTemperatureUnavailable(t *testing.T) {
	dir := t.TempDir()
	s := newFixtureStats(dir) // no thermal fixture
	temp, err := s.GetTemperature()
	if err != nil {
		t.Fatalf("GetTemperature should not error when absent: %v", err)
	}
	if temp.Available {
		t.Errorf("Available = true, want false")
	}
	if temp.ThrottleCelsius != 80 {
		t.Errorf("ThrottleCelsius = %v, want 80", temp.ThrottleCelsius)
	}
}

func TestGetHostInfo(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "proc/uptime", "273153.45 1000000.00\n")
	writeFixture(t, dir, "etc/os-release",
		"PRETTY_NAME=\"Raspberry Pi OS (64-bit)\"\nNAME=\"Debian\"\n")
	writeFixture(t, dir, "proc/sys/kernel/osrelease", "6.6.31-v8+\n")
	writeFixture(t, dir, "proc/device-tree/model", "Raspberry Pi 5 Model B Rev 1.0\x00")
	s := newFixtureStats(dir)

	info, err := s.GetHostInfo()
	if err != nil {
		t.Fatalf("GetHostInfo: %v", err)
	}
	if info.UptimeSeconds != 273153 {
		t.Errorf("UptimeSeconds = %d, want 273153", info.UptimeSeconds)
	}
	if info.OSName != "Raspberry Pi OS (64-bit)" {
		t.Errorf("OSName = %q", info.OSName)
	}
	if info.KernelVersion != "6.6.31-v8+" {
		t.Errorf("KernelVersion = %q", info.KernelVersion)
	}
	if info.BoardModel != "Raspberry Pi 5 Model B Rev 1.0" {
		t.Errorf("BoardModel = %q (NUL not trimmed?)", info.BoardModel)
	}
}

func TestGetHostInfoDegraded(t *testing.T) {
	dir := t.TempDir()
	// Only uptime present — board/os/kernel absent (WSL-like).
	writeFixture(t, dir, "proc/uptime", "42.0 0.0\n")
	s := newFixtureStats(dir)

	info, err := s.GetHostInfo()
	if err != nil {
		t.Fatalf("GetHostInfo should not error when optional files absent: %v", err)
	}
	if info.UptimeSeconds != 42 {
		t.Errorf("UptimeSeconds = %d, want 42", info.UptimeSeconds)
	}
	if info.BoardModel != "" {
		t.Errorf("BoardModel = %q, want empty", info.BoardModel)
	}
}

func TestGetDiskUsageRealRoot(t *testing.T) {
	// statfs cannot be faked via fixture dirs; just exercise it against "/" and
	// assert internal consistency.
	s := NewRealSystemStats()
	du, err := s.GetDiskUsage("/")
	if err != nil {
		t.Fatalf("GetDiskUsage(/): %v", err)
	}
	if du.TotalBytes == 0 {
		t.Fatalf("TotalBytes = 0, want > 0")
	}
	if du.UsedBytes > du.TotalBytes {
		t.Errorf("UsedBytes %d > TotalBytes %d", du.UsedBytes, du.TotalBytes)
	}
	if du.Percent < 0 || du.Percent > 100 {
		t.Errorf("Percent = %v, out of range", du.Percent)
	}
}
