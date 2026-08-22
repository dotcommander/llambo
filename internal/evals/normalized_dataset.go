package evals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"
)

const NormalizedDatasetSchemaVersion = "llambo-normalized-evals-v1"

type NormalizedDatasetManifest struct {
	SchemaVersion         string                               `json:"schema_version"`
	GeneratedAt           time.Time                            `json:"generated_at"`
	FormulaVersion        string                               `json:"formula_version"`
	SourceRegistry        string                               `json:"source_registry_version"`
	EditorialRanking      string                               `json:"editorial_ranking"`
	SemanticDeduplication string                               `json:"semantic_deduplication"`
	RowCounts             map[string]int                       `json:"row_counts"`
	Artifacts             map[string]NormalizedDatasetArtifact `json:"artifacts"`
}

type NormalizedDatasetArtifact struct {
	SHA256 string `json:"sha256"`
	Rows   int    `json:"rows"`
}

type NormalizedSourceRecord struct {
	SourceID           string    `json:"source_id"`
	Name               string    `json:"name"`
	URL                string    `json:"url,omitempty"`
	Cache              string    `json:"cache"`
	FetchedAt          time.Time `json:"fetched_at,omitempty"`
	Models             int       `json:"models"`
	Observations       int       `json:"observations"`
	OperationalMetrics int       `json:"operational_metrics"`
	ScoreContributions int       `json:"score_contributions"`
	FrozenCohorts      int       `json:"frozen_cohorts"`
	Version            string    `json:"version,omitempty"`
	CommitSHA          string    `json:"commit_sha,omitempty"`
	ContentSHA256      string    `json:"content_sha256,omitempty"`
	Methodology        string    `json:"methodology,omitempty"`
	RegistryVersion    string    `json:"source_registry_version,omitempty"`
	EvidenceGrade      string    `json:"evidence_grade,omitempty"`
	Error              string    `json:"error,omitempty"`
	Roles              []string  `json:"roles"`
}

type NormalizedModelRecord struct {
	Key           string          `json:"key"`
	Name          string          `json:"name"`
	Organization  string          `json:"organization,omitempty"`
	IdentityMatch IdentityMatch   `json:"identity_match"`
	Open          *bool           `json:"open,omitempty"`
	License       string          `json:"license,omitempty"`
	Context       *int64          `json:"context,omitempty"`
	RowKind       string          `json:"row_kind"`
	Projection    *ProjectionInfo `json:"projection,omitempty"`
}

type NormalizedModelIdentityRecord struct {
	SourceID           string        `json:"source_id"`
	SourceModelKey     string        `json:"source_model_key"`
	SourceModelName    string        `json:"source_model_name"`
	Organization       string        `json:"organization,omitempty"`
	NormalizedIdentity string        `json:"normalized_identity"`
	CanonicalModelKey  string        `json:"canonical_model_key,omitempty"`
	IdentityMatch      IdentityMatch `json:"identity_match"`
}

type NormalizedObservationMirror struct {
	SourceID       string `json:"source_id"`
	SourceClass    string `json:"source_class,omitempty"`
	SourceRevision string `json:"source_revision,omitempty"`
	ContentSHA256  string `json:"content_sha256,omitempty"`
	Locator        string `json:"provenance_locator,omitempty"`
	EvidenceGrade  string `json:"evidence_grade,omitempty"`
}

