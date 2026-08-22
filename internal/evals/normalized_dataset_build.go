package evals

import (
	"sort"
	"strconv"
	"strings"
)

type normalizedDataset struct {
	Sources            []NormalizedSourceRecord
	Models             []NormalizedModelRecord
	ModelIdentities    []NormalizedModelIdentityRecord
	SourceObservations []NormalizedSourceObservationRecord
	Scores             []NormalizedScoreRecord
	ScoreContributions []NormalizedScoreContributionRecord
	OperationalMetrics []NormalizedOperationalMetricRecord
	Projections        []NormalizedProjectionRecord
	FrozenCohorts      []NormalizedFrozenCohortRecord
	DriftDiagnostics   []NormalizedDriftDiagnosticRecord
}

type normalizedCanonicalMatch struct {
	key      string
	identity IdentityMatch
}

type normalizedObservationCandidate struct {
	sourceID       string
	sourceModel    Model
	benchmark      string
	result         BenchmarkResult
	canonical      normalizedCanonicalMatch
	semanticKey    string
	effectiveClass string
	effectiveGrade string
}

func buildNormalizedDataset(result Result, report Report) normalizedDataset {
	dataset := normalizedDataset{}
	canonicalByKey, canonicalByIdentity, canonicalByName := normalizedCanonicalIndexes(report.Models)

	dataset.Models = normalizedModelRecords(report.Models, result.Models)
	dataset.Scores, dataset.ScoreContributions = normalizedScoreRecords(report)
	dataset.OperationalMetrics = normalizedOperationalRecords(report.Models)
	dataset.Projections = normalizedProjectionRecords(report.Models)

	contributions := normalizedContributionIndex(dataset.ScoreContributions)
	for _, sourceModel := range result.SourceModels {
		match := matchNormalizedCanonical(sourceModel.Model, canonicalByKey, canonicalByIdentity, canonicalByName)
		identity := normalizedSourceIdentity(sourceModel.Model)
		dataset.ModelIdentities = append(dataset.ModelIdentities, NormalizedModelIdentityRecord{
			SourceID: sourceModel.SourceID, SourceModelKey: sourceModel.Model.Key, SourceModelName: sourceModel.Model.Name,
			Organization: sourceModel.Model.Organization, NormalizedIdentity: identity,
			CanonicalModelKey: match.key, IdentityMatch: match.identity,
		})
	}
	sortNormalizedIdentities(dataset.ModelIdentities)

	groups := make(map[string][]normalizedObservationCandidate)
	for _, sourceModel := range result.SourceModels {
		match := matchNormalizedCanonical(sourceModel.Model, canonicalByKey, canonicalByIdentity, canonicalByName)
		semanticIdentity := normalizedSourceIdentity(sourceModel.Model)
		benchmarks := make([]string, 0, len(sourceModel.Model.Benchmarks))
		for benchmark := range sourceModel.Model.Benchmarks {
			benchmarks = append(benchmarks, benchmark)
		}
		sort.Strings(benchmarks)
		for _, benchmark := range benchmarks {
			observation := sourceModel.Model.Benchmarks[benchmark]
			candidate := normalizedObservationCandidate{
				sourceID: sourceModel.SourceID, sourceModel: sourceModel.Model, benchmark: benchmark, result: observation, canonical: match,
				semanticKey:    normalizedSemanticKey(semanticIdentity, benchmark, observation),
				effectiveClass: normalizedEffectiveSourceClass(sourceModel.SourceID, observation.SourceClass),
				effectiveGrade: normalizedEffectiveEvidenceGrade(sourceModel.SourceID, observation.EvidenceGrade),
			}
			groups[candidate.semanticKey] = append(groups[candidate.semanticKey], candidate)
		}
	}
	frozenCohorts := make(map[string]NormalizedFrozenCohortRecord)
	for _, group := range groups {
		sort.SliceStable(group, func(i, j int) bool { return normalizedEditorialObservationLess(group[i], group[j]) })
		selected := group[0]
		record := normalizedObservationRecord(selected, group, contributions, canonicalByKey)
		dataset.SourceObservations = append(dataset.SourceObservations, record)
		normalizedAddFrozenCohort(frozenCohorts, record)
	}
	sort.Slice(dataset.SourceObservations, func(i, j int) bool {
		return dataset.SourceObservations[i].SemanticFingerprint < dataset.SourceObservations[j].SemanticFingerprint
	})
	for _, contribution := range dataset.ScoreContributions {
		key, revision := normalizedCohortKey(contribution.Benchmark, contribution.SourceRevision)
		cohort, ok := frozenCohorts[key]
		if !ok {
			cohort = NormalizedFrozenCohortRecord{Benchmark: contribution.Benchmark, Revision: revision}
		}
		cohort.SourceIDs = normalizedAppendUnique(cohort.SourceIDs, contribution.SourceID)
		if _, targeted := frozenLLMStatsCohortPopulations[contribution.Benchmark]; targeted {
			cohort.SourceIDs = normalizedAppendUnique(cohort.SourceIDs, "llm-stats-frozen-cohorts")
		}
		frozenCohorts[key] = cohort
	}
	for key, cohort := range frozenCohorts {
		if len(cohort.Values) == 0 {
			cohort.Values = normalizedFrozenCohortValues(cohort.Benchmark, cohort.Revision)
		}
		cohort.Population = len(cohort.Values)
		sort.Strings(cohort.SourceIDs)
		frozenCohorts[key] = cohort
	}
	dataset.FrozenCohorts = make([]NormalizedFrozenCohortRecord, 0, len(frozenCohorts))
	for _, cohort := range frozenCohorts {
		if cohort.Population > 0 {
			dataset.FrozenCohorts = append(dataset.FrozenCohorts, cohort)
		}
	}
	sort.Slice(dataset.FrozenCohorts, func(i, j int) bool {
		if dataset.FrozenCohorts[i].Benchmark != dataset.FrozenCohorts[j].Benchmark {
			return dataset.FrozenCohorts[i].Benchmark < dataset.FrozenCohorts[j].Benchmark
		}
		return dataset.FrozenCohorts[i].Revision < dataset.FrozenCohorts[j].Revision
	})

	dataset.DriftDiagnostics = normalizedDriftRecords(report)
	dataset.Sources = normalizedSourceRecords(result, dataset)
	return dataset
}

