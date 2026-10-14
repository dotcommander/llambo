package cmd

import (
	"fmt"
	"strings"

	"github.com/dotcommander/llambo/internal/evals"
)

func renderWritingCoverage(out *strings.Builder, catalog evals.WritingCatalog) {
	out.WriteString("\n## Open-weight leaderboard coverage\n\n")
	if len(catalog.OpenModelCoverage) == 0 {
		out.WriteString("No live coverage match was run. Use `llambo evals writing --refresh` to compare the reviewed queue with the current primary leaderboard.\n")
	} else {
		out.WriteString("Measured rows use explicit reviewed aliases. `variant` matches may differ by serving mode or size. `needs-run` means no comparable public row was found, not that the model is weak. No score is inferred for missing rows.\n\n")
		out.WriteString("| Model | Status | Match type | Matched row | Rank | Comparison score | Win chance | Notes |\n")
		out.WriteString("| --- | --- | --- | --- | ---: | ---: | ---: | --- |\n")
		for _, item := range catalog.OpenModelCoverage {
			fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
				writingCatalogCell(item.ModelName),
				writingCatalogCell(item.Status),
				writingCatalogCell(item.MatchType),
				writingCatalogCell(item.MatchedLeaderboardModel),
				writingCatalogNumber(item.Rank),
				writingCatalogFloat(item.ComparisonScore),
				writingCatalogPercent(item.WinChancePercent),
				writingCatalogCell(item.Notes),
			)
		}
	}

}

func renderWritingLeaderboards(out *strings.Builder, catalog evals.WritingCatalog) {
	out.WriteString("\n## Live leaderboard snapshot\n\n")
	if len(catalog.Leaderboards) == 0 {
		out.WriteString("No live snapshot was fetched. Run `llambo evals writing --refresh` to scrape the primary public leaderboard.\n")
		return
	}
	for _, leaderboard := range catalog.Leaderboards {
		fmt.Fprintf(out, "### `%s`\n\n", writingCatalogCell(leaderboard.BenchmarkID))
		fmt.Fprintf(out, "Source: %s  \nFetched: `%s`\n\n", writingCatalogLink(leaderboard.SourceURL, leaderboard.SourceURL), writingCatalogTime(leaderboard.FetchedAt))
		if leaderboard.Error != "" {
			fmt.Fprintf(out, "Fetch error: `%s`\n\n", writingCatalogCell(leaderboard.Error))
			continue
		}
		out.WriteString("| Rank | Model | Comparison score | Win chance | Uncertainty |\n")
		out.WriteString("| ---: | --- | ---: | ---: | --- |\n")
		for _, row := range leaderboard.Rows {
			fmt.Fprintf(out, "| %d | %s | %.1f | %.0f%% | %.1f to %.1f |\n", row.Rank, writingCatalogCell(row.Model), row.Score, row.WinChance, row.Lower, row.Upper)
		}
		out.WriteString("\n")
	}
}
