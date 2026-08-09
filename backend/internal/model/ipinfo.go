package model

import "time"

// IPInfoLookup is the result of a Public IP Info lookup (docs/ref/todo/
// statistics-host-ipinfo-plan.md T-01) — backs the Statistics -> Traffic ->
// Host page's "Public IP Info" card, which replaces "Top peers" when the
// drilled-in IP is public (plan §1). Every field except Ip is `omitempty`
// because the field set is provider-agnostic (plan §7 risk note): the
// current provider (ipinfo.io, no token) does not guarantee any of them will
// be present, and a future provider swap (e.g. MaxMind GeoLite2) may return
// an entirely different subset.
type IPInfoLookup struct {
	Ip          string `json:"ip"`
	Hostname    string `json:"hostname,omitempty"`
	City        string `json:"city,omitempty"`
	Region      string `json:"region,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryName string `json:"countryName,omitempty"`
	Org         string `json:"org,omitempty"`
	Asn         string `json:"asn,omitempty"`
	AsName      string `json:"asName,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	Loc         string `json:"loc,omitempty"`
	// Source is "cache" when served from the in-memory cache without an
	// outbound request this call, or "live" when a fresh provider lookup was
	// performed (plan T-05).
	Source string `json:"source,omitempty"`
	// CachedAt is when this entry was stored in the cache — set on every
	// successful lookup (both Source == "live", using the moment it was just
	// stored, and Source == "cache", using the original store time). Left at
	// the zero value only on an error path where no IPInfoLookup is ever
	// returned to the caller.
	CachedAt time.Time `json:"cachedAt,omitempty"`
}

// IPInfoCacheTTLDefault/IPInfoNegativeTTL/IPInfoCacheMaxEntries/
// IPInfoHTTPTimeout/IPInfoMaxResponseBytes are the tuning constants for the
// Public IP Info feature (plan T-01). All lookups/cache entries are RAM-only
// (plan Caution 5) — never persisted to SQLite.
const (
	// IPInfoCacheTTLDefault is how long a successful lookup is cached before
	// a fresh provider request is made for the same IP again.
	IPInfoCacheTTLDefault = 24 * time.Hour
	// IPInfoNegativeTTL is how long a FAILED lookup is cached, to avoid
	// hammering the provider for an IP that just errored/timed out.
	IPInfoNegativeTTL = 1 * time.Hour
	// IPInfoCacheMaxEntries bounds the cache's RAM footprint.
	IPInfoCacheMaxEntries = 1000
	// IPInfoHTTPTimeout bounds how long the outbound HTTP request to the
	// provider may take.
	IPInfoHTTPTimeout = 5 * time.Second
	// IPInfoMaxResponseBytes caps how much of the provider's HTTP response
	// body is read, regardless of what Content-Length claims.
	IPInfoMaxResponseBytes = 16 * 1024
)
