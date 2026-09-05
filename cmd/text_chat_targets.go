package cmd

import (
	"fmt"
	"io"

	"github.com/dotcommander/llambo/internal/catalog"
)

func filterTextChatTargets[T any](errOut io.Writer, cat *catalog.Catalog, items []T, identity func(T) (string, string)) ([]T, int) {
	filtered := items[:0]
	skipped := 0
	for _, item := range items {
		provider, model := identity(item)
		if ok, reason := catalog.TextChatCapability(provider, model, modelEntryForTarget(cat, provider, model)); !ok {
			skipped++
			fmt.Fprintf(errOut, "Chat filter: skipped %s/%s (%s)\n", provider, model, reason)
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, skipped
}

func writeTextChatSkippedSummary(errOut io.Writer, skipped int) {
	if skipped > 0 {
		fmt.Fprintf(errOut, "Chat filter: skipped %d non-text chat target(s)\n", skipped)
	}
}
