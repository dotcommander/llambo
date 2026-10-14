package evals

import (
	"slices"
	"sort"
	"testing"
)

type officialCardCohortFixture struct {
	cache      string
	cohorts    map[string][]float64
	revision   string
	targetKeys []string
}

func assertOfficialCardTargetRows(t *testing.T, snapshot sourceSnapshot, targetKeys []string) {
	t.Helper()
	keys := make(map[string]bool, len(snapshot.Models))
	for _, model := range snapshot.Models {
		keys[model.Key] = true
	}
	for _, key := range targetKeys {
		if !keys[key] {
			t.Fatalf("missing canonical target row %q: %#v", key, snapshot.Models)
		}
	}
	for _, model := range snapshot.Models {
		categories := []string{"agents", "coding", "reasoning"}
		switch model.Key {
		case "lfm-2.5-vl-3b":
			categories = []string{"agents"}
		case "lfm-2.5-8b-a1b":
			categories = []string{"instruction-following", "reasoning"}
		}
		for _, category := range categories {
			if score := scoreCategoryV3(model, categorySpecNamed(t, category), []Model{model}); score == nil {
				t.Fatalf("%s did not resolve %s", model.Key, category)
			}
		}
	}
}

func assertOfficialCardFrozenCohorts(t *testing.T, test officialCardCohortFixture) {
	t.Helper()
	for benchmark, want := range test.cohorts {
		got := frozenBenchmarkCohort(benchmark, test.revision)
		if len(got) != len(want) {
			t.Fatalf("%s cohort size = %d, want %d: %#v", benchmark, len(got), len(want), got)
		}
		sort.Float64s(want)
		if !slices.Equal(got, want) {
			t.Fatalf("%s cohort = %#v, want %#v", benchmark, got, want)
		}
	}
}
