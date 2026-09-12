package catalog

import (
	"context"
	"reflect"
	"time"

	"github.com/dotcommander/llambo/providers"
	"github.com/sourcegraph/conc/pool"
)

// RefreshOptions configures catalog refresh caching behavior.
type RefreshOptions struct {
	TTL   time.Duration // Minimum duration to retain cached provider models. If > 0, skips providers refreshed within TTL.
	Force bool          // If true, bypasses the TTL cache and forces upstream fetch.
}

// RefreshResult summarises what happened for one provider during a refresh.
type RefreshResult struct {
	Provider    string
	Endpoint    string
	Total       int
	NewIDs      []string // first_seen == this refresh
	UpdatedN    int      // existing models whose version/metadata was updated
	UnchangedN  int
	StaleIDs    []string // in catalog but not returned by upstream this refresh
	Cached      bool     // true if result was served from local cache within TTL
	LastRefresh time.Time
	Err         error
}

// Refresh fetches models for the named providers (all enabled if names is nil) and updates
// catalog.json in place. Default behavior performs an unconditional fetch for backwards compatibility.
func Refresh(ctx context.Context, names []string, cfgs map[string]providers.Config, catalogPath string) ([]RefreshResult, error) {
	return RefreshWithOptions(ctx, names, cfgs, catalogPath, RefreshOptions{})
}

// RefreshWithOptions fetches or returns cached models according to RefreshOptions.
// Providers refreshed within opts.TTL are served from cache unless opts.Force is true.
func RefreshWithOptions(ctx context.Context, names []string, cfgs map[string]providers.Config, catalogPath string, opts RefreshOptions) ([]RefreshResult, error) {
	cat, err := Load(catalogPath)
	if err != nil {
		return nil, err
	}

	targets := buildTargetList(names, cfgs)
	now := time.Now().UTC()

	var toFetch []providers.ProviderEntry
	resultsMap := make(map[string]RefreshResult, len(targets))

	for _, entry := range targets {
		if !opts.Force && opts.TTL > 0 {
			if pc := cat.Providers[entry.Name]; pc != nil && !pc.LastRefresh.IsZero() && !pc.LastRefresh.After(now) && now.Sub(pc.LastRefresh) < opts.TTL {
				current := 0
				for _, model := range pc.Models {
					if model != nil && !model.LastSeen.Before(pc.LastRefresh) {
						current++
					}
				}
				resultsMap[entry.Name] = RefreshResult{
					Provider:    entry.Name,
					Endpoint:    pc.EndpointUsed,
					Total:       current,
					UnchangedN:  current,
					Cached:      true,
					LastRefresh: pc.LastRefresh,
				}
				continue
			}
		}
		toFetch = append(toFetch, entry)
	}

	if len(toFetch) == 0 {
		results := make([]RefreshResult, len(targets))
		for i, entry := range targets {
			results[i] = resultsMap[entry.Name]
		}
		return results, nil
	}

	p := pool.NewWithResults[refreshFetch]().WithMaxGoroutines(8).WithContext(ctx)
	for _, entry := range toFetch {
		name := entry.Name
		cfg := entry.Config
		p.Go(func(ctx context.Context) (refreshFetch, error) {
			return fetchRefresh(ctx, name, cfg), nil
		})
	}

	fetched, _ := p.Wait()
	for _, f := range fetched {
		resultsMap[f.result.Provider] = f.result
	}

	hasSuccess := false
	for _, f := range fetched {
		if f.result.Err == nil {
			hasSuccess = true
			break
		}
	}

	if hasSuccess {
		if err := Update(ctx, catalogPath, func(cat *Catalog) error {
			for i := range fetched {
				if fetched[i].result.Err != nil {
					continue
				}
				res := mergeRefresh(cat, fetched[i], now)
				resultsMap[fetched[i].result.Provider] = res
			}
			return nil
		}); err != nil {
			results := make([]RefreshResult, len(targets))
			for i, entry := range targets {
				results[i] = resultsMap[entry.Name]
			}
			return results, err
		}
	}

	results := make([]RefreshResult, len(targets))
	for i, entry := range targets {
		results[i] = resultsMap[entry.Name]
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
			updated := false
			if !m.UpstreamCreated.IsZero() && !m.UpstreamCreated.Equal(entry.UpstreamCreated) {
				entry.UpstreamCreated = m.UpstreamCreated
				updated = true
			}
			if m.OwnedBy != "" && m.OwnedBy != entry.OwnedBy {
				entry.OwnedBy = m.OwnedBy
				updated = true
			}
			if hasModelMetadata(m.Metadata) {
				if updateModelMetadata(&entry.Metadata, m.Metadata) {
					updated = true
				}
			}
			if updated {
				result.UpdatedN++
			} else {
				result.UnchangedN++
			}
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
		m.Description != "" ||
		m.ContextLength > 0 ||
		m.InputTokenLimit > 0 ||
		m.OutputTokenLimit > 0 ||
		len(m.SupportedGenerationMethods) > 0 ||
		len(m.SupportedParameters) > 0 ||
		len(m.DefaultParameters) > 0 ||
		m.Pricing != (ModelPricing{}) ||
		hasModelArchitecture(m.Architecture) ||
		m.TopProvider != (ModelTopProvider{}) ||
		len(m.Reasoning) > 0 ||
		len(m.Benchmarks) > 0
}

func hasModelArchitecture(a ModelArchitecture) bool {
	return a.Modality != "" ||
		len(a.InputModalities) > 0 ||
		len(a.OutputModalities) > 0 ||
		a.Tokenizer != "" ||
		a.InstructType != ""
}

func updateModelMetadata(dst *ModelMetadata, src ModelMetadata) bool {
	before := *dst
	if src.Name != "" && src.Name != dst.Name {
		dst.Name = src.Name
	}
	if src.CanonicalSlug != "" && src.CanonicalSlug != dst.CanonicalSlug {
		dst.CanonicalSlug = src.CanonicalSlug
	}
	if src.Description != "" && src.Description != dst.Description {
		dst.Description = src.Description
	}
	if src.ContextLength > 0 && src.ContextLength != dst.ContextLength {
		dst.ContextLength = src.ContextLength
	}
	if src.InputTokenLimit > 0 && src.InputTokenLimit != dst.InputTokenLimit {
		dst.InputTokenLimit = src.InputTokenLimit
	}
	if src.OutputTokenLimit > 0 && src.OutputTokenLimit != dst.OutputTokenLimit {
		dst.OutputTokenLimit = src.OutputTokenLimit
	}
	if len(src.SupportedParameters) > 0 {
		dst.SupportedParameters = src.SupportedParameters
	}
	if len(src.SupportedGenerationMethods) > 0 {
		dst.SupportedGenerationMethods = src.SupportedGenerationMethods
	}
	if len(src.DefaultParameters) > 0 {
		dst.DefaultParameters = src.DefaultParameters
	}
	if src.Pricing != (ModelPricing{}) && src.Pricing != dst.Pricing {
		dst.Pricing = src.Pricing
	}
	if hasModelArchitecture(src.Architecture) {
		dst.Architecture = src.Architecture
	}
	if src.TopProvider != (ModelTopProvider{}) && src.TopProvider != dst.TopProvider {
		dst.TopProvider = src.TopProvider
	}
	if len(src.Reasoning) > 0 {
		dst.Reasoning = src.Reasoning
	}
	if len(src.Benchmarks) > 0 {
		dst.Benchmarks = src.Benchmarks
	}
	return !reflect.DeepEqual(before, *dst)
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
