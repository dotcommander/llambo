package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
)

func TestPromptRepeatedInvocationOwnsOptions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LLAMBO_PROMPT_MODELS", "")
	t.Setenv("LLAMBO_PROMPT_FUSE", "false")
	original := executePromptAgainstAllProviders
	t.Cleanup(func() { executePromptAgainstAllProviders = original })
	var systems []string
	var timeouts []int
	executePromptAgainstAllProviders = func(prompt, system string, timeout int) ([]PromptResult, error) {
		systems = append(systems, system)
		timeouts = append(timeouts, timeout)
		return []PromptResult{{Provider: "fixture", Model: "chat", Response: prompt}}, nil
	}
	var out bytes.Buffer
	file := filepath.Join(t.TempDir(), "first.md")
	if err := execute(context.Background(), []string{"prompt", "--system", "first system", "--timeout", "7", "--output", file, "first"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := execute(context.Background(), []string{"prompt", "second"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if len(systems) != 2 || systems[0] != "first system" || systems[1] != "You are a helpful assistant." || timeouts[0] != 7 || timeouts[1] != 60 {
		t.Fatalf("leaked options: systems=%v timeouts=%v", systems, timeouts)
	}
	if !strings.Contains(out.String(), "second") || strings.Contains(out.String(), "Results written") {
		t.Fatalf("output option leaked: %s", out.String())
	}
}

func TestPromptCancellationStopsFusionAndOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	original, fusionOriginal := executePromptAgainstAllProviders, executePromptFusionResponse
	t.Cleanup(func() { executePromptAgainstAllProviders, executePromptFusionResponse = original, fusionOriginal })
	executePromptAgainstAllProviders = func(_, _ string, _ int) ([]PromptResult, error) {
		cancel()
		return []PromptResult{{Response: "draft"}}, nil
	}
	executePromptFusionResponse = func(_, _ string, _ []PromptResult, _ int, _ string) (*PromptFusion, error) {
		t.Fatal("fusion after cancellation")
		return nil, nil
	}
	var out bytes.Buffer
	path := filepath.Join(t.TempDir(), "canceled.md")
	err := execute(ctx, []string{"prompt", "--fuse", "--output", path, "prompt"}, &out, &out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("output after cancellation: %s", out.String())
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output file after cancellation: %v", err)
	}
}

func TestPromptJoinsOutputAndFusionErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	original, fusionOriginal := executePromptAgainstAllProviders, executePromptFusionResponse
	t.Cleanup(func() { executePromptAgainstAllProviders, executePromptFusionResponse = original, fusionOriginal })
	sentinel := errors.New("fusion fixture")
	executePromptAgainstAllProviders = func(_, _ string, _ int) ([]PromptResult, error) {
		return []PromptResult{{Provider: "fixture", Model: "chat", Response: "draft"}}, nil
	}
	executePromptFusionResponse = func(_, _ string, _ []PromptResult, _ int, _ string) (*PromptFusion, error) { return nil, sentinel }
	var out bytes.Buffer
	path := filepath.Join(t.TempDir(), "missing", "result.md")
	err := execute(context.Background(), []string{"prompt", "--fuse", "--output", path, "prompt"}, &out, &out)
	if !errors.Is(err, sentinel) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("joined error lost identity: %v", err)
	}
	if !strings.Contains(out.String(), "draft") || !strings.Contains(out.String(), "Displaying results to stdout") {
		t.Fatalf("missing fallback: %s", out.String())
	}
}

func TestBackendDistributionUsesAllAttributedResults(t *testing.T) {
	var out bytes.Buffer
	printRequestBackendDistribution(commandPrinter{&out}, map[string]int{"a": 3, "b": 1}, 1)
	if !strings.Contains(out.String(), "75.0%") || !strings.Contains(out.String(), "25.0%") {
		t.Fatalf("wrong population: %s", out.String())
	}
	out.Reset()
	printRequestBackendDistribution(commandPrinter{&out}, map[string]int{"a": 0}, 0)
	if strings.Contains(out.String(), "NaN") || strings.Contains(out.String(), "Inf") {
		t.Fatalf("nonfinite empty distribution: %s", out.String())
	}
}

