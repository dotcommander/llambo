package evals

import (
	"sort"
	"strconv"
	"strings"
)

// Repeated string literals consolidated by goconst.
const (
	artificialAnalysis = "artificial-analysis"
	eqbenchCreativeV3  = "eqbench-creative-v3"
	llmStats           = "llm-stats"
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
	dataset.FrozenCohorts = normalizedCompleteCohorts(dataset.ScoreContributions, frozenCohorts)

	dataset.DriftDiagnostics = normalizedDriftRecords(report)
	dataset.Sources = normalizedSourceRecords(result, dataset)
	return dataset
}

func normalizedCanonicalIndexes(rows []ReportModel) (byKey map[string]ReportModel, byIdentity, byName map[string][]ReportModel) {
	byKey = make(map[string]ReportModel, len(rows))
	byIdentity = make(map[string][]ReportModel)
	byName = make(map[string][]ReportModel)
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
	case writingbench1, eqbenchCreativeV3:
		return string(SourceOwnerResult)
	case artificialAnalysisName:
		return string(SourceAggregatorResult)
	case llmStats:
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
	case writingbench1, eqbenchCreativeV3:
		return owner1
	case llmStats:
		return aggregatorSelfReported
	default:
		return firstParty
	}
}
