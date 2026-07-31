package evals

import (
	"encoding/json"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestEmpiricalPercentileUsesMidranks(t *testing.T) {
	values := []float64{1, 2, 2, 4}
	closeTo(t, empiricalPercentile(values, 1, true), 12.5)
	closeTo(t, empiricalPercentile(values, 2, true), 50)
	closeTo(t, empiricalPercentile(values, 4, true), 87.5)
	closeTo(t, empiricalPercentile(values, 1, false), 87.5)
}

func TestBuildReportExternalScoresAreOrderInvariantAndMonotonic(t *testing.T) {
	models := []Model{
		externalTestModel("low", 20, 20, 20, 10, 10),
		externalTestModel("middle", 50, 50, 50, 50, 50),
		externalTestModel("high", 80, 80, 80, 90, 90),
	}
	result := Result{GeneratedAt: time.Unix(1, 0).UTC(), AAVersion: 4.1, Models: models}
	report, err := BuildReport(result, "coding")
	if err != nil {
		t.Fatal(err)
	}
	if report.FormulaVersion != FormulaVersion || report.RankingProfile != "coding" {
		t.Fatalf("unexpected report metadata: %#v", report)
	}
	if report.Models[0].Key != "high" || report.Models[2].Key != "low" {
		t.Fatalf("coding rank is not monotonic: %#v", report.Models)
	}
	assertAllScoresBounded(t, report)

	reversed := slices.Clone(models)
	slices.Reverse(reversed)
	reordered, err := BuildReport(Result{Models: reversed}, "coding")
	if err != nil {
		t.Fatal(err)
	}
	for key, score := range scoresByKey(report, "coding") {
		closeTo(t, scoresByKey(reordered, "coding")[key], score)
	}
	for i := range report.Models {
		if report.Models[i].Key != reordered.Models[i].Key {
			t.Fatalf("input order changed output ordering: %v vs %v", report.Models, reordered.Models)
		}
	}
}

func TestMissingMetricContributesNeutralValueAndEvidenceBounds(t *testing.T) {
	models := []Model{
		{Key: "a", Name: "a", LLMStats: &LLMStatsMetrics{Indexes: map[string]Index{"code": {Conservative: 10}}}},
		{Key: "b", Name: "b", LLMStats: &LLMStatsMetrics{Indexes: map[string]Index{"code": {Conservative: 20}}}},
	}
	report, err := BuildReport(Result{Models: models}, "coding")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range report.Models {
		score := row.Scores["coding"]
		if score == nil {
			t.Fatalf("half-covered coding score was omitted: %#v", row)
		}
		closeTo(t, score.Coverage, .5)
		closeTo(t, score.Score, .5*row.MetricPercentiles["llm_code_index"]+25)
		if !(score.Low < score.Score && score.Score < score.High) {
			t.Fatalf("missing evidence did not widen bounds: %#v", score)
		}
	}
}

func TestOverallRequiresSixtyPercentCoverage(t *testing.T) {
	model := Model{Key: "sparse", Name: "sparse", AA: &ArtificialMetrics{Intelligence: float64Ptr(50)}}
	report, err := BuildReport(Result{Models: []Model{model}}, "overall")
	if err != nil {
		t.Fatal(err)
	}
	if report.Models[0].Scores["overall"] != nil {
		t.Fatalf("sparse model received overall score: %#v", report.Models[0].Scores["overall"])
	}
}

func TestNormalizedIdentityCapsScoreConfidenceAtMedium(t *testing.T) {
	models := []Model{
		externalTestModel("low", 20, 20, 20, 20, 20),
		externalTestModel("high", 80, 80, 80, 80, 80),
	}
	models[1].IdentityMatch = IdentityMatchNormalized
	reference, err := loadFormulaReference()
	if err != nil {
		t.Fatal(err)
	}
	metrics, _, _ := normalizeExternalMetrics(models, reference)
	rows := buildScoredRows(models, metrics, "")
	for _, row := range rows {
		if row.Key != "high" {
			continue
		}
		if score := row.Scores["overall"]; score == nil || score.Confidence != "medium" {
			t.Fatalf("normalized identity confidence was not capped: %#v", score)
		}
		return
	}
	t.Fatal("normalized model missing from scored rows")
}

func TestJackknifePreservesCoverageGates(t *testing.T) {
	models := []Model{{
		Key: "sparse", Name: "sparse",
		LLMStats: &LLMStatsMetrics{SWEPro: float64Ptr(50), Indexes: map[string]Index{}},
	}}
	reference, err := loadFormulaReference()
	if err != nil {
		t.Fatal(err)
	}
	metrics, _, _ := normalizeExternalMetrics(models, reference)
	rows := buildScoredRows(models, metrics, "aa_intelligence_general")
	if score := rows[0].Scores["coding"]; score != nil {
		t.Fatalf("jackknife admitted a model below the normal coverage gate: %#v", score)
	}
}

func TestSourceComponentRequiresHalfCoverageBeforeFusion(t *testing.T) {
	models := []Model{
		{Key: "low", Name: "low", AA: &ArtificialMetrics{Intelligence: float64Ptr(20), Coding: float64Ptr(20), Agentic: float64Ptr(20)}, LLMStats: &LLMStatsMetrics{Indexes: map[string]Index{"reasoning": {Conservative: 20}}}},
		{Key: "high", Name: "high", AA: &ArtificialMetrics{Intelligence: float64Ptr(80), Coding: float64Ptr(80), Agentic: float64Ptr(80)}, LLMStats: &LLMStatsMetrics{Indexes: map[string]Index{"reasoning": {Conservative: 80}}}},
	}
	reference, err := loadFormulaReference()
	if err != nil {
		t.Fatal(err)
	}
	metrics, _, _ := normalizeExternalMetrics(models, reference)
	rows := buildScoredRows(models, metrics, "")
	for _, row := range rows {
		score := row.Scores["agents"]
		if score == nil || !slices.Equal(score.Sources, []string{"artificial_analysis"}) {
			t.Fatalf("under-covered LLM Stats component was fused: %#v", score)
		}
		if score.Confidence != "medium" {
			t.Fatalf("single-source confidence was not capped: %#v", score)
		}
	}
}

func TestBuildReportJSONIsDeterministic(t *testing.T) {
	result := Result{GeneratedAt: time.Unix(123, 0).UTC(), AAVersion: 4.1, Models: []Model{
		externalTestModel("low", 20, 20, 20, 20, 20),
		externalTestModel("high", 80, 80, 80, 80, 80),
	}}
	var baseline []byte
	for i := 0; i < 20; i++ {
		report, err := BuildReport(result, "overall")
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			baseline = data
			continue
		}
		if !slices.Equal(data, baseline) {
			t.Fatal("identical input produced different report JSON")
		}
	}
}

