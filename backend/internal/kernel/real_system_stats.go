//go:build linux

package kernel

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"pigate/internal/model"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// RealSystemStats implements SystemStatsManager by reading Linux kernel virtual
// filesystems (/proc, /sys), statfs, and netlink — never by shelling out. This
// keeps the project's no-exec anti-command-injection guarantee intact and needs
// no extra privileges (all sources are world-readable / already-held caps).
//
// The procRoot/sysRoot/etcRoot fields are overridable so unit tests can point
// the parsers at fixture directories. In production they default to the real
// /proc, /sys, /etc.
type RealSystemStats struct {
	procRoot string
	sysRoot  string
	etcRoot  string

	// warnOnce suppresses per-tick log spam for genuinely-absent optional nodes
	// (thermal zone, cpufreq, device-tree): each key is logged at most once.
	warnMu   sync.Mutex
	warnOnce map[string]bool
}

// NewRealSystemStats returns a RealSystemStats rooted at the real /proc, /sys, /etc.
func NewRealSystemStats() *RealSystemStats {
	return &RealSystemStats{
		procRoot: "/proc",
		sysRoot:  "/sys",
		etcRoot:  "/etc",
		warnOnce: make(map[string]bool),
	}
}

// warnOnceKey logs msg the first time it is seen for key, then stays silent.
func (r *RealSystemStats) warnOnceKey(key, format string, args ...interface{}) {
	r.warnMu.Lock()
	defer r.warnMu.Unlock()
	if r.warnOnce == nil {
		r.warnOnce = make(map[string]bool)
	}
	if r.warnOnce[key] {
		return
	}
	r.warnOnce[key] = true
	log.Printf("[RealSystemStats] "+format, args...)
}

// GetCPUSnapshot parses the aggregate "cpu" line of /proc/stat into total and
// idle jiffies. Fields: user nice system idle iowait irq softirq steal ...
func (r *RealSystemStats) GetCPUSnapshot() (*model.CPUSnapshot, error) {
	f, err := os.Open(filepath.Join(r.procRoot, "stat"))
	if err != nil {
		return nil, fmt.Errorf("read /proc/stat: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:] // drop the "cpu" label
		var total, idle uint64
		for i, fv := range fields {
			v, perr := strconv.ParseUint(fv, 10, 64)
			if perr != nil {
				continue
			}
			total += v
			// index 3 = idle, index 4 = iowait (both count as not-busy)
			if i == 3 || i == 4 {
				idle += v
			}
		}
		return &model.CPUSnapshot{Idle: idle, Total: total}, nil
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan /proc/stat: %w", err)
	}
	return nil, fmt.Errorf("no aggregate cpu line in /proc/stat")
}

// GetCPUInfo reads core count + model name from /proc/cpuinfo and the current
// scaling frequency from /sys (optional). runtime.NumCPU is intentionally not
// used for the core count so a fixture-backed test is deterministic.
func (r *RealSystemStats) GetCPUInfo() (*model.CPUInfo, error) {
	info := &model.CPUInfo{}

	f, err := os.Open(filepath.Join(r.procRoot, "cpuinfo"))
	if err != nil {
		return nil, fmt.Errorf("read /proc/cpuinfo: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		key, val, ok := splitProcKV(line)
		if !ok {
			continue
		}
		switch key {
		case "processor":
			info.Cores++
		case "model name", "Model", "Hardware":
			// x86 uses "model name"; ARM/Pi often only has "Hardware"/"Model".
			// Keep the first non-empty, but let "model name" win if present.
			if info.ModelName == "" || key == "model name" {
				if val != "" {
					info.ModelName = val
				}
			}
		}
	}
	if info.Cores == 0 {
		info.Cores = 1
	}

	// Optional: current scaling frequency (kHz). Absent on WSL / most VMs.
	freqPath := filepath.Join(r.sysRoot, "devices/system/cpu/cpu0/cpufreq/scaling_cur_freq")
	if raw, ferr := os.ReadFile(freqPath); ferr == nil {
		if khz, perr := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64); perr == nil {
			info.FreqMHz = float64(khz) / 1000.0
			info.FreqAvailable = true
		}
	} else {
		r.warnOnceKey("cpufreq", "cpufreq not available (%s); freq reported as unavailable", freqPath)
	}

	return info, nil
}

// GetMemoryInfo derives used = MemTotal - MemAvailable from /proc/meminfo.
// Values in the file are in kB.
func (r *RealSystemStats) GetMemoryInfo() (*model.MemoryInfo, error) {
	f, err := os.Open(filepath.Join(r.procRoot, "meminfo"))
	if err != nil {
		return nil, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	defer f.Close()

	var totalKB, availKB uint64
	haveTotal, haveAvail := false, false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := splitProcKV(sc.Text())
		if !ok {
			continue
		}
		// val looks like "8123456 kB"
		numStr := strings.TrimSpace(strings.TrimSuffix(val, "kB"))
		n, perr := strconv.ParseUint(strings.TrimSpace(numStr), 10, 64)
		if perr != nil {
			continue
		}
		switch key {
		case "MemTotal":
			totalKB, haveTotal = n, true
		case "MemAvailable":
			availKB, haveAvail = n, true
		}
	}
	if !haveTotal {
		return nil, fmt.Errorf("MemTotal missing from /proc/meminfo")
	}
	// Older kernels (<3.14) lack MemAvailable; fall back to 0 available so we at
	// least report total rather than erroring the whole response.
	_ = haveAvail

	total := totalKB * 1024
	avail := availKB * 1024
	if avail > total {
		avail = total
	}
	used := total - avail
	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100.0
	}
	return &model.MemoryInfo{UsedBytes: used, TotalBytes: total, Percent: round1(pct)}, nil
}

