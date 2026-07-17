package service

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// Sampling cadence and traffic-history sizing. CPU usage is a delta figure so it
// must be sampled by a background goroutine (two snapshots), never inside a
// request handler. Traffic history lives in a RAM ring buffer only (never
// SQLite) to preserve SD-card write cycles — it is accepted that it resets on
// reboot, exactly like the firewall log ring buffer.
const (
	cpuSampleInterval   = 3 * time.Second
	trafficPollInterval = 10 * time.Second
	trafficBucketSpan   = 5 * time.Minute
	trafficBucketMax    = 288 // 288 × 5min = 24h
	diskUsagePath       = "/"
	// metricsPushInterval is how often the SSE broadcaster composes and fans out a
	// full SystemMetrics snapshot. Aligned with cpuSampleInterval so a push never
	// carries a CPU figure older than one sampler tick.
	metricsPushInterval = 3 * time.Second
)

// SystemStatusService owns host-telemetry sampling for the dashboard. It runs
// two background goroutines (a CPU usage sampler and a WAN traffic collector),
// caches their results under RWMutexes, and composes the DTOs the API serves.
type SystemStatusService struct {
	stats    kernel.SystemStatsManager
	repo     *db.Repository
	hostname *HostnameService
	timeSvc  *TimeService
	version  string

	// CPU sampler cache.
	cpuMu       sync.RWMutex
	cpuUsage    float64
	lastCPUSnap *model.CPUSnapshot

	// Traffic collector state.
	trafMu       sync.RWMutex
	buckets      []model.TrafficBucket        // newest last, capped at trafficBucketMax
	lastCounters map[string]model.NetCounters // baseline for delta computation
	totalIn      uint64                       // cumulative WAN rx since boot
	totalOut     uint64                       // cumulative WAN tx since boot

	// Metrics SSE subscribers. The broadcaster goroutine fans a full snapshot to
	// each on a timer; a slow subscriber is dropped rather than stalling the loop.
	subMu       sync.Mutex
	metricsSubs map[*metricsSub]struct{}
}

type metricsSub struct {
	ch chan model.SystemMetrics
}

// NewSystemStatusService constructs the service. version is the build-time
// PiGate version string surfaced by /api/system/info.
func NewSystemStatusService(
	stats kernel.SystemStatsManager,
	repo *db.Repository,
	hostname *HostnameService,
	timeSvc *TimeService,
	version string,
) *SystemStatusService {
	return &SystemStatusService{
		stats:        stats,
		repo:         repo,
		hostname:     hostname,
		timeSvc:      timeSvc,
		version:      version,
		lastCounters: make(map[string]model.NetCounters),
		metricsSubs:  make(map[*metricsSub]struct{}),
	}
}

// Start primes the initial CPU/traffic baselines and launches the sampler and
// collector goroutines. Both stop when ctx is cancelled (shutdown). Call once
// from main.go, alongside the netlink monitor.
func (s *SystemStatusService) Start(ctx context.Context) {
	if snap, err := s.stats.GetCPUSnapshot(); err == nil {
		s.cpuMu.Lock()
		s.lastCPUSnap = snap
		s.cpuMu.Unlock()
	} else {
		log.Printf("[SystemStatus] Initial CPU snapshot failed: %v", err)
	}
	if counters, err := s.stats.GetNetCounters(); err == nil {
		s.trafMu.Lock()
		s.lastCounters = counters
		s.trafMu.Unlock()
	} else {
		log.Printf("[SystemStatus] Initial net counters read failed: %v", err)
	}

	go s.runCPUSampler(ctx)
	go s.runTrafficCollector(ctx)
	go s.runMetricsBroadcaster(ctx)
	log.Printf("[SystemStatus] Started CPU sampler (%s), traffic collector (%s), metrics broadcaster (%s)", cpuSampleInterval, trafficPollInterval, metricsPushInterval)
}

// SubscribeMetrics registers an SSE listener and returns its receive channel plus
// a cancel func that unregisters it (idempotent). buf is the channel buffer; the
// broadcaster never blocks on a full channel — the snapshot is dropped instead
// (the next tick carries a fresh full snapshot anyway), so a slow/stalled client
// can't stall the broadcaster loop.
func (s *SystemStatusService) SubscribeMetrics(buf int) (<-chan model.SystemMetrics, func()) {
	if buf < 1 {
		buf = 1
	}
	sub := &metricsSub{ch: make(chan model.SystemMetrics, buf)}

	s.subMu.Lock()
	s.metricsSubs[sub] = struct{}{}
	s.subMu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.subMu.Lock()
			delete(s.metricsSubs, sub)
			s.subMu.Unlock()
		})
	}
	return sub.ch, cancel
}

