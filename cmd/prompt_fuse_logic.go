package cmd

import (
	"fmt"
	"strings"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
)

func successfulPromptResults(results []PromptResult) []PromptResult {
	successful := make([]PromptResult, 0, len(results))
	for _, result := range results {
		if result.Error == nil && strings.TrimSpace(result.Response) != "" {
			successful = append(successful, result)
		}
	}
	return successful
}

func computeFusionConsensus(results []PromptResult) FusionConsensus {
	if len(results) == 0 {
		return FusionConsensus{}
	}
	if len(results) == 1 {
		return FusionConsensus{
			Label:     "single source",
			Score:     1,
			Agreement: 1,
			Note:      "Only one successful draft was available; this is not independent agreement.",
		}
	}

	var sum float64
	var pairs int
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			sum += draftAgreement(results[i].Response, results[j].Response)
			pairs++
		}
	}
	if pairs == 0 {
		return FusionConsensus{}
	}

	agreement := sum / float64(pairs)
	label := "contested"
	note := "Source drafts diverged; inspect the original responses before trusting the fused answer."
	switch {
	case agreement >= 0.55:
		label = "strong consensus"
		note = "Source drafts substantially agree; fusion is mostly compression and reconciliation."
	case agreement >= 0.25:
		label = "mixed consensus"
		note = "Source drafts overlap but preserve meaningful differences; fusion may be resolving trade-offs."
	}

	return FusionConsensus{
		Label:     label,
		Score:     agreement,
		Agreement: agreement,
		Note:      note,
	}
}

func draftAgreement(a, b string) float64 {
	aTokens := tokenSet(a)
	bTokens := tokenSet(b)
	if len(aTokens) == 0 && len(bTokens) == 0 {
		return 1
	}
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return 0
	}

	intersection := 0
	for token := range aTokens {
		if _, ok := bTokens[token]; ok {
			intersection++
		}
	}
	union := len(aTokens) + len(bTokens) - intersection
	if union == 0 {
		return 1
	}
	return float64(intersection) / float64(union)
}

func tokenSet(text string) map[string]struct{} {
	words := tokenizeWords(text)
	set := make(map[string]struct{}, len(words))
	for _, word := range words {
		set[word] = struct{}{}
	}
	return set
}

func promptTextForModel(promptText, systemPrompt string, index, total int, provider, model string) string {
	if !promptFuse {
		return promptText
	}
	return buildFusionDraftPrompt(promptText, systemPrompt, index, total, provider, model)
}

func resolveFirstFusionTarget(selector string) (catalog.ModelTarget, error) {
	selector = normalizedFusionSelector(selector)
	globalCfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return catalog.ModelTarget{}, err
	}
	costMap, err := costs.LoadAll()
	if err != nil {
		return catalog.ModelTarget{}, fmt.Errorf("load model costs: %w", err)
	}
	catPath, err := catalog.CatalogPath()
	if err != nil {
		return catalog.ModelTarget{}, err
	}
	cat, err := catalog.Load(catPath)
	if err != nil {
		return catalog.ModelTarget{}, fmt.Errorf("load catalog: %w", err)
	}
	targets, err := catalog.ResolveModels(cat, globalCfg.Providers, costMap, catalog.SelectorOptions{
		Selector:           selector,
		IncludeQuarantine:  promptIncludeQuarantine,
		Blocklist:          providers.NewBlocklist(globalCfg.Blocklist),
		MaxOutputCost:      globalCfg.MaxOutputCost,
		IncludeUnknownCost: true,
	})
	if err != nil {
		return catalog.ModelTarget{}, fmt.Errorf("resolve fusion model %q: %w", selector, err)
	}
	return targets[0], nil
}

func normalizedFusionSelector(selector string) string {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return defaultPromptFuseModels
	}
	return selector
}

