package evals

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadFrozenLLMStatsObservationArtifactValidatesAndAttachesTargets(t *testing.T) {
	data, modelID := frozenLLMStatsObservationFixture(t)
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	observations, err := readFrozenLLMStatsObservationArtifact(path, SealBytes(data), llmStatsFrozenCohortObservationRows)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 267 {
		t.Fatalf("validated target observations = %d, want 267", len(observations))
	}
	models := []Model{{Key: modelID, Benchmarks: map[string]BenchmarkResult{}}}
	attachLLMStatsBenchmarkLeads(models, observations)
	result, ok := models[0].Benchmarks["longbench-v2"]
	if !ok || result.Score == nil || result.Identity != IdentityMatchExact || result.SourceRevision == "" || result.SourceRevision != result.ContentSHA {
		t.Fatalf("frozen target was not attached with exact provenance: %#v", models[0])
	}
}

func TestReadFrozenLLMStatsObservationArtifactRejectsDigestMismatch(t *testing.T) {
	data, _ := frozenLLMStatsObservationFixture(t)
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readFrozenLLMStatsObservationArtifact(path, strings.Repeat("0", 64), llmStatsFrozenCohortObservationRows); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("digest mismatch was accepted: %v", err)
	}
}

func TestFrozenLLMStatsObservationValidationRejectsChangedPopulation(t *testing.T) {
	data, _ := frozenLLMStatsObservationFixture(t)
	observations, err := DecodeObservationsJSONL(strings.NewReader(string(data)), maxSourceBody)
	if err != nil {
		t.Fatal(err)
	}
	for i := range observations {
		if observations[i].Benchmark == "math" {
			observations[i].RawScore += .001
			break
		}
	}
	if _, err := validateFrozenLLMStatsObservationArtifact(observations); err == nil || !strings.Contains(err.Error(), "score population") {
		t.Fatalf("changed target population was accepted: %v", err)
	}
}

func TestFrozenLLMStatsObservationValidationRejectsDuplicateTargetModelID(t *testing.T) {
	data, _ := frozenLLMStatsObservationFixture(t)
	observations, err := DecodeObservationsJSONL(strings.NewReader(string(data)), maxSourceBody)
	if err != nil {
		t.Fatal(err)
	}
	first := -1
	for index := range observations {
		if observations[index].Benchmark != "math" {
			continue
		}
		if first < 0 {
			first = index
			continue
		}
		observations[index].ModelID = observations[first].ModelID
		break
	}
	if _, err := validateFrozenLLMStatsObservationArtifact(observations); err == nil || !strings.Contains(err.Error(), "duplicate model ID") {
		t.Fatalf("duplicate target model ID was accepted: %v", err)
	}
}

func TestPrepareFrozenLLMStatsCohortResultsStripsRefreshedTargetWithoutPinnedArtifact(t *testing.T) {
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		t.Fatal(err)
	}
	refreshed := frozenLLMStatsResult(cohorts.Benchmarks["longbench-v2"])
	models := []Model{{Key: "refreshed", Benchmarks: map[string]BenchmarkResult{"longbench-v2": refreshed}}}
	if err := prepareFrozenLLMStatsCohortResults(models, t.TempDir()); err == nil || !strings.Contains(err.Error(), "sealed LLM Stats observations") {
		t.Fatalf("missing pinned artifact was accepted: %v", err)
	}
	if _, ok := models[0].Benchmarks["longbench-v2"]; ok {
		t.Fatalf("refreshed target bypassed pinned artifact gate: %#v", models[0])
	}
}

func TestRemoveFrozenLLMStatsCohortResultsRetainsStatsV1Replacement(t *testing.T) {
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		t.Fatal(err)
	}
	legacy := frozenLLMStatsResult(cohorts.Benchmarks["math"])
	statsV1 := legacy
	statsV1.SourceID = llmStatsStatsV1SourceID
	statsV1.Version = llmStatsStatsV1BenchmarkVersion
	statsV1.Cohort = llmStatsStatsV1Cohort
	statsV1.Method = llmStatsStatsV1Methodology
	model := Model{Benchmarks: map[string]BenchmarkResult{"math": statsV1}}
	removeFrozenLLMStatsCohortResults([]Model{model})
	if result := model.Benchmarks["math"]; result.SourceID != llmStatsStatsV1SourceID {
		t.Fatalf("Stats v1 replacement was stripped with legacy cohort: %#v", result)
	}
}

