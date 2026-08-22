package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

func TestVerifyWritingOMLXModelsUsesLiveAdminInventory(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"id": "live-model"}}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	model := evals.WritingModelSpec{Provider: "omlx", Model: "live-model"}
	manifest := evals.WritingRunManifest{Identity: evals.WritingRunIdentity{Models: []evals.WritingModelSpec{model}}}
	configs := map[string]providers.Config{model.ID(): {BaseURL: server.URL}}
	if err := verifyWritingOMLXModels(t.Context(), manifest, configs); err != nil {
		t.Fatal(err)
	}

	manifest.Identity.Models[0].Model = "stale-model"
	configs["omlx/stale-model"] = providers.Config{BaseURL: server.URL}
	if err := verifyWritingOMLXModels(t.Context(), manifest, configs); err == nil {
		t.Fatal("stale model passed live admin verification")
	}
}
