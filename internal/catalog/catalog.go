package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/dotcommander/llambo/providers"
)

// Catalog is the root catalog state stored at ~/.config/llambo/catalog.json.
// It is SEPARATE from config.json — refreshing the catalog never mutates config.json.
type Catalog struct {
	Version   int                         `json:"version"`
	Providers map[string]*ProviderCatalog `json:"providers"`
}

// ProviderCatalog holds the model catalog for one provider.
type ProviderCatalog struct {
	LastRefresh  time.Time              `json:"last_refresh"`
	EndpointUsed string                 `json:"endpoint_used"`
	Models       map[string]*ModelEntry `json:"models"`
}

// ModelEntry tracks a single upstream model across refreshes.
// Models that disappear upstream are NOT deleted — they just stop getting last_seen updates.
type ModelEntry struct {
	FirstSeen       time.Time                    `json:"first_seen"`
	LastSeen        time.Time                    `json:"last_seen"`
	UpstreamCreated time.Time                    `json:"upstream_created,omitzero"`
	OwnedBy         string                       `json:"owned_by,omitempty"`
	Metadata        ModelMetadata                `json:"metadata,omitzero"`
	Pinned          bool                         `json:"pinned,omitempty"`
	Avoid           bool                         `json:"avoid,omitempty"`
	AvoidReason     string                       `json:"avoid_reason,omitempty"`
	AvoidSince      time.Time                    `json:"avoid_since,omitzero"`
	Tags            []string                     `json:"tags,omitempty"`
	Quality         map[string]QualityEvidence   `json:"quality,omitempty"`
	Benchmarks      map[string]BenchmarkEvidence `json:"benchmarks,omitempty"`
	LastPing        PingState                    `json:"last_ping,omitzero"`
	QuarantineUntil time.Time                    `json:"quarantine_until,omitzero"`
	FailureCount    int                          `json:"failure_count,omitempty"`
}

// ensureProviderCatalog returns the provider catalog, creating its model map when
// the provider has not been observed yet. Callers own Catalog initialization.
func ensureProviderCatalog(cat *Catalog, name string) *ProviderCatalog {
	pc := cat.Providers[name]
	if pc == nil {
		pc = &ProviderCatalog{Models: make(map[string]*ModelEntry)}
		cat.Providers[name] = pc
		return pc
	}
	if pc.Models == nil {
		pc.Models = make(map[string]*ModelEntry)
	}
	return pc
}

// ensureModelEntry returns the model entry, recording its first observation when
// it has not been seen before.
func ensureModelEntry(pc *ProviderCatalog, id string, seenAt time.Time) *ModelEntry {
	entry := pc.Models[id]
	if entry == nil {
		entry = &ModelEntry{FirstSeen: seenAt, LastSeen: seenAt}
		pc.Models[id] = entry
	}
	return entry
}

// ModelMetadata stores optional provider-supplied model facts. It is additive
// catalog metadata; pin/avoid/tags remain the user-owned policy state.
type ModelMetadata struct {
	Name                       string            `json:"name,omitempty"`
	CanonicalSlug              string            `json:"canonical_slug,omitempty"`
	Description                string            `json:"description,omitempty"`
	ContextLength              int               `json:"context_length,omitempty"`
	InputTokenLimit            int               `json:"input_token_limit,omitempty"`
	OutputTokenLimit           int               `json:"output_token_limit,omitempty"`
	SupportedParameters        []string          `json:"supported_parameters,omitempty"`
	SupportedGenerationMethods []string          `json:"supported_generation_methods,omitempty"`
	DefaultParameters          map[string]any    `json:"default_parameters,omitempty"`
	Pricing                    ModelPricing      `json:"pricing,omitzero"`
	Architecture               ModelArchitecture `json:"architecture,omitzero"`
	TopProvider                ModelTopProvider  `json:"top_provider,omitzero"`
	Reasoning                  map[string]any    `json:"reasoning,omitempty"`
	Benchmarks                 map[string]any    `json:"benchmarks,omitempty"`
}

