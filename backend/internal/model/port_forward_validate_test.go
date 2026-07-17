package model

import "testing"

func pf(name, iface, extPort, proto, ip, intPort string, status bool) PortForward {
	return PortForward{
		Name:         name,
		InInterface:  iface,
		ExternalPort: extPort,
		Protocol:     proto,
		InternalIP:   ip,
		InternalPort: intPort,
		Status:       status,
	}
}

func TestValidatePortForward(t *testing.T) {
	cases := []struct {
		name    string
		in      PortForward
		wantErr bool
	}{
		{"valid single translated", pf("web", "eth0", "8080", "tcp", "192.168.1.10", "80", true), false},
		{"valid single keep-port", pf("web", "eth0", "80", "udp", "192.168.1.10", "", true), false},
		{"valid range keep-port", pf("web", "eth0", "8000-8010", "tcp", "192.168.1.10", "", true), false},
		{"empty name", pf("", "eth0", "80", "tcp", "192.168.1.10", "80", true), true},
		{"bad interface", pf("web", "eth0;rm", "80", "tcp", "192.168.1.10", "80", true), true},
		{"bad protocol", pf("web", "eth0", "80", "icmp", "192.168.1.10", "80", true), true},
		{"non-ipv4 internal", pf("web", "eth0", "80", "tcp", "not-an-ip", "80", true), true},
		{"ipv6 internal rejected", pf("web", "eth0", "80", "tcp", "fe80::1", "80", true), true},
		{"port zero", pf("web", "eth0", "0", "tcp", "192.168.1.10", "80", true), true},
		{"port too high", pf("web", "eth0", "70000", "tcp", "192.168.1.10", "80", true), true},
		{"reversed range", pf("web", "eth0", "9000-8000", "tcp", "192.168.1.10", "", true), true},
		{"range with translated port rejected", pf("web", "eth0", "8000-8010", "tcp", "192.168.1.10", "9000", true), true},
		{"bad internal port", pf("web", "eth0", "80", "tcp", "192.168.1.10", "99999", true), true},
		{"uppercase proto ok", pf("web", "eth0", "80", "TCP", "192.168.1.10", "80", true), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePortForward(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidatePortForward(%+v) err=%v, wantErr=%v", c.in, err, c.wantErr)
			}
		})
	}
}

func TestPortForwardsConflict(t *testing.T) {
	cases := []struct {
		name string
		a, b PortForward
		want bool
	}{
		{"same single port same proto/iface", pf("a", "eth0", "80", "tcp", "1.1.1.1", "80", true), pf("b", "eth0", "80", "tcp", "2.2.2.2", "80", true), true},
		{"different proto no conflict", pf("a", "eth0", "80", "tcp", "1.1.1.1", "80", true), pf("b", "eth0", "80", "udp", "2.2.2.2", "80", true), false},
		{"different iface no conflict", pf("a", "eth0", "80", "tcp", "1.1.1.1", "80", true), pf("b", "eth1", "80", "tcp", "2.2.2.2", "80", true), false},
		{"range overlaps single", pf("a", "eth0", "8000-8010", "tcp", "1.1.1.1", "", true), pf("b", "eth0", "8005", "tcp", "2.2.2.2", "80", true), true},
		{"range no overlap", pf("a", "eth0", "8000-8010", "tcp", "1.1.1.1", "", true), pf("b", "eth0", "9000", "tcp", "2.2.2.2", "80", true), false},
		{"adjacent ranges touch", pf("a", "eth0", "8000-8010", "tcp", "1.1.1.1", "", true), pf("b", "eth0", "8010-8020", "tcp", "2.2.2.2", "", true), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PortForwardsConflict(c.a, c.b); got != c.want {
				t.Fatalf("PortForwardsConflict = %v, want %v", got, c.want)
			}
		})
	}
}
