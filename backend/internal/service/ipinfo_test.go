package service

import "testing"

// TestIsGloballyRoutable covers every range called out by plan T-02,
// including the IPv4-mapped IPv6 unmap requirement.
func TestIsGloballyRoutable(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		{"public IPv4", "8.8.8.8", true},
		{"public IPv4 2", "1.1.1.1", true},
		{"public IPv6", "2606:4700:4700::1111", true},

		{"RFC1918 10/8", "10.0.0.1", false},
		{"RFC1918 172.16/12", "172.16.5.4", false},
		{"RFC1918 192.168/16", "192.168.1.10", false},
		{"loopback IPv4", "127.0.0.1", false},
		{"loopback IPv6", "::1", false},
		{"link-local unicast IPv4", "169.254.1.1", false},
		{"link-local multicast IPv6", "ff02::1", false},
		{"multicast IPv4", "224.0.0.1", false},
		{"unspecified IPv4", "0.0.0.0", false},
		{"unspecified IPv6", "::", false},
		{"interface-local multicast IPv6", "ff01::1", false},

		{"CGNAT 100.64.0.0/10", "100.64.0.1", false},
		{"CGNAT boundary high", "100.127.255.255", false},
		{"0.0.0.0/8", "0.1.2.3", false},
		{"169.254.0.0/16 (dup check)", "169.254.255.255", false},
		{"TEST-NET-1 192.0.2.0/24", "192.0.2.55", false},
		{"TEST-NET-2 198.51.100.0/24", "198.51.100.5", false},
		{"TEST-NET-3 203.0.113.0/24", "203.0.113.99", false},
		{"reserved 240.0.0.0/4", "240.0.0.1", false},
		{"reserved 255.255.255.255", "255.255.255.255", false},

		{"IPv6 ULA fc00::/7", "fc00::1", false},
		{"IPv6 ULA fd00::/8", "fd12:3456::1", false},
		{"IPv6 doc 2001:db8::/32", "2001:db8::1", false},

		// IPv4-mapped IPv6 — must be unmapped before classification (plan
		// Caution 1 / T-02 explicit requirement), otherwise this private
		// address would incorrectly be judged globally routable.
		{"IPv4-mapped IPv6 private", "::ffff:192.168.1.1", false},
		{"IPv4-mapped IPv6 CGNAT", "::ffff:100.64.0.1", false},
		{"IPv4-mapped IPv6 public", "::ffff:8.8.8.8", true},

		{"unparseable", "not-an-ip", false},
		{"empty", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isGloballyRoutable(c.ip)
			if got != c.want {
				t.Errorf("isGloballyRoutable(%q) = %v, want %v", c.ip, got, c.want)
			}
		})
	}
}
