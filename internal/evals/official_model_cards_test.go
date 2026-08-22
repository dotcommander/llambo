package evals

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestOfficialCardParsersUseOnlyPublishedTableRows(t *testing.T) {
	lfm, err := parseLFM25Card([]byte(`
| Benchmark | LFM | A | B | C | D |
| AIME25 | 51.87 | 26.33 | 34.27 | 49.33 | 56.07 |
| LiveCodeBenchv6 | 59.41 | 54.92 | 63.77 | 60.85 | 69.86 |
| IFBench | 59.17 | 34.08 | 39.24 | 48.40 | 56.47 |
| Multi-IF | 80.07 | 69.44 | 77.35 | 55.67 | 62.55 |
| IFStruct | 85.49 | 64.85 | 76.65 | 36.25 | 78.50 |
| BFCLv4 | 56.88 | 36.98 | 46.39 | 50.56 | 60.13 |
| ToolSandbox | 77.83 | 52.40 | 65.00 | 75.55 | 76.44 |
`))
	if err != nil || lfm["bfcl-v4"][0] != 56.88 || len(lfm["ifstruct"]) != 5 {
		t.Fatalf("LFM table parsing lost published rows: rows=%#v err=%v", lfm, err)
	}
	qwen, err := parseQwen38Card([]byte(`
<table><tr><td>SWE-bench Pro</td><td>61.7</td><td>53.5</td><td>57.6</td><td>51.2</td><td>53.4</td></tr>
<tr><td>IFBench</td><td>79.5</td><td>69.1</td><td>79.1</td><td>77.0</td><td>62.5</td></tr>
<tr><td>GPQA Diamond</td><td>89.2</td><td>87.8</td><td>90.3</td><td>83.5</td><td>91.3</td></tr>
<tr><td>LiveCodeBench v6</td><td>90.3</td><td>83.9</td><td>89.6</td><td>--</td><td>88.8</td></tr></table>`))
	if err != nil || qwen["gpqa"][0] != 89.2 || len(qwen["livecodebench-v6"]) != 4 {
		t.Fatalf("Qwen table parsing changed source values: rows=%#v err=%v", qwen, err)
	}
	gpt, err := parseGPTOSSCard([]byte(`
<table><tr><td>AIME 2025 (no tools)</td><td>50.4</td><td>80.0</td><td>92.5</td><td>37.1</td><td>72.1</td><td>91.7</td></tr>
<tr><td>GPQA Diamond (no tools)</td><td>67.1</td><td>73.1</td><td>80.1</td><td>56.8</td><td>66.0</td><td>71.5</td></tr>
<tr><td>SWE-Bench Verified</td><td>47.9</td><td>52.6</td><td>62.4</td><td>37.4</td><td>53.2</td><td>60.7</td></tr>
<tr><td>Tau-Bench Retail</td><td>49.4</td><td>62.0</td><td>67.8</td><td>35.0</td><td>47.3</td><td>54.8</td></tr>
<tr><td>Aider Polyglot</td><td>24.0</td><td>34.2</td><td>44.4</td><td>16.6</td><td>26.6</td><td>34.2</td></tr></table>`))
	if err != nil || gpt["aime"][5] != 91.7 || len(gpt["aider"]) != 6 {
		t.Fatalf("gpt-oss Table 3 parsing changed configuration arms: rows=%#v err=%v", gpt, err)
	}
	lfmVL, err := parseLFM25VLCard([]byte(`
| ToolSandBox | 59.5 | 26.4<br>(<span>-33.1</span>) | 56.5 | <ins>61.6</ins> | *n/a*<sup>1</sup> | 47.7 | **65.0** |
| BFCLv4 | 32.5 | 20.5<br>(<span>-12.0</span>) | 33.2 | <ins>40.0</ins> | *n/a*<sup>1</sup> | 33.9 | **53.6** |
`))
	if err != nil || strings.Join(floatSliceStrings(lfmVL["toolsandbox"]), ",") != "59.5,26.4,56.5,61.6,47.7,65" || len(lfmVL["bfcl-v4"]) != 6 {
		t.Fatalf("LFM VL table parsing changed source values: rows=%#v err=%v", lfmVL, err)
	}
	gemma, err := parseGemma4Card([]byte(`
| MMLU Pro | 85.2% | 82.6% | 77.2% | 69.4% | 60.0% | 67.6% |
| AIME 2026 no tools | 89.2% | 88.3% | 77.5% | 42.5% | 37.5% | 20.8% |
| LiveCodeBench v6 | 80.0% | 77.1% | 72.0% | 52.0% | 44.0% | 29.1% |
| GPQA Diamond | 84.3% | 82.3% | 78.8% | 58.6% | 43.4% | 42.4% |
| Tau2 (average over 3) | 76.9% | 68.2% | 69.0% | 42.2% | 24.5% | 16.2% |
`))
	if err != nil || gemma["mmlu-pro"][0] != 85.2 || gemma["mmlu-pro"][1] != 82.6 || len(gemma["tau2-bench"]) != 6 {
		t.Fatalf("Gemma table parsing changed source values: rows=%#v err=%v", gemma, err)
	}
}

