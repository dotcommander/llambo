package evals

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestDiscoverOMLXModelsPrefersAdminMetadata(t *testing.T) {
	fallbackCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing bearer token: %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/admin/api/models":
			fmt.Fprint(w, `{"models":[{"id":"z-chat","model_type":"llm"},{"id":"a-vlm","model_type":"vlm"},{"id":"hidden","model_type":"llm","is_hidden":true},{"id":"helper","model_type":"llm","is_helper":true},{"id":"embed","model_type":"embedding"},{"id":"virtual","model_type":"llm","virtual":true}]}`)
		case "/v1/models":
			fallbackCalls++
			fmt.Fprint(w, `{"data":[{"id":"wrong"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	discovery, err := DiscoverOMLXModels(context.Background(), server.URL+"/v1", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if fallbackCalls != 0 || discovery.Endpoint != server.URL+"/admin/api/models" {
		t.Fatalf("admin inventory was not preferred: %#v", discovery)
	}
	if !slices.Equal(discovery.Models, []string{"a-vlm", "z-chat"}) || !slices.Equal(discovery.Excluded, []string{"embed", "helper", "hidden", "virtual"}) {
		t.Fatalf("unexpected admin filtering: %#v", discovery)
	}
}

func TestDiscoverOMLXModelsFallsBackWithConservativeFiltering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/api/models" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"z-chat"},{"id":"local"},{"id":"Qwen3-Embedding-4B"},{"id":"a-chat"},{"id":"speech-asr"},{"id":"z-chat"}]}`)
	}))
	defer server.Close()

	discovery, err := DiscoverOMLXModels(context.Background(), server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if discovery.Endpoint != server.URL+"/v1/models" || !slices.Equal(discovery.Models, []string{"a-chat", "z-chat"}) {
		t.Fatalf("unexpected fallback models: %#v", discovery)
	}
	if !slices.Equal(discovery.Excluded, []string{"Qwen3-Embedding-4B", "local", "speech-asr"}) {
		t.Fatalf("unexpected fallback exclusions: %#v", discovery.Excluded)
	}
}

func TestDiscoverOMLXModelsHonorsContextAndResponseLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DiscoverOMLXModels(ctx, "http://127.0.0.1:1", "", nil); err == nil {
		t.Fatal("canceled context unexpectedly succeeded")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", omlxResponseLimit+1))
	}))
	defer server.Close()
	if _, err := DiscoverOMLXModels(context.Background(), server.URL, "", server.Client()); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize response was not rejected: %v", err)
	}
}

func TestDiscoverOMLXModelsDoesNotFollowRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/models", http.StatusFound)
	}))
	defer server.Close()
	if _, err := DiscoverOMLXModels(context.Background(), server.URL, "secret", server.Client()); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("OMLX discovery followed or accepted redirect: %v", err)
	}
}

func TestValidateOMLXLoopbackBaseURL(t *testing.T) {
	for _, valid := range []string{"http://localhost:8000", "http://127.0.0.1:8000/v1", "http://[::1]:8000"} {
		if err := ValidateOMLXLoopbackBaseURL(valid); err != nil {
			t.Fatalf("loopback URL %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{"https://example.com/v1", "ftp://127.0.0.1", "http://127.0.0.1/admin", "http://user@127.0.0.1", "http://127.0.0.1?x=1"} {
		if err := ValidateOMLXLoopbackBaseURL(invalid); err == nil {
			t.Errorf("unsafe OMLX URL %q accepted", invalid)
		}
	}
}

func TestFilterProjectedRowsToLiveOMLXTouchesOnlyProjections(t *testing.T) {
	formula := FormulaDiagnostics{Population: 77, DualSourceModels: 9}
	report := Report{Formula: formula, Models: []ReportModel{
		{Key: "canonical", Name: "Canonical"},
		{Key: "live", Projection: &ProjectionInfo{SourceKey: "source", Confidence: "medium"}},
		{Key: "stale", Projection: &ProjectionInfo{SourceKey: "source", Confidence: "low"}},
	}}
	discovery := OMLXDiscovery{Endpoint: "http://omlx/admin/api/models", Models: []string{"live", "untracked"}, Excluded: []string{"embed"}, Attempts: []string{"http://omlx/admin/api/models"}}
	FilterProjectedRowsToLiveOMLX(&report, discovery, nil)
	if !slices.Equal(omlxModelKeys(report.Models), []string{"canonical", "live"}) || report.Formula.Population != formula.Population || report.Formula.DualSourceModels != formula.DualSourceModels {
		t.Fatalf("live filtering changed canonical rows or formula: %#v", report)
	}
	if report.OMLX == nil || report.OMLX.Status != "filtered" || !slices.Equal(report.OMLX.MatchedModels, []string{"live"}) || !slices.Equal(report.OMLX.UnmatchedModels, []string{"untracked"}) || !slices.Equal(report.OMLX.InactiveReviewedModels, []string{"stale"}) {
		t.Fatalf("unexpected live diagnostics: %#v", report.OMLX)
	}
}

func TestUnavailableOMLXOmitUnprovenProjectedRows(t *testing.T) {
	report := Report{Models: []ReportModel{{Key: "canonical"}, {Key: "projected", Projection: &ProjectionInfo{SourceKey: "source", Confidence: "low"}}}}
	FilterProjectedRowsToLiveOMLX(&report, OMLXDiscovery{Attempts: []string{"admin", "fallback"}}, fmt.Errorf("offline"))
	if !slices.Equal(omlxModelKeys(report.Models), []string{"canonical"}) {
		t.Fatalf("unavailable discovery retained unproven projections: %#v", report.Models)
	}
	if report.OMLX == nil || report.OMLX.Status != "unavailable" || report.OMLX.Error != "offline" {
		t.Fatalf("unavailable discovery was not diagnosed: %#v", report.OMLX)
	}
	markdown := RenderMarkdown(report, 0)
	if !strings.Contains(markdown, "OMLX live selection: unavailable; projected availability omitted") {
		t.Fatalf("markdown omitted unavailable discovery:\n%s", markdown)
	}
}

func omlxModelKeys(rows []ReportModel) []string {
	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = row.Key
	}
	return keys
}
