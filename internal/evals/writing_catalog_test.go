package evals

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const writingLeaderboardFixture = `# Example

| Rank | Model | Comparison score | Estimated win chance | Uncertainty range |
|-----:|:------|-----------------:|---------------------:|:------------------|
| 1 | [Alpha](https://example.com/alpha)§ | 3.3 | 91% | 3.2 to 3.4 |
| 2 | Beta † | -0.4 | 44% | -0.5 to -0.2 |
`

func TestParseWritingLeaderboard(t *testing.T) {
	rows, err := ParseWritingLeaderboard(writingLeaderboardFixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %#v", len(rows), rows)
	}
	if rows[0].Rank != 1 || rows[0].Model != "Alpha" || rows[0].Score != 3.3 || rows[0].WinChance != 91 || rows[0].Lower != 3.2 || rows[0].Upper != 3.4 {
		t.Fatalf("unexpected first row: %#v", rows[0])
	}
	if rows[1].Model != "Beta" || rows[1].Score != -0.4 || rows[1].Lower != -0.5 || rows[1].Upper != -0.2 {
		t.Fatalf("unexpected second row: %#v", rows[1])
	}
}

func TestParseWritingLeaderboardRejectsMissingTable(t *testing.T) {
	for _, input := range []string{"", "| Rank | Model |\n| --- | --- |\n"} {
		if _, err := ParseWritingLeaderboard(input); err == nil {
			t.Fatalf("ParseWritingLeaderboard(%q) unexpectedly succeeded", input)
		}
	}
}

func TestDefaultWritingCatalogIncludesSourcesAndOpenModels(t *testing.T) {
	now := time.Date(2026, time.August, 5, 4, 0, 0, 0, time.FixedZone("test", 3600))
	catalog := DefaultWritingCatalog(now)
	if catalog.RegistryVersion != WritingCatalogVersion || !catalog.GeneratedAt.Equal(now.UTC()) {
		t.Fatalf("unexpected catalog metadata: %#v", catalog)
	}
	if len(catalog.Benchmarks) < 7 {
		t.Fatalf("catalog has too few benchmark sources: %d", len(catalog.Benchmarks))
	}
	for _, want := range []string{"writingbench", "eqbench-creative-v3", "eqbench-longform", "arena-creative-writing", "ifeval", "ifbench"} {
		if !containsWritingBenchmark(catalog.Benchmarks, want) {
			t.Fatalf("catalog missing benchmark %q", want)
		}
	}
	for _, want := range []string{
		"deepseek-ai/DeepSeek-V4-Flash-0731",
		"moonshotai/Kimi-K3",
		"zai-org/GLM-5.2",
		"MiniMaxAI/MiniMax-M3",
		"Qwen/Qwen3.6-35B-A3B",
		"openai/gpt-oss-20b",
	} {
		if !containsWritingModel(catalog.OpenModels, want) {
			t.Fatalf("catalog missing open model %q", want)
		}
	}
	if benchmark := writingBenchmarkByID(catalog.Benchmarks, "writingbench"); benchmark.PromptCount != 1000 || benchmark.PromptFormat != "JSONL" || benchmark.PromptURL == "" || benchmark.RunMode == "" {
		t.Fatalf("writing benchmark is not run-ready: %#v", benchmark)
	}
}

func TestBuildWritingModelCoverageDistinguishesExactAndVariantMatches(t *testing.T) {
	models := []WritingOpenModel{
		{ID: "exact", Name: "Kimi K3", LeaderboardAliases: []string{"Kimi K3"}},
		{ID: "variant", Name: "Gemma 4 31B it", LeaderboardAliases: []string{"Gemma 4 31B"}},
		{ID: "missing", Name: "DeepSeek V4 Flash-0731", LeaderboardAliases: []string{"DeepSeek V4 Flash-0731"}},
	}
	rows := []WritingLeaderboardRow{
		{Rank: 1, Model: "Kimi K3", Score: 2.9, WinChance: 87},
		{Rank: 2, Model: "Gemma 4 31B Reasoning", Score: -1.4, WinChance: 29},
	}
	coverage := buildWritingModelCoverage(models, rows)
	if coverage[0].Status != "measured" || coverage[0].MatchType != "exact" || coverage[0].Rank != 1 {
		t.Fatalf("unexpected exact coverage: %#v", coverage[0])
	}
	if coverage[1].Status != "measured" || coverage[1].MatchType != "variant" || coverage[1].Rank != 2 {
		t.Fatalf("unexpected variant coverage: %#v", coverage[1])
	}
	if coverage[2].Status != "needs-run" || coverage[2].ComparisonScore != nil {
		t.Fatalf("unexpected missing coverage: %#v", coverage[2])
	}
}

func TestFetchWritingCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "llambo-writing-benchmarks/1" {
			t.Errorf("User-Agent = %q", got)
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "text/markdown") {
			t.Errorf("Accept = %q", got)
		}
		_, _ = w.Write([]byte(writingLeaderboardFixture))
	}))
	defer server.Close()

	now := time.Date(2026, time.August, 5, 4, 0, 0, 0, time.UTC)
	catalog, err := FetchWritingCatalog(context.Background(), WritingCatalogOptions{
		Client: server.Client(),
		URL:    server.URL,
		Now:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Leaderboards) != 1 || len(catalog.Leaderboards[0].Rows) != 2 {
		t.Fatalf("unexpected live leaderboard: %#v", catalog.Leaderboards)
	}
	if catalog.Leaderboards[0].SourceURL != server.URL || !catalog.Leaderboards[0].FetchedAt.Equal(now) {
		t.Fatalf("unexpected live source metadata: %#v", catalog.Leaderboards[0])
	}
}

func TestFetchWritingCatalogRejectsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()

	if _, err := FetchWritingCatalog(context.Background(), WritingCatalogOptions{Client: server.Client(), URL: server.URL}); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchWritingCatalogValidatesSourcesAndModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/primary":
			_, _ = w.Write([]byte(writingLeaderboardFixture))
		case "/source/writingbench":
			_, _ = w.Write([]byte("{\"prompt\":\"one\"}\n{\"prompt\":\"two\"}\n"))
		case "/source/eqbench-creative-v3":
			_, _ = w.Write([]byte(`{"1":{"writing_prompt":"creative one"},"2":{"writing_prompt":"creative two"}}`))
		case "/source/ifeval":
			_, _ = w.Write([]byte("{\"prompt\":\"one\"}\n"))
		case "/model":
			modelID := r.URL.Query().Get("id")
			_, _ = fmt.Fprintf(w, `{"id":%q,"private":false,"pipeline_tag":"text-generation","downloads":42,"gated":false,"createdAt":"2026-07-31T07:30:24.000Z","lastModified":"2026-08-01T03:07:41.000Z","tags":["license:mit"],"cardData":{"license":"mit"}}`, modelID)
		default:
			if strings.HasPrefix(r.URL.Path, "/source/") {
				_, _ = w.Write([]byte("<html>available</html>"))
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	catalog := DefaultWritingCatalog(time.Unix(1, 0).UTC())
	sourceURLs := make(map[string]string, len(catalog.Benchmarks))
	for _, benchmark := range catalog.Benchmarks {
		if benchmark.ID != WritingPrimaryID {
			sourceURLs[benchmark.ID] = server.URL + "/source/" + benchmark.ID
		}
	}
	modelURLs := make(map[string]string, len(catalog.OpenModels))
	for _, model := range catalog.OpenModels {
		modelURLs[model.ID] = server.URL + "/model?id=" + model.ID
	}

	fetched, err := FetchWritingCatalog(context.Background(), WritingCatalogOptions{
		Client:               server.Client(),
		URL:                  server.URL + "/primary",
		Now:                  func() time.Time { return time.Unix(1, 0).UTC() },
		ValidateSources:      true,
		ValidateModels:       true,
		IncludePromptRecords: true,
		SourceURLs:           sourceURLs,
		ModelURLs:            modelURLs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.SourceChecks) != len(catalog.Benchmarks) {
		t.Fatalf("source checks = %d, want %d: %#v", len(fetched.SourceChecks), len(catalog.Benchmarks), fetched.SourceChecks)
	}
	if len(fetched.OpenModelChecks) != len(catalog.OpenModels) {
		t.Fatalf("model checks = %d, want %d", len(fetched.OpenModelChecks), len(catalog.OpenModels))
	}
	if check := writingSourceCheckByID(fetched.SourceChecks, "writingbench"); check.Status != "available" || check.Records != 2 {
		t.Fatalf("unexpected WritingBench check: %#v", check)
	}
	if check := writingSourceCheckByID(fetched.SourceChecks, "eqbench-creative-v3"); check.Status != "available" || check.Records != 2 {
		t.Fatalf("unexpected EQ-Bench check: %#v", check)
	}
	if check := writingSourceCheckByID(fetched.SourceChecks, "ifeval"); check.Status != "available" || check.Records != 1 {
		t.Fatalf("unexpected IFEval check: %#v", check)
	}
	if len(fetched.PromptRecords) != 5 {
		t.Fatalf("prompt records = %d, want 5: %#v", len(fetched.PromptRecords), fetched.PromptRecords)
	}
	if fetched.PromptRecords[0].BenchmarkID != "writingbench" || fetched.PromptRecords[0].Prompt != "one" || fetched.PromptRecords[0].SourceURL == "" {
		t.Fatalf("unexpected first normalized prompt: %#v", fetched.PromptRecords[0])
	}
	if fetched.PromptRecords[2].BenchmarkID != "eqbench-creative-v3" || fetched.PromptRecords[2].ID != "1" || fetched.PromptRecords[2].Prompt != "creative one" {
		t.Fatalf("unexpected EQ-Bench normalized prompt: %#v", fetched.PromptRecords[2])
	}
	if check := writingModelCheckByID(fetched.OpenModelChecks, "deepseek-ai/DeepSeek-V4-Flash-0731"); check.Status != "available" || check.RemoteLicense != "mit" || !check.LicenseMatch || check.Downloads != 42 {
		t.Fatalf("unexpected DeepSeek metadata check: %#v", check)
	}
}

func TestFetchWritingCatalogDiscoversFreshPublicModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/primary":
			_, _ = w.Write([]byte(writingLeaderboardFixture))
		case "/models":
			_, _ = w.Write([]byte(`[
				{"id":"deepseek-ai/DeepSeek-V4-Flash-0731","pipeline_tag":"text-generation","private":false,"gated":false,"downloads":100,"tags":["license:mit"]},
				{"id":"fresh/CreativeWriter-7B","pipeline_tag":"text-generation","createdAt":"2026-08-04T00:00:00.000Z","lastModified":"2026-08-05T03:00:00.000Z","downloads":12,"likes":4,"private":false,"gated":false,"tags":["transformers","license:apache-2.0"]},
				{"id":"private/model","pipeline_tag":"text-generation","private":true,"gated":false},
				{"id":"gated/model","pipeline_tag":"text-generation","private":false,"gated":true},
				{"id":"fresh/SecondWriter","pipeline_tag":"text-generation","private":false,"gated":false,"tags":[]},
				{"id":"not-a-text-model","pipeline_tag":"text-to-image","private":false,"gated":false}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fetched, err := FetchWritingCatalog(context.Background(), WritingCatalogOptions{
		Client:             server.Client(),
		URL:                server.URL + "/primary",
		Now:                func() time.Time { return time.Unix(2, 0).UTC() },
		DiscoverOpenModels: true,
		DiscoverLimit:      2,
		DiscoveryURL:       server.URL + "/models",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fetched.OpenModelDiscovery == nil || fetched.OpenModelDiscovery.Status != "available" || fetched.OpenModelDiscovery.Candidates != 2 {
		t.Fatalf("unexpected discovery status: %#v", fetched.OpenModelDiscovery)
	}
	if len(fetched.DiscoveredOpenModels) != 2 {
		t.Fatalf("discovered models = %d, want 2: %#v", len(fetched.DiscoveredOpenModels), fetched.DiscoveredOpenModels)
	}
	first := fetched.DiscoveredOpenModels[0]
	if first.ID != "fresh/CreativeWriter-7B" || first.Name != "CreativeWriter-7B" || first.License != "apache-2.0" || first.ReviewStatus != "needs-review" {
		t.Fatalf("unexpected first discovered model: %#v", first)
	}
	if first.Notes == "" || first.HuggingFaceURL == "" || first.Downloads != 12 || first.Likes != 4 {
		t.Fatalf("discovered model lost review metadata: %#v", first)
	}
	if fetched.DiscoveredOpenModels[1].ID != "fresh/SecondWriter" {
		t.Fatalf("unexpected second discovered model: %#v", fetched.DiscoveredOpenModels[1])
	}
}

func writingSourceCheckByID(checks []WritingSourceStatus, id string) WritingSourceStatus {
	for _, check := range checks {
		if check.BenchmarkID == id {
			return check
		}
	}
	return WritingSourceStatus{BenchmarkID: id, Status: "missing"}
}

func writingModelCheckByID(checks []WritingModelStatus, id string) WritingModelStatus {
	for _, check := range checks {
		if check.ModelID == id {
			return check
		}
	}
	return WritingModelStatus{ModelID: id, Status: "missing"}
}

func containsWritingBenchmark(benchmarks []WritingBenchmark, id string) bool {
	for _, benchmark := range benchmarks {
		if benchmark.ID == id {
			return true
		}
	}
	return false
}

func containsWritingModel(models []WritingOpenModel, id string) bool {
	for _, model := range models {
		if model.ID == id {
			return true
		}
	}
	return false
}

func writingBenchmarkByID(benchmarks []WritingBenchmark, id string) WritingBenchmark {
	for _, benchmark := range benchmarks {
		if benchmark.ID == id {
			return benchmark
		}
	}
	return WritingBenchmark{ID: id}
}
