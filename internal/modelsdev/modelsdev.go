package modelsdev

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/dotcommander/llambo/internal/costs"
)

// modelCost is the per-1M-token pricing block in api.json (USD per 1M tokens).
type modelCost struct {
	Input   float64 `json:"input"`
	Output  float64 `json:"output"`
	invalid bool
}

type model struct {
	Cost *modelCost `json:"cost"`
}

// ProviderModels is one top-level provider entry in api.json (only the fields we use).
type ProviderModels struct {
	Models map[string]model `json:"models"`
}

// Parse decodes a models.dev api.json body into a provider-keyed map.
func Parse(data []byte) (map[string]ProviderModels, error) {
	var api map[string]ProviderModels
	if err := json.Unmarshal(data, &api); err != nil {
		return nil, err
	}
	return api, nil
}

// Normalize maps api.json data to llambo pricing keys "<llamboProvider>:<modelid>",
// using providerToKey (llambo provider name -> models.dev top-level key). When filter
// is non-empty only those llambo providers are included. Models without a cost block
// are skipped (priced-only).
func Normalize(api map[string]ProviderModels, providerToKey map[string]string, filter []string) costs.ModelsDevFile {
	allow := allowedProviders(filter)
	out := costs.ModelsDevFile{}
	for llamboName, devKey := range providerToKey {
		if allow != nil && !allow[strings.ToLower(llamboName)] {
			continue
		}
		pm, ok := api[devKey]
		if !ok {
			continue
		}
		mergeProviderPrices(out, llamboName, pm.Models)
	}
	applyOfficialPriceBackfills(out, providerToKey, allow)
	return out
}

func allowedProviders(filter []string) map[string]bool {
	if len(filter) == 0 {
		return nil
	}
	allow := make(map[string]bool, len(filter))
	for _, provider := range filter {
		allow[strings.ToLower(strings.TrimSpace(provider))] = true
	}
	return allow
}

func mergeProviderPrices(out costs.ModelsDevFile, provider string, models map[string]model) {
	for id, entry := range models {
		if !eligibleModelPrice(entry.Cost) {
			continue
		}
		out[costs.Key(provider, id)] = costs.ModelsDevPrice{InputPer1M: entry.Cost.Input, OutputPer1M: entry.Cost.Output}
	}
}

func eligibleModelPrice(price *modelCost) bool {
	if price == nil || price.invalid {
		return false
	}
	return price.Input >= 0 && price.Output >= 0 &&
		!math.IsNaN(price.Input) && !math.IsNaN(price.Output) &&
		!math.IsInf(price.Input, 0) && !math.IsInf(price.Output, 0)
}

func applyOfficialPriceBackfills(out costs.ModelsDevFile, providerToKey map[string]string, allow map[string]bool) {
	for llamboName, devKey := range providerToKey {
		if allow != nil && !allow[strings.ToLower(llamboName)] {
			continue
		}
		if devKey != "google" {
			continue
		}
		for modelID, price := range officialGeminiPrices {
			key := costs.Key(llamboName, modelID)
			if _, ok := out[key]; !ok {
				out[key] = price
			}
		}
	}
}

var officialGeminiPrices = map[string]costs.ModelsDevPrice{
	// Gemini Developer API standard paid tier, text/image/video token pricing.
	"gemini-3.1-flash-lite": {InputPer1M: 0.25, OutputPer1M: 1.50},
}

func (m *modelCost) UnmarshalJSON(data []byte) error {
	var raw struct {
		Input  *float64 `json:"input"`
		Output *float64 `json:"output"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.invalid = raw.Input == nil || raw.Output == nil
	if raw.Input != nil {
		m.Input = *raw.Input
	}
	if raw.Output != nil {
		m.Output = *raw.Output
	}
	return nil
}