// runMetricsBroadcaster composes one SystemMetrics snapshot per tick and fans it
// out to every subscriber without blocking. Composing once per tick (rather than
// per connection) keeps /proc reads to a single pass regardless of connection
// count. Stops when ctx is cancelled (shutdown).
func (s *SystemStatusService) runMetricsBroadcaster(ctx context.Context) {
	t := time.NewTicker(metricsPushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !s.hasMetricsSubs() {
				continue // nobody listening — skip the compose entirely
			}
			s.broadcastMetrics(s.GetSystemMetrics())
		}
	}
}

func (s *SystemStatusService) hasMetricsSubs() bool {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	return len(s.metricsSubs) > 0
}

// broadcastMetrics fans a snapshot out to every subscriber without blocking. Split
// out from the goroutine so tests can drive it directly without the 3s ticker.
func (s *SystemStatusService) broadcastMetrics(snapshot model.SystemMetrics) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for sub := range s.metricsSubs {
		select {
		case sub.ch <- snapshot:
		default:
			// Subscriber's buffer full — drop; next tick carries a fresh snapshot.
		}
	}
}

func (s *SystemStatusService) runCPUSampler(ctx context.Context) {
	t := time.NewTicker(cpuSampleInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sampleCPU()
		}
	}
}

func (s *SystemStatusService) runTrafficCollector(ctx context.Context) {
	t := time.NewTicker(trafficPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.collectTraffic()
		}
	}
}

// sampleCPU takes a fresh /proc/stat snapshot and derives usage% from the delta
// against the previous one. A counter that goes backwards (should not happen for
// CPU jiffies, but guarded anyway) resets the baseline instead of producing a
// bogus figure.
func (s *SystemStatusService) sampleCPU() {
	snap, err := s.stats.GetCPUSnapshot()
	if err != nil {
		return
	}
	s.cpuMu.Lock()
	defer s.cpuMu.Unlock()

	if s.lastCPUSnap != nil && snap.Total >= s.lastCPUSnap.Total && snap.Idle >= s.lastCPUSnap.Idle {
		totalDelta := snap.Total - s.lastCPUSnap.Total
		idleDelta := snap.Idle - s.lastCPUSnap.Idle
		if totalDelta > 0 {
			usage := (1 - float64(idleDelta)/float64(totalDelta)) * 100
			s.cpuUsage = clampPercent(usage)
		}
	}
	s.lastCPUSnap = snap
}

// collectTraffic reads current per-interface counters, sums the positive delta
// over the WAN interfaces since the last poll into the current 5-minute bucket,
// and accumulates the since-boot totals. A negative delta (interface re-created
// or counter wrap) is clamped to 0 so the chart never dips or spikes.
func (s *SystemStatusService) collectTraffic() {
	counters, err := s.stats.GetNetCounters()
	if err != nil {
		return
	}
	wan := s.resolveWANIfaces()
	useAll := len(wan) == 0
	wanSet := make(map[string]bool, len(wan))
	for _, n := range wan {
		wanSet[n] = true
	}

	s.trafMu.Lock()
	defer s.trafMu.Unlock()

	var dIn, dOut uint64
	for name, cur := range counters {
		if useAll {
			if name == "lo" {
				continue
			}
		} else if !wanSet[name] {
			continue
		}
		if prev, ok := s.lastCounters[name]; ok {
			if cur.RxBytes >= prev.RxBytes {
				dIn += cur.RxBytes - prev.RxBytes
			}
			if cur.TxBytes >= prev.TxBytes {
				dOut += cur.TxBytes - prev.TxBytes
			}
		}
	}

	s.lastCounters = counters
	s.totalIn += dIn
	s.totalOut += dOut
	s.addToBucketLocked(time.Now(), dIn, dOut)
}

// addToBucketLocked accumulates a delta into the 5-minute bucket containing now,
// rolling into a new bucket (and evicting the oldest past 24h) when the boundary
// is crossed. now is a parameter so tests can drive rollover deterministically.
// Caller must hold trafMu.
func (s *SystemStatusService) addToBucketLocked(now time.Time, dIn, dOut uint64) {
	ts := now.Truncate(trafficBucketSpan).Format(time.RFC3339)
	if n := len(s.buckets); n > 0 && s.buckets[n-1].Ts == ts {
		s.buckets[n-1].RxBytes += dIn
		s.buckets[n-1].TxBytes += dOut
		return
	}
	s.buckets = append(s.buckets, model.TrafficBucket{Ts: ts, RxBytes: dIn, TxBytes: dOut})
	if len(s.buckets) > trafficBucketMax {
		s.buckets = s.buckets[len(s.buckets)-trafficBucketMax:]
	}
}

