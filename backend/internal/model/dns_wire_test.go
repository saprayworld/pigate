package model

import "testing"

func TestEncodeDNSNameHex(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"basic name", "ns1.example.com", "036e7331076578616d706c6503636f6d00", false},
		{"single label", "ns1", "036e733100", false},
		{"uppercase normalizes to lowercase", "NS1.Example.COM", "036e7331076578616d706c6503636f6d00", false},
		{"trailing dot", "ns1.example.com.", "036e7331076578616d706c6503636f6d00", false},

		{"empty", "", "", true},
		{"double dot", "ns1..example.com", "", true},
		{"leading dot", ".a", "", true},
		{"empty label mid", "a..b", "", true},
		{"label too long", "a123456789012345678901234567890123456789012345678901234567890123.com", "", true},
		{"name too long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.com", "", true},
		{"underscore rejected", "ns_1", "", true},
		{"leading hyphen", "-ns", "", true},
		{"trailing hyphen", "ns-", "", true},
		{"embedded newline", "ns1\nx", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeDNSNameHex(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EncodeDNSNameHex(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("EncodeDNSNameHex(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