func TestFetchCachedOnlyAttachesFrozenTargetsAndReportsMissingArtifact(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		t.Fatal(err)
	}
	legacyTarget := frozenLLMStatsResult(cohorts.Benchmarks["longbench-v2"])
	if err := writeSnapshot(filepath.Join(dir, "llm-stats.json"), sourceSnapshot{FetchedAt: now, Models: []Model{{Key: "longbench-v2-000", Name: "Target", LLMStats: &LLMStatsMetrics{}, Benchmarks: map[string]BenchmarkResult{"longbench-v2": legacyTarget}}}}); err != nil {
		t.Fatal(err)
	}
	if err := writeSnapshot(filepath.Join(dir, "artificial-analysis.json"), sourceSnapshot{FetchedAt: now, Models: []Model{}}); err != nil {
		t.Fatal(err)
	}
	opts := Options{CacheDir: dir, Now: func() time.Time { return now }}
	result, err := fetchCachedOnly(opts, sourceSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	status := sourceStatusNamed(t, result.Sources, "LLM Stats frozen cohorts")
	if _, ok := result.Models[0].Benchmarks["longbench-v2"]; ok || status.Cache != "unavailable" || !strings.Contains(status.Error, "frozen LLM Stats cohorts unavailable") || len(result.Models) != 1 {
		t.Fatalf("missing sealed artifact did not fail closed while preserving cached models: %#v %#v", status, result.Models)
	}
	artifactPath := llmStatsFrozenCohortArtifactPath(dir)
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = fetchCachedOnly(opts, sourceSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Models[0].Benchmarks["longbench-v2"]; ok || !strings.Contains(sourceStatusNamed(t, result.Sources, "LLM Stats frozen cohorts").Error, "digest") {
		t.Fatalf("corrupt sealed artifact did not fail closed: %#v", result)
	}

	data, _ := frozenLLMStatsObservationFixture(t)
	observations, err := DecodeObservationsJSONL(strings.NewReader(string(data)), maxSourceBody)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := validateFrozenLLMStatsObservationArtifact(observations)
	if err != nil {
		t.Fatal(err)
	}
	previous := loadFrozenLLMStatsCachedObservationsSource
	loadFrozenLLMStatsCachedObservationsSource = func(string) ([]Observation, error) { return targets, nil }
	t.Cleanup(func() { loadFrozenLLMStatsCachedObservationsSource = previous })
	result, err = fetchCachedOnly(opts, sourceSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if benchmark := result.Models[0].Benchmarks["longbench-v2"]; benchmark.Score == nil || benchmark.Identity != IdentityMatchExact {
		t.Fatalf("cache-only fetch did not attach frozen target: %#v", result.Models[0])
	}
	if status := sourceStatusNamed(t, result.Sources, "LLM Stats frozen cohorts"); status.Cache != "sealed" || status.Error != "" || status.RegistryVersion != SourceRegistryVersion || status.ContentSHA != llmStatsFrozenCohortsArtifactDigest || status.Observations != llmStatsFrozenCohortObservationRows {
		t.Fatalf("attached frozen artifact status = %#v", status)
	}
}

func TestFrozenLLMStatsCohortStatusChangesReportFingerprint(t *testing.T) {
	sealed := Report{Sources: []SourceStatus{frozenLLMStatsCohortStatus(nil)}}
	unavailable := Report{Sources: []SourceStatus{frozenLLMStatsCohortStatus(fmt.Errorf("missing"))}}
	if reportCacheFingerprint(sealed) == reportCacheFingerprint(unavailable) {
		t.Fatal("sealed and unavailable frozen cohorts share a report fingerprint")
	}
}

func TestFrozenLLMStatsCohortAdmissionReasonsAreSpecific(t *testing.T) {
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		t.Fatal(err)
	}
	result := frozenLLMStatsResult(cohorts.Benchmarks["math"])
	result.Method = "other"
	if got := frozenLLMStatsCohortAdmissionReason("math", result); got != "frozen LLM Stats cohort requires the pinned methodology" {
		t.Fatalf("methodology reason = %q", got)
	}
	result = frozenLLMStatsResult(cohorts.Benchmarks["math"])
	result.SourceRevision, result.ContentSHA = strings.Repeat("f", 64), strings.Repeat("f", 64)
	if got := frozenLLMStatsCohortAdmissionReason("math", result); got != "frozen LLM Stats cohort source revision is outside the pinned digest set" {
		t.Fatalf("revision reason = %q", got)
	}
	result = frozenLLMStatsResult(cohorts.Benchmarks["math"])
	result.Identity = IdentityMatchNormalized
	if got := frozenLLMStatsCohortAdmissionReason("math", result); got != "frozen LLM Stats cohort requires exact identity" {
		t.Fatalf("identity reason = %q", got)
	}
}

func TestBuildReportCommonCohortSupersedesSourceNativeScale(t *testing.T) {
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		t.Fatal(err)
	}
	result := frozenLLMStatsResult(cohorts.Benchmarks["math"])
	result.Method = "other"
	report, err := BuildReport(Result{GeneratedAt: time.Unix(1, 0).UTC(), Models: []Model{{Key: "target", Name: "Target", Benchmarks: map[string]BenchmarkResult{"math": result}}}}, "matrix")
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Models[0].UnresolvedReasons["reasoning"]; got != "" {
		t.Fatalf("common cohort row remained unresolved with reason %q", got)
	}
	if score := report.Models[0].LlamboScores["reasoning"]; score == nil || score.Primary.ReferencePopulation != 71 {
		t.Fatalf("common cohort row did not activate the broad Stats v1 scale: %#v", score)
	}
}

