package evals

import (
	"strings"
)

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
	case IdentityMatchUnmatched:
		return 4
	default:
		return 4
	}
}