func normalizedCanonicalIndexes(rows []ReportModel) (map[string]ReportModel, map[string][]ReportModel, map[string][]ReportModel) {
	byKey := make(map[string]ReportModel, len(rows))
	byIdentity := make(map[string][]ReportModel)
	byName := make(map[string][]ReportModel)
	for _, row := range rows {
		byKey[row.Key] = row
		if identity, ok := externalIdentity(Model{Key: row.Key, Name: row.Name, Organization: row.Organization}); ok {
			byIdentity[identity] = append(byIdentity[identity], row)
		}
		if name := normalizeIdentityText(row.Name, true); name != "" {
			byName[name] = append(byName[name], row)
		}
	}
	return byKey, byIdentity, byName
}

func matchNormalizedCanonical(source Model, byKey map[string]ReportModel, byIdentity, byName map[string][]ReportModel) normalizedCanonicalMatch {
	if _, ok := byKey[source.Key]; ok {
		return normalizedCanonicalMatch{key: source.Key, identity: IdentityMatchExact}
	}
	identity, hasIdentity := externalIdentity(source)
	if hasIdentity && len(byIdentity[identity]) == 1 {
		return normalizedCanonicalMatch{key: byIdentity[identity][0].Key, identity: IdentityMatchNormalized}
	}
	if hasIdentity && len(byIdentity[identity]) > 1 {
		return normalizedCanonicalMatch{identity: IdentityMatchAmbiguous}
	}
	name := normalizeIdentityText(source.Name, true)
	if source.Organization == "" && name != "" && len(byName[name]) == 1 {
		return normalizedCanonicalMatch{key: byName[name][0].Key, identity: IdentityMatchNormalized}
	}
	if name != "" && len(byName[name]) > 1 {
		return normalizedCanonicalMatch{identity: IdentityMatchAmbiguous}
	}
	return normalizedCanonicalMatch{identity: IdentityMatchUnmatched}
}

