package evals

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestSourceRegistryAdmitsOnlyLLMStatsResultRows(t *testing.T) {
	eligible := map[string]bool{
		"llm-stats-benchmark-results": true,
		"llm-stats-stats-v1-scores":   true,
	}
	for _, source := range SourceRegistry() {
		if strings.HasPrefix(source.ID, "llm-stats-") && source.Eligible != eligible[source.ID] {
			t.Fatalf("LLM Stats source %q has wrong eligibility: %v", source.ID, source.Eligible)
		}
	}
}

func TestSourceRegistryAdmitsPinnedV3OfficialCards(t *testing.T) {
	want := map[string]SourceClass{
		"liquidai-lfm25-vl-3b-card":  SourceFirstPartyResult,
		"liquidai-lfm25-8b-a1b-card": SourceFirstPartyResult,
		"google-gemma4-model-card":   SourceFirstPartyResult,
	}
	for _, source := range SourceRegistry() {
		if class, ok := want[source.ID]; ok {
			if source.Class != class || !source.Public || !source.Eligible {
				t.Fatalf("pinned source registration = %#v", source)
			}
			delete(want, source.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing pinned source registrations: %#v", want)
	}
}

func TestObservationValidationAndDuplicateConflict(t *testing.T) {
	observation := Observation{SourceID: "writingbench", SourceRevision: "v1", SourceSHA256: strings.Repeat("a", 64), Benchmark: "writingbench", BenchmarkVersion: "v1", Cohort: "official", ModelID: "model", RawScore: 1, Unit: "points", Direction: "higher", Methodology: "published", EvidenceGrade: "owner", FetchedAt: time.Unix(1, 0).UTC(), Locator: "row:1", SourceClass: SourceOwnerResult}
	b, _ := json.Marshal(observation)
	duplicates, err := DecodeObservationsJSONL(strings.NewReader(string(b)+"\n"+string(b)+"\n"), 1<<20)
	if err != nil || len(duplicates) != 2 {
		t.Fatalf("identical duplicate provenance was not preserved: rows=%d err=%v", len(duplicates), err)
	}
	conflict := observation
	conflict.RawScore = 2
	c, _ := json.Marshal(conflict)
	if _, err := DecodeObservationsJSONL(strings.NewReader(string(b)+"\n"+string(c)+"\n"), 1<<20); err == nil || !strings.Contains(err.Error(), "conflicting duplicate") {
		t.Fatalf("expected duplicate conflict, got %v", err)
	}
	observation.RawScore = math.NaN()
	if err := observation.Validate(); err == nil {
		t.Fatal("non-finite score accepted")
	}
}

func TestBenchmarkMergePrecedenceDeduplicatesMirrorsAndQuarantinesPeerConflict(t *testing.T) {
	results := map[string]BenchmarkResult{}
	aggregator := sealedBenchmark(10)
	aggregator.Version, aggregator.SourceID, aggregator.SourceClass = "public-leaderboard", "mirror", string(SourceAggregatorResult)
	owner := sealedBenchmark(80)
	owner.Version, owner.SourceID, owner.SourceClass = "public-leaderboard", "owner", string(SourceOwnerResult)
	storeBenchmarkResult(results, "gpqa", aggregator)
	storeBenchmarkResult(results, "gpqa", owner)
	selected := results["gpqa"]
	if selected.SourceID != "owner" || len(selected.Mirrors) != 1 || selected.Quarantined {
		t.Fatalf("owner did not replace deduplicated aggregator mirror: %#v", selected)
	}
	peer := sealedBenchmark(70)
	peer.Version, peer.SourceID, peer.SourceClass = "public-leaderboard", "peer-owner", string(SourceOwnerResult)
	storeBenchmarkResult(results, "gpqa", peer)
	selected = results["gpqa"]
	if !selected.Quarantined || !strings.Contains(selected.Conflict, "equal-authority") || len(selected.Mirrors) != 2 {
		t.Fatalf("equal-authority conflict was not quarantined and exposed: %#v", selected)
	}
	model := Model{Benchmarks: results}
	if score := scoreCategoryV3(model, categorySpecs[4], []Model{model}); score != nil {
		t.Fatalf("quarantined contribution entered scorer: %#v", score)
	}
}

func TestLLAMBO7EqualFamilyRawCommonCohortScore(t *testing.T) {
	models := make([]Model, 5)
	for i := range models {
		value := float64(i + 1)
		if i == 2 {
			value = 2
		}
		result := sealedBenchmark(value)
		result.Version = "public-leaderboard"
		models[i] = Model{Key: string(rune('a' + i)), Benchmarks: map[string]BenchmarkResult{"gpqa": result}}
	}
	score := scoreCategoryV3(models[1], categorySpecs[4], models)
	cohorts, err := loadFrozenLLMStatsStatsV1Cohorts()
	if err != nil {
		t.Fatal(err)
	}
	cohort := cohorts.Benchmarks["gpqa"].Scores
	want := empiricalPercentile(cohort, 2, true)
	if score == nil || CategoryFormulaVersion != "LLAMBO-9-category" || math.Abs(score.Coverage-.25) > 1e-12 || math.Abs(score.TrustedCoverage-.25) > 1e-12 || math.Abs(score.Score-want) > 1e-12 || len(score.Families) != 1 || score.Families[0] != "advanced-science" {
		t.Fatalf("unexpected sparse equal-family raw common-cohort score: %#v", score)
	}
	newResult := sealedBenchmark(99)
	newResult.Version = "public-leaderboard"
	withNewCacheRow := append(append([]Model(nil), models...), Model{Key: "new", Benchmarks: map[string]BenchmarkResult{"gpqa": newResult}})
	if got := scoreCategoryV3(models[1], categorySpecs[4], withNewCacheRow); got == nil || got.Score != score.Score {
		t.Fatalf("mutable cache row changed frozen-reference score: got %#v want %#v", got, score)
	}
}

func TestLLMStatsFrozenCohortsUseExactAdmissionContract(t *testing.T) {
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		t.Fatal(err)
	}
	targets := []struct {
		benchmark  string
		category   string
		family     string
		population int
	}{
		{"longbench-v2", "long-context", "multi-document-reasoning", 17},
		{"math", "reasoning", "competition-mathematics", 71},
		{"humaneval", "coding", "function-program-generation", 66},
		{"ifeval", "instruction-following", "verifiable-constraints", 67},
		{"arena-hard", "instruction-following", "preference-adherence", 26},
		{"osworld", "agents", "environment-task-completion", 20},
	}
	if got := len(BenchmarkRegistry()); got != 45 {
		t.Fatalf("reviewed benchmark registrations = %d, want 45", got)
	}
	for _, target := range targets {
		t.Run(target.benchmark, func(t *testing.T) {
			cohort := cohorts.Benchmarks[target.benchmark]
			result := frozenLLMStatsResult(cohort)
			model := Model{Key: target.benchmark, Benchmarks: map[string]BenchmarkResult{target.benchmark: result}}
			score := scoreCategoryV3(model, categorySpecNamed(t, target.category), []Model{model})
			if !IsReviewedBenchmark(target.category, target.family, target.benchmark) || score == nil || len(score.Contributions) != 1 {
				t.Fatalf("target did not activate its intended family: %#v", score)
			}
			contribution := score.Contributions[0]
			if contribution.Benchmark != target.benchmark || contribution.Family != target.family || contribution.RawScore == nil || *contribution.RawScore != cohort.Scores[0] || contribution.Percentile == nil || contribution.ReferencePopulation != target.population || contribution.SourceRevision != cohort.Digests[0] || contribution.EvidenceGrade != "aggregator_self_reported" {
				t.Fatalf("unexpected frozen contribution: %#v", contribution)
			}
		})
	}
}

func TestCommonCohortRejectsDirectionAndIdentityMismatch(t *testing.T) {
	cohorts, err := loadFrozenLLMStatsStatsV1Cohorts()
	if err != nil {
		t.Fatal(err)
	}
	cohort := cohorts.Benchmarks["math"]
	score := cohort.Scores[0]
	base := BenchmarkResult{
		Score: &score, Identity: IdentityMatchExact, Version: llmStatsStatsV1BenchmarkVersion,
		ContentSHA: cohort.Digests[0], Method: llmStatsStatsV1Methodology,
		SourceClass: string(SourceAggregatorResult), EvidenceGrade: "aggregator_self_reported",
		Direction: "higher", Cohort: llmStatsStatsV1Cohort, SourceID: llmStatsStatsV1SourceID,
		SourceRevision: cohort.Digests[0],
	}
	for _, test := range []struct {
		name   string
		mutate func(*BenchmarkResult)
	}{
		{"direction", func(result *BenchmarkResult) { result.Direction = "lower" }},
		{"identity", func(result *BenchmarkResult) { result.Identity = IdentityMatchNormalized }},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := base
			test.mutate(&result)
			model := Model{Benchmarks: map[string]BenchmarkResult{"math": result}}
			if score := scoreCategoryV3(model, categorySpecNamed(t, "reasoning"), []Model{model}); score != nil {
				t.Fatalf("mismatched row activated a cohort: %#v", score)
			}
		})
	}
}