func TestOfficialCardFetchSealsBytesAndRecordsProvenance(t *testing.T) {
	body := []byte("published source fixture")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	value := 64.0
	snapshot, err := fetchOfficialCard(context.Background(), Options{Client: client}, officialCardSpec{
		sourceID: "synthetic-card", modelKey: "lfm-2.5-2.6b", modelName: "LFM", organization: "Liquid AI", revision: "test-revision", contentSHA: SealBytes(body),
		url: func(Options) string { return "https://example.test/card" }, parse: func([]byte) (map[string][]float64, error) { return map[string][]float64{"bfcl-v4": {value}}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	result := snapshot.Models[0].Benchmarks["bfcl-v4"]
	if result.SourceID != "synthetic-card" || result.SourceClass != string(SourceFirstPartyResult) || result.ContentSHA != SealBytes(body) || result.Score == nil || *result.Score != value {
		t.Fatalf("sealed official result lost provenance: %#v", result)
	}
}

func TestOfficialCardCohortsAreImmutableAndTargetCategoriesResolve(t *testing.T) {
	lfm := cardModel(t, "lfm-2.5-2.6b", "LFM", "Liquid AI", "liquidai-lfm25-2.6b-card", lfm25Revision, lfm25ContentSHA, map[string]float64{"bfcl-v4": 56.88, "livecodebench-v6": 59.41, "ifbench": 59.17, "multi-if": 80.07, "aime": 51.87})
	qwen := cardModel(t, "qwen3.8-27b", "Qwen", "Qwen", "qwen3.8-27b-card", qwen38Revision, qwen38ContentSHA, map[string]float64{"swe-bench-pro": 61.7, "ifbench": 79.5, "gpqa": 89.2})
	gpt := cardModel(t, "gpt-oss-20b-high", "gpt-oss", "OpenAI", "openai-gpt-oss-model-card", gptOSSRevision, gptOSSContentSHA, map[string]float64{"aime": 91.7, "gpqa": 71.5, "swe-bench-verified": 60.7, "tau-bench-retail": 54.8, "aider": 34.2})
	for _, test := range []struct {
		name       string
		model      Model
		categories []string
	}{
		{"lfm", lfm, []string{"agents", "coding", "instruction-following", "reasoning"}},
		{"qwen", qwen, []string{"coding", "instruction-following", "reasoning"}},
		{"gpt-oss", gpt, []string{"agents", "coding", "reasoning"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, category := range test.categories {
				if score := scoreCategoryV3(test.model, categorySpecNamed(t, category), []Model{test.model}); score == nil {
					t.Fatalf("%s did not resolve %s", test.name, category)
				}
			}
		})
	}
	before := scoreCategoryV3(lfm, categorySpecNamed(t, "reasoning"), []Model{lfm})
	extra := cardModel(t, "extra", "extra", "Lab", "liquidai-lfm25-2.6b-card", lfm25Revision, lfm25ContentSHA, map[string]float64{"aime": 100})
	after := scoreCategoryV3(lfm, categorySpecNamed(t, "reasoning"), []Model{lfm, extra})
	if before == nil || after == nil || before.Score != after.Score {
		t.Fatalf("extra cache evidence changed frozen source cohort: before=%#v after=%#v", before, after)
	}
}

func TestV3OfficialCardTargetRowsAndSixColumnCohortsAreSealed(t *testing.T) {
	for _, test := range []struct {
		cache      string
		cohorts    map[string][]float64
		revision   string
		targetKeys []string
	}{
		{
			cache: "official-lfm25-vl-3b", revision: lfm25VLRevision, targetKeys: []string{"lfm-2.5-vl-3b"},
			cohorts: map[string][]float64{"toolsandbox": {59.5, 26.4, 56.5, 61.6, 47.7, 65.0}, "bfcl-v4": {32.5, 20.5, 33.2, 40.0, 33.9, 53.6}},
		},
		{
			cache: "official-gemma4", revision: gemma4Revision, targetKeys: []string{"gemma-4-31b-it", "gemma-4-26b-a4b-it"},
			cohorts: map[string][]float64{"mmlu-pro": {85.2, 82.6, 77.2, 69.4, 60.0, 67.6}, "aime": {89.2, 88.3, 77.5, 42.5, 37.5, 20.8}, "livecodebench-v6": {80.0, 77.1, 72.0, 52.0, 44.0, 29.1}, "gpqa": {84.3, 82.3, 78.8, 58.6, 43.4, 42.4}, "tau2-bench": {76.9, 68.2, 69.0, 42.2, 24.5, 16.2}},
		},
	} {
		t.Run(test.cache, func(t *testing.T) {
			snapshot := validOfficialSnapshot(t, test.cache)
			if err := validateOfficialCardSnapshot(test.cache, snapshot); err != nil {
				t.Fatalf("pinned target row rejected: %v", err)
			}
			keys := make(map[string]bool, len(snapshot.Models))
			for _, model := range snapshot.Models {
				keys[model.Key] = true
			}
			for _, key := range test.targetKeys {
				if !keys[key] {
					t.Fatalf("missing canonical target row %q: %#v", key, snapshot.Models)
				}
			}
			for _, model := range snapshot.Models {
				categories := []string{"agents", "coding", "reasoning"}
				if model.Key == "lfm-2.5-vl-3b" {
					categories = []string{"agents"}
				}
				for _, category := range categories {
					if score := scoreCategoryV3(model, categorySpecNamed(t, category), []Model{model}); score == nil {
						t.Fatalf("%s did not resolve %s", model.Key, category)
					}
				}
			}
			for benchmark, want := range test.cohorts {
				got := frozenBenchmarkCohort(benchmark, test.revision)
				if len(got) != 6 {
					t.Fatalf("%s cohort size = %d, want 6: %#v", benchmark, len(got), got)
				}
				sort.Float64s(want)
				if !slices.Equal(got, want) {
					t.Fatalf("%s cohort = %#v, want %#v", benchmark, got, want)
				}
			}
		})
	}
}

func TestOfficialCardCachedSnapshotFailsClosedOnEveryPinnedField(t *testing.T) {
	for _, mutation := range []struct {
		name  string
		apply func(*sourceSnapshot)
	}{
		{"revision", func(snapshot *sourceSnapshot) { snapshot.Version = "tampered" }},
		{"content digest", func(snapshot *sourceSnapshot) { snapshot.ContentSHA = strings.Repeat("0", 64) }},
		{"snapshot source URL", func(snapshot *sourceSnapshot) { snapshot.URL = "https://tampered.test/card" }},
		{"registry version", func(snapshot *sourceSnapshot) { snapshot.RegistryVersion = "tampered" }},
		{"source URL", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { result.URL = "https://tampered.test/card" })
		}},
		{"method", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { result.Method = "tampered" })
		}},
		{"evidence grade", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { result.EvidenceGrade = "owner" })
		}},
		{"unit", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { result.Unit = "points" })
		}},
		{"direction", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { result.Direction = "lower" })
		}},
		{"cohort", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { result.Cohort = "tampered" })
		}},
		{"source id", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { result.SourceID = "tampered" })
		}},
		{"source class", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { result.SourceClass = string(SourceAggregatorResult) })
		}},
		{"source revision", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { result.SourceRevision = "tampered" })
		}},
		{"identity", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { result.Identity = IdentityMatchProjected })
		}},
		{"locator", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { result.Locator = "tampered" })
		}},
		{"canonical model key", func(snapshot *sourceSnapshot) { snapshot.Models[0].Key = "lookalike" }},
		{"target score", func(snapshot *sourceSnapshot) {
			snapshot.Models[0].Benchmarks["bfcl-v4"] = mutateResult(snapshot.Models[0].Benchmarks["bfcl-v4"], func(result *BenchmarkResult) { value := 0.0; result.Score = &value })
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			dir := t.TempDir()
			snapshot := validOfficialSnapshot(t, "official-lfm25-2.6b")
			mutation.apply(&snapshot)
			path := filepath.Join(dir, "official-lfm25-2.6b.json")
			if err := writeSnapshot(path, snapshot); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			loaded, status := readOptionalOfficialSnapshot(Options{CacheDir: dir, Now: time.Now}, "official-lfm25-2.6b")
			if len(loaded.Models) != 0 || status.Cache != "unavailable" || !strings.Contains(status.Error, "invalid official card cache") {
				t.Fatalf("tampered snapshot remained scoreable: models=%#v status=%#v", loaded.Models, status)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("invalid official cache was rewritten")
			}
		})
	}
}