// resolveWANIfaces returns the names of interfaces whose DB role is WAN. When
// none are configured (or the DB read fails) it returns nil, signalling callers
// to fall back to "all non-loopback interfaces".
func (s *SystemStatusService) resolveWANIfaces() []string {
	ifaces, err := s.repo.GetInterfaces()
	if err != nil {
		return nil
	}
	var wan []string
	for _, i := range ifaces {
		if strings.EqualFold(i.Role, "WAN") {
			wan = append(wan, i.Name)
		}
	}
	return wan
}

// GetSystemMetrics composes the /api/dashboard/performance DTO. Each sub-read
// degrades to a zero/unavailable value rather than failing the whole response,
// so a missing thermal zone or cpufreq node on WSL still yields usable metrics.
func (s *SystemStatusService) GetSystemMetrics() model.SystemMetrics {
	usage := s.cpuUsagePercent()

	metrics := model.SystemMetrics{
		CPU:        usage,
		CPUDetail:  model.CPUDetail{UsagePercent: usage},
		TempDetail: model.TemperatureInfo{ThrottleCelsius: 80},
		Storage:    model.DiskUsage{Path: diskUsagePath},
	}

	if info, err := s.stats.GetCPUInfo(); err == nil && info != nil {
		metrics.CPUDetail.Cores = info.Cores
		metrics.CPUDetail.ModelName = info.ModelName
		metrics.CPUDetail.FreqMHz = info.FreqMHz
		metrics.CPUDetail.FreqAvailable = info.FreqAvailable
	}
	if mem, err := s.stats.GetMemoryInfo(); err == nil && mem != nil {
		metrics.Memory = mem.Percent
		metrics.MemDetail = *mem
	}
	if temp, err := s.stats.GetTemperature(); err == nil && temp != nil {
		metrics.TempDetail = *temp
		if temp.Available {
			metrics.Temp = temp.Celsius
		}
	}
	if disk, err := s.stats.GetDiskUsage(diskUsagePath); err == nil && disk != nil {
		metrics.Storage = *disk
	}
	return metrics
}

// GetSystemInfo composes the /api/system/info DTO. Hostname and time/timezone
// are sourced from the existing HostnameService / TimeService (never
// reimplemented); host identity comes from the kernel stats reader.
func (s *SystemStatusService) GetSystemInfo() model.SystemInfo {
	info := model.SystemInfo{Version: s.version}

	if host, err := s.stats.GetHostInfo(); err == nil && host != nil {
		info.OSName = host.OSName
		info.BoardModel = host.BoardModel
		info.KernelVersion = host.KernelVersion
		info.UptimeSeconds = host.UptimeSeconds
	}

	if hn, err := s.hostname.Get(); err == nil && hn != nil {
		info.Hostname = hn.Hostname
	}

	if t, err := s.timeSvc.Get(); err == nil && t != nil {
		info.Timezone = t.Timezone
		if t.Status != nil && t.Status.CurrentTime != "" {
			info.SystemTime = t.Status.CurrentTime
		}
	}
	if info.SystemTime == "" {
		info.SystemTime = time.Now().Format(time.RFC3339)
	}
	return info
}

// GetTrafficHistory returns a snapshot copy of the RAM traffic ring buffer.
func (s *SystemStatusService) GetTrafficHistory() model.TrafficHistory {
	wan := s.resolveWANIfaces()
	if wan == nil {
		wan = []string{}
	}

	s.trafMu.RLock()
	defer s.trafMu.RUnlock()

	buckets := make([]model.TrafficBucket, len(s.buckets))
	copy(buckets, s.buckets)
	return model.TrafficHistory{Interfaces: wan, Buckets: buckets}
}

// GetTrafficTotals returns cumulative WAN rx/tx bytes observed since boot.
func (s *SystemStatusService) GetTrafficTotals() (in, out uint64) {
	s.trafMu.RLock()
	defer s.trafMu.RUnlock()
	return s.totalIn, s.totalOut
}

func (s *SystemStatusService) cpuUsagePercent() float64 {
	s.cpuMu.RLock()
	defer s.cpuMu.RUnlock()
	return s.cpuUsage
}

// clampPercent constrains a value to [0,100] and rounds to one decimal.
func clampPercent(v float64) float64 {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return float64(int64(v*10+0.5)) / 10.0
}
