package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

func (c *evalsOMLXCommand) Run(parent *evalsCommand, io *commandIO) error {
	for _, name := range []string{"refresh", "refresh-official-model-cards", "output", "export-prompts"} {
		if io.FlagChanged(name) {
			return fmt.Errorf("--%s is not supported with evals omlx", name)
		}
	}
	live := c.Live || parent.LiveOMLX
	if live && parent.Offline {
		return fmt.Errorf("--live and --offline cannot be used together")
	}
	if live && parent.NoOMLX {
		return fmt.Errorf("--live and --no-omlx cannot be used together")
	}
	format := strings.ToLower(strings.TrimSpace(parent.Format))
	if format == "" {
		format = "markdown"
	}
	if format != "markdown" && format != "json" {
		return fmt.Errorf("evals omlx supports only markdown and json formats")
	}
	cacheDir, err := evaluationCacheDirectory(parent.CacheDir)
	if err != nil {
		return err
	}
	if !live {
		snapshotPath, err := evals.OMLXScoreSnapshotPath()
		if err != nil {
			return err
		}
		snapshot, err := evals.LoadOMLXScoreSnapshot(snapshotPath)
		if err != nil {
			return err
		}
		if snapshot == nil {
			return fmt.Errorf("no cached OMLX score snapshot; run `llambo evals omlx --live`")
		}
		return writeOMLXScoreOutput(io, snapshot, format, nil)
	}
	if err := validateOMLXURL(parent.OMLXURL); err != nil {
		return err
	}
	discovery, err := evals.DiscoverOMLXModels(io.Context(), parent.OMLXURL, os.Getenv("OMLX_API_KEY"), omlxDiscoveryHTTPClient)
	if err != nil {
		return fmt.Errorf("discover live OMLX inventory: %w", err)
	}
	result, err := evals.Fetch(io.Context(), evals.Options{
		CacheDir:     cacheDir,
		Offline:      true,
		AllowPartial: parent.AllowPartial,
	})
	if err != nil {
		return err
	}
	report, err := evals.BuildReportWithProjectionFile(result, "matrix", parent.Projections)
	if err != nil {
		return err
	}
	evals.FilterProjectedRowsToLiveOMLX(&report, discovery, nil)
	pending, err := prepareEvalsOMLXScores(&report, discovery, true)
	if err != nil {
		return err
	}
	if err := publishOMLXScores(pending); err != nil {
		return err
	}
	return writeOMLXScoreOutput(io, &pending.snapshot, format, report.OMLX)
}

func evaluationCacheDirectory(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(cacheRoot, "llambo", "evals"), nil
}

func writeOMLXScoreOutput(io *commandIO, snapshot *evals.OMLXScoreSnapshot, format string, diagnostics *evals.OMLXDiagnostics) error {
	if format == "json" {
		data, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			return err
		}
		_, err = io.OutOrStdout().Write(append(data, '\n'))
		return err
	}
	_, err := io.OutOrStdout().Write([]byte(renderOMLXScoreSnapshot(snapshot, diagnostics)))
	return err
}

func renderOMLXScoreSnapshot(snapshot *evals.OMLXScoreSnapshot, diagnostics *evals.OMLXDiagnostics) string {
	if snapshot == nil {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# OMLX LLAMBO Scores\n\n")
	fmt.Fprintf(&out, "Generated: `%s`  \nFormula: `%s`  \nCache fingerprint: `%s`\n\n", snapshot.GeneratedAt.Format(time.RFC3339), snapshot.FormulaVersion, snapshot.PopulationFingerprint)
	out.WriteString("Each cell shows the raw common-cohort score, confidence, and trusted coverage.\n\n")
	out.WriteString("| Model | Agents | Coding | Instruction Following | Long Context | Reasoning | Writing |\n")
	out.WriteString("|---|---:|---:|---:|---:|---:|---:|\n")
	benchmarkBacked, estimated, unresolved := 0, 0, 0
	for _, model := range snapshot.Inventory {
		scores := snapshot.CategoryScores[model]
		values := make([]string, 0, len(categoryOrderForOMLXDisplay))
		for _, category := range categoryOrderForOMLXDisplay {
			score := scores[category]
			if score == nil {
				values = append(values, "—")
				unresolved++
				continue
			}
			if score.Estimated {
				estimated++
			} else {
				benchmarkBacked++
			}
			values = append(values, formatOMLXScore(score))
		}
		fmt.Fprintf(&out, "| %s | %s |\n", model, strings.Join(values, " | "))
	}
	total := len(snapshot.Inventory) * len(categoryOrderForOMLXDisplay)
	fmt.Fprintf(&out, "\n**Benchmark-backed cells:** %d / %d\n", benchmarkBacked, total)
	fmt.Fprintf(&out, "**Estimated cells:** %d / %d\n", estimated, total)
	fmt.Fprintf(&out, "**Unresolved cells:** %d / %d\n", unresolved, total)
	if estimated > 0 {
		out.WriteString("\n`ᵉ` marks a frozen calibrated estimate rather than benchmark evidence. Estimates have zero coverage, low confidence, and never satisfy the external evidence campaign.\n")
	}
	if diagnostics != nil {
		fmt.Fprintf(&out, "\n**Live discovery:** %d eligible text/VLM model(s)", diagnostics.Discovered)
		if len(diagnostics.UnmatchedModels) > 0 {
			fmt.Fprintf(&out, "; unmatched: %s", strings.Join(diagnostics.UnmatchedModels, ", "))
		}
		out.WriteString("\n")
		if len(diagnostics.ExcludedModels) > 0 {
			fmt.Fprintf(&out, "\n**Excluded helper/audio/embedding/hidden models:** %s\n", strings.Join(diagnostics.ExcludedModels, ", "))
		}
	}
	return out.String()
}

func formatOMLXScore(score *evals.LlamboScore) string {
	estimateMark := ""
	if score.Estimated {
		estimateMark = "ᵉ"
	}
	return fmt.Sprintf("%.1f%s (%s, %.0f%%)", score.Score, estimateMark, score.Confidence, score.TrustedCoverage*100)
}

var categoryOrderForOMLXDisplay = []string{
	"agents", "coding", "instruction-following", "long-context", "reasoning", "writing",
}
