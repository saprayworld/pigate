package kernel

// dnsmasq query-log tailer: parser + constants (docs/ref/todo/
// statistics-dns-top-domain-plan.md T-04). Deliberately in a file with no
// build tag (unlike real_dns_query_log.go) so the pure parsing/sanitizing
// functions here can be unit-tested on any OS, including the CI/dev box this
// is written on (WSL/x86), not only linux/arm64.

import (
	"net/netip"
	"strings"
	"time"

	"pigate/internal/model"
)

const (
	// DNSQueryLogPath is where dnsmasq is configured to write its query log
	// (log-facility directive, see buildDNSConfig in dns_server.go). tmpfs
	// (/run) only — never written to the SD card (plan §0/Caution 16).
	DNSQueryLogPath = "/run/pigate/dnsmasq-queries.log"

	// maxQueryLogBytes is the size at which WatchDNSLog truncates the log file
	// back to empty, bounding tmpfs usage under sustained query load (plan §5
	// item 3).
	maxQueryLogBytes = 8 << 20 // 8 MiB

	// queryLogPollInterval is how often WatchDNSLog checks the file for new
	// data (no inotify dependency — plain poll, matching the plan's "tail
	// every 1s" design).
	queryLogPollInterval = 1 * time.Second

	// maxQueryLogReadPerTick bounds how many new bytes a single tick will
	// read/parse; if more has accumulated (poll fell behind under heavy
	// query load) the reader jumps to the tail of the file and drops the gap
	// rather than trying to catch up all at once (plan §5 item 3).
	maxQueryLogReadPerTick = 2 << 20 // 2 MiB
)

// parseDNSLogLine parses one line of dnsmasq's query log (the part after the
// "Mon DD HH:MM:SS dnsmasq[PID]: " syslog-style prefix has already been
// stripped by the caller) into a model.DNSLogEvent. ok is false when the line
// should be dropped entirely (not a query/answer line, or failed
// sanitization) — see plan §2 "กติกา parser" for the exact rules.
//
// This function and sanitizeDomain below are pure (no I/O), so they are
// unit-testable without touching the filesystem (plan T-04 acceptance).
func parseDNSLogLine(line string) (model.DNSLogEvent, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return model.DNSLogEvent{}, false
	}

	if strings.HasPrefix(line, "query[") {
		return parseQueryLine(line)
	}

	return parseAnswerLine(line)
}

// parseQueryLine handles: `query[A] www.example.com from 192.168.1.101`
func parseQueryLine(line string) (model.DNSLogEvent, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return model.DNSLogEvent{}, false
	}
	// fields[0] == "query[A]" (or query[AAAA], query[HTTPS], ...)
	qType := strings.TrimSuffix(strings.TrimPrefix(fields[0], "query["), "]")
	domain, ok := sanitizeDomain(fields[1])
	if !ok {
		return model.DNSLogEvent{}, false
	}
	// fields[2] must be "from"; fields[3] is the client IP. A malformed line
	// missing "from" is dropped rather than guessed at.
	if fields[2] != "from" {
		return model.DNSLogEvent{}, false
	}
	clientIP := ""
	if addr, err := netip.ParseAddr(fields[3]); err == nil {
		clientIP = addr.String()
	}
	return model.DNSLogEvent{
		Kind:      model.DNSLogQuery,
		Domain:    domain,
		QueryType: qType,
		ClientIP:  clientIP,
	}, true
}

// parseAnswerLine handles every "<verb> <domain> is <value>" shaped line
// (reply/cached/config/etc — plan §2: one branch covers all of them) where
// value parses as an IP address. Any other shape (forwarded, "is <CNAME>",
// "is NXDOMAIN", "is NODATA-IPv4/6", "is <REDACTED>", DHCP lines, garbage) is
// dropped.
func parseAnswerLine(line string) (model.DNSLogEvent, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[2] != "is" {
		return model.DNSLogEvent{}, false
	}
	addr, err := netip.ParseAddr(fields[3])
	if err != nil {
		return model.DNSLogEvent{}, false
	}
	domain, ok := sanitizeDomain(fields[1])
	if !ok {
		return model.DNSLogEvent{}, false
	}
	return model.DNSLogEvent{
		Kind:     model.DNSLogAnswer,
		Domain:   domain,
		AnswerIP: addr.String(),
	}, true
}

// sanitizeDomain validates a domain string coming from dnsmasq's query log —
// untrusted input on BOTH sides (plan §5 item 2): the queried name is
// attacker-controlled by any LAN client, and the answered/CNAME name is
// attacker-controlled by any DNS server the client's query happens to reach.
// It lower-cases, strips a single trailing dot, and REJECTS (does not strip)
// anything outside [a-z0-9.-_*] or longer than 253 bytes — never silently
// mutate a string that will be logged/stored/rendered.
func sanitizeDomain(s string) (string, bool) {
	s = strings.ToLower(s)
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > 253 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '-' || c == '_' || c == '*':
		default:
			return "", false
		}
	}
	return s, true
}
