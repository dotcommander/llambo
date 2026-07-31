package providers

// contextHeadroomTokens is the completion reserve (in tokens) added to the
// estimated prompt size before checking it against a model's context window.
// We deliberately use a small fixed floor rather than the configured MaxTokens:
// gating on the full max-completion budget would falsely drop models for
// ordinary prompts (an 8k-context / 8k-max_tokens model would then reject any
// non-empty prompt). 512 tokens reserves minimal useful completion room and
// absorbs slack in the chars/4 token estimate, so the precheck fires only when
// the prompt itself is structurally too large for the window.
const contextHeadroomTokens = 512

// contextFits reports whether a model with the given context window (in tokens)
// can hold the estimated prompt plus a completion headroom reserve.
//
// A contextLength of 0 (or negative) means "unknown" and ALWAYS fits — the
// graceful-degradation path that mirrors CostWithinCap's includeUnknown
// behavior, so models without context metadata are never dropped and behavior
// is unchanged for the common no-data case.
func contextFits(contextLength, estimatedPromptTokens, headroom int) bool {
	if contextLength <= 0 {
		return true
	}
	return estimatedPromptTokens+headroom <= contextLength
}
