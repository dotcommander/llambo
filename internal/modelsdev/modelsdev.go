package modelsdev

import (
	"encoding/json"

	"github.com/dotcommander/llambo/internal/costs"
)

// modelCost is the per-1M-token pricing block in api.json (USD per 1M tokens).
type modelCost struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
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
	var allow map[string]bool
	if len(filter) > 0 {
		allow = make(map[string]bool, len(filter))
		for _, f := range filter {
			allow[f] = true
		}
	}
	out := costs.ModelsDevFile{}
	for llamboName, devKey := range providerToKey {
		if allow != nil && !allow[llamboName] {
			continue
		}
		pm, ok := api[devKey]
		if !ok {
			continue
		}
		for id, m := range pm.Models {
			if m.Cost == nil {
				continue
			}
			out[costs.Key(llamboName, id)] = costs.ModelsDevPrice{InputPer1M: m.Cost.Input, OutputPer1M: m.Cost.Output}
		}
	}
	applyOfficialPriceBackfills(out, providerToKey, allow)
	return out
}

func applyOfficialPriceBackfills(out costs.ModelsDevFile, providerToKey map[string]string, allow map[string]bool) {
	for llamboName, devKey := range providerToKey {
		if allow != nil && !allow[llamboName] {
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
