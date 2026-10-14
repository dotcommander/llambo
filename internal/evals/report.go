package evals

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Repeated string literals consolidated by goconst.
const (
	matrix1 = "matrix"
)

// FormulaVersion aliases the capability formula version stamped on reports.
const FormulaVersion = CategoryFormulaVersion

// Report is the full evals report: sources, diagnostics, rankings, and model
// rows.
type Report struct {
	ReportSchemaVersion int                        `json:"report_schema_version"`
	GeneratedAt         time.Time                  `json:"generated_at"`
	FormulaVersion      string                     `json:"formula_version"`
	RankingProfile      string                     `json:"ranking_profile"`
	AAVersion           float64                    `json:"artificial_analysis_index_version,omitempty"`
	Sources             []SourceStatus             `json:"sources"`
	Projections         ProjectionDiagnostics      `json:"projections"`
	Eligibility         *EligibilityDiagnostics    `json:"eligibility,omitempty"`
	OMLX                *OMLXDiagnostics           `json:"omlx,omitempty"`
	Formula             FormulaDiagnostics         `json:"formula"`
	CoverageCampaign    *CoverageCampaign          `json:"coverage_campaign,omitempty"`
	Validation          *ValidationDiagnostic      `json:"validation_diagnostic,omitempty"`
	Models              []ReportModel              `json:"models"`
	ProjectedModels     []ReportModel              `json:"local_projections,omitempty"`
	CategoryRankings    map[string]CategoryRanking `json:"category_rankings,omitempty"`
}

// CategoryRanking is the ranked model list for one capability category.
type CategoryRanking struct {
	Category string                `json:"category"`
	Entries  []CategoryRankedModel `json:"entries"`
	Winner   string                `json:"winner,omitempty"`
	Status   string                `json:"status"`
}

// CategoryRankedModel is one ranked model row with winner status.
type CategoryRankedModel struct {
	Key                string  `json:"key"`
	Score              float64 `json:"score"`
	TrustedCoverage    float64 `json:"trusted_coverage"`
	IdentityConfidence float64 `json:"identity_confidence"`
	WinnerStatus       string  `json:"winner_status,omitempty"`
	Estimated          bool    `json:"estimated,omitempty"`
}

// FormulaDiagnostics explains how capability scores were computed.
type FormulaDiagnostics struct {
	Method           string                  `json:"method"`
	Population       int                     `json:"population"`
	Reference        FormulaReferenceSummary `json:"reference"`
	Drift            DriftDiagnostics        `json:"drift"`
	DualSourceModels int                     `json:"dual_source_models,omitempty"`
	Metrics          []MetricDiagnostic      `json:"metrics,omitempty"`
	Profiles         []ProfileDiagnostic     `json:"profiles,omitempty"`
	Stability        StabilityDiagnostics    `json:"stability,omitempty"`
}

// MetricDiagnostic explains one benchmark metric's weight and population.
type MetricDiagnostic struct {
	Name, Dimension, Source, Direction string
	Weight                             float64
	Population, ReferencePopulation    int
}

// ProfileDiagnostic lists the effective weights for one ranking profile.
type ProfileDiagnostic struct {
	Name    string
	Weights map[string]float64
}

// StabilityDiagnostics summarizes jackknife stability of the rankings.
type StabilityDiagnostics struct {
	Status          string
	Variants        int     `json:"jackknife_variants"`
	MeanKendall     float64 `json:"mean_kendall"`
	MinTop20Overlap float64 `json:"min_top_20_overlap"`
	WorstMetric     string  `json:"worst_metric,omitempty"`
}

// ExternalScore remains the operational speed/price representation. It is not
// a capability score and is intentionally excluded from the JSON score schema.
type ExternalScore struct {
	Score        float64  `json:"score"`
	RawScore     float64  `json:"raw_score"`
	Low          float64  `json:"evidence_low"`
	High         float64  `json:"evidence_high"`
	Coverage     float64  `json:"coverage"`
	Confidence   string   `json:"confidence"`
	Signals      int      `json:"signals"`
	Sources      []string `json:"sources,omitempty"`
	Disagreement *float64 `json:"source_disagreement,omitempty"`
}

// ReportModel is one published model row with scores and evidence.
type ReportModel struct {
	Key               string                     `json:"key"`
	Name              string                     `json:"name"`
	Organization      string                     `json:"organization,omitempty"`
	IdentityMatch     IdentityMatch              `json:"identity_match"`
	Open              *bool                      `json:"open,omitempty"`
	LlamboScores      map[string]*LlamboScore    `json:"llambo_scores"`
	Scores            map[string]*ExternalScore  `json:"operational_scores,omitempty"`
	MetricPercentiles map[string]float64         `json:"metric_percentiles,omitempty"`
	LLMStats          *LLMStatsMetrics           `json:"llm_stats,omitempty"`
	AA                *ArtificialMetrics         `json:"artificial_analysis,omitempty"`
	Benchmarks        map[string]BenchmarkResult `json:"benchmarks,omitempty"`
	Projection        *ProjectionInfo            `json:"projection,omitempty"`
	// LlamboScore remains private compatibility state while v1 artifacts are
	// readable. It is never rendered, serialized, or calculated by LLAMBO-6.
	LlamboScore       *OverallScore     `json:"-"`
	UnresolvedReasons map[string]string `json:"unresolved_reasons,omitempty"`
}

