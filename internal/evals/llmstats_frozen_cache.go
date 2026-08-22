package evals

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const llmStatsFrozenCohortObservationRows = 5544

func llmStatsFrozenCohortArtifactPath(cacheDir string) string {
	return filepath.Join(cacheDir, "sealed", "llm-stats", "observations-"+llmStatsFrozenCohortsArtifactDigest+".jsonl")
}

// removeFrozenLLMStatsCohortResults prevents a legacy llm-stats.json row from
// bypassing the sealed-artifact gate. These six benchmarks are restored only
// after the complete immutable population has passed validation.
func removeFrozenLLMStatsCohortResults(models []Model) {
	for index := range models {
		for benchmark := range frozenLLMStatsCohortPopulations {
			delete(models[index].Benchmarks, benchmark)
		}
	}
}

// loadFrozenLLMStatsCachedObservations reads only the exact sealed artifact
// approved for LLAMBO-6-category-v5. It deliberately does not consult the
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
	removeFrozenLLMStatsCohortResults(models)
	observations, err := loadFrozenLLMStatsCachedObservationsSource(cacheDir)
	if err != nil {
		return err
	}
	attachLLMStatsBenchmarkLeads(models, observations)
	return nil
}

func readFrozenLLMStatsObservationArtifact(path, expectedDigest string, expectedRows int) ([]Observation, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read sealed LLM Stats observations: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read sealed LLM Stats observations: not a regular file")
	}
	if info.Size() > maxSourceBody {
		return nil, fmt.Errorf("read sealed LLM Stats observations: artifact exceeds %d bytes", maxSourceBody)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open sealed LLM Stats observations: %w", err)
	}
	defer file.Close()
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
		if len(rows) != cohort.RowCount {
			return nil, fmt.Errorf("frozen LLM Stats cohort %s has %d rows, want %d", benchmark, len(rows), cohort.RowCount)
		}
		scores := make([]float64, 0, len(rows))
		digests := make(map[string]struct{}, len(cohort.Digests))
		modelIDs := make(map[string]struct{}, len(rows))
		for _, observation := range rows {
			if observation.SourceID != cohort.SourceID || string(observation.SourceClass) != cohort.SourceClass || observation.BenchmarkVersion != cohort.BenchmarkVersion || observation.Cohort != cohort.Cohort || observation.Methodology != cohort.Methodology || observation.Direction != cohort.Direction || observation.SourceRevision == "" || observation.SourceRevision != observation.SourceSHA256 {
				return nil, fmt.Errorf("frozen LLM Stats cohort %s has an incompatible row", benchmark)
			}
			if _, duplicate := modelIDs[observation.ModelID]; duplicate {
				return nil, fmt.Errorf("frozen LLM Stats cohort %s has duplicate model ID %s", benchmark, observation.ModelID)
			}
			modelIDs[observation.ModelID] = struct{}{}
			digests[observation.SourceRevision] = struct{}{}
			scores = append(scores, observation.RawScore)
		}
		if len(digests) != len(cohort.Digests) {
			return nil, fmt.Errorf("frozen LLM Stats cohort %s has incompatible digest set", benchmark)
		}
		for _, digest := range cohort.Digests {
			if _, ok := digests[digest]; !ok {
				return nil, fmt.Errorf("frozen LLM Stats cohort %s is missing digest %s", benchmark, digest)
			}
		}
		sort.Float64s(scores)
		for index, score := range cohort.Scores {
			if scores[index] != score {
				return nil, fmt.Errorf("frozen LLM Stats cohort %s has incompatible score population", benchmark)
			}
		}
		targets = append(targets, rows...)
	}
	sort.Slice(targets, func(i, j int) bool { return observationLess(targets[i], targets[j]) })
	return targets, nil
}
