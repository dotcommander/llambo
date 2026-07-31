package evals

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const FormulaVersion = "LES-1"

type Report struct {
	GeneratedAt    time.Time               `json:"generated_at"`
	FormulaVersion string                  `json:"formula_version"`
	RankingProfile string                  `json:"ranking_profile"`
	AAVersion      float64                 `json:"artificial_analysis_index_version,omitempty"`
	Sources        []SourceStatus          `json:"sources"`
	Projections    ProjectionDiagnostics   `json:"projections"`
	Eligibility    *EligibilityDiagnostics `json:"eligibility,omitempty"`
	OMLX           *OMLXDiagnostics        `json:"omlx,omitempty"`
	Formula        FormulaDiagnostics      `json:"formula"`
	Models         []ReportModel           `json:"models"`
}

type FormulaDiagnostics struct {
	Method           string                  `json:"method"`
	Population       int                     `json:"population"`
	DualSourceModels int                     `json:"dual_source_models"`
	Metrics          []MetricDiagnostic      `json:"metrics"`
	Profiles         []ProfileDiagnostic     `json:"profiles"`
	Stability        StabilityDiagnostics    `json:"stability"`
	Reference        FormulaReferenceSummary `json:"reference"`
	Drift            DriftDiagnostics        `json:"drift"`
}

type StabilityDiagnostics struct {
	Status          string  `json:"status"`
	Variants        int     `json:"jackknife_variants"`
	MeanKendall     float64 `json:"mean_kendall"`
	MinTop20Overlap float64 `json:"min_top_20_overlap"`
	WorstMetric     string  `json:"worst_metric,omitempty"`
}

type MetricDiagnostic struct {
	Name                string  `json:"name"`
	Dimension           string  `json:"dimension"`
	Source              string  `json:"source"`
	Weight              float64 `json:"weight"`
	Direction           string  `json:"direction"`
	Population          int     `json:"population"`
	ReferencePopulation int     `json:"reference_population"`
}

type ProfileDiagnostic struct {
	Name    string             `json:"name"`
	Weights map[string]float64 `json:"weights"`
}

type ExternalScore struct {
	Score         float64  `json:"score"`
	RawScore      float64  `json:"raw_score"`
	Low           float64  `json:"evidence_low"`
	High          float64  `json:"evidence_high"`
	Coverage      float64  `json:"coverage"`
	Confidence    string   `json:"confidence"`
	Signals       int      `json:"signals"`
	Sources       []string `json:"sources"`
	Disagreement  *float64 `json:"source_disagreement,omitempty"`
	RankSpread    int      `json:"rank_spread"`
	RankStability string   `json:"rank_stability,omitempty"`
}

type ReportModel struct {
	Key               string                    `json:"key"`
	Name              string                    `json:"name"`
	Organization      string                    `json:"organization,omitempty"`
	IdentityMatch     IdentityMatch             `json:"identity_match"`
	Open              *bool                     `json:"open,omitempty"`
	Scores            map[string]*ExternalScore `json:"scores"`
	MetricPercentiles map[string]float64        `json:"metric_percentiles,omitempty"`
	LLMStats          *LLMStatsMetrics          `json:"llm_stats,omitempty"`
	AA                *ArtificialMetrics        `json:"artificial_analysis,omitempty"`
	Projection        *ProjectionInfo           `json:"projection,omitempty"`
}

func BuildReport(result Result, rankBy string) (Report, error) {
	return BuildReportWithProjectionFile(result, rankBy, "")
}

