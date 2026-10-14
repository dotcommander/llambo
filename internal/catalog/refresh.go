package catalog

import (
	"context"
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

	resultsMap := make(map[string]RefreshResult, len(targets))
	toFetch := pendingRefreshTargets(cat, targets, opts, now, resultsMap)
	if len(toFetch) == 0 {
		return assembleResults(targets, resultsMap), nil
	}

	fetched := fetchProviders(ctx, toFetch)
	for _, f := range fetched {
		resultsMap[f.result.Provider] = f.result
	}

	if hasFetchSuccess(fetched) {
		if err := mergeFetchedRefreshes(ctx, catalogPath, fetched, resultsMap, now); err != nil {
			return assembleResults(targets, resultsMap), err
		}
	}

	return assembleResults(targets, resultsMap), nil
}

// pendingRefreshTargets records TTL-cache hits into resultsMap and returns the
// entries that still need an upstream fetch.
func pendingRefreshTargets(cat *Catalog, targets []providers.ProviderEntry, opts RefreshOptions, now time.Time, resultsMap map[string]RefreshResult) []providers.ProviderEntry {
	var toFetch []providers.ProviderEntry
	for _, entry := range targets {
		if result, cached := cachedRefreshResult(cat, entry.Name, opts, now); cached {
			resultsMap[entry.Name] = result
			continue
		}
		toFetch = append(toFetch, entry)
	}
	return toFetch
}

// cachedRefreshResult serves a provider refreshed within opts.TTL, unless
// opts.Force bypasses the cache. The served result counts models seen since
// that refresh as current.
func cachedRefreshResult(cat *Catalog, name string, opts RefreshOptions, now time.Time) (RefreshResult, bool) {
	if opts.Force || opts.TTL <= 0 {
		return RefreshResult{}, false
	}
	pc := cat.Providers[name]
	if pc == nil || pc.LastRefresh.IsZero() || pc.LastRefresh.After(now) || now.Sub(pc.LastRefresh) >= opts.TTL {
		return RefreshResult{}, false
	}
	current := 0
	for _, model := range pc.Models {
		if model != nil && !model.LastSeen.Before(pc.LastRefresh) {
			current++
		}
	}
	return RefreshResult{
		Provider:    name,
		Endpoint:    pc.EndpointUsed,
		Total:       current,
		UnchangedN:  current,
		Cached:      true,
		LastRefresh: pc.LastRefresh,
	}, true
}

// fetchProviders fetches the pending providers concurrently, preserving every
// per-provider result including errors.
func fetchProviders(ctx context.Context, toFetch []providers.ProviderEntry) []refreshFetch {
	p := pool.NewWithResults[refreshFetch]().WithMaxGoroutines(8).WithContext(ctx)
	for _, entry := range toFetch {
		name := entry.Name
		cfg := entry.Config
		p.Go(func(ctx context.Context) (refreshFetch, error) {
			return fetchRefresh(ctx, name, cfg), nil
		})
	}
	fetched, _ := p.Wait()
	return fetched
}

// hasFetchSuccess reports whether any provider fetched without error.
func hasFetchSuccess(fetched []refreshFetch) bool {
	for _, f := range fetched {
		if f.result.Err == nil {
			return true
		}
	}
	return false
}

// mergeFetchedRefreshes persists successful fetches and mirrors the merged
// results into resultsMap; per-provider failures keep their fetch-time result.
func mergeFetchedRefreshes(ctx context.Context, catalogPath string, fetched []refreshFetch, resultsMap map[string]RefreshResult, now time.Time) error {
	return Update(ctx, catalogPath, func(cat *Catalog) error {
		for i := range fetched {
			if fetched[i].result.Err != nil {
				continue
			}
			resultsMap[fetched[i].result.Provider] = mergeRefresh(cat, fetched[i], now)
		}
		return nil
	})
}

// assembleResults orders results to match the target list.
func assembleResults(targets []providers.ProviderEntry, resultsMap map[string]RefreshResult) []RefreshResult {
	results := make([]RefreshResult, len(targets))
	for i, entry := range targets {
		results[i] = resultsMap[entry.Name]
	}
	return results
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
