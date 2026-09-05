package providers

import "testing"

func TestPromoteCanaryConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cfg        GlobalConfig
		canary     CanaryConfig
		wantName   string
		wantTarget int
		wantValues map[string]int
	}{
		{
			name: "uses configured baseline priority",
			cfg: GlobalConfig{
				Providers: map[string]Config{
					"baseline": {Enabled: true, Priority: 7},
					"canary":   {Enabled: true, Priority: 19},
				},
				Routing: RoutingConfig{Canary: &CanaryConfig{Provider: "canary"}},
			},
			canary:     CanaryConfig{Provider: "canary", Baseline: "baseline"},
			wantName:   "canary",
			wantTarget: 7,
			wantValues: map[string]int{"baseline": 7, "canary": 7},
		},
		{
			name: "missing baseline keeps default top priority",
			cfg: GlobalConfig{
				Providers: map[string]Config{
					"canary": {Enabled: true, Priority: 19},
				},
				Routing: RoutingConfig{Canary: &CanaryConfig{Provider: "canary"}},
			},
			canary:     CanaryConfig{Provider: "canary", Baseline: "missing"},
			wantName:   "canary",
			wantTarget: 1,
			wantValues: map[string]int{"canary": 1},
		},
		{
			name: "no baseline keeps default top priority",
			cfg: GlobalConfig{
				Providers: map[string]Config{
					"canary": {Enabled: true, Priority: 19},
					"other":  {Enabled: true, Priority: 5},
				},
				Routing: RoutingConfig{Canary: &CanaryConfig{Provider: "canary"}},
			},
			canary:     CanaryConfig{Provider: "canary"},
			wantName:   "canary",
			wantTarget: 1,
			wantValues: map[string]int{"canary": 1, "other": 5},
		},
		{
			name: "missing canary still clears active route",
			cfg: GlobalConfig{
				Providers: map[string]Config{
					"baseline": {Enabled: true, Priority: 4},
				},
				Routing: RoutingConfig{Canary: &CanaryConfig{Provider: "missing"}},
			},
			canary:     CanaryConfig{Provider: "missing", Baseline: "baseline"},
			wantName:   "missing",
			wantTarget: 4,
			wantValues: map[string]int{"baseline": 4},
		},
		{
			name: "uses captured identity and clears the loaded route",
			cfg: GlobalConfig{
				Providers: map[string]Config{
					"baseline": {Enabled: true, Priority: 3},
					"captured": {Enabled: true, Priority: 19},
					"current":  {Enabled: true, Priority: 12},
				},
				Routing: RoutingConfig{Canary: &CanaryConfig{Provider: "current"}},
			},
			canary:     CanaryConfig{Provider: "captured", Baseline: "baseline"},
			wantName:   "captured",
			wantTarget: 3,
			wantValues: map[string]int{"baseline": 3, "captured": 3, "current": 12},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name, target := PromoteCanaryConfig(&test.cfg, test.canary)
			if name != test.wantName || target != test.wantTarget {
				t.Fatalf("PromoteCanaryConfig() = (%q, %d), want (%q, %d)", name, target, test.wantName, test.wantTarget)
			}
			if test.cfg.Routing.Canary != nil {
				t.Fatalf("active canary = %#v, want removed", test.cfg.Routing.Canary)
			}
			for provider, want := range test.wantValues {
				if got := test.cfg.Providers[provider].Priority; got != want {
					t.Errorf("provider %q priority = %d, want %d", provider, got, want)
				}
			}
		})
	}
}
