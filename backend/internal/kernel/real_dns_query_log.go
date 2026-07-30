//go:build linux

package kernel

import (
	"context"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"pigate/internal/model"
)

// dnsQueryLogSyslogPrefixFields is the number of whitespace-separated fields
// dnsmasq's syslog-style prefix occupies before the actual message: "Mon DD
// HH:MM:SS dnsmasq[PID]:" — 5 fields (month, day, time, "dnsmasq[pid]:").
// Rather than parse the timestamp (unused) we just find the "dnsmasq[...]:"
// token and take everything after it, which is robust to any timestamp
// format dnsmasq happens to emit.
const dnsmasqLogTag = "dnsmasq["

// WatchDNSLog tails DNSQueryLogPath (a tmpfs file dnsmasq writes to when
// query logging is enabled — see ApplyZones/buildDNSConfig) and streams
// parsed query/answer events to cb. It never opens the file when the
// feature is off: a missing file is not an error, WatchDNSLog just waits
// quietly for the next poll tick (docs/ref/todo/
// statistics-dns-top-domain-plan.md T-05 acceptance — this matters on WSL/
// dev boxes with no dnsmasq at all, plan §5 item 15).
func (m *RealDNSServerManager) WatchDNSLog(ctx context.Context, cb func(model.DNSLogEvent)) error {
	ticker := time.NewTicker(queryLogPollInterval)
	defer ticker.Stop()

	var offset int64
	var pending strings.Builder // a line fragment carried over from the previous tick

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			offset, pending = m.tailQueryLogOnce(offset, pending, cb)
		}
	}
}

// tailQueryLogOnce performs one poll tick: open, seek to offset, read up to
// maxQueryLogReadPerTick new bytes, split into complete lines, parse+dispatch
// each, and return the updated (offset, pending-fragment) for the next tick.
// A trailing line with no newline yet (dnsmasq wrote a partial line between
// our reads) is kept in `pending` and prefixed onto the next tick's data
// rather than parsed as-is (plan §2 "บรรทัดที่ค้างครึ่ง ๆ เก็บต่อ tick หน้า") —
// bufio.Scanner alone can't express this because ScanLines treats an
// unterminated final read as a complete token at EOF, so line-splitting is
// done by hand here instead.
func (m *RealDNSServerManager) tailQueryLogOnce(offset int64, pending strings.Builder, cb func(model.DNSLogEvent)) (int64, strings.Builder) {
	f, err := os.Open(DNSQueryLogPath)
	if err != nil {
		// File not present (feature off, or dnsmasq hasn't been (re)started
		// with query logging yet) — wait quietly, no per-tick log spam (plan
		// T-05 item 3: "ห้าม log ทุกวินาที").
		return 0, strings.Builder{}
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return offset, pending
	}
	size := info.Size()

	// dnsmasq (re)opened the file, or something truncated it out from under
	// us — restart from the top rather than seeking past EOF.
	if size < offset {
		offset = 0
		pending = strings.Builder{}
	}

	// Enforce the truncate ceiling. dnsmasq holds the fd open O_APPEND, so
	// (per the plan's documented assumption, unverified on real hardware —
	// plan §5 item 5) it keeps appending past the truncate point rather than
	// erroring; if that assumption ever proves false on a real device, the
	// worst case is dnsmasq's own write failing silently, not pigate crashing.
	if size > maxQueryLogBytes {
		if err := os.Truncate(DNSQueryLogPath, 0); err != nil {
			log.Printf("[DNS Server] Warning: failed to truncate query log %s: %v", DNSQueryLogPath, err)
		} else {
			return 0, strings.Builder{}
		}
	}

	readFrom := offset
	dropped := false
	if size-offset > maxQueryLogReadPerTick {
		// Fell behind under heavy query load — jump to the tail and drop the
		// gap rather than trying to catch up all at once (plan §5 item 3).
		readFrom = size - maxQueryLogReadPerTick
		dropped = true
		pending = strings.Builder{}
	}
	if size == readFrom {
		// Nothing new since last tick.
		return offset, pending
	}

	if _, err := f.Seek(readFrom, io.SeekStart); err != nil {
		return offset, pending
	}

	buf := make([]byte, size-readFrom)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return offset, pending
	}
	buf = buf[:n]

	chunk := pending.String() + string(buf)
	lines := strings.Split(chunk, "\n")
	// The last element is either "" (chunk ended in \n) or an incomplete
	// trailing line — carry it into the next tick either way.
	next := lines[len(lines)-1]
	complete := lines[:len(lines)-1]

	for i, line := range complete {
		if dropped && i == 0 {
			// The first line after a forced seek is very likely a fragment
			// of a longer line we jumped into the middle of — discard it.
			continue
		}
		m.dispatchQueryLogLine(line, cb)
	}

	var nextPending strings.Builder
	nextPending.WriteString(next)
	return readFrom + int64(n), nextPending
}

// dispatchQueryLogLine strips the syslog-style "Mon DD HH:MM:SS dnsmasq[pid]:
// " prefix (if present — defensive if it's ever absent) and hands the rest to
// parseDNSLogLine, invoking cb on success. Never logs the domain itself (plan
// §5 item 2).
func (m *RealDNSServerManager) dispatchQueryLogLine(line string, cb func(model.DNSLogEvent)) {
	if idx := strings.Index(line, dnsmasqLogTag); idx >= 0 {
		if colon := strings.IndexByte(line[idx:], ':'); colon >= 0 {
			line = strings.TrimSpace(line[idx+colon+1:])
		}
	}
	ev, ok := parseDNSLogLine(line)
	if !ok {
		return
	}
	cb(ev)
}
