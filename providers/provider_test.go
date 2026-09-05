package providers

import (
	"reflect"
	"testing"
)

func TestGetAPIKeyUsesFirstOrderedAvailableKey(t *testing.T) {
	const envVar = "LLAMBO_TEST_PRIMARY_API_KEY"

	tests := []struct {
		name string
		cfg  Config
		env  string
		keys []string
		want string
	}{
		{
			name: "leading empty array entry",
			cfg:  Config{APIKeys: []string{"", "array-first", "array-second"}, EnvVar: envVar},
			keys: []string{"array-first", "array-second"},
			want: "array-first",
		},
		{
			name: "array single and environment precedence with deduplication",
			cfg:  Config{APIKeys: []string{"array-first", "array-second"}, APIKey: "array-second", EnvVar: envVar},
			env:  "environment-key",
			keys: []string{"array-first", "array-second", "environment-key"},
			want: "array-first",
		},
		{
			name: "single key",
			cfg:  Config{APIKey: "single-key", EnvVar: envVar},
			keys: []string{"single-key"},
			want: "single-key",
		},
		{
			name: "environment key",
			cfg:  Config{EnvVar: envVar},
			env:  "environment-key",
			keys: []string{"environment-key"},
			want: "environment-key",
		},
		{
			name: "no available key",
			cfg:  Config{EnvVar: envVar},
			keys: nil,
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(envVar, test.env)
			if got := GetAPIKeys("primary", test.cfg); !reflect.DeepEqual(got, test.keys) {
				t.Fatalf("GetAPIKeys() = %q, want %q", got, test.keys)
			}
			if got := GetAPIKey("primary", test.cfg); got != test.want {
				t.Fatalf("GetAPIKey() = %q, want %q", got, test.want)
			}
		})
	}
}
