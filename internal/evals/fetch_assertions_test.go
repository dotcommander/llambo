package evals

import (
	"testing"
)

func fetchSourceCachesMatch(t *testing.T, statuses []SourceStatus, want map[string]string) bool {
	t.Helper()
	for name, cache := range want {
		if sourceStatusNamed(t, statuses, name).Cache != cache {
			return false
		}
	}
	return true
}