func normalizedSourceIdentity(source Model) string {
	if identity, ok := externalIdentity(source); ok {
		return identity
	}
	return "\x00" + normalizeIdentityText(source.Name, true)
}

func normalizedSemanticKey(identity string, benchmark string, result BenchmarkResult) string {
	score := "missing"
	if result.Score != nil && finite(*result.Score) {
		score = strconv.FormatFloat(*result.Score, 'g', 12, 64)
	}
	return strings.Join([]string{identity, benchmark, strings.TrimSpace(result.Version), strings.TrimSpace(result.Direction), score}, "\x00")
}

func normalizedEffectiveSourceClass(sourceID, resultClass string) string {
	if strings.TrimSpace(resultClass) != "" {
		return strings.TrimSpace(resultClass)
	}
	switch sourceID {
	case "writingbench", "eqbench-creative-v3":
		return string(SourceOwnerResult)
	case "artificial-analysis":
		return string(SourceAggregatorResult)
	case "llm-stats":
		return string(SourceAggregatorResult)
	default:
		return string(SourceFirstPartyResult)
	}
}

func normalizedEffectiveEvidenceGrade(sourceID, grade string) string {
	if strings.TrimSpace(grade) != "" {
		return strings.TrimSpace(grade)
	}
	switch sourceID {
	case "writingbench", "eqbench-creative-v3":
		return "owner"
	case "llm-stats":
		return "aggregator_self_reported"
	default:
		return "first_party"
	}
}

func normalizedEditorialObservationLess(left, right normalizedObservationCandidate) bool {
	if leftSource, rightSource := sourceAuthority(left.effectiveClass), sourceAuthority(right.effectiveClass); leftSource != rightSource {
		return leftSource > rightSource
	}
	if leftGrade, rightGrade := evidenceMultiplier(left.effectiveGrade), evidenceMultiplier(right.effectiveGrade); leftGrade != rightGrade {
		return leftGrade > rightGrade
	}
	if leftIdentity, rightIdentity := normalizedIdentityRank(left.canonical.identity), normalizedIdentityRank(right.canonical.identity); leftIdentity != rightIdentity {
		return leftIdentity < rightIdentity
	}
	return left.sourceID < right.sourceID
}

func normalizedIdentityRank(identity IdentityMatch) int {
	switch identity {
	case IdentityMatchExact:
		return 0
	case IdentityMatchNormalized:
		return 1
	case IdentityMatchProjected:
		return 2
	case IdentityMatchAmbiguous:
		return 3
	default:
		return 4
	}
}