func TestPingCostFilterUsesCallOptionsOnce(t *testing.T) {
	configs := map[string]providers.Config{"fixture": {Enabled: true, Model: "cheap", Models: []string{"expensive"}}}
	prices := map[string]costs.ModelCost{
		costs.Key("fixture", "cheap"):     {InputExplicit: true, OutputExplicit: true, OutputPer1M: 1},
		costs.Key("fixture", "expensive"): {InputExplicit: true, OutputExplicit: true, OutputPer1M: 3},
	}
	var out bytes.Buffer
	targets := buildPingTargetsWithWriter(&out, configs, prices, providers.Blocklist{}, 2, false, nil)
	if len(targets) != 1 || targets[0].Config.Model != "cheap" {
		t.Fatalf("wrong targets: %#v", targets)
	}
	if strings.Count(out.String(), "Cost filter:") != 1 || !strings.Contains(out.String(), "skipped 1") {
		t.Fatalf("wrong accounting: %s", out.String())
	}
}

func TestPingFallbackLatencyIncludesStreamAttempt(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Stream {
			time.Sleep(40 * time.Millisecond)
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		io.WriteString(w, `{"id":"fixture","model":"chat","choices":[{"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	result := pingProviderContext(context.Background(), "fixture", providers.Config{Model: "chat", BaseURL: srv.URL, APIKey: "fixture-key", RequiresKey: false}, "prompt", time.Second)
	if !result.Success || calls != 2 || result.Latency < 40*time.Millisecond {
		t.Fatalf("lost first attempt latency: calls=%d result=%#v", calls, result)
	}
}

func TestFusionRejectsNonChatSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	old := providers.ConfigFile()
	t.Cleanup(func() { providers.SetConfigFile(old) })
	path := writeModelsTestConfig(t, `{"default_provider":"fixture","providers":{"fixture":{"enabled":true,"base_url":"http://127.0.0.1:1","model":"embed","requires_key":false}}}`)
	providers.SetConfigFile(path)
	catPath, err := catalog.CatalogPath()
	if err != nil {
		t.Fatal(err)
	}
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{"fixture": {Models: map[string]*catalog.ModelEntry{"embed": {Metadata: catalog.ModelMetadata{Architecture: catalog.ModelArchitecture{OutputModalities: []string{"embedding"}}}}}}}}
	if err := catalog.Save(catPath, cat); err != nil {
		t.Fatal(err)
	}
	_, err = (&invocationOptions{}).resolveFirstFusionTarget("fixture/embed")
	if err == nil || !strings.Contains(err.Error(), "no text chat-capable") {
		t.Fatalf("non-chat fusion target accepted: %v", err)
	}
}

func TestDiscoverFreeUsesItsOwnPromptAndContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	old := providers.ConfigFile()
	t.Cleanup(func() { providers.SetConfigFile(old) })
	entered := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			io.WriteString(w, `{"data":[{"id":"chat"}]}`)
			return
		}
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if len(request.Messages) == 0 {
			t.Error("missing discovery prompt")
			return
		}
		entered <- request.Messages[len(request.Messages)-1].Content
		<-r.Context().Done()
	}))
	defer srv.Close()
	config := writeModelsTestConfig(t, `{"default_provider":"fixture","providers":{"fixture":{"enabled":true,"model":"chat","api_key":"fixture-key","requires_key":false,"base_url":"`+srv.URL+`"}}}`)
	providers.SetConfigFile(config)
	pricePath := filepath.Join(home, ".config", "llambo", "model-costs.csv")
	if err := os.MkdirAll(filepath.Dir(pricePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pricePath, []byte("provider,model,input_per_1m_usd,output_per_1m_usd\nfixture,chat,0,0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- execute(ctx, []string{"models", "discover-free", "-P", "fixture"}, io.Discard, io.Discard)
	}()
	select {
	case prompt := <-entered:
		if prompt != defaultDiscoverFreePrompt || strings.TrimSpace(prompt) == "" {
			t.Errorf("wrong discovery prompt: %q", prompt)
		}
		cancel()
	case err := <-result:
		t.Fatalf("discovery ended before request: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("discovery never called fixture")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lost discovery cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("discovery ignored command context")
	}
}