// BuildReport scores and ranks eval results for the requested profile.
func BuildReport(result Result, rankBy string) (Report, error) {
	return BuildReportWithProjectionFile(result, rankBy, "")
}

// BuildReportWithProjectionFile applies reviewed local projections from a
// projection file after canonical scoring.
func BuildReportWithProjectionFile(result Result, rankBy, path string) (Report, error) {
	return buildReportWithScoringLoader(result, rankBy, path, loadScoringContext)
}

func buildReportWithScoringLoader(result Result, rankBy, path string, loadContext func() (scoringContext, error)) (Report, error) {
	if rankBy == "" {
		rankBy = matrix1
	}
	if !isSupportedProfile(rankBy) {
		return Report{}, fmt.Errorf("unsupported ranking profile %q (supported: %s)", rankBy, strings.Join(SupportedRankingProfiles(), ", "))
	}
	context, err := loadContext()
	if err != nil {
		return Report{}, fmt.Errorf("initialize scoring context: %w", err)
	}
	reference := context.reference
	rows := make([]ReportModel, len(result.Models))
	for i, model := range result.Models {
		row := ReportModel{Key: model.Key, Name: model.Name, Organization: model.Organization, IdentityMatch: model.IdentityMatch, Open: model.Open, LLMStats: model.LLMStats, AA: model.AA, Benchmarks: model.Benchmarks, LlamboScores: make(map[string]*LlamboScore, len(categorySpecs)), UnresolvedReasons: map[string]string{}, Scores: map[string]*ExternalScore{}}
		for _, category := range categorySpecs {
			row.LlamboScores[category.name] = scoreCategoryV3WithContext(model, category, result.Models, context)
			if row.LlamboScores[category.name] == nil {
				row.UnresolvedReasons[category.name] = categoryUnresolvedReasonWithContext(model, category, context)
			}
		}
		for _, dimension := range []string{speed1, price1} {
			row.Scores[dimension] = operationalScore(model, dimension, reference)
		}
		rows[i] = row
	}
	registry, err := loadProjectionRegistry(path)
	if err != nil {
		return Report{}, err
	}
	rows, missing, err := appendProjectedRows(rows, registry.Projections)
	if err != nil {
		return Report{}, err
	}
	estimator, err := loadCategoryEstimator()
	if err != nil {
		return Report{}, err
	}
	estimateOMLXRows(rows, estimator)
	projectionSource := "embedded"
	if path != "" {
		projectionSource = path
	}
	rankings := buildCategoryRankings(rows)
	applyCategoryWinnerStatuses(rows, rankings)
	sort.SliceStable(rows, func(i, j int) bool { return compareReportRows(rows[i], rows[j], rankBy) })
	coverageCampaign, err := buildCoverageCampaign(rows)
	if err != nil {
		return Report{}, err
	}
	driftModels := result.ReferenceModels
	if len(driftModels) == 0 {
		driftModels = result.Models
	}
	return Report{ReportSchemaVersion: 6, GeneratedAt: result.GeneratedAt, FormulaVersion: FormulaVersion, RankingProfile: rankBy, AAVersion: result.AAVersion, Sources: result.Sources, Projections: ProjectionDiagnostics{RegistrySource: projectionSource, Version: registry.Version, Configured: len(registry.Projections), Applied: len(registry.Projections) - len(missing), Missing: missing}, Formula: FormulaDiagnostics{Method: "LLAMBO-9 reviewed category registry; direct common frozen-cohort percentiles plus frozen calibrated OMLX-only gap estimates; evidence and estimates remain separate", Population: len(result.Models), Reference: FormulaReferenceSummary{CreatedAt: reference.CreatedAt, AAVersion: reference.AAVersion, SourceModelCounts: sourceModelCounts(result.Models), SourceFingerprints: sourceFingerprints(result.Models)}, Drift: evaluateReferenceDrift(driftModels, result.AAVersion, reference, currentReferenceValues(driftModels))}, CoverageCampaign: coverageCampaign, Models: rows, CategoryRankings: rankings}, nil
}

func categoryUnresolvedReasonWithContext(model Model, category categorySpec, context scoringContext) string {
	for _, family := range category.families {
		for _, benchmark := range family.benchmarks {
			if result, ok := model.Benchmarks[benchmark]; ok {
				if result.Quarantined {
					return "quarantined benchmark conflict: " + result.Conflict
				}
				if reason := frozenLLMStatsCohortAdmissionReasonFromCohorts(benchmark, result, context.legacyCohorts); reason != "" {
					return reason
				}
				if reason := frozenLLMStatsStatsV1CohortAdmissionReasonFromCohorts(benchmark, result, context.statsV1Cohorts); reason != "" {
					return reason
				}
			}
		}
	}
	return "no eligible reviewed frozen benchmark observation"
}
