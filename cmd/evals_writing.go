package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

func runWritingCatalog(cmd *commandIO, format, output string, refresh bool) error {
	catalog := evals.DefaultWritingCatalog(time.Now().UTC())
	if refresh {
		var err error
		catalog, err = evals.FetchWritingCatalog(cmd.Context(), evals.WritingCatalogOptions{
			Client:          &http.Client{Timeout: 30 * time.Second},
			Now:             time.Now,
			ValidateSources: true,
			ValidateModels:  true,
		})
		if err != nil {
			return err
		}
	}
	data, err := encodeWritingCatalog(catalog, format)
	if err != nil {
		return err
	}
	if output == "" {
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}
	return writeWritingCatalogTo(cmd.ErrOrStderr(), output, data)
}

func encodeWritingCatalog(catalog evals.WritingCatalog, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "markdown", "md":
		return []byte(renderWritingCatalogMarkdown(catalog)), nil
	case "json":
		data, err := json.MarshalIndent(catalog, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	default:
		return nil, fmt.Errorf("unsupported --format %q (supported: markdown, json)", format)
	}
}

func renderWritingCatalogMarkdown(catalog evals.WritingCatalog) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Writing Benchmark Catalog\n\n")
	fmt.Fprintf(&out, "Generated: `%s`  \n", writingCatalogTime(catalog.GeneratedAt))
	fmt.Fprintf(&out, "Registry version: `%d`\n\n", catalog.RegistryVersion)
	out.WriteString("This catalog keeps writing benchmark scores source-native. It does not mix these results into the LES-1 evaluation report.\n\n")

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
		fmt.Fprintf(&out, "| %s | %s | %s | %s | %s |\n",
			name,
			writingCatalogCell(benchmark.Focus),
			writingCatalogCell(benchmark.Scoring),
			surface,
			writingCatalogCell(benchmark.UpdateCadence),
		)
	}

	out.WriteString("\n## Public source checks\n\n")
	if len(catalog.SourceChecks) == 0 {
		out.WriteString("No live source checks were run. Use `llambo evals writing --refresh` to fetch the registered artifacts.\n")
	} else {
		out.WriteString("Refresh checks the primary leaderboard plus each registered public artifact. An `available` check confirms the endpoint was fetched; parsed record counts are shown where the source format is known.\n\n")
		out.WriteString("| Benchmark | Status | HTTP | Records | Bytes | Fetched | Error |\n")
		out.WriteString("| --- | --- | ---: | ---: | ---: | --- | --- |\n")
		for _, check := range catalog.SourceChecks {
			fmt.Fprintf(&out, "| %s | %s | %d | %s | %d | `%s` | %s |\n",
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

	out.WriteString("\n## Latest open-weight model queue\n\n")
	out.WriteString("Coverage labels distinguish models already measured by a public leaderboard from candidates that still need a writing rerun. License labels are copied from the reviewed model registry and should be checked before redistribution.\n\n")
	out.WriteString("| Model | Provider | License | Coverage | Priority | Hugging Face | Notes |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, model := range catalog.OpenModels {
		fmt.Fprintf(&out, "| %s | %s | %s | %s | %s | %s | %s |\n",
			writingCatalogCell(model.Name),
			writingCatalogCell(model.Provider),
			writingCatalogCell(model.License),
			writingCatalogCell(model.Coverage),
			writingCatalogCell(model.Priority),
			writingCatalogLink(model.ID, model.HuggingFaceURL),
			writingCatalogCell(model.Notes),
		)
	}

	out.WriteString("\n## Open-weight metadata checks\n\n")
	if len(catalog.OpenModelChecks) == 0 {
		out.WriteString("No live model metadata checks were run. Use `llambo evals writing --refresh` to verify the queue against Hugging Face.\n")
	} else {
		out.WriteString("These checks verify that the reviewed model IDs are public and expose current creation, modification, license, and download metadata. They do not discover every new model on Hugging Face.\n\n")
		out.WriteString("| Model | Status | Remote license | Match | Created | Last modified | Downloads | Gated | Error |\n")
		out.WriteString("| --- | --- | --- | --- | --- | --- | ---: | --- | --- |\n")
		for _, check := range catalog.OpenModelChecks {
			fmt.Fprintf(&out, "| %s | %s | %s | %s | %s | %s | %d | %t | %s |\n",
				writingCatalogLink(check.ModelName, check.MetadataURL),
				writingCatalogCell(check.Status),
				writingCatalogCell(check.RemoteLicense),
				writingCatalogLicenseMatch(check),
				writingCatalogCell(check.CreatedAt),
				writingCatalogCell(check.LastModified),
				check.Downloads,
				check.Gated,
				writingCatalogCell(check.Error),
			)
		}
	}

	out.WriteString("\n## Live leaderboard snapshot\n\n")
	if len(catalog.Leaderboards) == 0 {
		out.WriteString("No live snapshot was fetched. Run `llambo evals writing --refresh` to scrape the primary public leaderboard.\n")
		return out.String()
	}
	for _, leaderboard := range catalog.Leaderboards {
		fmt.Fprintf(&out, "### `%s`\n\n", writingCatalogCell(leaderboard.BenchmarkID))
		fmt.Fprintf(&out, "Source: %s  \nFetched: `%s`\n\n", writingCatalogLink(leaderboard.SourceURL, leaderboard.SourceURL), writingCatalogTime(leaderboard.FetchedAt))
		if leaderboard.Error != "" {
			fmt.Fprintf(&out, "Fetch error: `%s`\n\n", writingCatalogCell(leaderboard.Error))
			continue
		}
		out.WriteString("| Rank | Model | Comparison score | Win chance | Uncertainty |\n")
		out.WriteString("| ---: | --- | ---: | ---: | --- |\n")
		for _, row := range leaderboard.Rows {
			fmt.Fprintf(&out, "| %d | %s | %.1f | %.0f%% | %.1f to %.1f |\n", row.Rank, writingCatalogCell(row.Model), row.Score, row.WinChance, row.Lower, row.Upper)
		}
		out.WriteString("\n")
	}
	return out.String()
}

func writingCatalogLink(label, url string) string {
	return fmt.Sprintf("[%s](%s)", writingCatalogCell(label), url)
}

func writingCatalogCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.ReplaceAll(value, "|", "\\|")
}

func writingCatalogCount(value int) string {
	if value == 0 {
		return "—"
	}
	return fmt.Sprintf("%d", value)
}

func writingCatalogLicenseMatch(check evals.WritingModelStatus) string {
	if check.RemoteLicense == "" {
		return "—"
	}
	if check.LicenseMatch {
		return "yes"
	}
	return "no"
}

func writingCatalogTime(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.UTC().Format(time.RFC3339)
}

func writeWritingCatalogTo(errOut io.Writer, path string, data []byte) error {
	if err := writeAtomicOutput(path, data); err != nil {
		return err
	}
	_, err := fmt.Fprintf(errOut, "Writing benchmark catalog written to %s\n", path)
	return err
}
