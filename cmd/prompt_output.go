package cmd

import (
	"fmt"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
)

func recordPromptCatalogHealth(results []PromptResult) error {
	if len(results) == 0 {
		return nil
	}
	catPath, err := catalog.CatalogPath()
	if err != nil {
		return err
	}
	cat, err := catalog.Load(catPath)
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}
	now := time.Now().UTC()
	for _, result := range results {
		errText := ""
		if result.Error != nil {
			errText = result.Error.Error()
		}
		priorQuarantine, hadPriorQuarantine := promptCatalogQuarantine(cat, result.Provider, result.Model)
		catalog.RecordPing(cat, result.Provider, result.Model, result.Error == nil, result.Latency, 0, 0, errText, now)
		if result.Error == nil && result.Latency > catalog.SlowPingThreshold {
			restorePromptCatalogQuarantine(cat, result.Provider, result.Model, priorQuarantine, hadPriorQuarantine)
		}
	}
	if err := catalog.Save(catPath, cat); err != nil {
		return fmt.Errorf("save catalog health: %w", err)
	}
	return nil
}

func promptCatalogQuarantine(cat *catalog.Catalog, providerName, modelID string) (time.Time, bool) {
	if cat == nil || cat.Providers == nil {
		return time.Time{}, false
	}
	providerCatalog := cat.Providers[providerName]
	if providerCatalog == nil || providerCatalog.Models == nil {
		return time.Time{}, false
	}
	entry := providerCatalog.Models[modelID]
	if entry == nil {
		return time.Time{}, false
	}
	return entry.QuarantineUntil, true
}

func restorePromptCatalogQuarantine(cat *catalog.Catalog, providerName, modelID string, prior time.Time, hadPrior bool) {
	if cat == nil || cat.Providers == nil {
		return
	}
	providerCatalog := cat.Providers[providerName]
	if providerCatalog == nil || providerCatalog.Models == nil {
		return
	}
	entry := providerCatalog.Models[modelID]
	if entry == nil {
		return
	}
	if hadPrior {
		entry.QuarantineUntil = prior
		return
	}
	entry.QuarantineUntil = time.Time{}
}

func promptCostLabel(result PromptResult) string {
	label := catalog.PriceLabel(catalog.CostStatus(result.CostStatus), result.InputCostPer1M, result.OutputCostPer1M)
	if result.CostStatus != string(catalog.CostPaid) {
		return label
	}
	return fmt.Sprintf("%s, est input $%.6f, est max output $%.6f", label, result.EstimatedInputCost, result.EstimatedOutputCost)
}

func estimateTokens(text string) int {
	runes := len([]rune(text))
	if runes == 0 {
		return 0
	}
	tokens := runes / 4
	if tokens == 0 {
		return 1
	}
	return tokens
}

func estimateTokenCost(tokens int, perMillion float64) float64 {
	if tokens <= 0 || perMillion <= 0 {
		return 0
	}
	return float64(tokens) * perMillion / 1_000_000
}

// outputPromptResults formats and displays the prompt results in markdown format
func outputPromptResults(cmd *commandIO, promptText string, results []PromptResult) {
	out := cmd.OutOrStdout()

	// Title and prompt
	fmt.Fprintln(out, "# Multi-Model Prompt Comparison")
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "**Prompt:** %s\n", promptText)
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "**Results from %d provider(s)**\n", len(results))
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "---")
	fmt.Fprintln(out, "")

	// Individual results in distinct sections
	for i, result := range results {
		// Section header with provider name
		fmt.Fprintf(out, "## %d. %s\n", i+1, result.Provider)
		fmt.Fprintln(out, "")

		// Metadata table
		fmt.Fprintln(out, "| Field | Value |")
		fmt.Fprintln(out, "|-------|-------|")
		fmt.Fprintf(out, "| **Model** | `%s` |\n", result.Model)
		fmt.Fprintf(out, "| **Latency** | `%v` |\n", result.Latency)
		if result.CostStatus != "" {
			fmt.Fprintf(out, "| **Cost** | `%s` |\n", promptCostLabel(result))
		}

		if result.Error != nil {
			fmt.Fprintln(out, "| **Status** | ❌ FAILED |")
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "### Error")
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "```")
			fmt.Fprintf(out, "%v\n", result.Error)
			fmt.Fprintln(out, "```")
		} else {
			fmt.Fprintln(out, "| **Status** | ✅ SUCCESS |")
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "### Response")
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "%s\n", result.Response)
		}

		fmt.Fprintln(out, "")
		if i < len(results)-1 {
			fmt.Fprintln(out, "---")
			fmt.Fprintln(out, "")
		}
	}
}
