package evals

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const OMLXScoreSnapshotVersion = 2

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
	CategoryScores       map[string]map[string]*LlamboScore `json:"category_scores,omitempty"`
	PriorFormulaVersions []string                           `json:"prior_formula_versions,omitempty"`
	// Scores is a v1 reader compatibility surface only. New snapshots never
	// populate it, preventing the retired operational/capability composite from
	// leaking into model presentation.
	Scores map[string]*OverallScore `json:"scores,omitempty"`
	// OMLXPopulationFingerprint accepts the v1 field name. It is normalized into
	// PopulationFingerprint on read and never emitted by v2 writers.
	OMLXPopulationFingerprint string `json:"omlx_population_fingerprint,omitempty"`
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
	var snapshot OMLXScoreSnapshot
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("decode LLAMBO score snapshot: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode LLAMBO score snapshot: %w", err)
	}
	if snapshot.SchemaVersion != 1 && snapshot.SchemaVersion != OMLXScoreSnapshotVersion {
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
	immediatePrior, priorLineage, err := preservePriorScoreSnapshot(path, snapshot.FormulaVersion)
	if err != nil {
		return err
	}
	snapshot.PriorFormulaVersions = retainedFormulaVersions(immediatePrior, append(priorLineage, snapshot.PriorFormulaVersions...))
	snapshot.SchemaVersion = OMLXScoreSnapshotVersion
	snapshot.Inventory = sortedSnapshotInventory(snapshot.Inventory)
	if snapshot.CategoryScores == nil {
		snapshot.CategoryScores = make(map[string]map[string]*LlamboScore)
	}
	snapshot.CategoryScores = categorySnapshotEntries(snapshot.Inventory, snapshot.CategoryScores)
	// v1 values are preserved only when this is explicitly a legacy import;
	// normal score publication must not serialize an overall composite.
	if snapshot.SchemaVersion == OMLXScoreSnapshotVersion {
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

func preservePriorScoreSnapshot(path, nextFormula string) (string, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, nil
		}
		return "", nil, fmt.Errorf("read prior LLAMBO score snapshot: %w", err)
	}
	var prior struct {
		FormulaVersion       string   `json:"formula_version"`
		PriorFormulaVersions []string `json:"prior_formula_versions"`
	}
	if err := json.Unmarshal(data, &prior); err != nil {
		return "", nil, fmt.Errorf("decode prior LLAMBO score snapshot formula: %w", err)
	}
	prior.FormulaVersion = strings.TrimSpace(prior.FormulaVersion)
	if prior.FormulaVersion == "" {
		return "", nil, nil
	}
	if prior.FormulaVersion == strings.TrimSpace(nextFormula) {
		return "", prior.PriorFormulaVersions, nil
	}
	archive := strings.TrimSuffix(path, filepath.Ext(path)) + "." + safeFormulaFilename(prior.FormulaVersion) + filepath.Ext(path)
	if info, err := os.Lstat(archive); err == nil {
		if !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("prior LLAMBO score snapshot archive is not a regular file: %s", archive)
		}
		if err := validateArchivedScoreSnapshot(archive, prior.FormulaVersion, data); err != nil {
			return "", nil, err
		}
		return prior.FormulaVersion, prior.PriorFormulaVersions, nil
	} else if !os.IsNotExist(err) {
		return "", nil, fmt.Errorf("stat prior LLAMBO score snapshot archive: %w", err)
	}
	if err := writeArchiveAtomically(archive, data); err != nil {
		if errors.Is(err, os.ErrExist) {
			if validateErr := validateArchivedScoreSnapshot(archive, prior.FormulaVersion, data); validateErr != nil {
				return "", nil, validateErr
			}
			return prior.FormulaVersion, prior.PriorFormulaVersions, nil
		}
		return "", nil, err
	}
	if err := validateArchivedScoreSnapshot(archive, prior.FormulaVersion, data); err != nil {
		return "", nil, err
	}
	return prior.FormulaVersion, prior.PriorFormulaVersions, nil
}

func writeArchiveAtomically(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".llambo-score-archive-*.tmp")
	if err != nil {
		return fmt.Errorf("create prior LLAMBO score snapshot archive temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("set prior LLAMBO score snapshot archive permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write prior LLAMBO score snapshot archive: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close prior LLAMBO score snapshot archive: %w", err)
	}
	if err := os.Link(tmpPath, path); err != nil {
		return err
	}
	return nil
}

func validateArchivedScoreSnapshot(path, expectedFormula string, expectedBytes []byte) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("lstat prior LLAMBO score snapshot archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("validate prior LLAMBO score snapshot archive: not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read prior LLAMBO score snapshot archive: %w", err)
	}
	if !bytes.Equal(data, expectedBytes) {
		return fmt.Errorf("validate prior LLAMBO score snapshot archive: contents do not match active predecessor")
	}
	archive, err := LoadOMLXScoreSnapshot(path)
	if err != nil {
		return fmt.Errorf("validate prior LLAMBO score snapshot archive: %w", err)
	}
	if archive == nil || strings.TrimSpace(archive.FormulaVersion) == "" || archive.FormulaVersion != expectedFormula {
		return fmt.Errorf("validate prior LLAMBO score snapshot archive: expected formula %q", expectedFormula)
	}
	return nil
}

func retainedFormulaVersions(immediate string, existing []string) []string {
	result := make([]string, 0, len(existing)+1)
	seen := make(map[string]struct{}, len(existing)+1)
	appendVersion := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	appendVersion(immediate)
	for _, value := range existing {
		appendVersion(value)
	}
	return result
}

func safeFormulaFilename(value string) string {
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	return value
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
	return OMLXScoreSnapshot{SchemaVersion: OMLXScoreSnapshotVersion, GeneratedAt: generatedAt.UTC(), FormulaVersion: CategoryFormulaVersion, PopulationFingerprint: fingerprint, Inventory: inventory, Endpoint: strings.TrimSpace(endpoint), CategoryScores: scores, PriorFormulaVersions: []string{"LLAMBO-6-category-v4", "LLAMBO-6-category-v3", "LLAMBO-6-category-v2", "LLAMBO-6-category-v1", OverallFormulaVersion}}
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
