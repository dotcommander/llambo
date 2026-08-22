package evals

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

const SourceRegistryVersion = "LLAMBO-6-sources-v3"
const CategoryFormulaVersion = "LLAMBO-6-category-v5"

type SourceClass string

const (
	SourceOwnerResult      SourceClass = "owner_result"
	SourceFirstPartyResult SourceClass = "first_party_result"
	SourceOfficialJudgment SourceClass = "official_judgments_votes"
	SourceAggregatorResult SourceClass = "aggregator_result"
	SourceAggregatorLead   SourceClass = "aggregator_lead"
	SourceDefinitionOnly   SourceClass = "definition_only"
)

type SourceRegistration struct {
	ID       string      `json:"id"`
	Class    SourceClass `json:"class"`
	Public   bool        `json:"public"`
	Eligible bool        `json:"eligible"`
}

var sourceRegistryV3 = []SourceRegistration{
	{"writingbench", SourceOwnerResult, true, true},
	{"eqbench-creative-v3", SourceOwnerResult, true, true},
	{"ifeval-official", SourceFirstPartyResult, true, true},
	{"llm-stats-models", SourceAggregatorLead, true, false},
	{"llm-stats-full-results", SourceAggregatorLead, true, false},
	{"llm-stats-indexes", SourceAggregatorLead, true, false},
	{"llm-stats-benchmark-catalog", SourceDefinitionOnly, true, false},
	{"llm-stats-benchmark-results", SourceAggregatorResult, true, true},
	{"hugging-face-leaderboards", SourceFirstPartyResult, true, true},
	{"hugging-face-model-cards", SourceFirstPartyResult, true, true},
	{"swe-bench", SourceOwnerResult, true, true},
	{"livecodebench", SourceOwnerResult, true, true},
	{"bfcl", SourceOwnerResult, true, true},
	{"gaia", SourceOwnerResult, true, true},
	{"tau-bench", SourceOwnerResult, true, true},
	{"ruler", SourceOwnerResult, true, true},
	{"liquidai-lfm25-2.6b-card", SourceFirstPartyResult, true, true},
	{"qwen3.8-27b-card", SourceFirstPartyResult, true, true},
	{"openai-gpt-oss-model-card", SourceFirstPartyResult, true, true},
	{"liquidai-lfm25-vl-3b-card", SourceFirstPartyResult, true, true},
	{"google-gemma4-model-card", SourceFirstPartyResult, true, true},
}

func SourceRegistry() []SourceRegistration {
	return append([]SourceRegistration(nil), sourceRegistryV3...)
}

// registeredSourceEligible is the scorer's source-admission boundary. Cache
// rows remain inspectable even when their source is unknown or not reviewed,
// but only the exact reviewed source ID/class pair may contribute a score.
func registeredSourceEligible(result BenchmarkResult) bool {
	for _, source := range sourceRegistryV3 {
		if source.ID == result.SourceID && string(source.Class) == result.SourceClass {
			return source.Eligible
		}
	}
	return false
}

// BenchmarkRegistration is deliberately separate from ingest adapters. It is
// the small reviewed admission boundary used by the offline scorer; unlisted
// cached observations remain inspectable but cannot alter a score.
type BenchmarkRegistration struct {
	Benchmark          string `json:"benchmark"`
	Category           string `json:"category"`
	Family             string `json:"family"`
	MinPopulation      int    `json:"min_population"`
	CompatibleRevision string `json:"compatible_revision"`
}

func BenchmarkRegistry() []BenchmarkRegistration {
	entries := make([]BenchmarkRegistration, 0)
	for _, category := range categorySpecs {
		for _, family := range category.families {
			for _, benchmark := range family.benchmarks {
				entries = append(entries, BenchmarkRegistration{Benchmark: benchmark, Category: category.name, Family: family.name, MinPopulation: 5, CompatibleRevision: "exact"})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Category != entries[j].Category {
			return entries[i].Category < entries[j].Category
		}
		if entries[i].Family != entries[j].Family {
			return entries[i].Family < entries[j].Family
		}
		return entries[i].Benchmark < entries[j].Benchmark
	})
	return entries
}

func IsReviewedBenchmark(category, family, benchmark string) bool {
	for _, entry := range BenchmarkRegistry() {
		if entry.Category == category && entry.Family == family && entry.Benchmark == benchmark {
			return true
		}
	}
	return false
}

type Observation struct {
	SourceID         string      `json:"source_id"`
	SourceRevision   string      `json:"source_revision"`
	SourceSHA256     string      `json:"source_sha256"`
	Benchmark        string      `json:"benchmark"`
	BenchmarkVersion string      `json:"benchmark_version"`
	Cohort           string      `json:"cohort"`
	ModelID          string      `json:"model_id"`
	OrganizationID   string      `json:"organization_id,omitempty"`
	RawScore         float64     `json:"raw_score"`
	Unit             string      `json:"unit"`
	Direction        string      `json:"direction"`
	Methodology      string      `json:"methodology"`
	Judge            string      `json:"judge,omitempty"`
	SampleSize       int         `json:"sample_size,omitempty"`
	EvidenceGrade    string      `json:"evidence_grade"`
	FetchedAt        time.Time   `json:"fetched_at"`
	Locator          string      `json:"provenance_locator"`
	SourceClass      SourceClass `json:"source_class"`
}

func (o Observation) Validate() error {
	for name, value := range map[string]string{"source_id": o.SourceID, "source_revision": o.SourceRevision, "source_sha256": o.SourceSHA256, "benchmark": o.Benchmark, "benchmark_version": o.BenchmarkVersion, "cohort": o.Cohort, "model_id": o.ModelID, "unit": o.Unit, "direction": o.Direction, "methodology": o.Methodology, "evidence_grade": o.EvidenceGrade, "provenance_locator": o.Locator} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("missing %s", name)
		}
	}
	if !validSHA256(o.SourceSHA256) {
		return errors.New("source_sha256 is not a SHA-256 digest")
	}
	if math.IsNaN(o.RawScore) || math.IsInf(o.RawScore, 0) {
		return errors.New("raw_score is not finite")
	}
	if o.Direction != "higher" && o.Direction != "lower" {
		return fmt.Errorf("unknown direction %q", o.Direction)
	}
	if o.FetchedAt.IsZero() {
		return errors.New("missing fetched_at")
	}
	return nil
}

func DecodeObservationsJSONL(r io.Reader, maxRowBytes int) ([]Observation, error) {
	if maxRowBytes <= 0 {
		maxRowBytes = 1 << 20
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), maxRowBytes)
	observations := make([]Observation, 0)
	seen := make(map[string]Observation)
	for line := 1; scanner.Scan(); line++ {
		var observation Observation
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&observation); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if err := observation.Validate(); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		key := strings.Join([]string{observation.SourceID, observation.SourceRevision, observation.Benchmark, observation.BenchmarkVersion, observation.Cohort, observation.ModelID}, "\x00")
		if prior, ok := seen[key]; ok {
			if prior.RawScore != observation.RawScore || prior.SourceSHA256 != observation.SourceSHA256 {
				return nil, fmt.Errorf("line %d: conflicting duplicate observation", line)
			}
		}
		seen[key] = observation
		observations = append(observations, observation)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan observations: %w", err)
	}
	sort.Slice(observations, func(i, j int) bool {
		return observationLess(observations[i], observations[j])
	})
	return observations, nil
}

func observationLess(left, right Observation) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) < string(rightJSON)
}

func SealBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
