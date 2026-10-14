package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

// Repeated string literals consolidated by goconst.
const (
	no1  = "no"
	yes1 = "yes"
)

type writingCatalogOptions struct {
	format, output              string
	refresh                     bool
	exportPrompts, promptSource string
	promptLimit                 int
	discoverModels              bool
	discoverLimit               int
}

func runWritingCatalog(cmd *commandIO, options writingCatalogOptions) error {
	format, output, refresh := options.format, options.output, options.refresh
	exportPrompts, promptSource, promptLimit := options.exportPrompts, options.promptSource, options.promptLimit
	discoverModels, discoverLimit := options.discoverModels, options.discoverLimit

	if err := validateWritingCatalogOptions(options); err != nil {
		return err
	}
	catalog := evals.DefaultWritingCatalog(time.Now().UTC())
	if refresh {
		var err error
		catalog, err = evals.FetchWritingCatalog(cmd.Context(), evals.WritingCatalogOptions{
			Client:               &http.Client{Timeout: 30 * time.Second},
			Now:                  time.Now,
			ValidateSources:      true,
			ValidateModels:       true,
			IncludePromptRecords: exportPrompts != "",
			DiscoverOpenModels:   discoverModels,
			DiscoverLimit:        discoverLimit,
		})
		if err != nil {
			return err
		}
	}
	if exportPrompts != "" {
		data, err := encodeWritingPromptRecords(catalog.PromptRecords, promptSource, promptLimit)
		if err != nil {
			return err
		}
		if err := writeAtomicOutput(exportPrompts, data); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Writing prompt records written to %s\n", exportPrompts); err != nil {
			return err
		}
		// Keep the catalog report compact. Prompt records are written to the
		// dedicated JSONL artifact rather than duplicated in JSON report output.
		catalog.PromptRecords = nil
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

func encodeWritingPromptRecords(records []evals.WritingPromptRecord, source string, limit int) ([]byte, error) {
	source = strings.TrimSpace(strings.ToLower(source))
	if source == "" {
		source = "writingbench"
	}
	if source != "all" && source != "writingbench" && source != "eqbench-creative-v3" && source != "ifeval" {
		return nil, fmt.Errorf("unsupported --prompt-source %q (supported: all, writingbench, eqbench-creative-v3, ifeval)", source)
	}
	selected := make([]evals.WritingPromptRecord, 0, len(records))
	for _, record := range records {
		if source != "all" && record.BenchmarkID != source {
			continue
		}
		selected = append(selected, record)
		if limit > 0 && len(selected) >= limit {
			break
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no prompt records available for --prompt-source %q", source)
	}
	var out strings.Builder
	encoder := json.NewEncoder(&out)
	for _, record := range selected {
		if err := encoder.Encode(record); err != nil {
			return nil, err
		}
	}
	return []byte(out.String()), nil
}

func encodeWritingCatalog(catalog evals.WritingCatalog, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case markdown1, "md":
		return []byte(renderWritingCatalogMarkdown(catalog)), nil
	case json1:
		data, err := json.MarshalIndent(catalog, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	default:
		return nil, fmt.Errorf("unsupported --format %q (supported: markdown, json)", format)
	}
}

func writeWritingCatalogTo(errOut io.Writer, path string, data []byte) error {
	if err := writeAtomicOutput(path, data); err != nil {
		return err
	}
	_, err := fmt.Fprintf(errOut, "Writing benchmark catalog written to %s\n", path)
	return err
}

func validateWritingCatalogOptions(options writingCatalogOptions) error {
	format, output, refresh := options.format, options.output, options.refresh
	exportPrompts, promptSource, promptLimit := options.exportPrompts, options.promptSource, options.promptLimit
	discoverModels, discoverLimit := options.discoverModels, options.discoverLimit
	if exportPrompts != "" && !refresh {
		return errors.New("--export-prompts requires --refresh so the public prompt artifact is fetched")
	}
	if discoverModels && !refresh {
		return errors.New("--discover-open-models requires --refresh so the public model listing is fetched")
	}
	if promptLimit < 0 {
		return errors.New("--prompt-limit cannot be negative")
	}
	if discoverLimit < 0 {
		return errors.New("--discover-limit cannot be negative")
	}
	if exportPrompts != "" && output != "" && filepath.Clean(exportPrompts) == filepath.Clean(output) {
		return errors.New("--export-prompts and --output must name different files")
	}
	_ = format
	_ = promptSource
	return nil
}
