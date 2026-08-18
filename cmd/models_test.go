package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/providers"
)

func TestModelsListHelpDescribesInstantPath(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"models", "list", "--help"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"immediately",
		"never calls provider APIs unless --available is set",
		"standardized to 0-100",
		"--available -P omlx",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("model list help missing %q:\n%s", want, out.String())
		}
	}
}

func TestModelsListSkipsProviderAPIsByDefault(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls++
	}))
	defer server.Close()

	configPath := writeModelsTestConfig(t, fmt.Sprintf(`{
		"default_provider":"local",
		"providers":{
			"local":{
				"base_url":%q,
				"model":"configured-model",
				"models":["second-model"],
				"enabled":true,
				"requires_key":false
			}
		}
	}`, server.URL))
	oldConfig := providers.ConfigFile()
	t.Cleanup(func() { providers.SetConfigFile(oldConfig) })

	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"--config", configPath, "models", "list"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("instant model list called provider API %d time(s)", calls)
	}
	for _, want := range []string{"local", "configured-model", "second-model"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("model list output missing %q:\n%s", want, out.String())
		}
	}
}

func TestModelsListAvailableCanTargetOneProvider(t *testing.T) {
	var omlxCalls, otherCalls int
	omlx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		omlxCalls++
		if r.URL.Path != "/v1/models" {
			t.Errorf("OMLX request path = %q, want /v1/models", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"id":"local-a"},{"id":"local-b"}]}`)
	}))
	defer omlx.Close()
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		otherCalls++
	}))
	defer other.Close()

	configPath := writeModelsTestConfig(t, fmt.Sprintf(`{
		"default_provider":"omlx",
		"providers":{
			"omlx":{
				"base_url":%q,
				"model":"configured-omlx",
				"enabled":true,
				"requires_key":false
			},
			"other":{
				"base_url":%q,
				"model":"configured-other",
				"enabled":true,
				"requires_key":false
			}
		}
	}`, omlx.URL, other.URL))
	oldConfig := providers.ConfigFile()
	t.Cleanup(func() { providers.SetConfigFile(oldConfig) })

	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{
		"--config", configPath, "models", "list", "--available", "-P", "omlx", "--timeout-seconds", "1",
	}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if omlxCalls != 1 {
		t.Fatalf("OMLX API calls = %d, want 1", omlxCalls)
	}
	if otherCalls != 0 {
		t.Fatalf("unselected provider API calls = %d, want 0", otherCalls)
	}
	for _, want := range []string{"omlx", "local-a", "local-b"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("targeted model list output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "configured-other") {
		t.Fatalf("targeted model list included unselected provider:\n%s", out.String())
	}
}

func TestModelsListMetricsUsesCatalogWithoutProviderCalls(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls++
	}))
	defer server.Close()

	configPath := writeModelsTestConfig(t, fmt.Sprintf(`{
		"default_provider":"local",
		"providers":{
			"local":{
				"base_url":%q,
				"model":"measured-model",
				"models":["unmeasured-model"],
				"enabled":true,
				"requires_key":false
			}
		}
	}`, server.URL))
	oldConfig := providers.ConfigFile()
	t.Cleanup(func() { providers.SetConfigFile(oldConfig) })
	now := time.Now().UTC()
	catPath := filepath.Join(home, ".config", "llambo", "catalog.json")
	if err := catalog.Save(catPath, &catalog.Catalog{
		Version: 1,
		Providers: map[string]*catalog.ProviderCatalog{
			"local": {
				Models: map[string]*catalog.ModelEntry{
					"measured-model": {
						Quality: map[string]catalog.QualityEvidence{
							"writing": {Score: 0.98, Source: "test", UpdatedAt: now},
						},
						LastPing: catalog.PingState{Success: true, LatencyMS: 200, TokensOut: 40, CheckedAt: now},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"--config", configPath, "models", "list", "--metrics"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("metrics model list called provider API %d time(s)", calls)
	}
	for _, want := range []string{"SCORE", "SPEED", "LATENCY", "writing 98.0/100", "200.0 tok/s", "200ms", "unmeasured-model", "—"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("metrics model list output missing %q:\n%s", want, out.String())
		}
	}

	var csvOut bytes.Buffer
	if err := execute(context.Background(), []string{"--config", configPath, "models", "list", "--metrics", "--csv"}, &csvOut, &errOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"provider,enabled,model,primary,score,speed,latency", "writing 98.0/100"} {
		if !strings.Contains(csvOut.String(), want) {
			t.Errorf("metrics CSV output missing %q:\n%s", want, csvOut.String())
		}
	}
}

func writeModelsTestConfig(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