func TestOfficialCardExactKeyMergeKeepsOneCanonicalRowAndMirror(t *testing.T) {
	aggregate := 10.0
	official := 80.0
	models := []Model{{Key: "lfm-2.5-2.6b", Name: "canonical", Benchmarks: map[string]BenchmarkResult{"bfcl-v4": {Score: &aggregate, SourceID: "llm-stats-benchmark-results", SourceClass: string(SourceAggregatorResult), Version: lfm25Revision}}}}
	cards := []Model{{Key: "lfm-2.5-2.6b", Name: "LFM2.5-2.6B", IdentityMatch: IdentityMatchExact, Benchmarks: map[string]BenchmarkResult{"bfcl-v4": {Score: &official, SourceID: "liquidai-lfm25-2.6b-card", SourceClass: string(SourceFirstPartyResult), Version: lfm25Revision}}}}
	merged := mergeOfficialCardModels(models, cards)
	if len(merged) != 1 {
		t.Fatalf("exact canonical merge created %d rows", len(merged))
	}
	result := merged[0].Benchmarks["bfcl-v4"]
	if result.Score == nil || *result.Score != official || result.SourceID != "liquidai-lfm25-2.6b-card" || len(result.Mirrors) != 1 || result.Mirrors[0].SourceID != "llm-stats-benchmark-results" {
		t.Fatalf("first-party result did not replace and retain aggregate mirror: %#v", result)
	}
}