func TestFormulaWeightsSumToOne(t *testing.T) {
	byDimensionSource := map[string]float64{}
	for _, metric := range externalMetrics {
		byDimensionSource[metric.dimension+"/"+metric.source] += metric.weight
	}
	for key, sum := range byDimensionSource {
		closeTo(t, sum, 1)
		if sum != 1 {
			t.Fatalf("metric weights for %s sum to %v", key, sum)
		}
	}
	for _, profile := range externalProfiles {
		sum := 0.0
		for _, weight := range profile.weights {
			sum += weight
		}
		closeTo(t, sum, 1)
	}
}

func TestBuildReportRejectsUnknownProfile(t *testing.T) {
	_, err := BuildReport(Result{}, "local-secret-score")
	if err == nil || !strings.Contains(err.Error(), "supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProductionEvalCodeHasNoLocalTargetDependency(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"calibrationAnchor", "RidgeRow", "effective_score", ".work/", "local-model-eval"} {
			if strings.Contains(string(data), forbidden) {
				t.Fatalf("%s retains forbidden local-eval dependency %q", entry.Name(), forbidden)
			}
		}
	}
}

func TestRenderMarkdownDescribesExternalOnlyFormula(t *testing.T) {
	report, err := BuildReport(Result{GeneratedAt: time.Unix(1, 0).UTC(), Models: []Model{
		externalTestModel("a", 40, 40, 40, 40, 40),
		externalTestModel("b", 60, 60, 60, 60, 60),
	}}, "overall")
	if err != nil {
		t.Fatal(err)
	}
	markdown := RenderMarkdown(report, 1)
	for _, want := range []string{FormulaVersion, "Ranked by: `overall`", "do not use local evaluation targets", "Missing evidence contributes a neutral 50", "Output $/1M"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

func TestFormatOutputPrice(t *testing.T) {
	tests := []struct {
		name string
		row  ReportModel
		want string
	}{
		{name: "missing", row: ReportModel{}, want: "—"},
		{name: "llm only", row: ReportModel{LLMStats: &LLMStatsMetrics{OutputPrice: float64Ptr(.25)}}, want: "$0.250"},
		{name: "same quote", row: ReportModel{LLMStats: &LLMStatsMetrics{OutputPrice: float64Ptr(2)}, AA: &ArtificialMetrics{OutputPrice: float64Ptr(2)}}, want: "$2.00"},
		{name: "source range", row: ReportModel{LLMStats: &LLMStatsMetrics{OutputPrice: float64Ptr(3)}, AA: &ArtificialMetrics{OutputPrice: float64Ptr(1.5)}}, want: "$1.50–$3.00"},
		{name: "free", row: ReportModel{AA: &ArtificialMetrics{OutputPrice: float64Ptr(0)}}, want: "$0"},
		{name: "projection", row: ReportModel{Projection: &ProjectionInfo{SourceKey: "upstream", Confidence: "medium"}, AA: &ArtificialMetrics{OutputPrice: float64Ptr(2)}}, want: "local/projected"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := fmtOutputPrice(test.row); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func externalTestModel(key string, intelligence, coding, agentic, general, code float64) Model {
	return Model{
		Key: key, Name: key, IdentityMatch: "exact",
		AA: &ArtificialMetrics{Intelligence: float64Ptr(intelligence), Coding: float64Ptr(coding), Agentic: float64Ptr(agentic)},
		LLMStats: &LLMStatsMetrics{
			GPQA: float64Ptr(general / 100), SWEVerified: float64Ptr(code / 100), SWEPro: float64Ptr(code / 100),
			Indexes: map[string]Index{
				"general": {Conservative: general}, "reasoning": {Conservative: general}, "instruction_following": {Conservative: general}, "factuality": {Conservative: general},
				"code": {Conservative: code}, "agents": {Conservative: general}, "tool_calling": {Conservative: general}, "structured_output": {Conservative: general},
				"writing": {Conservative: general}, "creativity": {Conservative: general}, "language": {Conservative: general}, "communication": {Conservative: general},
				"long_context": {Conservative: general}, "grounding": {Conservative: general},
			},
		},
	}
}

func scoresByKey(report Report, profile string) map[string]float64 {
	result := map[string]float64{}
	for _, row := range report.Models {
		if score := row.Scores[profile]; score != nil {
			result[row.Key] = score.Score
		}
	}
	return result
}

func assertAllScoresBounded(t *testing.T, report Report) {
	t.Helper()
	for _, row := range report.Models {
		for name, score := range row.Scores {
			if score == nil {
				continue
			}
			for _, value := range []float64{score.Score, score.RawScore, score.Low, score.High, score.Coverage} {
				if math.IsNaN(value) || math.IsInf(value, 0) {
					t.Fatalf("%s/%s contains non-finite value: %#v", row.Key, name, score)
				}
			}
			if score.Score < 0 || score.Score > 100 || score.Low < 0 || score.High > 100 || score.Low > score.High {
				t.Fatalf("%s/%s is outside bounds: %#v", row.Key, name, score)
			}
		}
	}
}

func float64Ptr(value float64) *float64 { return &value }

func closeTo(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %v, want %v", got, want)
	}
}
