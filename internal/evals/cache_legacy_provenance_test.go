package evals

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadSnapshotLeavesLegacyWritingBenchCacheUnresolvedWithoutRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "writingbench.json")
	legacy := sourceSnapshot{
		FetchedAt:  time.Unix(1, 0).UTC(),
		URL:        "https://huggingface.co/spaces/WritingBench/WritingBench/resolve/main/score.xlsx",
		Version:    "d9338ce9b09792ea7167279fee7ccc1910e28c5d",
		CommitSHA:  "c06986e05aea53d625837e67c88944a2234271f1",
		ContentSHA: "623a59886c6b755724828a5e302df30e93b5aa30cd86bfff09ec512746d4b3d9",
		Method:     "WritingBench official score.xlsx Overall",
		Models: []Model{{Key: "writingbench:legacy", Name: "Legacy", Organization: "Example", Benchmarks: map[string]BenchmarkResult{
			"writingbench": {Score: float64Ptr(81)},
		}}},
	}
	if err := writeSnapshot(path, legacy); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := readSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("legacy cache was rewritten during read")
	}
	result := snapshot.Models[0].Benchmarks["writingbench"]
	if result.SourceID != "" || result.SourceClass != "" || result.EvidenceGrade != "" || result.SourceRevision != "" {
		t.Fatalf("legacy cache was unexpectedly admitted: %#v", result)
	}
	result.Identity = IdentityMatchExact
	model := Model{Key: snapshot.Models[0].Key, Benchmarks: map[string]BenchmarkResult{"writingbench": result}}
	if score := scoreCategoryV3(model, categorySpecNamed(t, "writing"), []Model{model}); score != nil {
		t.Fatalf("legacy cache was not unresolved: %#v", score)
	}
}

func TestReadSnapshotLeavesAlteredLegacyWritingBenchCacheUnresolved(t *testing.T) {
	for _, mutate := range []struct {
		name  string
		apply func([]Model) []Model
	}{
		{"altered score", func(models []Model) []Model {
			value := *models[0].Benchmarks["writingbench"].Score + 1
			models[0].Benchmarks["writingbench"] = BenchmarkResult{Score: &value}
			return models
		}},
		{"added model", func(models []Model) []Model {
			value := 81.0
			return append(models, Model{Key: "writingbench:added-model", Name: "Added Model", Organization: "Example", Benchmarks: map[string]BenchmarkResult{"writingbench": {Score: &value}}})
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "writingbench.json")
			legacy := sourceSnapshot{
				FetchedAt:  time.Unix(1, 0).UTC(),
				URL:        "https://huggingface.co/spaces/WritingBench/WritingBench/resolve/main/score.xlsx",
				Version:    "d9338ce9b09792ea7167279fee7ccc1910e28c5d",
				CommitSHA:  "c06986e05aea53d625837e67c88944a2234271f1",
				ContentSHA: "623a59886c6b755724828a5e302df30e93b5aa30cd86bfff09ec512746d4b3d9",
				Method:     "WritingBench official score.xlsx Overall",
				Models:     mutate.apply([]Model{{Key: "writingbench:legacy", Name: "Legacy", Organization: "Example", Benchmarks: map[string]BenchmarkResult{"writingbench": {Score: float64Ptr(81)}}}}),
			}
			if err := writeSnapshot(path, legacy); err != nil {
				t.Fatal(err)
			}
			snapshot, err := readSnapshot(path)
			if err != nil {
				t.Fatal(err)
			}
			result := snapshot.Models[0].Benchmarks["writingbench"]
			if result.SourceID != "" || result.SourceClass != "" || result.EvidenceGrade != "" || result.SourceRevision != "" {
				t.Fatalf("altered legacy cache was admitted: %#v", result)
			}
			result.Identity = IdentityMatchExact
			model := Model{Key: snapshot.Models[0].Key, Benchmarks: map[string]BenchmarkResult{"writingbench": result}}
			if score := scoreCategoryV3(model, categorySpecNamed(t, "writing"), []Model{model}); score != nil {
				t.Fatalf("altered legacy cache was not unresolved: %#v", score)
			}
		})
	}
}
