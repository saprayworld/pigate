package kernel

import "testing"

// TestMockNetwork_ConfigureInterface_SeedsRoutingBaseline covers the WAN
// failover Phase 2 QA finding ("kill-switch-off restore silently no-ops"
// under -mock=true): once wired via SetRoutingSeed, ConfigureInterface must
// seed a realistic default-route metric baseline into the linked
// MockRouting, mirroring what RealNetwork.ConfigureInterface + a real
// kernel default route would produce — so DefaultRouteMetric (Task 14,
// Decision C) has something to snapshot before a WAN failover override, and
// therefore something to restore afterwards.
func TestMockNetwork_ConfigureInterface_SeedsRoutingBaseline(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		gateway    string
		metric     int
		wantFound  bool
		wantMetric int
	}{
		{name: "dhcp with no configured metric seeds the historical default", mode: "dhcp", gateway: "", metric: 0, wantFound: true, wantMetric: 100},
		{name: "dhcp with a configured metric seeds that metric", mode: "dhcp", gateway: "", metric: 30, wantFound: true, wantMetric: 30},
		{name: "static with gateway and no configured metric seeds the historical default", mode: "static", gateway: "10.0.0.1", metric: 0, wantFound: true, wantMetric: 100},
		{name: "static with gateway and a configured metric seeds that metric", mode: "static", gateway: "10.0.0.1", metric: 42, wantFound: true, wantMetric: 42},
		{name: "static with no gateway installs no default route (matches RealNetwork)", mode: "static", gateway: "", metric: 0, wantFound: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := NewMockRouting()
			net := NewMockNetwork()
			net.SetRoutingSeed(rt)

			if err := net.ConfigureInterface("wan0", tc.mode, "", "", tc.gateway, tc.metric); err != nil {
				t.Fatalf("ConfigureInterface returned error: %v", err)
			}

			gotMetric, gotFound, err := rt.DefaultRouteMetric("wan0")
			if err != nil {
				t.Fatalf("DefaultRouteMetric returned error: %v", err)
			}
			if gotFound != tc.wantFound {
				t.Fatalf("DefaultRouteMetric found = %v, want %v", gotFound, tc.wantFound)
			}
			if tc.wantFound && gotMetric != tc.wantMetric {
				t.Errorf("DefaultRouteMetric metric = %d, want %d", gotMetric, tc.wantMetric)
			}
		})
	}
}

// TestMockNetwork_ConfigureInterface_NoRoutingSeedIsANoOp verifies that
// leaving SetRoutingSeed unwired (the pre-fix default, and every existing
// NewMockNetwork() test call site) is unaffected — ConfigureInterface stays
// a plain log-only no-op with no MockRouting to touch.
func TestMockNetwork_ConfigureInterface_NoRoutingSeedIsANoOp(t *testing.T) {
	net := NewMockNetwork()
	if err := net.ConfigureInterface("wan0", "dhcp", "", "", "", 0); err != nil {
		t.Fatalf("ConfigureInterface returned error: %v", err)
	}
	// Nothing to assert beyond "did not panic" — there is no MockRouting
	// wired in, so there is nothing else to observe.
}
