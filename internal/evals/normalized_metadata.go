package evals

import (
	"sort"
	"strings"
)

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
			add(llmStats, row.Key, "input_price", metrics.InputPrice, "USD/1M tokens")
			add(llmStats, row.Key, "output_price", metrics.OutputPrice, "USD/1M tokens")
			add(llmStats, row.Key, "throughput", metrics.Throughput, "tokens/second")
			add(llmStats, row.Key, "latency", metrics.Latency, "milliseconds")
			for name, index := range metrics.Indexes {
				add(llmStats, row.Key, "index:"+name+":conservative", &index.Conservative, "index")
			}
		}
		if metrics := row.AA; metrics != nil {
			add(artificialAnalysis, row.Key, "intelligence", metrics.Intelligence, "index")
			add(artificialAnalysis, row.Key, coding1, metrics.Coding, "index")
			add(artificialAnalysis, row.Key, "agentic", metrics.Agentic, "index")
			add(artificialAnalysis, row.Key, "input_price", metrics.InputPrice, "USD/1M tokens")
			add(artificialAnalysis, row.Key, "output_price", metrics.OutputPrice, "USD/1M tokens")
			add(artificialAnalysis, row.Key, "output_tokens_per_second", metrics.OutputTokensPS, "tokens/second")
			add(artificialAnalysis, row.Key, "time_to_first_token_seconds", metrics.TTFTSeconds, "seconds")
			add(artificialAnalysis, row.Key, "end_to_end_seconds", metrics.E2ESeconds, "seconds")
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

func normalizedCohortKey(benchmark, revision string) (cohort, family string) {
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
	case artificialAnalysisName:
		return artificialAnalysis
	case eqbenchCreativeV3Key:
		return eqbenchCreativeV3
	case ifevalOfficialKey:
		return ifevalOfficial
	case llmStats:
		return llmStats
	case writingbench1:
		return writingbench1
	default:
		return source
	}
}

func normalizedContributionIndex(rows []NormalizedScoreContributionRecord) map[string]struct{} {
	result := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		result[strings.Join([]string{row.ModelKey, row.Benchmark, row.SourceID, row.SourceRevision}, "\x00")] = struct{}{}
	}
	return result
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