type NormalizedSourceObservationRecord struct {
	SemanticFingerprint    string                        `json:"semantic_fingerprint"`
	SourceID               string                        `json:"source_id"`
	ResultSourceID         string                        `json:"result_source_id,omitempty"`
	ResultSourceClass      string                        `json:"result_source_class,omitempty"`
	EditorialSourceClass   string                        `json:"editorial_source_class"`
	SourceModelKey         string                        `json:"source_model_key"`
	SourceModelName        string                        `json:"source_model_name"`
	Organization           string                        `json:"organization,omitempty"`
	CanonicalModelKey      string                        `json:"canonical_model_key,omitempty"`
	Benchmark              string                        `json:"benchmark"`
	BenchmarkVersion       string                        `json:"benchmark_version,omitempty"`
	RawScore               *float64                      `json:"raw_score"`
	Unit                   string                        `json:"unit,omitempty"`
	Direction              string                        `json:"direction,omitempty"`
	IdentityMatch          IdentityMatch                 `json:"identity_match"`
	ResultEvidenceGrade    string                        `json:"result_evidence_grade,omitempty"`
	EditorialEvidenceGrade string                        `json:"editorial_evidence_grade"`
	SourceRevision         string                        `json:"source_revision,omitempty"`
	ContentSHA256          string                        `json:"content_sha256,omitempty"`
	URL                    string                        `json:"url,omitempty"`
	CommitSHA              string                        `json:"commit_sha,omitempty"`
	Methodology            string                        `json:"methodology,omitempty"`
	JudgeVersion           string                        `json:"judge_version,omitempty"`
	FetchedAt              time.Time                     `json:"fetched_at,omitempty"`
	Locator                string                        `json:"provenance_locator,omitempty"`
	SampleSize             int                           `json:"sample_size,omitempty"`
	CandidateCount         int                           `json:"candidate_count"`
	SelectedBy             string                        `json:"selected_by"`
	Mirrors                []NormalizedObservationMirror `json:"mirrors,omitempty"`
	AdmissionStatus        string                        `json:"admission_status"`
	AdmissionReason        string                        `json:"admission_reason,omitempty"`
	AffectsCapability      bool                          `json:"affects_capability"`
}

type NormalizedScoreRecord struct {
	Category         string   `json:"category"`
	EditorialRank    int      `json:"editorial_rank"`
	ModelKey         string   `json:"model_key"`
	ModelName        string   `json:"model_name"`
	RowKind          string   `json:"row_kind"`
	Score            *float64 `json:"score"`
	Coverage         float64  `json:"coverage,omitempty"`
	TrustedCoverage  float64  `json:"trusted_coverage,omitempty"`
	Confidence       string   `json:"confidence,omitempty"`
	Families         []string `json:"families,omitempty"`
	Stale            bool     `json:"stale,omitempty"`
	Estimated        bool     `json:"estimated,omitempty"`
	EstimateMethod   string   `json:"estimate_method,omitempty"`
	UnresolvedReason string   `json:"unresolved_reason,omitempty"`
	WinnerStatus     string   `json:"winner_status,omitempty"`
}

type NormalizedScoreContributionRecord struct {
	Category           string   `json:"category"`
	ModelKey           string   `json:"model_key"`
	RowKind            string   `json:"row_kind"`
	BenchmarkFamily    string   `json:"benchmark_family"`
	Benchmark          string   `json:"benchmark"`
	RawScore           *float64 `json:"raw_score"`
	Percentile         *float64 `json:"percentile"`
	FrozenPopulation   int      `json:"frozen_population"`
	EvidenceGrade      string   `json:"evidence_grade,omitempty"`
	SourceID           string   `json:"source_id"`
	SourceClass        string   `json:"source_class,omitempty"`
	SourceRevision     string   `json:"source_revision,omitempty"`
	ContentSHA256      string   `json:"content_sha256,omitempty"`
	NominalWeight      float64  `json:"nominal_weight,omitempty"`
	EvidenceMultiplier float64  `json:"evidence_multiplier,omitempty"`
	IdentityMultiplier float64  `json:"identity_multiplier,omitempty"`
	EffectiveWeight    float64  `json:"effective_weight,omitempty"`
}

type NormalizedOperationalMetricRecord struct {
	SourceID          string   `json:"source_id"`
	ModelKey          string   `json:"model_key"`
	Metric            string   `json:"metric"`
	Value             *float64 `json:"value"`
	Unit              string   `json:"unit,omitempty"`
	AffectsCapability bool     `json:"affects_capability"`
}

type NormalizedProjectionRecord struct {
	ArtifactKey     string  `json:"artifact_key"`
	SourceKey       string  `json:"source_key"`
	Confidence      string  `json:"confidence"`
	Basis           string  `json:"basis"`
	ReviewedAt      string  `json:"reviewed_at"`
	ScoreMultiplier float64 `json:"score_multiplier"`
}

type NormalizedFrozenCohortRecord struct {
	Benchmark  string    `json:"benchmark"`
	Revision   string    `json:"revision"`
	Population int       `json:"population"`
	Values     []float64 `json:"values"`
	SourceIDs  []string  `json:"source_ids"`
}

