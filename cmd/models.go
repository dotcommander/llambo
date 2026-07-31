package cmd

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dotcommander/llambo/providers"
)

var modelsAll bool
var modelsCSV bool
var modelsAvailable bool
var modelsTimeoutSec int
var modelsGrouped = true

type modelRow struct {
	Provider string
	Enabled  bool
	Model    string
	Primary  bool
}

func runModels(cmd *commandIO, args []string) error {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return err
	}

	rows := make([]modelRow, 0)
	for _, name := range sortedProviderNames(cfg.Providers) {
		pcfg := cfg.Providers[name]
		if !modelsAll && !pcfg.Enabled {
			continue
		}

		models := modelVariants(pcfg)
		if modelsAvailable {
			if avail := fetchAvailableModelsContext(cmd.Context(), name, pcfg, modelsTimeoutSec); len(avail) > 0 {
				models = avail
			}
		}

		for i, model := range models {
			rows = append(rows, modelRow{
				Provider: name,
				Enabled:  pcfg.Enabled,
				Model:    model,
				Primary:  i == 0 && !modelsAvailable,
			})
		}
	}

	if modelsCSV && modelsGrouped {
		w := csv.NewWriter(out)
		if err := w.Write([]string{"provider", "enabled", "models"}); err != nil {
			return err
		}
		for _, grouped := range groupRows(rows) {
			if err := w.Write([]string{grouped.Provider, fmt.Sprintf("%t", grouped.Enabled), strings.Join(grouped.Models, ", ")}); err != nil {
				return err
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return err
		}
		if len(rows) == 0 && !modelsAll {
			fmt.Fprintln(errOut, "No enabled providers found. Use --all to list disabled providers.")
		}
		return nil
	}

	if modelsCSV {
		w := csv.NewWriter(out)
		if err := w.Write([]string{"provider", "enabled", "model", "primary"}); err != nil {
			return err
		}
		for _, row := range rows {
			if err := w.Write([]string{row.Provider, fmt.Sprintf("%t", row.Enabled), row.Model, fmt.Sprintf("%t", row.Primary)}); err != nil {
				return err
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return err
		}
		if len(rows) == 0 && !modelsAll {
			fmt.Fprintln(errOut, "No enabled providers found. Use --all to list disabled providers.")
		}
		return nil
	}

	if modelsGrouped {
		fmt.Fprintf(out, "%-12s %-8s %s\n", "PROVIDER", "ENABLED", "MODELS")
		fmt.Fprintln(out, "--------------------------------------------------------------------------------")
		for _, grouped := range groupRows(rows) {
			fmt.Fprintf(out, "%-12s %-8t %s\n", grouped.Provider, grouped.Enabled, strings.Join(grouped.Models, ", "))
		}
	} else {
		fmt.Fprintf(out, "%-12s %-8s %-8s %s\n", "PROVIDER", "ENABLED", "PRIMARY", "MODEL")
		fmt.Fprintln(out, "--------------------------------------------------------------------------------")
		for _, row := range rows {
			fmt.Fprintf(out, "%-12s %-8t %-8t %s\n", row.Provider, row.Enabled, row.Primary, row.Model)
		}
	}

	if len(rows) == 0 {
		if modelsAll {
			fmt.Fprintln(out, "No providers found in config.")
		} else {
			fmt.Fprintln(out, "No enabled providers found. Use --all to list disabled providers.")
		}
	}

	return nil
}

type groupedModelRow struct {
	Provider string
	Enabled  bool
	Models   []string
}

func groupRows(rows []modelRow) []groupedModelRow {
	byProvider := make(map[string]*groupedModelRow)
	order := make([]string, 0)

	for _, row := range rows {
		g, ok := byProvider[row.Provider]
		if !ok {
			g = &groupedModelRow{Provider: row.Provider, Enabled: row.Enabled, Models: make([]string, 0)}
			byProvider[row.Provider] = g
			order = append(order, row.Provider)
		}
		g.Models = append(g.Models, row.Model)
	}

	out := make([]groupedModelRow, 0, len(order))
	for _, provider := range order {
		out = append(out, *byProvider[provider])
	}
	return out
}

func fetchAvailableModels(name string, cfg providers.Config, timeoutSec int) []string {
	return fetchAvailableModelsContext(context.Background(), name, cfg, timeoutSec)
}

func fetchAvailableModelsContext(ctx context.Context, name string, cfg providers.Config, timeoutSec int) []string {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}

	if cfg.GetProviderType() == "gemini" {
		return fetchGeminiModelsContext(ctx, name, cfg, timeoutSec)
	}
	return fetchOpenAICompatibleModelsContext(ctx, name, cfg, timeoutSec)
}

func fetchOpenAICompatibleModels(name string, cfg providers.Config, timeoutSec int) []string {
	return fetchOpenAICompatibleModelsContext(context.Background(), name, cfg, timeoutSec)
}

func fetchOpenAICompatibleModelsContext(ctx context.Context, name string, cfg providers.Config, timeoutSec int) []string {
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	if baseURL == "" {
		return nil
	}

	if !strings.HasSuffix(baseURL, "/v1") && !strings.HasSuffix(baseURL, "/v4") {
		baseURL += "/v1"
	}
	url := baseURL + "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")

	apiKey := providers.GetAPIKey(name, cfg)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range cfg.ExtraHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MB cap on model list responses
	if err != nil {
		return nil
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	out := make([]string, 0, len(payload.Data))
	for _, d := range payload.Data {
		if d.ID != "" {
			out = append(out, d.ID)
		}
	}
	sort.Strings(out)
	return out
}

func fetchGeminiModels(name string, cfg providers.Config, timeoutSec int) []string {
	return fetchGeminiModelsContext(context.Background(), name, cfg, timeoutSec)
}

func fetchGeminiModelsContext(ctx context.Context, name string, cfg providers.Config, timeoutSec int) []string {
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	if baseURL == "" {
		return nil
	}
	apiKey := providers.GetAPIKey(name, cfg)
	if apiKey == "" {
		return nil
	}

	url := baseURL + "/models?key=" + apiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}

	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MB cap on model list responses
	if err != nil {
		return nil
	}

	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	out := make([]string, 0, len(payload.Models))
	for _, m := range payload.Models {
		name := strings.TrimPrefix(m.Name, "models/")
		if name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
