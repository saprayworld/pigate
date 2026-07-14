//go:build linux

package kernel

import (
	"strings"
	"testing"

	"pigate/internal/model"
)

// TestBuildDNSConfig_ListenInterfaces covers the self-healing listen-interface
// emission (issue #50): every configured name is emitted (including ones that may
// not exist yet), invalid names are skipped, and an empty list emits no
// `interface=` line at all.
func TestBuildDNSConfig_ListenInterfaces(t *testing.T) {
	t.Run("emits interface line per name without skipping missing ones", func(t *testing.T) {
		cfg := buildDNSConfig(nil, []string{"eth0.301", "wlan1"}, nil)
		for _, want := range []string{"interface=eth0.301", "interface=wlan1"} {
			if !strings.Contains(cfg, want) {
				t.Errorf("expected config to contain %q, got:\n%s", want, cfg)
			}
		}
	})

	t.Run("skips invalid names but keeps valid ones", func(t *testing.T) {
		cfg := buildDNSConfig(nil, []string{"eth0", "bad\ninterface=evil", "wlan0"}, nil)
		if !strings.Contains(cfg, "interface=eth0") || !strings.Contains(cfg, "interface=wlan0") {
			t.Errorf("expected valid interfaces to be emitted, got:\n%s", cfg)
		}
		if strings.Contains(cfg, "interface=evil") {
			t.Errorf("injected directive must not appear in config, got:\n%s", cfg)
		}
		// The injected directive must not survive on its own line either.
		for _, line := range strings.Split(cfg, "\n") {
			if strings.TrimSpace(line) == "interface=evil" {
				t.Errorf("injection produced a standalone directive line: %q", line)
			}
		}
	})

	t.Run("no interfaces means no interface line", func(t *testing.T) {
		cfg := buildDNSConfig(nil, nil, nil)
		if strings.Contains(cfg, "interface=") {
			t.Errorf("expected no interface= line for empty list, got:\n%s", cfg)
		}
	})
}

// TestBuildDNSConfig_ZonesAndUpstreams sanity-checks that the interface refactor
// left the existing zone/upstream rendering intact.
func TestBuildDNSConfig_ZonesAndUpstreams(t *testing.T) {
	zones := []model.DNSZone{
		{
			ZoneName:        "internal.local",
			Enabled:         true,
			IsAuthoritative: true,
			Records: []model.DNSRecord{
				{Name: "www", Type: "A", Value: "192.168.1.10"},
			},
		},
		{
			ZoneName:        "corp.example",
			Enabled:         true,
			IsAuthoritative: false,
			ForwardTo:       "10.0.0.53",
		},
		{
			ZoneName:        "disabled.local",
			Enabled:         false,
			IsAuthoritative: true,
		},
	}
	cfg := buildDNSConfig(zones, []string{"eth0"}, []string{"1.1.1.1"})

	for _, want := range []string{
		"no-resolv",
		"server=1.1.1.1",
		"local=/internal.local/",
		"host-record=www.internal.local,192.168.1.10",
		"server=/corp.example/10.0.0.53",
		"interface=eth0",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("expected config to contain %q, got:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "disabled.local") {
		t.Errorf("disabled zone must not be emitted, got:\n%s", cfg)
	}
}