type NormalizedDriftDiagnosticRecord struct {
	SourceID             string `json:"source_id,omitempty"`
	Kind                 string `json:"kind"`
	Message              string `json:"message"`
	ReferenceFingerprint string `json:"reference_fingerprint,omitempty"`
	CurrentFingerprint   string `json:"current_fingerprint,omitempty"`
}

func WriteNormalizedDataset(result Result, report Report, outputDir string) (*NormalizedDatasetManifest, error) {
	dataset := buildNormalizedDataset(result, report)
	files := map[string][]byte{}
	counts := map[string]int{}
	for name, rows := range map[string]any{
		"sources.jsonl":             dataset.Sources,
		"models.jsonl":              dataset.Models,
		"model_identities.jsonl":    dataset.ModelIdentities,
		"source_observations.jsonl": dataset.SourceObservations,
		"scores.jsonl":              dataset.Scores,
		"score_contributions.jsonl": dataset.ScoreContributions,
		"operational_metrics.jsonl": dataset.OperationalMetrics,
		"projections.jsonl":         dataset.Projections,
		"frozen_cohorts.jsonl":      dataset.FrozenCohorts,
		"drift_diagnostics.jsonl":   dataset.DriftDiagnostics,
	} {
		data, count, err := encodeNormalizedJSONL(rows)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", name, err)
		}
		files[name], counts[name] = data, count
	}
	artifacts := make(map[string]NormalizedDatasetArtifact, len(files))
	for name, data := range files {
		artifacts[name] = NormalizedDatasetArtifact{SHA256: sha256Hex(data), Rows: counts[name]}
	}
	manifest := &NormalizedDatasetManifest{
		SchemaVersion:         NormalizedDatasetSchemaVersion,
		GeneratedAt:           report.GeneratedAt,
		FormulaVersion:        report.FormulaVersion,
		SourceRegistry:        SourceRegistryVersion,
		EditorialRanking:      "category score descending, trusted coverage descending, identity confidence descending, key ascending",
		SemanticDeduplication: "identity+benchmark+revision+direction+score; source authority selects one row and retains peers as mirrors",
		RowCounts:             counts,
		Artifacts:             artifacts,
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode normalized dataset manifest: %w", err)
	}
	files["manifest.json"] = append(manifestData, '\n')
	if err := writeNormalizedDatasetDirectory(outputDir, files); err != nil {
		return nil, err
	}
	return manifest, nil
}

func encodeNormalizedJSONL(rows any) ([]byte, int, error) {
	value := reflect.ValueOf(rows)
	for value.Kind() == reflect.Pointer && !value.IsNil() {
		value = value.Elem()
	}
	if value.Kind() != reflect.Slice {
		return nil, 0, fmt.Errorf("normalized rows must be a slice")
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	for i := 0; i < value.Len(); i++ {
		if err := encoder.Encode(value.Index(i).Interface()); err != nil {
			return nil, 0, err
		}
	}
	return output.Bytes(), value.Len(), nil
}

func writeNormalizedDatasetDirectory(outputDir string, files map[string][]byte) error {
	clean := filepath.Clean(outputDir)
	if clean == "" || clean == "." || filepath.Dir(clean) == clean {
		return fmt.Errorf("--output-dir must be an explicit new directory")
	}
	if _, err := os.Stat(clean); err == nil {
		return fmt.Errorf("normalized dataset output directory already exists: %s", clean)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect normalized dataset output directory: %w", err)
	}
	parent := filepath.Dir(clean)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create normalized dataset parent: %w", err)
	}
	temp, err := os.MkdirTemp(parent, "."+filepath.Base(clean)+"-*")
	if err != nil {
		return fmt.Errorf("create normalized dataset temp directory: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(temp)
		}
	}()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if filepath.Base(name) != name {
			return fmt.Errorf("normalized dataset artifact must not contain a path: %s", name)
		}
		if err := os.WriteFile(filepath.Join(temp, name), files[name], 0o600); err != nil {
			return fmt.Errorf("write normalized dataset artifact %s: %w", name, err)
		}
	}
	if err := os.Rename(temp, clean); err != nil {
		return fmt.Errorf("publish normalized dataset: %w", err)
	}
	complete = true
	return nil
}
