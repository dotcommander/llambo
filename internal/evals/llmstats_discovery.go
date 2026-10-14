package evals

func projectLLMStatsDiscovery(identities []llmIdentity, full []llmFull, indexes map[string]indexEnvelope) []Model {
	fullByID := make(map[string]llmFull, len(full))
	for _, row := range full {
		fullByID[row.ModelID] = row
	}
	indexByID := map[string]map[string]Index{}
	for category, envelope := range indexes {
		for _, row := range envelope.Models {
			if indexByID[row.ModelID] == nil {
				indexByID[row.ModelID] = map[string]Index{}
			}
			indexByID[row.ModelID][category] = Index{Conservative: row.Conservative, Mu: row.Mu, Sigma: row.Sigma, Rank: row.Rank, GamesPlayed: row.GamesPlayed}
		}
	}
	models := make([]Model, 0, len(identities))
	for _, identity := range identities {
		if identity.ModelType != llm1 {
			continue
		}
		f := fullByID[identity.ModelID]
		context := identity.Context
		if context == nil {
			context = f.Context
		}
		input, output := identity.InputPrice, identity.OutputPrice
		if input == nil {
			input = f.InputPrice
		}
		if output == nil {
			output = f.OutputPrice
		}
		models = append(models, Model{Key: identity.ModelID, Name: identity.Name, Organization: identity.Organization, License: identity.License, Open: boolPtr(identity.IsOpen), Context: context,
			LLMStats: &LLMStatsMetrics{InputPrice: input, OutputPrice: output, Throughput: f.Throughput, Latency: f.Latency, GPQA: f.GPQA, SWEVerified: f.SWEVerified, SWEPro: f.SWEPro, SciCode: f.SciCode, MCPAtlas: f.MCPAtlas, Indexes: indexByID[identity.ModelID]}})
	}
	return models
}
