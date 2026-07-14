package model

import "testing"

func TestValidateDNSZone(t *testing.T) {
	tests := []struct {
		name    string
		zone    DNSZone
		wantErr bool
	}{
		{"valid authoritative", DNSZone{ZoneName: "internal.example.com", IsAuthoritative: true}, false},
		{"valid forward with ip", DNSZone{ZoneName: "corp.local", ForwardTo: "10.0.0.53"}, false},
		{"valid forward with ip#port", DNSZone{ZoneName: "corp.local", ForwardTo: "10.0.0.53#5353"}, false},
		{"empty name", DNSZone{ZoneName: "  ", IsAuthoritative: true}, true},
		{"newline in name", DNSZone{ZoneName: "evil\naddress=/x/1.2.3.4", IsAuthoritative: true}, true},
		{"space in name", DNSZone{ZoneName: "bad zone", IsAuthoritative: true}, true},
		{"underscore rejected", DNSZone{ZoneName: "bad_zone", IsAuthoritative: true}, true},
		{"newline in forwardTo", DNSZone{ZoneName: "corp.local", ForwardTo: "1.2.3.4\nserver=/x/6.6.6.6"}, true},
		{"forwardTo ignored when authoritative", DNSZone{ZoneName: "corp.local", IsAuthoritative: true, ForwardTo: "junk value"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDNSZone(tt.zone)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDNSZone(%+v) err = %v, wantErr %v", tt.zone, err, tt.wantErr)
			}
		})
	}
}

func TestValidateDNSRecord(t *testing.T) {
	tests := []struct {
		name    string
		rec     DNSRecord
		wantErr bool
	}{
		{"A valid", DNSRecord{Name: "www", Type: "A", Value: "192.168.1.10"}, false},
		{"A empty name", DNSRecord{Name: "", Type: "A", Value: "192.168.1.10"}, false},
		{"A apex name", DNSRecord{Name: "@", Type: "A", Value: "192.168.1.10"}, false},
		{"A with ipv6 value", DNSRecord{Name: "www", Type: "A", Value: "fe80::1"}, true},
		{"A not an ip", DNSRecord{Name: "www", Type: "A", Value: "notanip"}, true},
		{"A injection", DNSRecord{Name: "www", Type: "A", Value: "1.2.3.4\naddress=/evil/6.6.6.6"}, true},
		{"AAAA valid", DNSRecord{Name: "www", Type: "AAAA", Value: "2001:db8::1"}, false},
		{"AAAA with ipv4 value", DNSRecord{Name: "www", Type: "AAAA", Value: "1.2.3.4"}, true},
		{"CNAME valid short", DNSRecord{Name: "alias", Type: "CNAME", Value: "www"}, false},
		{"CNAME valid fqdn", DNSRecord{Name: "alias", Type: "CNAME", Value: "host.example.com."}, false},
		{"CNAME injection", DNSRecord{Name: "alias", Type: "CNAME", Value: "www\ncname=x,y"}, true},
		{"MX pref+target", DNSRecord{Name: "@", Type: "MX", Value: "10 mail.example.com"}, false},
		{"MX bare target", DNSRecord{Name: "@", Type: "MX", Value: "mail.example.com"}, false},
		{"MX bad pref", DNSRecord{Name: "@", Type: "MX", Value: "high mail.example.com"}, true},
		{"MX injection", DNSRecord{Name: "@", Type: "MX", Value: "10 mail\nserver=/x/6.6.6.6"}, true},
		{"TXT valid", DNSRecord{Name: "@", Type: "TXT", Value: "v=spf1 -all"}, false},
		{"TXT with quote", DNSRecord{Name: "@", Type: "TXT", Value: `abc"def`}, true},
		{"TXT injection", DNSRecord{Name: "@", Type: "TXT", Value: "abc\ntxt-record=x,y"}, true},
		{"PTR valid", DNSRecord{Name: "1", Type: "PTR", Value: "host.example.com"}, false},
		{"PTR injection", DNSRecord{Name: "1", Type: "PTR", Value: "host\nptr-record=x,y"}, true},
		{"unsupported type", DNSRecord{Name: "www", Type: "SRV", Value: "x"}, true},
		{"invalid name chars", DNSRecord{Name: "bad name", Type: "A", Value: "1.2.3.4"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDNSRecord(tt.rec)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDNSRecord(%+v) err = %v, wantErr %v", tt.rec, err, tt.wantErr)
			}
		})
	}
}

func TestValidateInterfaceName(t *testing.T) {
	tests := []struct {
		name    string
		iface   string
		wantErr bool
	}{
		{"simple", "eth0", false},
		{"vlan subiface", "eth0.301", false},
		{"wlan", "wlan1", false},
		{"underscore", "br_lan", false},
		{"max length 15", "abcdefghij12345", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"too long", "abcdefghij123456", true},
		{"space inside", "eth 0", true},
		{"newline injection", "eth0\ninterface=eth1", true},
		{"slash", "eth0/1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInterfaceName(tt.iface)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInterfaceName(%q) err = %v, wantErr %v", tt.iface, err, tt.wantErr)
			}
		})
	}
}

func TestValidateReservation(t *testing.T) {
	tests := []struct {
		name    string
		res     DhcpReservation
		wantErr bool
	}{
		{"valid", DhcpReservation{DeviceName: "laptop", MacAddress: "aa:bb:cc:dd:ee:ff", IPAddress: "192.168.1.50"}, false},
		{"valid name with space", DhcpReservation{DeviceName: "My Laptop", MacAddress: "aa:bb:cc:dd:ee:ff", IPAddress: "192.168.1.50"}, false},
		{"empty name ok", DhcpReservation{DeviceName: "", MacAddress: "aa:bb:cc:dd:ee:ff", IPAddress: "192.168.1.50"}, false},
		{"hyphen mac", DhcpReservation{DeviceName: "pc", MacAddress: "aa-bb-cc-dd-ee-ff", IPAddress: "192.168.1.50"}, false},
		{"name injection", DhcpReservation{DeviceName: "pc\ndhcp-host=x", MacAddress: "aa:bb:cc:dd:ee:ff", IPAddress: "192.168.1.50"}, true},
		{"bad mac", DhcpReservation{DeviceName: "pc", MacAddress: "not-a-mac", IPAddress: "192.168.1.50"}, true},
		{"mac injection", DhcpReservation{DeviceName: "pc", MacAddress: "aa:bb:cc:dd:ee:ff\ndhcp-host=x", IPAddress: "192.168.1.50"}, true},
		{"bad ip", DhcpReservation{DeviceName: "pc", MacAddress: "aa:bb:cc:dd:ee:ff", IPAddress: "999.1.1.1"}, true},
		{"ip injection", DhcpReservation{DeviceName: "pc", MacAddress: "aa:bb:cc:dd:ee:ff", IPAddress: "1.2.3.4\ndhcp-host=x"}, true},
		{"name too long", DhcpReservation{DeviceName: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", MacAddress: "aa:bb:cc:dd:ee:ff", IPAddress: "192.168.1.50"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReservation(tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateReservation(%+v) err = %v, wantErr %v", tt.res, err, tt.wantErr)
			}
		})
	}
}
