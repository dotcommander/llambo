package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/writingcampaign"
	"github.com/dotcommander/llambo/providers"
)

type evalsWritingCampaignCommand struct {
	Roster        string        `default:"writingmodels.json" type:"path" help:"Locked OpenRouter campaign roster"`
	Model         string        `help:"Run one exact OpenRouter model ID from the locked roster"`
	Source        string        `type:"path" help:"Source Markdown; defaults to the roster asset"`
	SystemPrompt  string        `name:"system-prompt" type:"path" help:"Single-stage system prompt; defaults to the roster asset"`
	OutputDir     string        `name:"output-dir" type:"path" help:"Raw per-model receipt directory; defaults to the roster asset"`
	HTML          string        `type:"path" help:"Comparison HTML output; defaults to the roster asset"`
	InitialTokens int           `name:"initial-max-tokens" default:"8192" help:"Initial completion-token ceiling"`
	RetryTokens   int           `name:"length-retry-max-tokens" default:"32768" help:"Higher ceiling used only after finish_reason=length"`
	Concurrency   int           `default:"2" help:"Concurrent model requests (1..8)"`
	Timeout       time.Duration `default:"10m" help:"Timeout per OpenRouter request"`
	Execute       bool          `help:"Make OpenRouter requests; omitted validates and renders existing receipts only"`
}

func (c *evalsWritingCampaignCommand) Run(_ *evalsCommand, io *commandIO) error {
	for _, name := range []string{"refresh", "refresh-official-model-cards", "format", "output", "export-prompts", "prompt-source", "prompt-limit", "discover-open-models", "discover-limit", "limit", "allow-partial", "rank-by", "offline", "live-omlx", "projections", "validation-receipts", "min-score", "max-output-price", "omlx-url", "cache-dir"} {
		if io.FlagChanged(name) {
			return fmt.Errorf("--%s is not supported with evals writing campaign", name)
		}
	}
	roster, err := writingcampaign.LoadRoster(c.Roster)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.Model) != "" {
		selected := make([]writingcampaign.Model, 0, 1)
		for _, model := range roster.Models {
			if model.OpenRouterModelID == c.Model {
				if model.Disabled {
					return fmt.Errorf("model %q is disabled: %s", c.Model, model.DisabledReason)
				}
				selected = append(selected, model)
				break
			}
		}
		if len(selected) == 0 {
			return fmt.Errorf("model %q is not in the locked writing roster", c.Model)
		}
		roster.Models = selected
	} else {
		active := roster.Models[:0]
		for _, model := range roster.Models {
			if !model.Disabled {
				active = append(active, model)
			}
		}
		if len(active) == 0 {
			return fmt.Errorf("locked writing roster has no enabled models")
		}
		roster.Models = active
	}
	root := filepath.Dir(c.Roster)
	options := writingcampaign.RunOptions{
		RosterPath:    c.Roster,
		SourcePath:    campaignAssetPath(root, c.Source, roster.Assets.SourceInput),
		SystemPath:    campaignAssetPath(root, c.SystemPrompt, roster.Assets.SystemPrompt),
		OutputDir:     campaignAssetPath(root, c.OutputDir, roster.Assets.RawOutputDir),
		HTMLPath:      campaignAssetPath(root, c.HTML, roster.Assets.ComparisonPage),
		InitialTokens: c.InitialTokens,
		RetryTokens:   c.RetryTokens,
		Concurrency:   c.Concurrency,
		Execute:       c.Execute,
	}
	source, err := os.ReadFile(options.SourcePath)
	if err != nil {
		return fmt.Errorf("read source input: %w", err)
	}
	systemPrompt, err := os.ReadFile(options.SystemPath)
	if err != nil {
		return fmt.Errorf("read system prompt: %w", err)
	}
	client := &writingcampaign.Client{Now: time.Now}
	if c.Execute {
		cfg, err := providers.GetProviderConfig("openrouter")
		if err != nil {
			return err
		}
		client.BaseURL = cfg.BaseURL
		client.APIKey = cfg.APIKey
		client.Headers = cfg.ExtraHeaders
		client.HTTPClient = &http.Client{Timeout: c.Timeout}
	}
	receipts, plan, runErr := writingcampaign.Run(io.Context(), client, roster, string(source), string(systemPrompt), options)
	if err := writingcampaign.WriteHTML(options.HTMLPath, roster, receipts, time.Now()); err != nil {
		return fmt.Errorf("write comparison HTML: %w", err)
	}
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(io.OutOrStdout(), "%s\nComparison: %s\nReceipts: %s\n", encoded, options.HTMLPath, options.OutputDir); err != nil {
		return err
	}
	if runErr != nil {
		return runErr
	}
	if c.Execute {
		failed := 0
		for _, receipt := range receipts {
			if receipt.Status != "complete" {
				failed++
			}
		}
		if failed > 0 {
			return fmt.Errorf("campaign completed with %d failed model receipts; see %s", failed, options.OutputDir)
		}
	}
	return nil
}

func campaignAssetPath(root, override, rosterPath string) string {
	path := strings.TrimSpace(override)
	if path == "" {
		path = rosterPath
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, path)
}
