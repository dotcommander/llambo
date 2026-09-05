package catalog

import (
	"context"
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
	if _, err := Load(catalogPath); err != nil {
		return nil, err
	}

	// Build provider list to refresh.
	targets := buildTargetList(names, cfgs)
	now := time.Now().UTC()

	p := pool.NewWithResults[refreshFetch]().WithMaxGoroutines(8).WithContext(ctx)
	for _, entry := range targets {
		name := entry.Name
		cfg := entry.Config
		p.Go(func(ctx context.Context) (refreshFetch, error) {
			return fetchRefresh(ctx, name, cfg), nil
		})
	}

	fetched, _ := p.Wait()
	results := make([]RefreshResult, len(fetched))
	for i := range fetched {
		results[i] = fetched[i].result
	}
	if err := Update(ctx, catalogPath, func(cat *Catalog) error {
		for i := range fetched {
			if fetched[i].result.Err != nil {
				continue
			}
			results[i] = mergeRefresh(cat, fetched[i], now)
		}
		return nil
	}); err != nil {
		return results, err
	}
	return results, nil
}

type refreshFetch struct {
	result   RefreshResult
	upstream []UpstreamModel
}

func fetchRefresh(ctx context.Context, name string, cfg providers.Config) refreshFetch {
	fetched := refreshFetch{result: RefreshResult{Provider: name}}
	upstream, endpoint, err := FetchForProvider(ctx, name, cfg)
	fetched.result.Endpoint = endpoint
	if err != nil {
		fetched.result.Err = err
		return fetched
	}
	fetched.upstream = upstream
	fetched.result.Total = len(upstream)
	return fetched
}

func mergeRefresh(cat *Catalog, fetched refreshFetch, now time.Time) RefreshResult {
	result := fetched.result
	upstream := fetched.upstream
	upstreamIDs := make(map[string]struct{}, len(upstream))
	for _, m := range upstream {
		upstreamIDs[m.ID] = struct{}{}
	}

	pc := ensureProviderCatalog(cat, result.Provider)

	for _, m := range upstream {
		entry, exists := pc.Models[m.ID]
		if !exists {
			entry = ensureModelEntry(pc, m.ID, now)
			entry.UpstreamCreated = m.UpstreamCreated
			entry.OwnedBy = m.OwnedBy
			entry.Metadata = m.Metadata
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