// BuildReportWithProjectionFile replaces the embedded projection registry with
// path when path is non-empty. Projections are applied only after LES-1 scoring
// and stability analysis, so they never enter the formula population.
func BuildReportWithProjectionFile(result Result, rankBy, path string) (Report, error) {
	if rankBy == "" {
		rankBy = "overall"
	}
	if !isSupportedProfile(rankBy) {
		return Report{}, fmt.Errorf("unsupported ranking profile %q (supported: %s)", rankBy, strings.Join(SupportedRankingProfiles(), ", "))
	}

	reference, err := loadFormulaReference()
	if err != nil {
		return Report{}, err
	}
	metricPercentiles, diagnostics, currentValues := normalizeExternalMetrics(result.Models, reference)
	drift := evaluateReferenceDrift(result.Models, result.AAVersion, reference, currentValues)
	dualSource := 0
	for _, model := range result.Models {
		if model.AA != nil && model.LLMStats != nil {
			dualSource++
		}
	}
	rows := buildScoredRows(result.Models, metricPercentiles, "")
	stability := applyRankStability(rows, result.Models, metricPercentiles, rankBy)
	population := len(rows)
	projectionRegistry, err := loadProjectionRegistry(path)
	if err != nil {
		return Report{}, err
	}
	rows, missingProjections, err := appendProjectedRows(rows, projectionRegistry.Projections)
	if err != nil {
		return Report{}, err
	}
	projectionSource := "embedded"
	if path != "" {
		projectionSource = path
	}
	projectionDiagnostics := ProjectionDiagnostics{
		RegistrySource: projectionSource,
		Version:        projectionRegistry.Version,
		Configured:     len(projectionRegistry.Projections),
		Applied:        len(projectionRegistry.Projections) - len(missingProjections),
		Missing:        missingProjections,
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i].Scores[rankBy], rows[j].Scores[rankBy]
		if left == nil {
			return false
		}
		if right == nil {
			return true
		}
		if left.Score == right.Score {
			if rows[i].Name == rows[j].Name {
				return rows[i].Key < rows[j].Key
			}
			return rows[i].Name < rows[j].Name
		}
		return left.Score > right.Score
	})

	profiles := make([]ProfileDiagnostic, 0, len(dimensionNames)+len(externalProfiles))
	for _, name := range dimensionNames {
		profiles = append(profiles, ProfileDiagnostic{Name: name, Weights: map[string]float64{name: 1}})
	}
	for _, profile := range externalProfiles {
		profiles = append(profiles, ProfileDiagnostic{Name: profile.name, Weights: cloneWeights(profile.weights)})
	}
	return Report{
		GeneratedAt: result.GeneratedAt, FormulaVersion: FormulaVersion, RankingProfile: rankBy,
		AAVersion: result.AAVersion, Sources: result.Sources, Projections: projectionDiagnostics,
		Formula: FormulaDiagnostics{
			Method:     "within-metric empirical percentiles; missing signals contribute neutral 50 with exact evidence bounds; exact and normalized source matches are averaged; capability, coverage, disagreement, and stability remain separate",
			Population: population, DualSourceModels: dualSource, Metrics: diagnostics, Profiles: profiles, Stability: stability,
			Reference: FormulaReferenceSummary{CreatedAt: reference.CreatedAt, AAVersion: reference.AAVersion, SourceModelCounts: reference.SourceModelCounts, SourceFingerprints: reference.SourceFingerprints}, Drift: drift,
		},
		Models: rows,
	}, nil
}

func buildScoredRows(models []Model, metrics []normalizedMetric, omitMetric string) []ReportModel {
	rows := make([]ReportModel, len(models))
	dimensionCoverageGate, profileCoverageGate := .50, .60
	for i, model := range models {
		row := ReportModel{
			Key: model.Key, Name: model.Name, Organization: model.Organization,
			IdentityMatch: model.IdentityMatch, Open: model.Open,
			Scores: make(map[string]*ExternalScore), MetricPercentiles: make(map[string]float64), LLMStats: model.LLMStats, AA: model.AA,
		}
		for _, metric := range metrics {
			if percentile, ok := metric.percentile[i]; ok {
				row.MetricPercentiles[metric.spec.name] = percentile
			}
		}
		for _, dimension := range dimensionNames {
			row.Scores[dimension] = scoreDimension(i, dimension, metrics, omitMetric, dimensionCoverageGate)
		}
		for _, profile := range externalProfiles {
			row.Scores[profile.name] = scoreComposite(row.Scores, profile.weights, "", profileCoverageGate)
		}
		if row.IdentityMatch == IdentityMatchNormalized {
			for _, score := range row.Scores {
				if score != nil && score.Confidence == "high" {
					score.Confidence = "medium"
				}
			}
		}
		rows[i] = row
	}
	return rows
}
