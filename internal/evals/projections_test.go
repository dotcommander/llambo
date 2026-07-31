package evals

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuiltInProjectionRegistryHasReviewedRows(t *testing.T) {
	registry, err := loadProjectionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	want := []projectionSpec{
		{ArtifactKey: "Qwen-AgentWorld-35B-A3B-oQ4-MLX", SourceKey: "qwen3.6-35b-a3b", Confidence: "low", Basis: "Reviewed Qwen3.6 35B A3B base proxy; AgentWorld fine-tune and oQ4 MLX artifact may materially change capability.", ReviewedAt: "2026-07-10T00:00:00Z"},
		{ArtifactKey: "Qwen3.6-27B-MLX-4bit", SourceKey: "aa:qwen3.6-27b", Confidence: "medium", Basis: "Reviewed Qwen3.6 27B base identity; live OMLX metadata reports reasoning enabled for this MLX 4-bit artifact.", ReviewedAt: "2026-07-10T00:00:00Z"},
		{ArtifactKey: "Qwen3.6-35B-A3B-oQ4-fp16-mtp", SourceKey: "aa:qwen3-6-35b-a3b", Confidence: "medium", Basis: "Reviewed Qwen3.6 35B A3B base identity; live OMLX metadata reports reasoning enabled and preserved MTP-compatible weights.", ReviewedAt: "2026-07-10T00:00:00Z"},
		{ArtifactKey: "gemma-4-26B-A4B-it-heretic-4bit", SourceKey: "aa:gemma-4-26b-a4b-it", Confidence: "low", Basis: "Reviewed Gemma 4 26B A4B base identity; Heretic fine-tune and 4-bit quantization may change capability.", ReviewedAt: "2026-07-10T00:00:00Z"},
		{ArtifactKey: "gpt-oss-20b-MXFP4-Q8", SourceKey: "gpt-oss-20b-high", Confidence: "low", Basis: "Reviewed GPT OSS 20B base identity; quantization and reasoning-effort parity are not independently proven.", ReviewedAt: "2026-07-10T00:00:00Z"},
		{ArtifactKey: "granite-4.1-8b-nvfp4", SourceKey: "aa:granite-4-1-8b", Confidence: "medium", Basis: "Reviewed Granite 4.1 8B base identity with NVFP4 packaging variance.", ReviewedAt: "2026-07-10T00:00:00Z"},
	}
	if registry.Version != 1 || !slices.Equal(registry.Projections, want) {
		t.Fatalf("unexpected built-in registry: %#v", registry)
	}
}

func TestBuildReportAppendsProjectedRowsOutsideFormulaPopulation(t *testing.T) {
	registry, err := loadProjectionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	models := make([]Model, 0, len(registry.Projections))
	for i, projection := range registry.Projections {
		models = append(models, externalTestModel(projection.SourceKey, float64(30+i*10), 60, 60, 60, 60))
	}
	report, err := BuildReport(Result{Models: models}, "coding")
	if err != nil {
		t.Fatal(err)
	}
	if report.Formula.Population != len(models) || len(report.Models) != len(models)*2 {
		t.Fatalf("projections changed formula population: population=%d rows=%d", report.Formula.Population, len(report.Models))
	}
	if report.Projections.Configured != 6 || report.Projections.Applied != 6 || len(report.Projections.Missing) != 0 {
		t.Fatalf("unexpected projection diagnostics: %#v", report.Projections)
	}
	for _, projection := range registry.Projections {
		row, ok := reportModelByKey(report.Models, projection.ArtifactKey)
		if !ok || row.IdentityMatch != IdentityMatchProjected || row.Projection == nil {
			t.Fatalf("projection %q missing or unlabeled: %#v", projection.ArtifactKey, row)
		}
		if row.Projection.SourceKey != projection.SourceKey || row.Projection.Confidence != projection.Confidence {
			t.Fatalf("projection provenance mismatch: %#v", row.Projection)
		}
		if row.Projection.Basis != projection.Basis || row.Projection.ReviewedAt != projection.ReviewedAt {
			t.Fatalf("projection review provenance mismatch: %#v", row.Projection)
		}
		source, _ := reportModelByKey(report.Models, projection.SourceKey)
		if row.Scores["coding"].Score != source.Scores["coding"].Score {
			t.Fatalf("projection score differs from upstream: projected=%#v source=%#v", row.Scores["coding"], source.Scores["coding"])
		}
		if confidenceRank(row.Scores["coding"].Confidence) > confidenceRank(projection.Confidence) {
			t.Fatalf("projection confidence was not capped: %#v", row.Scores["coding"])
		}
	}
}

