package catalog

import (
	"reflect"
	"time"
)

// mergeRefresh folds one provider's upstream models into the catalog and
// returns the per-provider result.
func mergeRefresh(cat *Catalog, fetched refreshFetch, now time.Time) RefreshResult {
	result := fetched.result
	pc := ensureProviderCatalog(cat, result.Provider)

	upstreamIDs := make(map[string]struct{}, len(fetched.upstream))
	for _, m := range fetched.upstream {
		upstreamIDs[m.ID] = struct{}{}
		mergeUpstreamModel(pc, &result, m, now)
	}

	// Stale: in catalog but not seen this refresh.
	result.StaleIDs = staleCatalogIDs(pc, upstreamIDs)

	pc.LastRefresh = now
	pc.EndpointUsed = result.Endpoint

	return result
}

// mergeUpstreamModel records a new observation or applies upstream changes to
// the existing entry, updating the result counters.
func mergeUpstreamModel(pc *ProviderCatalog, result *RefreshResult, m UpstreamModel, now time.Time) {
	entry, exists := pc.Models[m.ID]
	if !exists {
		entry = ensureModelEntry(pc, m.ID, now)
		entry.UpstreamCreated = m.UpstreamCreated
		entry.OwnedBy = m.OwnedBy
		entry.Metadata = m.Metadata
		result.NewIDs = append(result.NewIDs, m.ID)
		return
	}

	entry.LastSeen = now
	if applyUpstreamChanges(entry, m) {
		result.UpdatedN++
		return
	}
	result.UnchangedN++
}

// applyUpstreamChanges copies non-empty upstream fields onto an existing entry,
// reporting whether anything changed.
func applyUpstreamChanges(entry *ModelEntry, m UpstreamModel) bool {
	updated := false
	if !m.UpstreamCreated.IsZero() && !m.UpstreamCreated.Equal(entry.UpstreamCreated) {
		entry.UpstreamCreated = m.UpstreamCreated
		updated = true
	}
	if m.OwnedBy != "" && m.OwnedBy != entry.OwnedBy {
		entry.OwnedBy = m.OwnedBy
		updated = true
	}
	if hasModelMetadata(m.Metadata) && updateModelMetadata(&entry.Metadata, m.Metadata) {
		updated = true
	}
	return updated
}

// staleCatalogIDs lists catalog models absent from the upstream response.
func staleCatalogIDs(pc *ProviderCatalog, upstreamIDs map[string]struct{}) []string {
	var stale []string
	for id := range pc.Models {
		if _, seen := upstreamIDs[id]; !seen {
			stale = append(stale, id)
		}
	}
	return stale
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

// updateModelMetadata merges upstream metadata onto dst, reporting whether any
// field changed. Empty or zero upstream fields leave existing values untouched;
// list, map, and architecture fields are replaced wholesale when provided.
func updateModelMetadata(dst *ModelMetadata, src ModelMetadata) bool {
	before := *dst
	updateScalarMetadata(dst, src)
	updateListMetadata(dst, src)
	updateCompositeMetadata(dst, src)
	return !reflect.DeepEqual(before, *dst)
}

// updateScalarMetadata overwrites scalar fields when upstream supplies a
// different non-empty value. Field order and empty-value precedence are part
// of the merge contract.
func updateScalarMetadata(dst *ModelMetadata, src ModelMetadata) {
	updateIdentityMetadata(dst, src)
	updateCapacityMetadata(dst, src)
	updateEconomicMetadata(dst, src)
}

// updateIdentityMetadata merges the display-identity scalars.
func updateIdentityMetadata(dst *ModelMetadata, src ModelMetadata) {
	if src.Name != "" && src.Name != dst.Name {
		dst.Name = src.Name
	}
	if src.CanonicalSlug != "" && src.CanonicalSlug != dst.CanonicalSlug {
		dst.CanonicalSlug = src.CanonicalSlug
	}
	if src.Description != "" && src.Description != dst.Description {
		dst.Description = src.Description
	}
}

// updateCapacityMetadata merges the context-window and token-limit scalars.
func updateCapacityMetadata(dst *ModelMetadata, src ModelMetadata) {
	if src.ContextLength > 0 && src.ContextLength != dst.ContextLength {
		dst.ContextLength = src.ContextLength
	}
	if src.InputTokenLimit > 0 && src.InputTokenLimit != dst.InputTokenLimit {
		dst.InputTokenLimit = src.InputTokenLimit
	}
	if src.OutputTokenLimit > 0 && src.OutputTokenLimit != dst.OutputTokenLimit {
		dst.OutputTokenLimit = src.OutputTokenLimit
	}
}

// updateEconomicMetadata merges the pricing and top-provider blocks.
func updateEconomicMetadata(dst *ModelMetadata, src ModelMetadata) {
	if src.Pricing != (ModelPricing{}) && src.Pricing != dst.Pricing {
		dst.Pricing = src.Pricing
	}
	if src.TopProvider != (ModelTopProvider{}) && src.TopProvider != dst.TopProvider {
		dst.TopProvider = src.TopProvider
	}
}

// updateListMetadata replaces list and map fields when upstream provides them.
func updateListMetadata(dst *ModelMetadata, src ModelMetadata) {
	if len(src.SupportedParameters) > 0 {
		dst.SupportedParameters = src.SupportedParameters
	}
	if len(src.SupportedGenerationMethods) > 0 {
		dst.SupportedGenerationMethods = src.SupportedGenerationMethods
	}
	if len(src.DefaultParameters) > 0 {
		dst.DefaultParameters = src.DefaultParameters
	}
	if len(src.Reasoning) > 0 {
		dst.Reasoning = src.Reasoning
	}
	if len(src.Benchmarks) > 0 {
		dst.Benchmarks = src.Benchmarks
	}
}

// updateCompositeMetadata replaces the architecture when upstream supplies one.
func updateCompositeMetadata(dst *ModelMetadata, src ModelMetadata) {
	if hasModelArchitecture(src.Architecture) {
		dst.Architecture = src.Architecture
	}
}
