package cmd

import (
	"fmt"
	"strings"

	"github.com/dotcommander/llambo/internal/evals"
)

func renderWritingModelQueue(out *strings.Builder, catalog evals.WritingCatalog) {
	out.WriteString("\n## Latest open-weight model queue\n\n")
	out.WriteString("Coverage labels distinguish models already measured by a public leaderboard from candidates that still need a writing rerun. License labels are copied from the reviewed model registry and should be checked before redistribution.\n\n")
	out.WriteString("| Model | Provider | License | Coverage | Priority | Hugging Face | Notes |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, model := range catalog.OpenModels {
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s | %s |\n",
			writingCatalogCell(model.Name),
			writingCatalogCell(model.Provider),
			writingCatalogCell(model.License),
			writingCatalogCell(model.Coverage),
			writingCatalogCell(model.Priority),
			writingCatalogLink(model.ID, model.HuggingFaceURL),
			writingCatalogCell(model.Notes),
		)
	}

}

func renderWritingModelChecks(out *strings.Builder, catalog evals.WritingCatalog) {
	out.WriteString("\n## Open-weight metadata checks\n\n")
	if len(catalog.OpenModelChecks) == 0 {
		out.WriteString("No live model metadata checks were run. Use `llambo evals writing --refresh` to verify the queue against Hugging Face.\n")
	} else {
		out.WriteString("These checks verify that the reviewed model IDs are public and expose current creation, modification, license, and download metadata. They do not discover every new model on Hugging Face.\n\n")
		out.WriteString("| Model | Status | Remote license | Match | Created | Last modified | Downloads | Gated | Error |\n")
		out.WriteString("| --- | --- | --- | --- | --- | --- | ---: | --- | --- |\n")
		for _, check := range catalog.OpenModelChecks {
			fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s | %d | %t | %s |\n",
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

}

func renderWritingDiscovery(out *strings.Builder, catalog evals.WritingCatalog) {
	out.WriteString("\n## Fresh public model discovery\n\n")
	if catalog.OpenModelDiscovery == nil {
		out.WriteString("No fresh model discovery was run. Use `llambo evals writing --refresh --discover-open-models` to list recent public text-generation candidates.\n")
	} else {
		discovery := catalog.OpenModelDiscovery
		fmt.Fprintf(out, "Discovery status: `%s`  \nSource: %s  \nFetched: `%s`  \nCandidates: `%d`\n\n", writingCatalogCell(discovery.Status), writingCatalogLink(discovery.SourceURL, discovery.SourceURL), writingCatalogTime(discovery.FetchedAt), discovery.Candidates)
		if discovery.Error != "" {
			fmt.Fprintf(out, "Discovery error: `%s`\n\n", writingCatalogCell(discovery.Error))
		}
		if len(catalog.DiscoveredOpenModels) > 0 {
			out.WriteString("These are fresh candidates, not reviewed models. No benchmark score or leaderboard coverage is inferred.\n\n")
			out.WriteString("| Candidate | License | Last modified | Downloads | Likes | Review status | Tags | Notes |\n")
			out.WriteString("| --- | --- | --- | ---: | ---: | --- | --- | --- |\n")
			for _, model := range catalog.DiscoveredOpenModels {
				fmt.Fprintf(out, "| %s | %s | %s | %d | %d | %s | %s | %s |\n",
					writingCatalogLink(model.Name, model.HuggingFaceURL),
					writingCatalogCell(model.License),
					writingCatalogCell(model.LastModified),
					model.Downloads,
					model.Likes,
					writingCatalogCell(model.ReviewStatus),
					writingCatalogCell(strings.Join(model.Tags, ", ")),
					writingCatalogCell(model.Notes),
				)
			}
		}
	}

}