// GetTemperature reads the SoC temperature from /sys/class/thermal/thermal_zone0
// (millidegrees C). Absent on WSL / x86 → Available=false, no error.
func (r *RealSystemStats) GetTemperature() (*model.TemperatureInfo, error) {
	const throttle = 80.0 // Pi 5 soft-throttle threshold
	path := filepath.Join(r.sysRoot, "class/thermal/thermal_zone0/temp")
	raw, err := os.ReadFile(path)
	if err != nil {
		r.warnOnceKey("thermal", "thermal zone not available (%s); temperature reported as unavailable", path)
		return &model.TemperatureInfo{Available: false, ThrottleCelsius: throttle}, nil
	}
	milli, perr := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if perr != nil {
		return &model.TemperatureInfo{Available: false, ThrottleCelsius: throttle}, nil
	}
	return &model.TemperatureInfo{
		Celsius:         round1(milli / 1000.0),
		ThrottleCelsius: throttle,
		Available:       true,
	}, nil
}

// GetDiskUsage returns usage for the filesystem containing path via statfs.
func (r *RealSystemStats) GetDiskUsage(path string) (*model.DiskUsage, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return nil, fmt.Errorf("statfs %q: %w", path, err)
	}
	bsize := uint64(st.Bsize)
	total := st.Blocks * bsize
	// Use Bavail (free to unprivileged users) so "used" matches what df shows to
	// a normal user; free-to-root reserved blocks count as used.
	free := st.Bavail * bsize
	if free > total {
		free = total
	}
	used := total - free
	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100.0
	}
	return &model.DiskUsage{Path: path, UsedBytes: used, TotalBytes: total, Percent: round1(pct)}, nil
}

// GetHostInfo composes OS name (/etc/os-release PRETTY_NAME), board model
// (/proc/device-tree/model, Pi only), kernel version (/proc/sys/kernel/osrelease),
// and uptime (/proc/uptime). Optional pieces are left empty when unreadable.
func (r *RealSystemStats) GetHostInfo() (*model.HostInfo, error) {
	info := &model.HostInfo{}

	// Uptime (first field of /proc/uptime, seconds as float).
	if raw, err := os.ReadFile(filepath.Join(r.procRoot, "uptime")); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) > 0 {
			if secs, perr := strconv.ParseFloat(fields[0], 64); perr == nil {
				info.UptimeSeconds = int64(secs)
			}
		}
	}

	// OS PRETTY_NAME from /etc/os-release.
	if name := r.readOSPrettyName(); name != "" {
		info.OSName = name
	}

	// Kernel version.
	if raw, err := os.ReadFile(filepath.Join(r.procRoot, "sys/kernel/osrelease")); err == nil {
		info.KernelVersion = strings.TrimSpace(string(raw))
	}

	// Board model — Pi / device-tree platforms only. Trim the trailing NUL that
	// device-tree strings carry.
	dtPath := filepath.Join(r.procRoot, "device-tree/model")
	if raw, err := os.ReadFile(dtPath); err == nil {
		info.BoardModel = strings.TrimRight(strings.TrimSpace(string(raw)), "\x00")
	} else {
		r.warnOnceKey("boardmodel", "board model not available (%s); omitted", dtPath)
	}

	return info, nil
}

func (r *RealSystemStats) readOSPrettyName() string {
	f, err := os.Open(filepath.Join(r.etcRoot, "os-release"))
	if err != nil {
		r.warnOnceKey("osrelease", "os-release not readable: %v", err)
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "PRETTY_NAME=") {
			continue
		}
		val := strings.TrimPrefix(line, "PRETTY_NAME=")
		return strings.Trim(val, `"'`)
	}
	return ""
}

// GetNetCounters reads cumulative rx/tx byte counters for every link via netlink.
func (r *RealSystemStats) GetNetCounters() (map[string]model.NetCounters, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("netlink LinkList: %w", err)
	}
	out := make(map[string]model.NetCounters, len(links))
	for _, l := range links {
		attrs := l.Attrs()
		if attrs == nil || attrs.Statistics == nil {
			continue
		}
		out[attrs.Name] = model.NetCounters{
			RxBytes: attrs.Statistics.RxBytes,
			TxBytes: attrs.Statistics.TxBytes,
		}
	}
	return out, nil
}

// splitProcKV splits a "key: value" or "key\tvalue" /proc line, trimming spaces.
func splitProcKV(line string) (key, val string, ok bool) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}
