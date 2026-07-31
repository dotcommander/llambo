package catalog

import (
	"context"
	"sync"
	"time"

	"github.com/dotcommander/llambo/providers"
	"github.com/sourcegraph/conc/pool"
)

// RefreshResult summarises what happened for one provider during a refresh.
type RefreshResult struct {
	Provider   string
	Endpoint   string
	Total      int
	NewIDs     []string // first_seen == this refresh
	UnchangedN int
	StaleIDs   []string // in catalog but not returned by upstream this refresh
	Err        error
}

// Refresh fetches models for the named providers (all enabled if names is nil) and updates
// catalog.json in place. Returns per-provider results. A fetch error for one provider does NOT
// lose other providers' data.
func Refresh(ctx context.Context, names []string, cfgs map[string]providers.Config, catalogPath string) ([]RefreshResult, error) {
	cat, err := Load(catalogPath)
	if err != nil {
		return nil, err
	}

	// Build provider list to refresh.
	targets := buildTargetList(names, cfgs)

	var mu sync.Mutex
	now := time.Now().UTC()

	p := pool.NewWithResults[RefreshResult]().WithMaxGoroutines(8).WithContext(ctx)
	for _, entry := range targets {
		name := entry.Name
		cfg := entry.Config
		p.Go(func(ctx context.Context) (RefreshResult, error) {
			result := refreshOne(ctx, name, cfg, cat, now, &mu)
			// Always return result — errors are surfaced via result.Err, not pool error.
			return result, nil
		})
	}

	results, _ := p.Wait()

	// Save once after all goroutines finish.
	if saveErr := Save(catalogPath, cat); saveErr != nil {
		return results, saveErr
	}
	return results, nil
}

// refreshOne fetches and merges one provider's model list into the shared catalog.
// Mutations to cat are protected by mu.
func refreshOne(ctx context.Context, name string, cfg providers.Config, cat *Catalog, now time.Time, mu *sync.Mutex) RefreshResult {
	result := RefreshResult{Provider: name}

	upstream, endpoint, err := FetchForProvider(ctx, name, cfg)
	result.Endpoint = endpoint
	if err != nil {
		result.Err = err
		return result
	}

	result.Total = len(upstream)
	upstreamIDs := make(map[string]struct{}, len(upstream))
	for _, m := range upstream {
		upstreamIDs[m.ID] = struct{}{}
	}

	mu.Lock()
	defer mu.Unlock()

	pc := cat.Providers[name]
	if pc == nil {
		pc = &ProviderCatalog{
			Models: make(map[string]*ModelEntry),
		}
		cat.Providers[name] = pc
	}
	if pc.Models == nil {
		pc.Models = make(map[string]*ModelEntry)
	}

	for _, m := range upstream {
		entry, exists := pc.Models[m.ID]
		if !exists {
			pc.Models[m.ID] = &ModelEntry{
				FirstSeen:       now,
				LastSeen:        now,
				UpstreamCreated: m.UpstreamCreated,
				OwnedBy:         m.OwnedBy,
				Metadata:        m.Metadata,
			}
			result.NewIDs = append(result.NewIDs, m.ID)
		} else {
			entry.LastSeen = now
			if !m.UpstreamCreated.IsZero() {
				entry.UpstreamCreated = m.UpstreamCreated
			}
			if m.OwnedBy != "" {
				entry.OwnedBy = m.OwnedBy
			}
			if hasModelMetadata(m.Metadata) {
				entry.Metadata = m.Metadata
			}
			result.UnchangedN++
		}
	}

	// Stale: in catalog but not seen this refresh.
	for id := range pc.Models {
		if _, seen := upstreamIDs[id]; !seen {
			result.StaleIDs = append(result.StaleIDs, id)
		}
	}

	pc.LastRefresh = now
	pc.EndpointUsed = result.Endpoint

	return result
}

func hasModelMetadata(m ModelMetadata) bool {
	return m.Name != "" ||
		m.CanonicalSlug != "" ||
		m.ContextLength > 0 ||
		len(m.SupportedParameters) > 0 ||
		len(m.DefaultParameters) > 0 ||
		m.Pricing != (ModelPricing{}) ||
		m.Architecture.Modality != "" ||
		len(m.Architecture.InputModalities) > 0 ||
		len(m.Architecture.OutputModalities) > 0 ||
		m.Architecture.Tokenizer != "" ||
		m.Architecture.InstructType != "" ||
		m.TopProvider != (ModelTopProvider{}) ||
		len(m.Reasoning) > 0 ||
		len(m.Benchmarks) > 0
}

// buildTargetList returns the providers to refresh.
// If names is empty, all enabled providers are used.
func buildTargetList(names []string, cfgs map[string]providers.Config) []providers.ProviderEntry {
	if len(names) == 0 {
		return providers.FilterEnabledProviders(cfgs)
	}
	var out []providers.ProviderEntry
	for _, name := range names {
		cfg, ok := cfgs[name]
		if ok {
			out = append(out, providers.ProviderEntry{Name: name, Config: cfg})
		}
	}
	return out
}
