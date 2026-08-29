package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritingCampaignRejectsInheritedCatalogFlags(t *testing.T) {
	t.Parallel()
	io := &commandIO{ctx: context.Background(), changed: map[string]bool{"refresh": true}}
	err := (&evalsWritingCampaignCommand{}).Run(&evalsCommand{}, io)
	if err == nil || !strings.Contains(err.Error(), "--refresh is not supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestWritingCampaignRejectsModelOutsideRoster(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	roster := `{"schema_version":1,"locked":true,"assets":{"source_input":"source.md","system_prompt":"system.md","comparison_page":"writing.html","raw_output_directory":"results","future_multistage":{}},"pricing":{"maximum_output_per_1m":10},"policy":{"provider":"openrouter","exact_model_ids_required":true},"models":[{"openrouter_model_id":"acme/writer:free","llambo_selector":"openrouter/acme/writer:free","max_completion_tokens":1024}]}`
	path := filepath.Join(dir, "roster.json")
	if err := os.WriteFile(path, []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	io := &commandIO{ctx: context.Background()}
	err := (&evalsWritingCampaignCommand{Roster: path, Model: "acme/missing"}).Run(&evalsCommand{}, io)
	if err == nil || !strings.Contains(err.Error(), "not in the locked writing roster") {
		t.Fatalf("error = %v", err)
	}
}

func TestWritingCampaignRejectsDisabledModel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	roster := `{"schema_version":1,"locked":true,"assets":{"source_input":"source.md","system_prompt":"system.md","comparison_page":"writing.html","raw_output_directory":"results","future_multistage":{}},"pricing":{"maximum_output_per_1m":10},"policy":{"provider":"openrouter","exact_model_ids_required":true},"models":[{"openrouter_model_id":"acme/writer:free","llambo_selector":"openrouter/acme/writer:free","max_completion_tokens":1024,"disabled":true,"disabled_reason":"HTTP 429"}]}`
	path := filepath.Join(dir, "roster.json")
	if err := os.WriteFile(path, []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	io := &commandIO{ctx: context.Background()}
	err := (&evalsWritingCampaignCommand{Roster: path, Model: "acme/writer:free"}).Run(&evalsCommand{}, io)
	if err == nil || !strings.Contains(err.Error(), `model "acme/writer:free" is disabled: HTTP 429`) {
		t.Fatalf("error = %v", err)
	}
}