func normalizedObservationRecord(selected normalizedObservationCandidate, group []normalizedObservationCandidate, contributions map[string]struct{}, canonicalByKey map[string]ReportModel) NormalizedSourceObservationRecord {
	mergedIdentity := selected.result.Identity
	mergedResult := BenchmarkResult{}
	if selected.canonical.key != "" {
		if row, ok := canonicalByKey[selected.canonical.key]; ok {
			mergedResult = row.Benchmarks[selected.benchmark]
			if mergedResult.Identity != "" {
				mergedIdentity = mergedResult.Identity
			}
		}
	}
	if mergedIdentity == "" {
		mergedIdentity = selected.canonical.identity
	}
	record := NormalizedSourceObservationRecord{
		SemanticFingerprint: sha256Hex([]byte(selected.semanticKey)), SourceID: selected.sourceID,
		ResultSourceID: selected.result.SourceID, ResultSourceClass: selected.result.SourceClass,
		EditorialSourceClass: selected.effectiveClass,
		SourceModelKey:       selected.sourceModel.Key, SourceModelName: selected.sourceModel.Name,
		Organization: selected.sourceModel.Organization, CanonicalModelKey: selected.canonical.key,
		Benchmark: selected.benchmark, BenchmarkVersion: selected.result.Version,
		RawScore: selected.result.Score, Unit: selected.result.Unit, Direction: selected.result.Direction,
		IdentityMatch: mergedIdentity, ResultEvidenceGrade: selected.result.EvidenceGrade,
		EditorialEvidenceGrade: selected.effectiveGrade,
		SourceRevision:         selected.result.SourceRevision, ContentSHA256: selected.result.ContentSHA,
		URL: selected.result.URL, CommitSHA: selected.result.CommitSHA, Methodology: selected.result.Method,
		JudgeVersion: selected.result.Judge, FetchedAt: selected.result.FetchedAt, Locator: selected.result.Locator,
		SampleSize: selected.result.SampleSize, CandidateCount: len(group),
		SelectedBy:      "source authority, evidence grade, identity confidence, stable source ID",
		AdmissionStatus: "not_admitted", AdmissionReason: normalizedAdmissionReason(selected, mergedIdentity),
	}
	contributionKey := strings.Join([]string{selected.canonical.key, selected.benchmark, selected.result.SourceID, selected.result.SourceRevision}, "\x00")
	if _, ok := contributions[contributionKey]; ok {
		record.AdmissionStatus, record.AdmissionReason, record.AffectsCapability = "admitted", "", true
	}
	for _, mirror := range group[1:] {
		record.Mirrors = append(record.Mirrors, NormalizedObservationMirror{
			SourceID: mirror.sourceID, SourceClass: normalizedEffectiveSourceClass(mirror.sourceID, mirror.result.SourceClass),
			SourceRevision: mirror.result.SourceRevision, ContentSHA256: mirror.result.ContentSHA,
			Locator: mirror.result.Locator, EvidenceGrade: normalizedEffectiveEvidenceGrade(mirror.sourceID, mirror.result.EvidenceGrade),
		})
	}
	return record
}

func normalizedAdmissionReason(candidate normalizedObservationCandidate, identity IdentityMatch) string {
	result := candidate.result
	if result.Quarantined {
		return "quarantined benchmark conflict"
	}
	if result.Score == nil || !finite(*result.Score) {
		return "missing or non-finite score"
	}
	if strings.TrimSpace(result.SourceID) == "" {
		return "legacy cached row has no result-level source_id"
	}
	if identity != IdentityMatchExact {
		return "identity is not exact"
	}
	if strings.TrimSpace(result.SourceClass) == "" || strings.TrimSpace(result.EvidenceGrade) == "" {
		return "legacy cached row lacks source class or evidence grade"
	}
	if !validSHA256(result.ContentSHA) || strings.TrimSpace(result.Version) == "" || strings.TrimSpace(result.Method) == "" {
		return "row fails provenance validation"
	}
	if !registeredSourceEligible(result) {
		return "source is not score-eligible"
	}
	if !normalizedReviewedBenchmark(candidate.benchmark) {
		return "benchmark is not in the reviewed family registry"
	}
	if len(resolvedFrozenBenchmarkCohort(candidate.benchmark, result)) < 5 {
		return "no compatible frozen cohort of at least five models"
	}
	return "eligible observation was not first in its family order"
}

func normalizedReviewedBenchmark(benchmark string) bool {
	for _, category := range categorySpecs {
		for _, family := range category.families {
			for _, candidate := range family.benchmarks {
				if candidate == benchmark {
					return true
				}
			}
		}
	}
	return false
}

