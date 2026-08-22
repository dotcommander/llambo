package evals

import (
	"sort"
	"time"
)

// OverallScore and its component types exist only to decode retained LLAMBO-5
// snapshots. Active LLAMBO-6 code never calculates, ranks, or renders them.
type OverallScore struct {
	Score                 *float64           `json:"score"`
	Coverage              float64            `json:"coverage"`
	Confidence            string             `json:"confidence"`
	FormulaVersion        string             `json:"formula_version"`
	PopulationFingerprint string             `json:"omlx_population_fingerprint"`
	LegacyFingerprint     string             `json:"population_fingerprint,omitempty"`
	Projection            *ProjectionInfo    `json:"projection,omitempty"`
	Components            []OverallComponent `json:"components"`
	UnresolvedReasons     []OverallReason    `json:"unresolved_reasons,omitempty"`
}

type OverallComponent struct {
	Name                 string    `json:"name"`
	Weight               float64   `json:"weight"`
	Value                float64   `json:"value"`
	Contribution         float64   `json:"contribution"`
	ObservedTrust        float64   `json:"observed_trust"`
	ProjectionMultiplier float64   `json:"projection_multiplier,omitempty"`
	Source               string    `json:"source,omitempty"`
	MeasuredAt           time.Time `json:"measured_at,omitzero"`
	TimestampUnknown     bool      `json:"measurement_timestamp_unknown,omitempty"`
	Reason               string    `json:"reason,omitempty"`
}

type OverallReason struct {
	Component string `json:"component"`
	Code      string `json:"code"`
	Detail    string `json:"detail"`
}

// OMLXScoreInput is a cache-resident local inventory row. Observations remain
// accepted for caller compatibility but are operational metadata only.
type OMLXScoreInput struct {
	Key          string
	Projection   *ProjectionInfo
	Capabilities map[string]*LlamboScore
	Observations []OMLXOperationalObservation
}

type OMLXOperationalObservation struct {
	Metric           string
	Value            float64
	Source           string
	MeasuredAt       time.Time
	Successful       bool
	Benchmark        bool
	TimestampUnknown bool
}

// ApplyOMLXCategoryScores is the LLAMBO-6 inventory boundary. It never inspects
// or blends speed, latency, price, or a cross-category composite.
func ApplyOMLXCategoryScores(report *Report, inventory []OMLXScoreInput) {
	if report == nil {
		return
	}
	// A subset rerank must never leave winner markers inherited from an earlier,
	// larger population. Clear every row before selecting the frozen inventory.
	for i := range report.Models {
		for _, score := range report.Models[i].LlamboScores {
			if score != nil {
				score.WinnerStatus = ""
			}
		}
	}
	byKey := make(map[string]int, len(report.Models))
	for i := range report.Models {
		byKey[report.Models[i].Key] = i
	}
	for _, input := range inventory {
		index, found := byKey[input.Key]
		if !found {
			report.Models = append(report.Models, ReportModel{Key: input.Key, Name: input.Key, IdentityMatch: IdentityMatchProjected, Projection: cloneProjection(input.Projection), LlamboScores: cloneCategoryScores(input.Capabilities), Scores: map[string]*ExternalScore{}, UnresolvedReasons: map[string]string{}})
			index = len(report.Models) - 1
			byKey[input.Key] = index
		}
		row := &report.Models[index]
		if input.Projection != nil {
			row.Projection = cloneProjection(input.Projection)
		}
		if input.Capabilities != nil {
			row.LlamboScores = cloneCategoryScores(input.Capabilities)
		}
		if row.LlamboScores == nil {
			row.LlamboScores = map[string]*LlamboScore{}
		}
		if row.UnresolvedReasons == nil {
			row.UnresolvedReasons = map[string]string{}
		}
		for _, category := range categorySpecs {
			if _, ok := row.LlamboScores[category.name]; !ok {
				row.UnresolvedReasons[category.name] = "no eligible reviewed cached evidence"
			}
		}
	}
	selectedRows := make([]ReportModel, 0, len(inventory))
	for _, input := range inventory {
		if index, ok := byKey[input.Key]; ok {
			selectedRows = append(selectedRows, report.Models[index])
		}
	}
	report.CategoryRankings = buildCategoryRankings(selectedRows)
	applyCategoryWinnerStatuses(report.Models, report.CategoryRankings)
	if isCapabilityCategory(report.RankingProfile) {
		sort.SliceStable(report.Models, func(i, j int) bool {
			return compareReportRows(report.Models[i], report.Models[j], report.RankingProfile)
		})
	}
}

func cloneProjection(source *ProjectionInfo) *ProjectionInfo {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}
