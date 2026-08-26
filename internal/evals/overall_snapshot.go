package evals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OMLXScoreSnapshotVersion identifies the current persisted OMLX score schema.
const OMLXScoreSnapshotVersion = 3

// OMLXScoreSnapshot is the sole durable LLAMBO-6 category-score state. It is a complete
// evaluator artifact, intentionally separate from catalog.json so catalog
// refreshes and task-quality updates cannot overwrite derived score state.
type OMLXScoreSnapshot struct {
	SchemaVersion         int       `json:"schema_version"`
	GeneratedAt           time.Time `json:"generated_at"`
	FormulaVersion        string    `json:"formula_version"`
	PopulationFingerprint string    `json:"cache_fingerprint"`
	Inventory             []string  `json:"inventory"`
	Endpoint              string    `json:"endpoint,omitempty"`
	// CategoryScores is the active authoritative artifact. It deliberately has
	// no overall field: six independent capability categories are the product.
	CategoryScores map[string]map[string]*LlamboScore `json:"category_scores,omitempty"`
	// Scores is a v1 reader compatibility surface only. New snapshots never
	// populate it, preventing the retired operational/capability composite from
	// leaking into model presentation.
	Scores map[string]*OverallScore `json:"scores,omitempty"`
	// OMLXPopulationFingerprint accepts the v1 field name. It is normalized into
	// PopulationFingerprint on read and never emitted by v2 writers.
	OMLXPopulationFingerprint string `json:"omlx_population_fingerprint,omitempty"`
}

// scoreSnapshotDecode accepts the retired lineage field only when reading an
// existing snapshot. Rolling snapshots never retain or emit it.
type scoreSnapshotDecode struct {
	OMLXScoreSnapshot
	PriorFormulaVersions []string `json:"prior_formula_versions,omitempty"`
}

func OMLXScoreSnapshotPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "llambo", "llambo-scores.json"), nil
}

func LoadOMLXScoreSnapshot(path string) (*OMLXScoreSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read LLAMBO score snapshot: %w", err)
	}
	var decoded scoreSnapshotDecode
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode LLAMBO score snapshot: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode LLAMBO score snapshot: %w", err)
	}
	snapshot := decoded.OMLXScoreSnapshot
	if snapshot.SchemaVersion != 1 && snapshot.SchemaVersion != 2 && snapshot.SchemaVersion != OMLXScoreSnapshotVersion {
		return nil, fmt.Errorf("unsupported LLAMBO score snapshot version %d", snapshot.SchemaVersion)
	}
	if snapshot.PopulationFingerprint == "" {
		snapshot.PopulationFingerprint = snapshot.OMLXPopulationFingerprint
	}
	snapshot.OMLXPopulationFingerprint = ""
	snapshot.Inventory = sortedSnapshotInventory(snapshot.Inventory)
	if snapshot.CategoryScores == nil {
		snapshot.CategoryScores = make(map[string]map[string]*LlamboScore)
	}
	snapshot.CategoryScores = categorySnapshotEntries(snapshot.Inventory, snapshot.CategoryScores)
	return &snapshot, nil
}

func SaveOMLXScoreSnapshot(path string, snapshot OMLXScoreSnapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create LLAMBO score directory: %w", err)
	}
	snapshot.SchemaVersion = OMLXScoreSnapshotVersion
	snapshot.FormulaVersion = CategoryFormulaVersion
	snapshot.Inventory = sortedSnapshotInventory(snapshot.Inventory)
	if snapshot.CategoryScores == nil {
		snapshot.CategoryScores = make(map[string]map[string]*LlamboScore)
	}
	snapshot.CategoryScores = categorySnapshotEntries(snapshot.Inventory, snapshot.CategoryScores)
	// v1 values are preserved only when this is explicitly a legacy import;
	// normal score publication must not serialize an overall composite.
	if snapshot.SchemaVersion >= 2 {
		snapshot.Scores = nil
		snapshot.OMLXPopulationFingerprint = ""
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal LLAMBO score snapshot: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".llambo-scores-*.tmp")
	if err != nil {
		return fmt.Errorf("create LLAMBO score snapshot temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("set LLAMBO score snapshot permissions: %w", err)
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write LLAMBO score snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close LLAMBO score snapshot: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename LLAMBO score snapshot: %w", err)
	}
	return nil
}

func NewOMLXScoreSnapshot(report Report, inventory []string, endpoint string, generatedAt time.Time) OMLXScoreSnapshot {
	inventory = sortedSnapshotInventory(inventory)
	scores := make(map[string]map[string]*LlamboScore, len(inventory))
	byKey := make(map[string]map[string]*LlamboScore, len(report.Models))
	for _, row := range report.Models {
		byKey[row.Key] = cloneCategoryScores(row.LlamboScores)
	}
	for _, key := range inventory {
		scores[key] = byKey[key]
	}
	fingerprint := reportCacheFingerprint(report)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	return OMLXScoreSnapshot{SchemaVersion: OMLXScoreSnapshotVersion, GeneratedAt: generatedAt.UTC(), FormulaVersion: CategoryFormulaVersion, PopulationFingerprint: fingerprint, Inventory: inventory, Endpoint: strings.TrimSpace(endpoint), CategoryScores: scores}
}

func (snapshot *OMLXScoreSnapshot) Contains(model string) bool {
	if snapshot == nil {
		return false
	}
	index := sort.SearchStrings(snapshot.Inventory, model)
	return index < len(snapshot.Inventory) && snapshot.Inventory[index] == model
}

func sortedSnapshotInventory(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func categorySnapshotEntries(inventory []string, scores map[string]map[string]*LlamboScore) map[string]map[string]*LlamboScore {
	result := make(map[string]map[string]*LlamboScore, len(inventory))
	for _, model := range inventory {
		result[model] = scores[model]
	}
	return result
}

func cloneCategoryScores(source map[string]*LlamboScore) map[string]*LlamboScore {
	if source == nil {
		return nil
	}
	result := make(map[string]*LlamboScore, len(source))
	for category, score := range source {
		if score == nil {
			continue
		}
		copy := *score
		copy.Contributions = append([]benchmarkEvidence(nil), score.Contributions...)
		copy.Families = append([]string(nil), score.Families...)
		copy.Conflicts = append([]string(nil), score.Conflicts...)
		copy.EstimateSources = append([]string(nil), score.EstimateSources...)
		if score.Primary != nil {
			primary := *score.Primary
			copy.Primary = &primary
		}
		result[category] = &copy
	}
	return result
}

func reportCacheFingerprint(report Report) string {
	parts := make([]string, 0, len(report.Sources))
	for _, source := range report.Sources {
		parts = append(parts, source.Name+"|"+source.Version+"|"+source.ContentSHA+"|"+source.FetchedAt.UTC().Format(time.RFC3339Nano))
	}
	sort.Strings(parts)
	return SealBytes([]byte(strings.Join(parts, "\n")))
}