func normalizedModelRecords(rows []ReportModel, sourceRows []Model) []NormalizedModelRecord {
	metadataByKey := make(map[string]Model, len(sourceRows))
	for _, row := range sourceRows {
		metadataByKey[row.Key] = row
	}
	records := make([]NormalizedModelRecord, 0, len(rows))
	for _, row := range rows {
		rowKind := "canonical"
		if row.Projection != nil {
			rowKind = "projection"
		}
		records = append(records, NormalizedModelRecord{
			Key: row.Key, Name: row.Name, Organization: row.Organization, IdentityMatch: row.IdentityMatch,
			Open: normalizedModelOpen(row, metadataByKey), License: metadataByKey[row.Key].License,
			Context: metadataByKey[row.Key].Context,
			RowKind: rowKind, Projection: cloneProjection(row.Projection),
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Key < records[j].Key })
	return records
}

func normalizedModelOpen(row ReportModel, metadataByKey map[string]Model) *bool {
	if row.Open != nil {
		return row.Open
	}
	return metadataByKey[row.Key].Open
}

func normalizedScoreRecords(report Report) ([]NormalizedScoreRecord, []NormalizedScoreContributionRecord) {
	categoryOrder := make(map[string]int, len(categorySpecs))
	for index, category := range categorySpecs {
		categoryOrder[category.name] = index
	}
	scores := make([]NormalizedScoreRecord, 0, len(report.Models)*len(categorySpecs))
	contributions := make([]NormalizedScoreContributionRecord, 0)
	unresolvedRank := make(map[string]int, len(categorySpecs))
	for category, ranking := range report.CategoryRankings {
		unresolvedKeys := make([]string, 0)
		ranked := make(map[string]struct{}, len(ranking.Entries))
		for _, entry := range ranking.Entries {
			ranked[entry.Key] = struct{}{}
		}
		for _, row := range report.Models {
			if row.LlamboScores[category] == nil {
				unresolvedKeys = append(unresolvedKeys, row.Key)
			} else if _, ok := ranked[row.Key]; !ok {
				unresolvedKeys = append(unresolvedKeys, row.Key)
			}
		}
		sort.Strings(unresolvedKeys)
		for index, key := range unresolvedKeys {
			unresolvedRank[category+"\x00"+key] = len(ranking.Entries) + index + 1
		}
	}
	for _, row := range report.Models {
		rowKind := "canonical"
		if row.Projection != nil {
			rowKind = "projection"
		}
		for _, category := range categorySpecs {
			score := row.LlamboScores[category.name]
			rank := unresolvedRank[category.name+"\x00"+row.Key]
			if ranking := report.CategoryRankings[category.name]; ranking.Entries != nil {
				for index, entry := range ranking.Entries {
					if entry.Key == row.Key {
						rank = index + 1
						break
					}
				}
			}
			record := NormalizedScoreRecord{
				Category: category.name, EditorialRank: rank, ModelKey: row.Key, ModelName: row.Name,
				RowKind: rowKind, Score: nil, UnresolvedReason: row.UnresolvedReasons[category.name],
			}
			if score != nil {
				record.Score = &score.Score
				record.Coverage, record.TrustedCoverage, record.Confidence = score.Coverage, score.TrustedCoverage, score.Confidence
				record.Families, record.Stale, record.Estimated, record.EstimateMethod = score.Families, score.Stale, score.Estimated, score.EstimateMethod
				record.WinnerStatus = score.WinnerStatus
			}
			if record.UnresolvedReason == "" && score == nil {
				record.UnresolvedReason = "no eligible reviewed cached evidence"
			}
			scores = append(scores, record)
			if score == nil {
				continue
			}
			for _, contribution := range score.Contributions {
				contributions = append(contributions, NormalizedScoreContributionRecord{
					Category: category.name, ModelKey: row.Key, RowKind: rowKind,
					BenchmarkFamily: contribution.Family, Benchmark: contribution.Benchmark,
					RawScore: contribution.RawScore, Percentile: contribution.Percentile,
					FrozenPopulation: contribution.ReferencePopulation, EvidenceGrade: contribution.EvidenceGrade,
					SourceID: contribution.SourceID, SourceClass: contribution.SourceClass,
					SourceRevision: contribution.SourceRevision, ContentSHA256: contribution.ContentSHA,
					NominalWeight: contribution.NominalWeight, EvidenceMultiplier: contribution.EvidenceMultiplier,
					IdentityMultiplier: contribution.IdentityMultiplier, EffectiveWeight: contribution.EffectiveWeight,
				})
			}
		}
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].Category != scores[j].Category {
			return categoryOrder[scores[i].Category] < categoryOrder[scores[j].Category]
		}
		if scores[i].EditorialRank != scores[j].EditorialRank {
			return scores[i].EditorialRank < scores[j].EditorialRank
		}
		return scores[i].ModelKey < scores[j].ModelKey
	})
	sort.SliceStable(contributions, func(i, j int) bool {
		if contributions[i].Category != contributions[j].Category {
			return categoryOrder[contributions[i].Category] < categoryOrder[contributions[j].Category]
		}
		if contributions[i].ModelKey != contributions[j].ModelKey {
			return contributions[i].ModelKey < contributions[j].ModelKey
		}
		if contributions[i].BenchmarkFamily != contributions[j].BenchmarkFamily {
			return contributions[i].BenchmarkFamily < contributions[j].BenchmarkFamily
		}
		return contributions[i].Benchmark < contributions[j].Benchmark
	})
	return scores, contributions
}

