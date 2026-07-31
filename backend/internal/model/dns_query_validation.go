package model

import "strings"

// NormalizeQueryDomain validates/normalizes a domain string coming from an
// HTTP client (the `domain` query param on GET /api/statistics/dns/domain —
// docs/ref/todo/dns-query-statistics-drilldown-plan.md T-03). It intentionally
// mirrors kernel.sanitizeDomain (backend/internal/kernel/dns_query_log.go)
// byte-for-byte so a normalized domain here always matches the key the
// service's (domain, client) ring stores it under — a mismatch would silently
// make drill-down "find nothing" even for domains that really were queried
// (plan §5 item 5). It lower-cases, strips a single trailing dot, and REJECTS
// (does not strip) anything outside [a-z0-9.-_*] or longer than 253 bytes —
// never silently mutate a string that will be logged/stored/rendered.
func NormalizeQueryDomain(s string) (string, bool) {
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