func buildFusionPrompt(promptText, systemPrompt string, results []PromptResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Original user prompt:\n%s\n\n", strings.TrimSpace(promptText))
	if strings.TrimSpace(systemPrompt) != "" {
		fmt.Fprintf(&b, "Original system prompt:\n%s\n\n", strings.TrimSpace(systemPrompt))
	}
	b.WriteString("Task: merge these diverse drafts into one final answer for the original user.\n\n")
	b.WriteString("Before writing, do a private source audit:\n")
	b.WriteString("- For each response, identify its strongest unique contribution.\n")
	b.WriteString("- Identify any caveat, risk, failure mode, or implementation detail that would improve the final answer if preserved.\n")
	b.WriteString("- Identify contradictions or weak claims and decide what to keep, soften, or discard.\n")
	b.WriteString("- Do not output the audit; use it only to compose the final answer.\n\n")
	b.WriteString("Fusion rules:\n")
	b.WriteString("- Start from the strongest direct answer, not from an average of all drafts.\n")
	b.WriteString("- Write as the final expert answer, not as a report about the drafts.\n")
	b.WriteString("- Keep only useful novelty: concrete details, better framing, edge cases, trade-offs, or creative options that improve the answer.\n")
	b.WriteString("- Preserve at least one high-value caveat, trade-off, or implementation detail from a non-primary draft when it materially improves the answer.\n")
	b.WriteString("- Prefer one coherent strategy over an exhaustive catalog when the user asks for a strategy, recommendation, or plan.\n")
	b.WriteString("- Resolve contradictions by choosing the best-supported claim or stating uncertainty briefly.\n")
	b.WriteString("- Remove repetition, filler, hidden chain-of-thought, and draft scaffolding.\n")
	b.WriteString("- Obey the original user's requested format and length in the final answer.\n")
	b.WriteString("- Do not mention models, drafts, or the fusion process unless the user asked for it.\n\n")
	for i, result := range results {
		fmt.Fprintf(&b, "Response %d from %s/%s:\n%s\n\n", i+1, result.Provider, result.Model, strings.TrimSpace(result.Response))
	}
	return strings.TrimSpace(b.String())
}

func buildFusionDraftPrompt(promptText, systemPrompt string, index, total int, provider, model string) string {
	lens := fusionDraftLenses[lensIndex(index)]
	if total <= 0 {
		total = 1
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Original user prompt:\n%s\n\n", strings.TrimSpace(promptText))
	if strings.TrimSpace(systemPrompt) != "" {
		fmt.Fprintf(&b, "Original system prompt:\n%s\n\n", strings.TrimSpace(systemPrompt))
	}
	fmt.Fprintf(&b, "You are draft contributor %d of %d in a merge-fuse workflow.\n", index+1, total)
	fmt.Fprintf(&b, "Assigned lens: %s.\n", lens)
	if strings.TrimSpace(provider) != "" || strings.TrimSpace(model) != "" {
		fmt.Fprintf(&b, "This draft source: %s/%s.\n", strings.TrimSpace(provider), strings.TrimSpace(model))
	}
	b.WriteString("\nYour job is to add distinct value for a later fusion model, not to converge on the obvious shortest answer.\n\n")
	b.WriteString("Draft rules:\n")
	b.WriteString("- Answer the original prompt, but optimize for useful diversity and high-signal raw material.\n")
	b.WriteString("- Include the direct answer plus non-obvious angles, alternatives, caveats, examples, or implementation details that fit your assigned lens.\n")
	b.WriteString("- Aim for 4-8 dense bullets or 2-4 short sections unless the original task requires a different shape.\n")
	b.WriteString("- Do not pad. Do not copy generic consensus wording. Prefer specific claims the fusion model can keep, reject, or combine.\n")
	b.WriteString("- If the original prompt asks for a very short answer, still provide a richer draft; the fusion model will compress the final answer.\n")
	b.WriteString("- Keep private reasoning hidden; output only the draft content.\n")
	return strings.TrimSpace(b.String())
}

func lensIndex(index int) int {
	if len(fusionDraftLenses) == 0 {
		return 0
	}
	if index < 0 {
		index = -index
	}
	return index % len(fusionDraftLenses)
}