func normalizedContributionIndex(rows []NormalizedScoreContributionRecord) map[string]struct{} {
	result := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		result[strings.Join([]string{row.ModelKey, row.Benchmark, row.SourceID, row.SourceRevision}, "\x00")] = struct{}{}
	}
	return result
}

func normalizedOperationalRecords(rows []ReportModel) []NormalizedOperationalMetricRecord {
	records := make([]NormalizedOperationalMetricRecord, 0)
	add := func(source, model, metric string, value *float64, unit string) {
		if value == nil || !finite(*value) {
			return
		}
		records = append(records, NormalizedOperationalMetricRecord{SourceID: source, ModelKey: model, Metric: metric, Value: value, Unit: unit})
	}
	for _, row := range rows {
		if row.Projection != nil {
			continue
		}
		if metrics := row.LLMStats; metrics != nil {
			add("llm-stats", row.Key, "input_price", metrics.InputPrice, "USD/1M tokens")
			add("llm-stats", row.Key, "output_price", metrics.OutputPrice, "USD/1M tokens")
			add("llm-stats", row.Key, "throughput", metrics.Throughput, "tokens/second")
			add("llm-stats", row.Key, "latency", metrics.Latency, "milliseconds")
			for name, index := range metrics.Indexes {
				add("llm-stats", row.Key, "index:"+name+":conservative", &index.Conservative, "index")
			}
		}
		if metrics := row.AA; metrics != nil {
			add("artificial-analysis", row.Key, "intelligence", metrics.Intelligence, "index")
			add("artificial-analysis", row.Key, "coding", metrics.Coding, "index")
			add("artificial-analysis", row.Key, "agentic", metrics.Agentic, "index")
			add("artificial-analysis", row.Key, "input_price", metrics.InputPrice, "USD/1M tokens")
			add("artificial-analysis", row.Key, "output_price", metrics.OutputPrice, "USD/1M tokens")
			add("artificial-analysis", row.Key, "output_tokens_per_second", metrics.OutputTokensPS, "tokens/second")
			add("artificial-analysis", row.Key, "time_to_first_token_seconds", metrics.TTFTSeconds, "seconds")
			add("artificial-analysis", row.Key, "end_to_end_seconds", metrics.E2ESeconds, "seconds")
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].SourceID != records[j].SourceID {
			return records[i].SourceID < records[j].SourceID
		}
		if records[i].ModelKey != records[j].ModelKey {
			return records[i].ModelKey < records[j].ModelKey
		}
		return records[i].Metric < records[j].Metric
	})
	return records
}

