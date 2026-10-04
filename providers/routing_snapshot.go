package providers

import "maps"

// routingSnapshot is published as a single immutable generation. Provider
// resources deliberately remain on OpenAIClients across routing publication.
type routingSnapshot struct {
	configs map[string]Config
	routing RoutingConfig
}

func (oc *OpenAIClients) routingSnapshot(fallback map[string]Config) (map[string]Config, RoutingConfig) {
	oc.routingMu.Lock()
	defer oc.routingMu.Unlock()
	if oc.routingState == nil {
		configs := oc.configs
		if configs == nil {
			configs = fallback
		}
		oc.routingState = &routingSnapshot{configs: cloneProviderConfigs(configs), routing: cloneRoutingConfig(oc.RoutingConfig)}
	}
	return oc.routingState.configs, oc.routingState.routing
}

func cloneProviderConfigs(configs map[string]Config) map[string]Config {
	result := make(map[string]Config, len(configs))
	for name, cfg := range configs {
		cfg.APIKeys = append([]string(nil), cfg.APIKeys...)
		cfg.Models = append([]string(nil), cfg.Models...)
		cfg.Capabilities = append([]string(nil), cfg.Capabilities...)
		cfg.ExtraHeaders = maps.Clone(cfg.ExtraHeaders)
		cfg.ExtraBody = cloneConfigObject(cfg.ExtraBody)
		if cfg.ExtraBodyByModel != nil {
			nested := make(map[string]map[string]any, len(cfg.ExtraBodyByModel))
			for model, body := range cfg.ExtraBodyByModel {
				nested[model] = cloneConfigObject(body)
			}
			cfg.ExtraBodyByModel = nested
		}
		result[name] = cfg
	}
	return result
}

func cloneConfigObject(object map[string]any) map[string]any {
	if object == nil {
		return nil
	}
	result := make(map[string]any, len(object))
	for key, value := range object {
		result[key] = cloneConfigValue(value)
	}
	return result
}

func cloneConfigValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneConfigObject(value)
	case []any:
		cloned := make([]any, len(value))
		for i, v := range value {
			cloned[i] = cloneConfigValue(v)
		}
		return cloned
	case []string:
		return append([]string(nil), value...)
	case map[string]string:
		return maps.Clone(value)
	default:
		return value
	}
}

func cloneRoutingConfig(r RoutingConfig) RoutingConfig {
	r.AllowedProviders = append([]string(nil), r.AllowedProviders...)
	r.DeniedProviders = append([]string(nil), r.DeniedProviders...)
	r.DailyMaxRequests = maps.Clone(r.DailyMaxRequests)
	r.DailyMaxTokens = maps.Clone(r.DailyMaxTokens)
	r.DailyMaxCostUSD = maps.Clone(r.DailyMaxCostUSD)
	if r.Canary != nil {
		canary := *r.Canary
		r.Canary = &canary
	}
	return r
}
