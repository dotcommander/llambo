package evals

// FilterProjectedRowsToLiveOMLX narrows projected rows only after successful
// discovery. Canonical rows and formula diagnostics are never modified. A
// discovery error omits projections whose live availability cannot be proven.
func FilterProjectedRowsToLiveOMLX(report *Report, discovery OMLXDiscovery, discoveryErr error) {
	if report == nil {
		return
	}
	diagnostics := &OMLXDiagnostics{Attempts: append([]string(nil), discovery.Attempts...), ExcludedModels: append([]string(nil), discovery.Excluded...)}
	if discoveryErr != nil {
		applyUnavailableOMLXDiscovery(report, diagnostics, discoveryErr)
		return
	}
	diagnostics.Status = "filtered"
	diagnostics.Endpoint = discovery.Endpoint
	diagnostics.Discovered = len(discovery.Models)
	live := make(map[string]struct{}, len(discovery.Models))
	for _, id := range discovery.Models {
		live[id] = struct{}{}
	}
	projected := make(map[string]struct{})
	for _, row := range report.Models {
		if row.Projection != nil {
			projected[row.Key] = struct{}{}
		}
	}
	for _, id := range discovery.Models {
		if _, ok := projected[id]; !ok {
			diagnostics.UnmatchedModels = append(diagnostics.UnmatchedModels, id)
		}
	}
	filtered := make([]ReportModel, 0, len(report.Models))
	for _, row := range report.Models {
		if row.Projection == nil {
			filtered = append(filtered, row)
			continue
		}
		if _, ok := live[row.Key]; ok {
			diagnostics.MatchedModels = append(diagnostics.MatchedModels, row.Key)
			filtered = append(filtered, row)
		} else {
			diagnostics.InactiveReviewedModels = append(diagnostics.InactiveReviewedModels, row.Key)
		}
	}
	diagnostics.MatchedModels = sortedUnique(diagnostics.MatchedModels)
	diagnostics.UnmatchedModels = sortedUnique(diagnostics.UnmatchedModels)
	diagnostics.InactiveReviewedModels = sortedUnique(diagnostics.InactiveReviewedModels)
	diagnostics.ExcludedModels = sortedUnique(diagnostics.ExcludedModels)
	report.Models = filtered
	report.OMLX = diagnostics
	refreshCoverageCampaign(report)
}

func applyUnavailableOMLXDiscovery(report *Report, diagnostics *OMLXDiagnostics, discoveryErr error) {
	diagnostics.Status = unavailable1
	diagnostics.Error = discoveryErr.Error()
	filtered := report.Models[:0]
	for _, row := range report.Models {
		if row.Projection == nil {
			filtered = append(filtered, row)
		} else {
			diagnostics.InactiveReviewedModels = append(diagnostics.InactiveReviewedModels, row.Key)
		}
	}
	diagnostics.InactiveReviewedModels = sortedUnique(diagnostics.InactiveReviewedModels)
	report.Models = filtered
	report.OMLX = diagnostics
	refreshCoverageCampaign(report)

}

func refreshCoverageCampaign(report *Report) {
	campaign, err := buildCoverageCampaign(report.Models)
	if err == nil {
		report.CoverageCampaign = campaign
	}
}