func normalizedProjectionRecords(rows []ReportModel) []NormalizedProjectionRecord {
	records := make([]NormalizedProjectionRecord, 0)
	for _, row := range rows {
		if row.Projection == nil {
			continue
		}
		records = append(records, NormalizedProjectionRecord{
			ArtifactKey: row.Key, SourceKey: row.Projection.SourceKey, Confidence: row.Projection.Confidence,
			Basis: row.Projection.Basis, ReviewedAt: row.Projection.ReviewedAt,
			ScoreMultiplier: projectionMultiplier(row.Projection),
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ArtifactKey < records[j].ArtifactKey })
	return records
}

func normalizedAddFrozenCohort(target map[string]NormalizedFrozenCohortRecord, record NormalizedSourceObservationRecord) {
	if strings.TrimSpace(record.Benchmark) == "" || strings.TrimSpace(record.BenchmarkVersion) == "" {
		return
	}
	key, revision := normalizedCohortKey(record.Benchmark, record.BenchmarkVersion)
	cohort := target[key]
	cohort.Benchmark, cohort.Revision = record.Benchmark, revision
	cohort.SourceIDs = normalizedAppendUnique(cohort.SourceIDs, record.SourceID)
	target[key] = cohort
}

func normalizedCohortKey(benchmark, revision string) (string, string) {
	if _, targeted := frozenLLMStatsCohortPopulations[benchmark]; targeted {
		return benchmark + "\x00sealed-artifact", "sealed-artifact:" + llmStatsFrozenCohortsArtifactDigest
	}
	return benchmark + "\x00" + revision, revision
}

func normalizedFrozenCohortValues(benchmark, revision string) []float64 {
	if _, targeted := frozenLLMStatsCohortPopulations[benchmark]; targeted {
		cohorts, err := loadFrozenLLMStatsCohorts()
		if err != nil {
			return nil
		}
		if cohort, ok := cohorts.Benchmarks[benchmark]; ok {
			return append([]float64(nil), cohort.Scores...)
		}
		return nil
	}
	return frozenBenchmarkCohort(benchmark, revision)
}

func normalizedDriftRecords(report Report) []NormalizedDriftDiagnosticRecord {
	records := make([]NormalizedDriftDiagnosticRecord, 0)
	for source, reference := range report.Formula.Reference.SourceFingerprints {
		current := report.Formula.Drift.CurrentFingerprints[source]
		kind := "fingerprint_current"
		message := "source fingerprint matches the frozen reference"
		if current != reference {
			kind, message = "fingerprint_changed", "source fingerprint differs from the frozen reference"
		}
		records = append(records, NormalizedDriftDiagnosticRecord{SourceID: normalizedDriftSourceID(source), Kind: kind, Message: message, ReferenceFingerprint: reference, CurrentFingerprint: current})
	}
	for _, reason := range report.Formula.Drift.Reasons {
		records = append(records, NormalizedDriftDiagnosticRecord{Kind: "drift_reason", Message: reason})
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Kind != records[j].Kind {
			return records[i].Kind < records[j].Kind
		}
		if records[i].SourceID != records[j].SourceID {
			return records[i].SourceID < records[j].SourceID
		}
		return records[i].Message < records[j].Message
	})
	return records
}

func normalizedDriftSourceID(source string) string {
	switch source {
	case "artificial_analysis":
		return "artificial-analysis"
	case "eqbench_creative_v3":
		return "eqbench-creative-v3"
	case "ifeval_official":
		return "ifeval-official"
	case "llm_stats":
		return "llm-stats"
	case "writingbench":
		return "writingbench"
	default:
		return source
	}
}