func TestMissingProjectionSourceIsReported(t *testing.T) {
	report, err := BuildReport(Result{Models: []Model{externalTestModel("gpt-oss-20b-high", 50, 50, 50, 50, 50)}}, "coding")
	if err != nil {
		t.Fatal(err)
	}
	if report.Projections.Applied != 1 || len(report.Projections.Missing) != 5 {
		t.Fatalf("missing projections were not diagnosed: %#v", report.Projections)
	}
	if _, ok := reportModelByKey(report.Models, "Qwen3.6-27B-MLX-4bit"); ok {
		t.Fatal("projection row was created without its upstream source")
	}
}

func TestProjectionDeepCopyDoesNotMutateUpstream(t *testing.T) {
	disagreement := 4.0
	price := 2.0
	source := ReportModel{
		Key: "upstream", Name: "Upstream", Open: boolPtr(true),
		Scores:            map[string]*ExternalScore{"coding": {Score: 80, Confidence: "high", Sources: []string{"llm_stats"}, Disagreement: &disagreement}},
		MetricPercentiles: map[string]float64{"llm_code_index": 80},
		LLMStats:          &LLMStatsMetrics{OutputPrice: &price, GPQA: float64Ptr(80), Indexes: map[string]Index{"code": {Conservative: 80}}},
		AA:                &ArtificialMetrics{Coding: float64Ptr(80)},
	}
	rows, missing, err := appendProjectedRows([]ReportModel{source}, []projectionSpec{{ArtifactKey: "local", SourceKey: "upstream", Confidence: "low"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 || len(rows) != 2 {
		t.Fatalf("unexpected projection result: rows=%#v missing=%#v", rows, missing)
	}
	projected := rows[1]
	projected.Scores["coding"].Score = 1
	projected.Scores["coding"].Sources[0] = "changed"
	*projected.Scores["coding"].Disagreement = 1
	projected.MetricPercentiles["llm_code_index"] = 1
	*projected.LLMStats.GPQA = 1
	projected.LLMStats.Indexes["code"] = Index{Conservative: 1}
	*projected.AA.Coding = 1
	if source.Scores["coding"].Score != 80 || source.Scores["coding"].Sources[0] != "llm_stats" || *source.Scores["coding"].Disagreement != 4 ||
		source.MetricPercentiles["llm_code_index"] != 80 || *source.LLMStats.OutputPrice != 2 || *source.LLMStats.GPQA != 80 || source.LLMStats.Indexes["code"].Conservative != 80 ||
		*source.AA.Coding != 80 || !*source.Open {
		t.Fatalf("projected row mutated upstream: %#v", source)
	}
}

func TestProjectionRemovesHostedOperationalEvidence(t *testing.T) {
	price, speed, latency := 2.0, 80.0, 0.5
	source := ReportModel{
		Key: "upstream", Name: "Upstream", Open: boolPtr(false),
		Scores: map[string]*ExternalScore{
			"overall": {Score: 70}, "price": {Score: 90}, "speed": {Score: 80}, "value": {Score: 75},
		},
		MetricPercentiles: map[string]float64{"aa_output_price": 90, "aa_output_speed": 80, "aa_intelligence_general": 70},
		LLMStats:          &LLMStatsMetrics{InputPrice: &price, OutputPrice: &price, Throughput: &speed, Latency: &latency},
		AA:                &ArtificialMetrics{InputPrice: &price, OutputPrice: &price, OutputTokensPS: &speed, TTFTSeconds: &latency, E2ESeconds: &latency},
	}
	rows, _, err := appendProjectedRows([]ReportModel{source}, []projectionSpec{{ArtifactKey: "local", SourceKey: "upstream", Confidence: "low"}})
	if err != nil {
		t.Fatal(err)
	}
	projected := rows[1]
	if projected.Open != nil || projected.Scores["price"] != nil || projected.Scores["speed"] != nil || projected.Scores["value"] != nil {
		t.Fatalf("projection retained hosted operational scores: %#v", projected)
	}
	if _, ok := projected.MetricPercentiles["aa_output_price"]; ok {
		t.Fatalf("projection retained hosted operational percentile: %#v", projected.MetricPercentiles)
	}
	if projected.LLMStats.InputPrice != nil || projected.LLMStats.OutputPrice != nil || projected.LLMStats.Throughput != nil || projected.LLMStats.Latency != nil ||
		projected.AA.InputPrice != nil || projected.AA.OutputPrice != nil || projected.AA.OutputTokensPS != nil || projected.AA.TTFTSeconds != nil || projected.AA.E2ESeconds != nil {
		t.Fatalf("projection retained hosted operational raw values: %#v %#v", projected.LLMStats, projected.AA)
	}
	if projected.Scores["overall"] == nil || projected.MetricPercentiles["aa_intelligence_general"] != 70 {
		t.Fatal("projection removed capability evidence")
	}
}

func TestProjectionRejectsCanonicalKeyCollision(t *testing.T) {
	rows := []ReportModel{{Key: "upstream", Scores: map[string]*ExternalScore{}}}
	if _, _, err := appendProjectedRows(rows, []projectionSpec{{ArtifactKey: "upstream", SourceKey: "upstream", Confidence: "low"}}); err == nil {
		t.Fatal("expected canonical key collision error")
	}
}

func TestReplacementProjectionRegistryIsDeterministicallyOrdered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projections.json")
	data := []byte(`{"version":1,"projections":[{"artifact_key":"z","source_key":"s","confidence":"low","basis":"reviewed","reviewed_at":"2026-07-10T00:00:00Z"},{"artifact_key":"a","source_key":"s","confidence":"medium","basis":"reviewed","reviewed_at":"2026-07-10T00:00:00Z"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := loadProjectionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{registry.Projections[0].ArtifactKey, registry.Projections[1].ArtifactKey}; !slices.Equal(got, []string{"a", "z"}) {
		t.Fatalf("replacement registry order is nondeterministic: %v", got)
	}
	report, err := BuildReportWithProjectionFile(Result{Models: []Model{externalTestModel("s", 50, 50, 50, 50, 50)}}, "coding", path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Projections.RegistrySource != path || report.Projections.Applied != 2 {
		t.Fatalf("replacement registry was not applied: %#v", report.Projections)
	}
}

func TestRenderMarkdownKeepsTrackedProjectionsOutsideCanonicalLimit(t *testing.T) {
	report := Report{
		RankingProfile: "coding",
		Projections:    ProjectionDiagnostics{RegistrySource: "embedded", Version: 1, Configured: 1, Applied: 1},
		Models: []ReportModel{
			{Key: "canonical", Name: "Canonical", Scores: map[string]*ExternalScore{"coding": {Score: 90, Confidence: "high"}}},
			{Key: "local", Name: "Local Artifact", Projection: &ProjectionInfo{SourceKey: "canonical", Confidence: "low"}, Scores: map[string]*ExternalScore{"coding": {Score: 90, Confidence: "low"}}},
		},
	}
	markdown := RenderMarkdown(report, 1)
	if !strings.Contains(markdown, "## Tracked local/OSS projections") || !strings.Contains(markdown, "| Local Artifact | canonical |") {
		t.Fatalf("tracked projection was hidden by canonical limit:\n%s", markdown)
	}
	if !strings.Contains(markdown, "local/projected") {
		t.Fatalf("projection copied upstream access or price labeling:\n%s", markdown)
	}
	if !strings.Contains(markdown, "Selected (coding)") {
		t.Fatalf("projection table omitted selected profile:\n%s", markdown)
	}
}

func reportModelByKey(rows []ReportModel, key string) (ReportModel, bool) {
	for _, row := range rows {
		if row.Key == key {
			return row, true
		}
	}
	return ReportModel{}, false
}

func confidenceRank(value string) int {
	return map[string]int{"low": 0, "medium": 1, "high": 2}[value]
}
