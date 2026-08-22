package evals

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const FormulaVersion = CategoryFormulaVersion

// OverallFormulaVersion is retained only so a v1 snapshot can be recognized
// and preserved. The active evaluation path never calculates an overall score.
const OverallFormulaVersion = "LLAMBO-5"

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

type CategoryRanking struct {
	Category string                `json:"category"`
	Entries  []CategoryRankedModel `json:"entries"`
	Winner   string                `json:"winner,omitempty"`
	Status   string                `json:"status"`
}

type CategoryRankedModel struct {
	Key                string  `json:"key"`
	Score              float64 `json:"score"`
	TrustedCoverage    float64 `json:"trusted_coverage"`
	IdentityConfidence float64 `json:"identity_confidence"`
	WinnerStatus       string  `json:"winner_status,omitempty"`
}

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

type MetricDiagnostic struct {
	Name, Dimension, Source, Direction string
	Weight                             float64
	Population, ReferencePopulation    int
}
type ProfileDiagnostic struct {
	Name    string
	Weights map[string]float64
}
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

func BuildReport(result Result, rankBy string) (Report, error) {
	return BuildReportWithProjectionFile(result, rankBy, "")
}

func BuildReportWithProjectionFile(result Result, rankBy, path string) (Report, error) {
	if rankBy == "" {
		rankBy = "matrix"
	}
	if !isSupportedProfile(rankBy) {
		return Report{}, fmt.Errorf("unsupported ranking profile %q (supported: %s)", rankBy, strings.Join(SupportedRankingProfiles(), ", "))
	}
	reference, err := loadFormulaReference()
	if err != nil {
		return Report{}, err
	}
	rows := make([]ReportModel, len(result.Models))
	for i, model := range result.Models {
		row := ReportModel{Key: model.Key, Name: model.Name, Organization: model.Organization, IdentityMatch: model.IdentityMatch, Open: model.Open, LLMStats: model.LLMStats, AA: model.AA, Benchmarks: model.Benchmarks, LlamboScores: make(map[string]*LlamboScore, len(categorySpecs)), UnresolvedReasons: map[string]string{}, Scores: map[string]*ExternalScore{}}
		for _, category := range categorySpecs {
			row.LlamboScores[category.name] = scoreCategoryV3(model, category, result.Models)
			if row.LlamboScores[category.name] == nil {
				row.UnresolvedReasons[category.name] = categoryUnresolvedReason(model, category)
			}
		}
		for _, dimension := range []string{"speed", "price"} {
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
	return Report{ReportSchemaVersion: 5, GeneratedAt: result.GeneratedAt, FormulaVersion: FormulaVersion, RankingProfile: rankBy, AAVersion: result.AAVersion, Sources: result.Sources, Projections: ProjectionDiagnostics{RegistrySource: projectionSource, Version: registry.Version, Configured: len(registry.Projections), Applied: len(registry.Projections) - len(missing), Missing: missing}, Formula: FormulaDiagnostics{Method: "LLAMBO-6 reviewed category registry; frozen revision percentiles; equal independent-family contribution; source and identity trust shrink partial evidence toward neutral 50", Population: len(result.Models), Reference: FormulaReferenceSummary{CreatedAt: reference.CreatedAt, AAVersion: reference.AAVersion, SourceModelCounts: sourceModelCounts(result.Models), SourceFingerprints: sourceFingerprints(result.Models)}, Drift: evaluateReferenceDrift(driftModels, result.AAVersion, reference, currentReferenceValues(driftModels))}, CoverageCampaign: coverageCampaign, Models: rows, CategoryRankings: rankings}, nil
}

func categoryUnresolvedReason(model Model, category categorySpec) string {
	for _, family := range category.families {
		for _, benchmark := range family.benchmarks {
			if result, ok := model.Benchmarks[benchmark]; ok {
				if result.Quarantined {
					return "quarantined benchmark conflict: " + result.Conflict
				}
				if reason := frozenLLMStatsCohortAdmissionReason(benchmark, result); reason != "" {
					return reason
				}
			}
		}
	}
	return "no eligible reviewed frozen benchmark observation"
}

func compareReportRows(left, right ReportModel, rankBy string) bool {
	if rankBy == "matrix" {
		leftScored, rightScored := hasCapabilityScore(left), hasCapabilityScore(right)
		if leftScored != rightScored {
			return leftScored
		}
		if left.Name == right.Name {
			return left.Key < right.Key
		}
		return left.Name < right.Name
	}
	var leftScore, rightScore *float64
	if isCapabilityCategory(rankBy) {
		if s := left.LlamboScores[rankBy]; s != nil {
			leftScore = &s.Score
		}
		if s := right.LlamboScores[rankBy]; s != nil {
			rightScore = &s.Score
		}
	} else {
		if s := left.Scores[rankBy]; s != nil {
			leftScore = &s.Score
		}
		if s := right.Scores[rankBy]; s != nil {
			rightScore = &s.Score
		}
	}
	if leftScore == nil {
		return false
	}
	if rightScore == nil {
		return true
	}
	if *leftScore == *rightScore {
		if left.Name == right.Name {
			return left.Key < right.Key
		}
		return left.Name < right.Name
	}
	return *leftScore > *rightScore
}

func buildCategoryRankings(rows []ReportModel) map[string]CategoryRanking {
	rankings := make(map[string]CategoryRanking, len(categorySpecs))
	for _, spec := range categorySpecs {
		ranking := CategoryRanking{Category: spec.name, Status: "unresolved"}
		for _, row := range rows {
			score := row.LlamboScores[spec.name]
			if score == nil {
				continue
			}
			identity := 1.0
			if row.Projection != nil {
				identity = projectionMultiplier(row.Projection)
			}
			ranking.Entries = append(ranking.Entries, CategoryRankedModel{Key: row.Key, Score: score.Score, TrustedCoverage: score.TrustedCoverage, IdentityConfidence: identity})
		}
		sort.Slice(ranking.Entries, func(i, j int) bool {
			left, right := ranking.Entries[i], ranking.Entries[j]
			if left.Score != right.Score {
				return left.Score > right.Score
			}
			if left.TrustedCoverage != right.TrustedCoverage {
				return left.TrustedCoverage > right.TrustedCoverage
			}
			if left.IdentityConfidence != right.IdentityConfidence {
				return left.IdentityConfidence > right.IdentityConfidence
			}
			return left.Key < right.Key
		})
		if len(ranking.Entries) > 0 {
			leader := ranking.Entries[0]
			tied := 1
			for tied < len(ranking.Entries) {
				candidate := ranking.Entries[tied]
				if candidate.Score != leader.Score || candidate.TrustedCoverage != leader.TrustedCoverage || candidate.IdentityConfidence != leader.IdentityConfidence {
					break
				}
				tied++
			}
			winner := 0
			official := false
			for i := 0; i < tied; i++ {
				if familyCount(rows, spec.name, ranking.Entries[i].Key) >= 2 {
					winner, official = i, true
					break
				}
			}
			if official {
				ranking.Status, ranking.Winner = "official", ranking.Entries[winner].Key
			} else {
				ranking.Status, ranking.Winner = "provisional", leader.Key
			}
			for i := 0; i < tied; i++ {
				candidate := &ranking.Entries[i]
				if i == winner {
					candidate.WinnerStatus = ranking.Status
				} else if official && familyCount(rows, spec.name, candidate.Key) >= 2 {
					candidate.WinnerStatus = "official_co_winner"
				} else {
					candidate.WinnerStatus = "provisional_co_winner"
				}
			}
		}
		rankings[spec.name] = ranking
	}
	return rankings
}

func familyCount(rows []ReportModel, category, key string) int {
	for _, row := range rows {
		if row.Key == key && row.LlamboScores[category] != nil {
			if row.LlamboScores[category].Estimated {
				return 0
			}
			return len(row.LlamboScores[category].Families)
		}
	}
	return 0
}

func applyCategoryWinnerStatuses(rows []ReportModel, rankings map[string]CategoryRanking) {
	for category, ranking := range rankings {
		for _, entry := range ranking.Entries {
			if entry.WinnerStatus == "" {
				continue
			}
			for i := range rows {
				if rows[i].Key == entry.Key && rows[i].LlamboScores[category] != nil {
					rows[i].LlamboScores[category].WinnerStatus = entry.WinnerStatus
				}
			}
		}
	}
}

func hasCapabilityScore(model ReportModel) bool {
	for _, score := range model.LlamboScores {
		if score != nil {
			return true
		}
	}
	return false
}
