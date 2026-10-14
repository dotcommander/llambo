package cmd

import (
	"fmt"
	"strings"

	"github.com/dotcommander/llambo/internal/evals"
)

func renderWritingCatalogMarkdown(catalog evals.WritingCatalog) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Writing Benchmark Catalog\n\n")
	fmt.Fprintf(&out, "Generated: `%s`  \n", writingCatalogTime(catalog.GeneratedAt))
	fmt.Fprintf(&out, "Registry version: `%d`\n\n", catalog.RegistryVersion)
	out.WriteString("This catalog keeps writing benchmark scores source-native. It does not mix local run results into LLAMBO-2 scores.\n\n")

	renderWritingBenchmarkPlan(&out, catalog)
	renderWritingSourceChecks(&out, catalog)
	renderWritingModelQueue(&out, catalog)
	renderWritingModelChecks(&out, catalog)
	renderWritingDiscovery(&out, catalog)
	renderWritingCoverage(&out, catalog)
	renderWritingLeaderboards(&out, catalog)
	return out.String()
}

func renderWritingBenchmarkPlan(out *strings.Builder, catalog evals.WritingCatalog) {
	out.WriteString("## Benchmarks\n\n")
	out.WriteString("| Benchmark | Focus | Scoring | Scrape surface | Update cadence |\n")
	out.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, benchmark := range catalog.Benchmarks {
		name := writingCatalogLink(benchmark.Name, benchmark.URL)
		dataURL := benchmark.DataURL
		if dataURL == "" {
			dataURL = benchmark.URL
		}
		surface := writingCatalogLink(benchmark.ScrapeMethod, dataURL)
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s |\n",
			name,
			writingCatalogCell(benchmark.Focus),
			writingCatalogCell(benchmark.Scoring),
			surface,
			writingCatalogCell(benchmark.UpdateCadence),
		)
	}

	out.WriteString("\n## Run plan\n\n")
	out.WriteString("Each row identifies a public prompt surface and the safest way to rerun that benchmark. A blank count means the source is live, multi-stage, or not a fixed corpus.\n\n")
	out.WriteString("| Benchmark | Prompt artifact | Format | Prompts | Run mode | Run notes |\n")
	out.WriteString("| --- | --- | --- | ---: | --- | --- |\n")
	for _, benchmark := range catalog.Benchmarks {
		promptURL := benchmark.PromptURL
		if promptURL == "" {
			promptURL = benchmark.DataURL
		}
		if promptURL == "" {
			promptURL = benchmark.URL
		}
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s |\n",
			writingCatalogCell(benchmark.Name),
			writingCatalogLink(benchmark.PromptFormat, promptURL),
			writingCatalogCell(benchmark.PromptFormat),
			writingCatalogCount(benchmark.PromptCount),
			writingCatalogCell(benchmark.RunMode),
			writingCatalogCell(benchmark.RunNotes),
		)
	}

}

func renderWritingSourceChecks(out *strings.Builder, catalog evals.WritingCatalog) {
	out.WriteString("\n## Public source checks\n\n")
	if len(catalog.SourceChecks) == 0 {
		out.WriteString("No live source checks were run. Use `llambo evals writing --refresh` to fetch the registered artifacts.\n")
	} else {
		out.WriteString("Refresh checks the primary leaderboard plus each registered public artifact. An `available` check confirms the endpoint was fetched; parsed record counts are shown where the source format is known.\n\n")
		out.WriteString("| Benchmark | Status | HTTP | Records | Bytes | Fetched | Error |\n")
		out.WriteString("| --- | --- | ---: | ---: | ---: | --- | --- |\n")
		for _, check := range catalog.SourceChecks {
			fmt.Fprintf(out, "| %s | %s | %d | %s | %d | `%s` | %s |\n",
				writingCatalogLink(check.BenchmarkID, check.SourceURL),
				writingCatalogCell(check.Status),
				check.HTTPStatus,
				writingCatalogCount(check.Records),
				check.Bytes,
				writingCatalogTime(check.FetchedAt),
				writingCatalogCell(check.Error),
			)
		}
	}

}