func frozenLLMStatsResult(cohort frozenLLMStatsCohort) BenchmarkResult {
	score := cohort.Scores[0]
	return BenchmarkResult{Score: &score, Identity: IdentityMatchExact, Version: cohort.BenchmarkVersion, ContentSHA: cohort.Digests[0], Method: cohort.Methodology, SourceClass: cohort.SourceClass, EvidenceGrade: "aggregator_self_reported", Direction: cohort.Direction, Cohort: cohort.Cohort, SourceID: cohort.SourceID, SourceRevision: cohort.Digests[0]}
}

func TestReviewedSourceRevisionsContributeToFrozenCohorts(t *testing.T) {
	value := 90.0
	sealed := func(sourceID string, class SourceClass, grade, version, commit, revision string) BenchmarkResult {
		return BenchmarkResult{Score: &value, Identity: IdentityMatchExact, Version: version, CommitSHA: commit, ContentSHA: strings.Repeat("a", 64), Method: "published methodology", SourceID: sourceID, SourceClass: string(class), EvidenceGrade: grade, SourceRevision: revision}
	}
	tests := []struct {
		name      string
		benchmark string
		result    BenchmarkResult
		category  string
	}{
		{"writingbench verified ETag", "writingbench", sealed("writingbench", SourceOwnerResult, "owner", "d9338ce9b09792ea7167279fee7ccc1910e28c5d", "c06986e05aea53d625837e67c88944a2234271f1", "c06986e05aea53d625837e67c88944a2234271f1"), "writing"},
		{"EQ-Bench pinned commit", "eqbench-creative-v3", sealed("eqbench-creative-v3", SourceOwnerResult, "owner", EQBenchCreativeCommit, EQBenchCreativeCommit, EQBenchCreativeCommit), "writing"},
		{"IFEval embedded commit", "ifeval-official", sealed("ifeval-official", SourceFirstPartyResult, "first_party", "b9aebfcbe28b6cb374042f495d733037550ab146", "b9aebfcbe28b6cb374042f495d733037550ab146", "b9aebfcbe28b6cb374042f495d733037550ab146"), "instruction-following"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := Model{Key: test.name, Benchmarks: map[string]BenchmarkResult{test.benchmark: test.result}}
			if score := scoreCategoryV3(model, categorySpecNamed(t, test.category), []Model{model}); score == nil || len(score.Contributions) != 1 || score.Contributions[0].Benchmark != test.benchmark {
				t.Fatalf("reviewed source did not enter frozen cohort: %#v", score)
			}
		})
	}
}

func TestUnknownAndIneligibleSourcesCannotScore(t *testing.T) {
	value := 90.0
	base := BenchmarkResult{Score: &value, Identity: IdentityMatchExact, Version: "public-leaderboard", ContentSHA: strings.Repeat("a", 64), Method: "published methodology", EvidenceGrade: "owner", SourceClass: string(SourceOwnerResult), SourceRevision: "public-leaderboard"}
	for _, test := range []struct {
		name  string
		id    string
		class SourceClass
	}{
		{"unknown", "unknown-source", SourceOwnerResult},
		{"ineligible registered", "llm-stats-models", SourceAggregatorLead},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := base
			result.SourceID, result.SourceClass = test.id, string(test.class)
			model := Model{Benchmarks: map[string]BenchmarkResult{"gpqa": result}}
			if score := scoreCategoryV3(model, categorySpecNamed(t, "reasoning"), []Model{model}); score != nil {
				t.Fatalf("unreviewed source entered scorer: %#v", score)
			}
		})
	}
}

func categorySpecNamed(t *testing.T, name string) categorySpec {
	t.Helper()
	for _, spec := range categorySpecs {
		if spec.name == name {
			return spec
		}
	}
	t.Fatalf("missing category spec %q", name)
	return categorySpec{}
}