func TestOfficialCardCacheAcceptsUnchangedV3Registry(t *testing.T) {
	snapshot := validOfficialSnapshot(t, "official-lfm25-2.6b")
	snapshot.RegistryVersion = "LLAMBO-6-sources-v3"
	if err := validateOfficialCardSnapshot("official-lfm25-2.6b", snapshot); err != nil {
		t.Fatalf("unchanged v3 official-card cache was rejected: %v", err)
	}
}

func validOfficialSnapshot(t *testing.T, cacheName string) sourceSnapshot {
	t.Helper()
	spec, ok := officialCardSpecForCache(cacheName)
	if !ok {
		t.Fatalf("unknown card cache %q", cacheName)
	}
	models := make([]Model, 0, len(officialCardTargetScores[cacheName]))
	for _, target := range spec.cardTargets() {
		benchmarks := make(map[string]BenchmarkResult, len(officialCardTargetScores[cacheName][target.modelKey]))
		for benchmark, score := range officialCardTargetScores[cacheName][target.modelKey] {
			score := score
			benchmarks[benchmark] = BenchmarkResult{Score: &score, Identity: IdentityMatchExact, Version: spec.revision, URL: pinnedOfficialCardURL(spec), ContentSHA: spec.contentSHA, Method: modelCardSourceType, EvidenceGrade: modelCardEvidence, Unit: modelCardUnit, Direction: modelCardDirection, Cohort: "official-comparison-table", Locator: "published benchmark table: " + benchmark, SourceID: spec.sourceID, SourceRevision: spec.revision, SourceClass: string(SourceFirstPartyResult)}
		}
		models = append(models, Model{Key: target.modelKey, Name: target.modelName, Organization: target.organization, IdentityMatch: IdentityMatchExact, Benchmarks: benchmarks})
	}
	return sourceSnapshot{FetchedAt: time.Unix(1, 0).UTC(), Models: models, URL: pinnedOfficialCardURL(spec), Version: spec.revision, ContentSHA: spec.contentSHA, RegistryVersion: SourceRegistryVersion}
}

func floatSliceStrings(values []float64) []string {
	formatted := make([]string, len(values))
	for i, value := range values {
		formatted[i] = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", value), "0"), ".")
	}
	return formatted
}

func mutateResult(result BenchmarkResult, apply func(*BenchmarkResult)) BenchmarkResult {
	apply(&result)
	return result
}

func cardModel(t *testing.T, key, name, organization, sourceID, revision, sha string, values map[string]float64) Model {
	t.Helper()
	benchmarks := make(map[string]BenchmarkResult, len(values))
	for benchmark, value := range values {
		value := value
		benchmarks[benchmark] = BenchmarkResult{Score: &value, Identity: IdentityMatchExact, Version: revision, ContentSHA: sha, Method: modelCardSourceType, EvidenceGrade: modelCardEvidence, Direction: modelCardDirection, SourceID: sourceID, SourceRevision: revision, SourceClass: string(SourceFirstPartyResult), FetchedAt: time.Unix(1, 0).UTC()}
	}
	return Model{Key: key, Name: name, Organization: organization, IdentityMatch: IdentityMatchExact, Benchmarks: benchmarks}
}
