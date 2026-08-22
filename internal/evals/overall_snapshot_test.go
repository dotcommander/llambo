package evals

import (
	"encoding/json"
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

func TestLoadV1SnapshotAndPreserveItBeforeCategoryUpgrade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llambo-scores.json")
	legacy := `{"schema_version":1,"generated_at":"2026-08-21T00:00:00Z","formula_version":"LLAMBO-5","omlx_population_fingerprint":"legacy-fp","inventory":["model"],"scores":{"model":{"coverage":0.5,"confidence":"low","formula_version":"LLAMBO-5","population_fingerprint":"legacy-fp"}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOMLXScoreSnapshot(path)
	if err != nil || loaded.PopulationFingerprint != "legacy-fp" || loaded.Scores["model"] == nil {
		t.Fatalf("legacy snapshot not decoded: %#v %v", loaded, err)
	}
	if err := SaveOMLXScoreSnapshot(path, OMLXScoreSnapshot{FormulaVersion: CategoryFormulaVersion, Inventory: []string{"model"}, CategoryScores: map[string]map[string]*LlamboScore{"model": {}}}); err != nil {
		t.Fatal(err)
	}
	archived, err := os.ReadFile(filepath.Join(dir, "llambo-scores.LLAMBO-5.json"))
	if err != nil || string(archived) != legacy {
		t.Fatalf("legacy snapshot archive mismatch: %q %v", archived, err)
	}
}

func TestSaveOMLXScoreSnapshotArchivesImmediateV1PredecessorAndRetainsLLAMBO5(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llambo-scores.json")
	v1 := OMLXScoreSnapshot{SchemaVersion: OMLXScoreSnapshotVersion, GeneratedAt: time.Unix(1, 0).UTC(), FormulaVersion: "LLAMBO-6-category-v1", Inventory: []string{"model"}, CategoryScores: map[string]map[string]*LlamboScore{"model": {"coding": {Score: 70}}}}
	v1Bytes, err := json.Marshal(v1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, v1Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	next := NewOMLXScoreSnapshot(Report{}, []string{"model"}, "", time.Unix(2, 0))
	if err := SaveOMLXScoreSnapshot(path, next); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(dir, "llambo-scores.LLAMBO-6-category-v1.json")
	archived, err := os.ReadFile(archivePath)
	if err != nil || string(archived) != string(v1Bytes) {
		t.Fatalf("v1 archive is not an exact retained predecessor: %q %v", archived, err)
	}
	loaded, err := LoadOMLXScoreSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.FormulaVersion != CategoryFormulaVersion || !containsFormulaVersion(loaded.PriorFormulaVersions, "LLAMBO-6-category-v4") || !containsFormulaVersion(loaded.PriorFormulaVersions, "LLAMBO-6-category-v3") || !containsFormulaVersion(loaded.PriorFormulaVersions, "LLAMBO-6-category-v2") || !containsFormulaVersion(loaded.PriorFormulaVersions, "LLAMBO-6-category-v1") || !containsFormulaVersion(loaded.PriorFormulaVersions, OverallFormulaVersion) {
		t.Fatalf("active v5 snapshot omitted retained formula lineage: %#v", loaded)
	}
}

func TestSaveOMLXScoreSnapshotCarriesV3LineageIntoRepeatedV4(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llambo-scores.json")
	v3 := OMLXScoreSnapshot{
		SchemaVersion:        OMLXScoreSnapshotVersion,
		GeneratedAt:          time.Unix(1, 0).UTC(),
		FormulaVersion:       "LLAMBO-6-category-v3",
		PriorFormulaVersions: []string{"LLAMBO-6-category-v2", "LLAMBO-6-category-v1", OverallFormulaVersion},
		Inventory:            []string{"model"},
		CategoryScores:       map[string]map[string]*LlamboScore{"model": {"coding": {Score: 70}}},
	}
	if err := SaveOMLXScoreSnapshot(path, v3); err != nil {
		t.Fatal(err)
	}
	if err := SaveOMLXScoreSnapshot(path, NewOMLXScoreSnapshot(Report{}, []string{"model"}, "", time.Unix(2, 0))); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOMLXScoreSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"LLAMBO-6-category-v3", "LLAMBO-6-category-v2", "LLAMBO-6-category-v1", OverallFormulaVersion} {
		if !containsFormulaVersion(loaded.PriorFormulaVersions, version) {
			t.Fatalf("v4 snapshot dropped retained lineage %q: %#v", version, loaded.PriorFormulaVersions)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "llambo-scores.LLAMBO-6-category-v3.json")); err != nil {
		t.Fatalf("v3 predecessor was not archived: %v", err)
	}
	if err := SaveOMLXScoreSnapshot(path, NewOMLXScoreSnapshot(Report{}, []string{"model"}, "", time.Unix(3, 0))); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadOMLXScoreSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFormulaVersion(loaded.PriorFormulaVersions, "LLAMBO-6-category-v1") {
		t.Fatalf("same-formula v4 publication dropped v1 lineage: %#v", loaded.PriorFormulaVersions)
	}
}

func TestSaveOMLXScoreSnapshotRejectsCorruptExistingArchiveWithoutReplacingActive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llambo-scores.json")
	active := OMLXScoreSnapshot{SchemaVersion: OMLXScoreSnapshotVersion, FormulaVersion: "LLAMBO-6-category-v1", Inventory: []string{"model"}, CategoryScores: map[string]map[string]*LlamboScore{"model": {}}}
	activeBytes, err := json.Marshal(active)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, activeBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(dir, "llambo-scores.LLAMBO-6-category-v1.json")
	if err := os.WriteFile(archivePath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveOMLXScoreSnapshot(path, NewOMLXScoreSnapshot(Report{}, []string{"replacement"}, "", time.Unix(2, 0))); err == nil || !strings.Contains(err.Error(), "validate prior LLAMBO score snapshot archive") {
		t.Fatalf("corrupt archive did not fail closed: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(activeBytes) {
		t.Fatalf("corrupt archive allowed active replacement: got %q want %q", after, activeBytes)
	}
}

func TestSaveOMLXScoreSnapshotRejectsSymlinkArchiveWithoutReplacingActive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llambo-scores.json")
	active := OMLXScoreSnapshot{SchemaVersion: OMLXScoreSnapshotVersion, FormulaVersion: "LLAMBO-6-category-v1", Inventory: []string{"model"}, CategoryScores: map[string]map[string]*LlamboScore{"model": {}}}
	activeBytes, err := json.Marshal(active)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, activeBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "archive-target.json")
	if err := os.WriteFile(target, []byte("not an archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(dir, "llambo-scores.LLAMBO-6-category-v1.json")
	if err := os.Symlink(target, archivePath); err != nil {
		t.Fatal(err)
	}
	if err := SaveOMLXScoreSnapshot(path, NewOMLXScoreSnapshot(Report{}, []string{"replacement"}, "", time.Unix(2, 0))); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink archive did not fail closed: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(activeBytes) {
		t.Fatalf("symlink archive allowed active replacement: got %q want %q", after, activeBytes)
	}
}

func containsFormulaVersion(versions []string, expected string) bool {
	for _, version := range versions {
		if version == expected {
			return true
		}
	}
	return false
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