type ModelPricing struct {
	Prompt            string `json:"prompt,omitempty"`
	Completion        string `json:"completion,omitempty"`
	Image             string `json:"image,omitempty"`
	Audio             string `json:"audio,omitempty"`
	WebSearch         string `json:"web_search,omitempty"`
	InternalReasoning string `json:"internal_reasoning,omitempty"`
	InputCacheRead    string `json:"input_cache_read,omitempty"`
	InputCacheWrite   string `json:"input_cache_write,omitempty"`
}

type ModelArchitecture struct {
	Modality         string   `json:"modality,omitempty"`
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
	Tokenizer        string   `json:"tokenizer,omitempty"`
	InstructType     string   `json:"instruct_type,omitempty"`
}

type ModelTopProvider struct {
	ContextLength       int  `json:"context_length,omitempty"`
	MaxCompletionTokens int  `json:"max_completion_tokens,omitempty"`
	IsModerated         bool `json:"is_moderated,omitempty"`
}

type QualityEvidence struct {
	Score     float64   `json:"score"`
	Source    string    `json:"source,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
}

// BenchmarkEvidence stores measured benchmark transport metrics separately
// from LastPing, which is reserved for lightweight health checks.
type BenchmarkEvidence struct {
	Score                float64   `json:"score,omitempty"`
	LatencyMS            int64     `json:"latency_ms,omitempty"`
	TokensOut            int       `json:"tokens_out,omitempty"`
	SpeedTokensPerSecond float64   `json:"speed_tokens_per_second,omitempty"`
	Source               string    `json:"source,omitempty"`
	Notes                string    `json:"notes,omitempty"`
	UpdatedAt            time.Time `json:"updated_at,omitzero"`
}

// PingState stores the latest lightweight health check for a model.
type PingState struct {
	Success              bool      `json:"success"`
	LatencyMS            int64     `json:"latency_ms,omitempty"`
	TTFBMS               int64     `json:"ttfb_ms,omitempty"`
	GenerationMS         int64     `json:"generation_duration_ms,omitempty"`
	SpeedTokensPerSecond float64   `json:"speed_tokens_per_second,omitempty"`
	ErrorCategory        string    `json:"error_category,omitempty"`
	Error                string    `json:"error,omitempty"`
	TokensIn             int       `json:"tokens_in,omitempty"`
	TokensOut            int       `json:"tokens_out,omitempty"`
	CheckedAt            time.Time `json:"checked_at"`
}

// CatalogPath returns ~/.config/llambo/catalog.json (platform-equivalent via $HOME).
func CatalogPath() (string, error) {
	home := os.Getenv("HOME")
	if home == "" {
		return "", fmt.Errorf("HOME env var not set")
	}
	return filepath.Join(home, ".config", "llambo", "catalog.json"), nil
}

// Load reads catalog.json. A missing file returns an empty initialized Catalog (not an error).
// Bad JSON returns an error.
func Load(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Catalog{
				Version:   1,
				Providers: make(map[string]*ProviderCatalog),
			}, nil
		}
		return nil, fmt.Errorf("read catalog: %w", err)
	}

	var cat Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}

	if cat.Providers == nil {
		cat.Providers = make(map[string]*ProviderCatalog)
	}
	return &cat, nil
}

// PinnedModels returns pinned, non-avoided model IDs for enabled providers.
// The result is sorted for deterministic backend construction.
func PinnedModels(cat *Catalog, cfgs map[string]providers.Config) map[string][]string {
	if cat == nil || len(cat.Providers) == 0 || len(cfgs) == 0 {
		return nil
	}

	out := make(map[string][]string)
	for _, entry := range providers.FilterEnabledProviders(cfgs) {
		pc := cat.Providers[entry.Name]
		if pc == nil || len(pc.Models) == 0 {
			continue
		}
		for id, model := range pc.Models {
			if model == nil || !model.Pinned || model.Avoid {
				continue
			}
			out[entry.Name] = append(out[entry.Name], id)
		}
		if len(out[entry.Name]) == 0 {
			delete(out, entry.Name)
			continue
		}
		sort.Strings(out[entry.Name])
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
