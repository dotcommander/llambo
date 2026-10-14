package modelsdev

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/internal/filetxn"
)

// SyncOptions configures a pricing sync run.
type SyncOptions struct {
	ProviderToKey map[string]string // llambo provider name -> models.dev top-level key
	Filter        []string          // optional: only these llambo providers
	URL           string            // defaults to DefaultURL when empty
	OutPath       string            // normalized pricing file to write
	DryRun        bool              // compute counts but do not write
}

// SyncResult summarizes a sync run.
type SyncResult struct {
	Providers int // distinct llambo providers in the new file
	Models    int // total priced models in the new file
	Added     int // keys new vs prior
	Changed   int // keys whose price changed
	Removed   int // keys present in prior but absent now
}

// RunSync fetches models.dev, normalizes pricing for the configured providers,
// diffs against any existing OutPath file, and (unless DryRun) writes OutPath
// atomically. It performs no printing.
func RunSync(ctx context.Context, opts SyncOptions) (*SyncResult, costs.ModelsDevFile, error) {
	url := opts.URL
	if url == "" {
		url = DefaultURL
	}
	data, err := Fetch(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	api, err := Parse(data)
	if err != nil {
		return nil, nil, err
	}
	filter := make([]string, 0, len(opts.Filter))
	for _, provider := range opts.Filter {
		filter = append(filter, strings.ToLower(strings.TrimSpace(provider)))
	}
	var res *SyncResult
	var next costs.ModelsDevFile
	apply := func(path string) error {
		prior := loadPrior(path)
		next = Normalize(api, opts.ProviderToKey, filter)
		if len(filter) > 0 {
			next = mergeUnfilteredPrior(next, prior, filter)
		}

		res = pricingChanges(next, prior)

		if opts.DryRun {
			return nil
		}
		if err := ctx.Err(); err != nil { //nolint:govet // apply closure must not race the outer err
			return err
		}
		return writeAtomic(path, next)
	}
	if opts.DryRun {
		err = apply(opts.OutPath)
	} else {
		err = filetxn.WithLock(ctx, opts.OutPath, apply)
	}
	if err != nil {
		return nil, nil, err
	}
	return res, next, nil
}

func pricingChanges(next, prior costs.ModelsDevFile) *SyncResult {
	result := &SyncResult{Models: len(next)}
	providers := map[string]bool{}
	for key, price := range next {
		if i := indexColon(key); i >= 0 {
			providers[key[:i]] = true
		}
		previous, exists := prior[key]
		switch {
		case !exists:
			result.Added++
		case previous.InputPer1M != price.InputPer1M || previous.OutputPer1M != price.OutputPer1M:
			result.Changed++
		}
	}
	for key := range prior {
		if _, exists := next[key]; !exists {
			result.Removed++
		}
	}
	result.Providers = len(providers)
	return result
}

func mergeUnfilteredPrior(next, prior costs.ModelsDevFile, filter []string) costs.ModelsDevFile {
	if len(prior) == 0 {
		return next
	}
	allowed := make(map[string]bool, len(filter))
	for _, provider := range filter {
		allowed[strings.ToLower(strings.TrimSpace(provider))] = true
	}
	for key, price := range prior {
		provider := key
		if i := indexColon(key); i >= 0 {
			provider = key[:i]
		}
		if !allowed[strings.ToLower(provider)] {
			next[key] = price
		}
	}
	return next
}

func indexColon(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}

func loadPrior(path string) costs.ModelsDevFile {
	if path == "" {
		return costs.ModelsDevFile{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return costs.ModelsDevFile{}
	}
	var f costs.ModelsDevFile
	if json.Unmarshal(b, &f) != nil {
		return costs.ModelsDevFile{}
	}
	return f
}

func writeAtomic(path string, f costs.ModelsDevFile) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return filetxn.WriteAtomic(path, b, 0o644)
}
