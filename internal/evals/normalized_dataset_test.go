package evals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizedDatasetSemanticDeduplicationAndEditorialReranking(t *testing.T) {
	result, report := normalizedDatasetFixture()
	dataset := buildNormalizedDataset(result, report)

	if len(dataset.SourceObservations) != 1 {
		t.Fatalf("semantic duplicates were not collapsed: %#v", dataset.SourceObservations)
	}
	observation := dataset.SourceObservations[0]
	if observation.SourceID != "writingbench" || observation.CandidateCount != 2 || observation.AffectsCapability != true {
		t.Fatalf("unexpected editorially selected observation: %#v", observation)
	}
	if len(observation.Mirrors) != 1 || observation.Mirrors[0].SourceID != "llm-stats" {
		t.Fatalf("deduplicated peer was not retained as a mirror: %#v", observation.Mirrors)
	}
	if observation.ResultSourceID != "writingbench" || observation.ResultEvidenceGrade != "owner" || observation.EditorialEvidenceGrade != "owner" {
		t.Fatalf("lower-authority aggregator outranked benchmark owner: %#v", observation)
	}

	var rankedModel string
	for _, score := range dataset.Scores {
		if score.Category == "writing" && score.EditorialRank == 1 {
			rankedModel = score.ModelKey
			break
		}
	}
	if rankedModel != "model-a" {
		t.Fatalf("editorial score rank did not follow score descending: %#v", dataset.Scores)
	}
	if dataset.Scores[0].Category != "agents" || dataset.Scores[len(dataset.Scores)-1].Category != "writing" {
		t.Fatalf("category order was not stable: %#v", dataset.Scores)
	}
	ranksByCategory := make(map[string][]int)
	for _, score := range dataset.Scores {
		ranksByCategory[score.Category] = append(ranksByCategory[score.Category], score.EditorialRank)
	}
	for category, ranks := range ranksByCategory {
		if len(ranks) != 2 || ranks[0] != 1 || ranks[1] != 2 {
			t.Fatalf("editorial ranks are not unique and ordered for %s: %#v", category, ranks)
		}
	}
}

func TestTaggedSourceModelsPreserveSourceNativeRows(t *testing.T) {
	score := 10.0
	model := Model{Key: "source", Benchmarks: map[string]BenchmarkResult{
		"gpqa": {Score: &score, SourceID: "source"},
	}}
	rows := taggedSourceModels([]sourceModelGroup{{sourceID: "source", models: []Model{model}}})
	model.Benchmarks["gpqa"] = BenchmarkResult{SourceID: "mutated-after-capture"}
	if rows[0].Model.Benchmarks["gpqa"].SourceID != "source" {
		t.Fatalf("merge mutation leaked into source-native rows: %#v", rows[0].Model)
	}
}

func TestWriteNormalizedDatasetWritesManifestAndHashedArtifacts(t *testing.T) {
	result, report := normalizedDatasetFixture()
	dir := filepath.Join(t.TempDir(), "dataset")
	manifest, err := WriteNormalizedDataset(result, report, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"manifest.json", "sources.jsonl", "models.jsonl", "model_identities.jsonl", "source_observations.jsonl",
		"scores.jsonl", "score_contributions.jsonl", "operational_metrics.jsonl", "projections.jsonl",
		"frozen_cohorts.jsonl", "drift_diagnostics.jsonl",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("artifact %s: %v", name, err)
		}
		if name == "manifest.json" {
			continue
		}
		if artifact, ok := manifest.Artifacts[name]; !ok || artifact.SHA256 == "" {
			t.Fatalf("manifest missing artifact %s: %#v", name, manifest.Artifacts)
		}
	}
	if manifest.SchemaVersion != NormalizedDatasetSchemaVersion || manifest.RowCounts["scores.jsonl"] != 12 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	if _, err := WriteNormalizedDataset(result, report, dir); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing output directory was not rejected: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "source_observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("unexpected JSONL row count %d: %s", len(lines), data)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatal(err)
	}
	if row["semantic_fingerprint"] == "" || row["candidate_count"].(float64) != 2 {
		t.Fatalf("unexpected normalized JSONL: %s", data)
	}
}

func normalizedDatasetFixture() (Result, Report) {
	scoreA, scoreB := 82.0, 64.0
	owner := sealedBenchmark(scoreA)
	owner.Version = "wb-v1"
	owner.SourceID = "writingbench"
	owner.SourceClass = string(SourceOwnerResult)
	owner.EvidenceGrade = "owner"
	owner.SourceRevision = "wb-v1"
	owner.Identity = IdentityMatchExact
	aggregator := sealedBenchmark(scoreA)
	aggregator.Version = "wb-v1"
	aggregator.SourceID = "llm-stats-benchmark-results"
	aggregator.SourceClass = string(SourceAggregatorResult)
	aggregator.EvidenceGrade = "aggregator_self_reported"
	aggregator.SourceRevision = "wb-v1"
	aggregator.Identity = IdentityMatchExact

	context := int64(128000)
	open := true
	result := Result{
		GeneratedAt: time.Unix(1, 0).UTC(),
		Models: []Model{{
			Key: "model-a", Name: "Model A", Organization: "Lab", License: "Apache-2.0", Open: &open,
			Context: &context, Benchmarks: map[string]BenchmarkResult{"writingbench": owner},
		}, {
			Key: "model-b", Name: "Model B", Organization: "Lab",
			Benchmarks: map[string]BenchmarkResult{"writingbench": aggregator},
		}},
		SourceModels: []SourceModel{
			{SourceID: "llm-stats", Model: Model{Key: "model-a", Name: "Model A", Organization: "Lab", Benchmarks: map[string]BenchmarkResult{"writingbench": aggregator}}},
			{SourceID: "writingbench", Model: Model{Key: "writingbench:model-a", Name: "Model A", Organization: "Lab", Benchmarks: map[string]BenchmarkResult{"writingbench": owner}}},
		},
		Sources: []SourceStatus{
			{Name: "LLM Stats", Cache: "cached"},
			{Name: "WritingBench", Cache: "cached"},
		},
	}
	writingA := &LlamboScore{Score: scoreA, Coverage: 1, TrustedCoverage: 1, Confidence: "high", Families: []string{"rubric-long-form"}}
	writingB := &LlamboScore{Score: scoreB, Coverage: 1, TrustedCoverage: .5, Confidence: "low", Families: []string{"rubric-long-form"}}
	writingA.Contributions = []benchmarkEvidence{{
		Benchmark: "writingbench", Family: "rubric-long-form", RawScore: &scoreA,
		Percentile: &scoreA, ReferencePopulation: 5, EvidenceGrade: "owner",
		SourceID: "writingbench", SourceRevision: "wb-v1",
	}}
	report := Report{
		GeneratedAt: result.GeneratedAt, FormulaVersion: "test-formula", RankingProfile: "matrix",
		Models: []ReportModel{
			{Key: "model-b", Name: "Model B", Organization: "Lab", IdentityMatch: IdentityMatchExact, LlamboScores: map[string]*LlamboScore{"writing": writingB}, UnresolvedReasons: map[string]string{}},
			{Key: "model-a", Name: "Model A", Organization: "Lab", IdentityMatch: IdentityMatchExact, LlamboScores: map[string]*LlamboScore{"writing": writingA}, UnresolvedReasons: map[string]string{}, Benchmarks: map[string]BenchmarkResult{"writingbench": owner}},
		},
	}
	report.CategoryRankings = buildCategoryRankings(report.Models)
	applyCategoryWinnerStatuses(report.Models, report.CategoryRankings)
	return result, report
}
