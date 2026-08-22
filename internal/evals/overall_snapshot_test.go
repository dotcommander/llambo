package evals

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOMLXScoreSnapshotRoundTripRetainsEmptyInventoryAndProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llambo-scores.json")
	snapshot := OMLXScoreSnapshot{
		GeneratedAt:           time.Unix(7, 0).UTC(),
		FormulaVersion:        CategoryFormulaVersion,
		PopulationFingerprint: "fingerprint",
		Inventory:             []string{"model"},
		CategoryScores: map[string]map[string]*LlamboScore{
			"model": {"coding": {Score: 71.25, Coverage: .6, TrustedCoverage: .45, Confidence: "low", Families: []string{"repository-editing"}}},
		},
	}
	if err := SaveOMLXScoreSnapshot(path, snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOMLXScoreSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || len(loaded.Inventory) != 1 || loaded.Inventory[0] != "model" || loaded.CategoryScores["model"]["coding"].Score != 71.25 || len(loaded.Scores) != 0 {
		t.Fatalf("incomplete score snapshot: %#v", loaded)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.FormulaVersion != CategoryFormulaVersion || strings.Contains(string(data), `"prior_formula_versions"`) {
		t.Fatalf("rolling snapshot retained formula lineage: %s", data)
	}
	if err := SaveOMLXScoreSnapshot(path, OMLXScoreSnapshot{FormulaVersion: CategoryFormulaVersion, Inventory: []string{}, CategoryScores: map[string]map[string]*LlamboScore{}}); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadOMLXScoreSnapshot(path)
	if err != nil || loaded == nil || loaded.Inventory == nil || len(loaded.Inventory) != 0 || len(loaded.CategoryScores) != 0 || len(loaded.Scores) != 0 {
		t.Fatalf("empty inventory did not replace stale snapshot: %#v %v", loaded, err)
	}
}

func TestNewOMLXScoreSnapshotRemovesStaleInventory(t *testing.T) {
	result := Report{Models: []ReportModel{{Key: "model-b", LlamboScores: map[string]*LlamboScore{"coding": {Score: 64}}}}}
	snapshot := NewOMLXScoreSnapshot(result, []string{"model-b"}, "", time.Unix(1, 0))
	if len(snapshot.Inventory) != 1 || snapshot.Inventory[0] != "model-b" || snapshot.CategoryScores["model-a"] != nil || snapshot.CategoryScores["model-b"]["coding"].Score != 64 || !snapshot.Contains("model-b") || snapshot.Contains("model-a") {
		t.Fatalf("stale inventory survived replacement: %#v", snapshot)
	}
}

func TestLoadLegacySnapshotsThenReplaceWithRollingSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llambo-scores.json")
	legacy := `{"schema_version":1,"generated_at":"2026-08-21T00:00:00Z","formula_version":"LLAMBO-5","omlx_population_fingerprint":"legacy-fp","inventory":["model"],"scores":{"model":{"coverage":0.5,"confidence":"low","formula_version":"LLAMBO-5","population_fingerprint":"legacy-fp"}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOMLXScoreSnapshot(path)
	if err != nil || loaded.FormulaVersion != "LLAMBO-5" || loaded.PopulationFingerprint != "legacy-fp" || loaded.Scores["model"] == nil {
		t.Fatalf("legacy snapshot not decoded: %#v %v", loaded, err)
	}
	if err := SaveOMLXScoreSnapshot(path, OMLXScoreSnapshot{FormulaVersion: CategoryFormulaVersion, Inventory: []string{"model"}, CategoryScores: map[string]map[string]*LlamboScore{"model": {}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "llambo-scores.LLAMBO-5.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy snapshot archive exists: %v", err)
	}
	loaded, err = LoadOMLXScoreSnapshot(path)
	if err != nil || loaded.FormulaVersion != CategoryFormulaVersion || loaded.Scores != nil {
		t.Fatalf("legacy snapshot was not atomically replaced: %#v %v", loaded, err)
	}
}

func TestLoadLegacySnapshotAcceptsRetiredFormulaLineage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llambo-scores.json")
	legacy := `{"schema_version":2,"generated_at":"2026-08-21T00:00:00Z","formula_version":"LLAMBO-6-category-v5","prior_formula_versions":["LLAMBO-6-category-v4"],"inventory":["model"],"category_scores":{"model":{"coding":{"score":70}}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOMLXScoreSnapshot(path)
	if err != nil || loaded == nil || loaded.FormulaVersion != "LLAMBO-6-category-v5" || loaded.CategoryScores["model"]["coding"].Score != 70 {
		t.Fatalf("legacy category snapshot not decoded: %#v %v", loaded, err)
	}
	if err := SaveOMLXScoreSnapshot(path, *loaded); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), fmt.Sprintf("\"formula_version\": %q", CategoryFormulaVersion)) || strings.Contains(string(data), `"prior_formula_versions"`) {
		t.Fatalf("rolling replacement retained legacy formula lineage: %s", data)
	}
}

func TestSaveOMLXScoreSnapshotConcurrentWritersRemainAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llambo-scores.json")
	const writers = 16
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			model := fmt.Sprintf("model-%02d", i)
			err := SaveOMLXScoreSnapshot(path, OMLXScoreSnapshot{
				GeneratedAt:    time.Unix(int64(i+1), 0).UTC(),
				FormulaVersion: CategoryFormulaVersion,
				Inventory:      []string{model},
				CategoryScores: map[string]map[string]*LlamboScore{model: {"coding": {Score: float64(i)}}},
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent save failed: %v", err)
	}
	loaded, err := LoadOMLXScoreSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || len(loaded.Inventory) != 1 || !loaded.Contains(loaded.Inventory[0]) {
		t.Fatalf("concurrent save produced incomplete snapshot: %#v", loaded)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".llambo-scores-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain after saves: %v", matches)
	}
}
