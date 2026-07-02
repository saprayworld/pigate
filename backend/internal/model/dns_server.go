package model

type DNSZone struct {
	ID              string      `json:"id"`
	ZoneName        string      `json:"zoneName"`
	ForwardTo       string      `json:"forwardTo"`
	AllowedIPs      string      `json:"allowedIps"`
	IsAuthoritative bool        `json:"isAuthoritative"`
	Enabled         bool        `json:"enabled"`
	Records         []DNSRecord `json:"records"`
}

type DNSRecord struct {
	ID     string `json:"id"`
	ZoneID string `json:"zoneId"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Value  string `json:"value"`
	TTL    int    `json:"ttl"`
}

type DNSZoneInput struct {
	ZoneName        string `json:"zoneName"`
	ForwardTo       string `json:"forwardTo"`
	AllowedIPs      string `json:"allowedIps"`
	IsAuthoritative bool   `json:"isAuthoritative"`
	Enabled         bool   `json:"enabled"`
}

type DNSRecordInput struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   int    `json:"ttl"`
}

// DNSServerSettings holds which real LAN interfaces the DNS Server should bind
// (auth-server) to. Independent from DHCP Server configs.
type DNSServerSettings struct {
	Interfaces []string `json:"interfaces"`
}