func normalizedSourceRecords(result Result, dataset normalizedDataset) []NormalizedSourceRecord {
	observationCounts := make(map[string]int)
	for _, row := range dataset.SourceObservations {
		observationCounts[row.SourceID]++
		for _, mirror := range row.Mirrors {
			observationCounts[mirror.SourceID]++
		}
	}
	operationalCounts := make(map[string]int)
	for _, row := range dataset.OperationalMetrics {
		operationalCounts[row.SourceID]++
	}
	contributionCounts := make(map[string]int)
	for _, row := range dataset.ScoreContributions {
		contributionCounts[normalizedStableContributionSource(row.SourceID)]++
	}
	frozenCounts := make(map[string]int)
	for _, row := range dataset.FrozenCohorts {
		for _, sourceID := range row.SourceIDs {
			if sourceID == "llm-stats-frozen-cohorts" {
				frozenCounts[sourceID]++
			}
		}
	}
	driftCounts := make(map[string]int)
	for _, row := range dataset.DriftDiagnostics {
		if row.SourceID != "" {
			driftCounts[row.SourceID]++
		}
	}
	records := make([]NormalizedSourceRecord, 0, len(result.Sources))
	for _, status := range result.Sources {
		sourceID := normalizedStableSourceID(status.Name)
		roles := make([]string, 0, 5)
		if contributionCounts[sourceID] > 0 {
			roles = append(roles, "capability_evidence")
		}
		if frozenCounts[sourceID] > 0 {
			roles = append(roles, "frozen_cohort")
		}
		if observationCounts[sourceID] > 0 {
			roles = append(roles, "source_observation")
		}
		if operationalCounts[sourceID] > 0 {
			roles = append(roles, "operational_metadata")
		}
		if driftCounts[sourceID] > 0 {
			roles = append(roles, "drift_diagnostic")
		}
		if len(roles) == 0 {
			roles = append(roles, "manifest_only")
		}
		records = append(records, NormalizedSourceRecord{
			SourceID: sourceID, Name: status.Name, URL: status.URL, Cache: status.Cache,
			FetchedAt: status.FetchedAt, Models: status.Models, Observations: observationCounts[sourceID],
			OperationalMetrics: operationalCounts[sourceID], ScoreContributions: contributionCounts[sourceID],
			FrozenCohorts: frozenCounts[sourceID], Version: status.Version, CommitSHA: status.CommitSHA,
			ContentSHA256: status.ContentSHA, Methodology: status.Methodology, RegistryVersion: status.RegistryVersion,
			EvidenceGrade: status.EvidenceGrade, Error: status.Error, Roles: roles,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].SourceID < records[j].SourceID })
	return records
}

func normalizedStableSourceID(name string) string {
	switch name {
	case "LLM Stats":
		return "llm-stats"
	case "LLM Stats frozen cohorts":
		return "llm-stats-frozen-cohorts"
	case "Artificial Analysis":
		return "artificial-analysis"
	case "WritingBench":
		return "writingbench"
	case "EQ-Bench Creative v3":
		return "eqbench-creative-v3"
	case "Official IFEval":
		return "ifeval-official"
	case "LiquidAI LFM2.5-2.6B card":
		return "liquidai-lfm25-2.6b-card"
	case "Qwen3.8-27B card":
		return "qwen3.8-27b-card"
	case "OpenAI gpt-oss model card":
		return "openai-gpt-oss-model-card"
	case "LiquidAI LFM2.5-VL-3B card":
		return "liquidai-lfm25-vl-3b-card"
	case "Google Gemma 4 model card":
		return "google-gemma4-model-card"
	default:
		return strings.ToLower(strings.Join(strings.Fields(name), "-"))
	}
}

func normalizedStableContributionSource(sourceID string) string {
	if sourceID == "llm-stats-benchmark-results" || sourceID == llmStatsStatsV1SourceID {
		return "llm-stats"
	}
	return sourceID
}

func normalizedAppendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func sortNormalizedIdentities(rows []NormalizedModelIdentityRecord) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].SourceID != rows[j].SourceID {
			return rows[i].SourceID < rows[j].SourceID
		}
		return rows[i].SourceModelKey < rows[j].SourceModelKey
	})
}