func frozenLLMStatsObservationFixture(t *testing.T) ([]byte, string) {
	t.Helper()
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 21, 13, 23, 0, 0, time.UTC)
	rows := make([]Observation, 0, llmStatsFrozenCohortObservationRows)
	for benchmark, cohort := range cohorts.Benchmarks {
		for index, score := range cohort.Scores {
			digest := cohort.Digests[index%len(cohort.Digests)]
			modelID := fmt.Sprintf("%s-%03d", benchmark, index)
			rows = append(rows, Observation{SourceID: cohort.SourceID, SourceRevision: digest, SourceSHA256: digest, Benchmark: benchmark, BenchmarkVersion: cohort.BenchmarkVersion, Cohort: cohort.Cohort, ModelID: modelID, RawScore: score, Unit: "source_native", Direction: cohort.Direction, Methodology: cohort.Methodology, EvidenceGrade: "aggregator_self_reported", FetchedAt: now, Locator: "fixture:" + modelID, SourceClass: SourceAggregatorResult})
		}
	}
	for index := len(rows); index < llmStatsFrozenCohortObservationRows; index++ {
		modelID := fmt.Sprintf("unreviewed-%04d", index)
		rows = append(rows, Observation{SourceID: "llm-stats-benchmark-results", SourceRevision: strings.Repeat("a", 64), SourceSHA256: strings.Repeat("a", 64), Benchmark: "unreviewed", BenchmarkVersion: "public-leaderboard", Cohort: "public", ModelID: modelID, RawScore: 0, Unit: "source_native", Direction: "higher", Methodology: "LLM Stats public leaderboard aggregation; verification status supplied per row", EvidenceGrade: "aggregator_self_reported", FetchedAt: now, Locator: "fixture:" + modelID, SourceClass: SourceAggregatorResult})
	}
	data, err := EncodeObservationsJSONL(rows)
	if err != nil {
		t.Fatal(err)
	}
	return data, "longbench-v2-000"
}

func sourceStatusNamed(t *testing.T, statuses []SourceStatus, name string) SourceStatus {
	t.Helper()
	for _, status := range statuses {
		if status.Name == name {
			return status
		}
	}
	t.Fatalf("missing source status %q: %#v", name, statuses)
	return SourceStatus{}
}
