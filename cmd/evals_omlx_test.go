package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

func TestEvalsOMLXShowsCachedSnapshotWithoutNetwork(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".config", "llambo", "llambo-scores.json")
	score := 73.5
	estimate := 52.5
	snapshot := evals.OMLXScoreSnapshot{
		GeneratedAt: time.Unix(1, 0).UTC(), FormulaVersion: evals.CategoryFormulaVersion,
		PopulationFingerprint: "fingerprint", Inventory: []string{"local-model"},
		CategoryScores: map[string]map[string]*evals.LlamboScore{
			"local-model": {
				"reasoning": {Score: score, Confidence: "low", TrustedCoverage: .25, Families: []string{"advanced-science"}},
				"writing":   {Score: estimate, Confidence: "low", TrustedCoverage: 0, Estimated: true},
			},
		},
	}
	if err := evals.SaveOMLXScoreSnapshot(path, snapshot); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"evals", "omlx"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	for _, want := range []string{
		"local-model", "—", "73.5 (low, 25%)", "52.5ᵉ (low, 0%)",
		"Benchmark-backed cells:** 1 / 6", "Estimated cells:** 1 / 6", "Unresolved cells:** 4 / 6",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("cached OMLX output missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "Resolved cells") {
		t.Fatalf("cached OMLX output retained misleading resolved count:\n%s", rendered)
	}
}

func TestEvalsOMLXLiveRejectsUnsafeAndIrrelevantFlagsBeforeWork(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "refresh", args: []string{"evals", "omlx", "--refresh"}, want: "--refresh is not supported"},
		{name: "official cards", args: []string{"evals", "omlx", "--refresh-official-model-cards"}, want: "--refresh-official-model-cards is not supported"},
		{name: "offline", args: []string{"evals", "omlx", "--live", "--offline"}, want: "--live and --offline cannot be used together"},
		{name: "no omlx", args: []string{"evals", "omlx", "--live", "--no-omlx"}, want: "--live and --no-omlx cannot be used together"},
		{name: "format", args: []string{"evals", "omlx", "--format", "html"}, want: "only markdown and json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			err := execute(context.Background(), test.args, &out, &errOut)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestEvalsSourcesRefreshRejectsUnknownAndConflictingFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown source", args: []string{"evals", "sources", "refresh", "artificial-analysis"}, want: "unsupported evaluation source"},
		{name: "mixed source classes", args: []string{"evals", "sources", "refresh", "llm-stats-stats-v1,writingbench"}, want: "llm-stats-stats-v1 must be refreshed alone"},
		{name: "global refresh", args: []string{"evals", "sources", "refresh", "--refresh"}, want: "--refresh is not supported"},
		{name: "offline", args: []string{"evals", "sources", "refresh", "--offline"}, want: "--offline is not supported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			err := execute(context.Background(), test.args, &out, &errOut)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestEvalsOMLXAndSourcesHelp(t *testing.T) {
	for _, args := range [][]string{
		{"evals", "omlx", "--help"},
		{"evals", "sources", "refresh", "--help"},
	} {
		var out, errOut bytes.Buffer
		if err := execute(context.Background(), args, &out, &errOut); err != nil {
			t.Fatal(err)
		}
		help := out.String()
		if !strings.Contains(help, "--live") && strings.Join(args, " ") == "evals omlx --help" {
			t.Fatalf("OMLX help missing --live:\n%s", help)
		}
		if !strings.Contains(help, "writingbench") && strings.Join(args, " ") == "evals sources refresh --help" {
			t.Fatalf("source refresh help missing supported source:\n%s", help)
		}
	}
}
