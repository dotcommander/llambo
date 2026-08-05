package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

func TestEncodeWritingCatalog(t *testing.T) {
	catalog := evals.DefaultWritingCatalog(time.Unix(1, 0).UTC())
	markdown, err := encodeWritingCatalog(catalog, "markdown")
	if err != nil {
		t.Fatal(err)
	}
	text := string(markdown)
	for _, want := range []string{"# Writing Benchmark Catalog", "WritingBench", "DeepSeek V4 Flash-0731", "## Run plan", "No live source checks", "No fresh model discovery", "No live coverage match", "No live snapshot"} {
		if !strings.Contains(text, want) {
			t.Errorf("markdown missing %q:\n%s", want, text)
		}
	}

	jsonData, err := encodeWritingCatalog(catalog, "json")
	if err != nil {
		t.Fatal(err)
	}
	var decoded evals.WritingCatalog
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RegistryVersion != evals.WritingCatalogVersion || len(decoded.OpenModels) == 0 {
		t.Fatalf("unexpected JSON catalog: %#v", decoded)
	}

	if _, err := encodeWritingCatalog(catalog, "html"); err == nil || !strings.Contains(err.Error(), "supported: markdown, json") {
		t.Fatalf("unexpected unsupported format error: %v", err)
	}
	catalog.OpenModelDiscovery = &evals.WritingOpenModelDiscovery{
		SourceURL:  "https://example.com/models",
		FetchedAt:  time.Unix(2, 0).UTC(),
		Status:     "available",
		Candidates: 1,
	}
	catalog.DiscoveredOpenModels = []evals.WritingDiscoveredOpenModel{{
		ID:             "fresh/Writer-7B",
		Name:           "Writer-7B",
		HuggingFaceURL: "https://huggingface.co/fresh/Writer-7B",
		License:        "apache-2.0",
		ReviewStatus:   "needs-review",
		Notes:          "review me",
	}}
	markdown, err = encodeWritingCatalog(catalog, "markdown")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "## Fresh public model discovery") || !strings.Contains(string(markdown), "Writer-7B") || !strings.Contains(string(markdown), "needs-review") {
		t.Fatalf("markdown missing discovery section: %s", markdown)
	}
}

func TestExecuteWritingCatalog(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"evals", "writing", "--format", "json"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"benchmarks"`) || !strings.Contains(out.String(), "DeepSeek-V4-Flash-0731") {
		t.Fatalf("unexpected writing catalog output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestEncodeWritingPromptRecords(t *testing.T) {
	records := []evals.WritingPromptRecord{
		{BenchmarkID: "writingbench", ID: "1", Prompt: "first"},
		{BenchmarkID: "ifeval", ID: "2", Prompt: "second"},
	}
	data, err := encodeWritingPromptRecords(records, "writingbench", 1)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"prompt":"first"`) {
		t.Fatalf("unexpected JSONL output: %q", string(data))
	}
	if _, err := encodeWritingPromptRecords(records, "missing", 0); err == nil || !strings.Contains(err.Error(), "unsupported --prompt-source") {
		t.Fatalf("unexpected source validation error: %v", err)
	}
	if _, err := encodeWritingPromptRecords(records, "eqbench-creative-v3", 0); err == nil || !strings.Contains(err.Error(), "no prompt records") {
		t.Fatalf("unexpected empty source error: %v", err)
	}
}

func TestWritingPromptExportRequiresRefresh(t *testing.T) {
	var out, errOut bytes.Buffer
	err := execute(context.Background(), []string{"evals", "writing", "--export-prompts", "/tmp/writing-prompts.jsonl"}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "requires --refresh") {
		t.Fatalf("unexpected export validation error: %v", err)
	}
}

func TestWritingModelDiscoveryRequiresRefresh(t *testing.T) {
	var out, errOut bytes.Buffer
	err := execute(context.Background(), []string{"evals", "writing", "--discover-open-models"}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "requires --refresh") {
		t.Fatalf("unexpected discovery validation error: %v", err)
	}
}
