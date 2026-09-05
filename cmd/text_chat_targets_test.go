package cmd

import (
	"bytes"
	"testing"

	"github.com/dotcommander/llambo/internal/catalog"
)

func TestFilterTextChatTargetsPreservesOrderAndReportsSkips(t *testing.T) {
	t.Parallel()
	type target struct{ provider, model string }
	items := []target{{"p", "chat-a"}, {"p", "embed"}, {"q", "chat-b"}}
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{
		"p": {Models: map[string]*catalog.ModelEntry{
			"chat-a": {Metadata: catalog.ModelMetadata{Architecture: catalog.ModelArchitecture{InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}},
			"embed":  {Metadata: catalog.ModelMetadata{Architecture: catalog.ModelArchitecture{InputModalities: []string{"text"}, OutputModalities: []string{"embedding"}}}},
		}},
	}}
	var errOut bytes.Buffer
	got, skipped := filterTextChatTargets(&errOut, cat, items, func(item target) (string, string) { return item.provider, item.model })
	writeTextChatSkippedSummary(&errOut, skipped)
	if len(got) != 2 || got[0].model != "chat-a" || got[1].model != "chat-b" {
		t.Fatalf("filtered targets = %#v", got)
	}
	want := "Chat filter: skipped p/embed (model does not produce text output)\nChat filter: skipped 1 non-text chat target(s)\n"
	if errOut.String() != want {
		t.Fatalf("diagnostics = %q, want %q", errOut.String(), want)
	}
}
