package evals

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const llmStatsFrozenCohortObservationRows = 5544

func llmStatsFrozenCohortArtifactPath(cacheDir string) string {
	return filepath.Join(cacheDir, "sealed", llmStats, "observations-"+llmStatsFrozenCohortsArtifactDigest+".jsonl")
}

// removeFrozenLLMStatsCohortResults prevents a legacy llm-stats.json row from
// bypassing the sealed-artifact gate. Stats v1 rows for the same benchmark are
// retained when the source-scoped refresh supplied the newer frozen contract.
func removeFrozenLLMStatsCohortResults(models []Model) {
	for index := range models {
		for benchmark := range frozenLLMStatsCohortPopulations {
			if result := models[index].Benchmarks[benchmark]; result.SourceID == llmStatsBenchmarkResults { //nolint:gosec // index is range-bounded over models
				delete(models[index].Benchmarks, benchmark) //nolint:gosec // bounded by construction
			}
		}
	}
}

// loadFrozenLLMStatsCachedObservations reads only the exact sealed artifact
// approved for LLAMBO-6-category. It deliberately does not consult the
// SQLite index: that index is derived cache state, while this JSONL artifact is
// the source of truth for the frozen percentile populations.
func loadFrozenLLMStatsCachedObservations(cacheDir string) ([]Observation, error) {
	return readFrozenLLMStatsObservationArtifact(llmStatsFrozenCohortArtifactPath(cacheDir), llmStatsFrozenCohortsArtifactDigest, llmStatsFrozenCohortObservationRows)
}

// prepareFrozenLLMStatsCohortResults strips the six target benchmarks from any
// legacy or freshly refreshed LLM Stats snapshot, then restores them only from
// the fully validated pinned artifact. Refresh may collect newer data, but it
// can never become a scoring fallback for this immutable cohort contract.
func prepareFrozenLLMStatsCohortResults(models []Model, cacheDir string) error {
	return prepareFrozenLLMStatsCohortResultsWithLoader(models, cacheDir, loadFrozenLLMStatsCachedObservationsSource)
}

func prepareFrozenLLMStatsCohortResultsWithLoader(models []Model, cacheDir string, loader func(string) ([]Observation, error)) error {
	removeFrozenLLMStatsCohortResults(models)
	observations, err := loader(cacheDir)
	if err != nil {
		return err
	}
	covered := statsV1BenchmarksPresent(models)
	retained := make([]Observation, 0, len(observations))
	for _, observation := range observations {
		if _, ok := covered[observation.Benchmark]; !ok {
			retained = append(retained, observation)
		}
	}
	observations = retained
	attachLLMStatsBenchmarkLeads(models, observations)
	return nil
}

func statsV1BenchmarksPresent(models []Model) map[string]struct{} {
	result := make(map[string]struct{})
	for _, model := range models {
		for benchmark, row := range model.Benchmarks {
			if row.SourceID == llmStatsStatsV1SourceID {
				result[benchmark] = struct{}{}
			}
		}
	}
	return result
}

func readFrozenLLMStatsObservationArtifact(path, expectedDigest string, expectedRows int) ([]Observation, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read sealed LLM Stats observations: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("read sealed LLM Stats observations: not a regular file")
	}
	if info.Size() > maxSourceBody {
		return nil, fmt.Errorf("read sealed LLM Stats observations: artifact exceeds %d bytes", maxSourceBody)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open sealed LLM Stats observations: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxSourceBody+1))
	if err != nil {
		return nil, fmt.Errorf("read sealed LLM Stats observations: %w", err)
	}
	if len(data) > maxSourceBody {
		return nil, fmt.Errorf("read sealed LLM Stats observations: artifact exceeds %d bytes", maxSourceBody)
	}
	if got := SealBytes(data); got != expectedDigest {
		return nil, fmt.Errorf("read sealed LLM Stats observations: digest %s does not match pinned %s", got, expectedDigest)
	}
	observations, err := DecodeObservationsJSONL(bytes.NewReader(data), maxSourceBody)
	if err != nil {
		return nil, fmt.Errorf("decode sealed LLM Stats observations: %w", err)
	}
	if len(observations) != expectedRows {
		return nil, fmt.Errorf("decode sealed LLM Stats observations: rows %d, want %d", len(observations), expectedRows)
	}
	return validateFrozenLLMStatsObservationArtifact(observations)
}

func validateFrozenLLMStatsObservationArtifact(observations []Observation) ([]Observation, error) {
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		return nil, err
	}
	byBenchmark := make(map[string][]Observation, len(cohorts.Benchmarks))
	for _, observation := range observations {
		if _, ok := cohorts.Benchmarks[observation.Benchmark]; ok {
			byBenchmark[observation.Benchmark] = append(byBenchmark[observation.Benchmark], observation)
		}
	}
	targets := make([]Observation, 0, 267)
	for benchmark, cohort := range cohorts.Benchmarks {
		rows := byBenchmark[benchmark]
		if err := validateFrozenLLMStatsCohortRows(benchmark, cohort, rows); err != nil {
			return nil, err
		}
		targets = append(targets, rows...)
	}
	sort.Slice(targets, func(i, j int) bool { return observationLess(targets[i], targets[j]) })
	return targets, nil
}
