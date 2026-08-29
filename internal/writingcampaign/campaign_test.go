package writingcampaign

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunRetriesOnlyLengthAndWritesReceipt(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var requestedTokens []int
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return nil, err
		}
		mu.Lock()
		requestedTokens = append(requestedTokens, request.MaxTokens)
		attempt := len(requestedTokens)
		mu.Unlock()
		finish := "length"
		content := "partial"
		if attempt == 2 {
			finish = "stop"
			content = "finished"
		}
		var body bytes.Buffer
		json.NewEncoder(&body).Encode(map[string]any{
			"model": "acme/writer", "provider": "Acme",
			"choices": []any{map[string]any{"message": map[string]any{"content": content}, "finish_reason": finish}},
			"usage":   map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "cost": 0.001},
		})
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Trace-ID": {"trace"}, "Set-Cookie": {"secret=value"}}, Body: io.NopCloser(&body), Request: r}, nil
	})}

	roster := testRoster()
	dir := t.TempDir()
	campaignClient := &Client{HTTPClient: client, BaseURL: "https://example.test/api", APIKey: "test-key"}
	options := RunOptions{OutputDir: dir, InitialTokens: 128, RetryTokens: 512, Concurrency: 1, Execute: true}
	receipts, _, err := Run(context.Background(), campaignClient, roster, "source", "system", options)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 || receipts[0].Status != "complete" || receipts[0].OutputText != "finished" {
		t.Fatalf("receipts = %#v", receipts)
	}
	if len(receipts[0].Attempts) != 2 || receipts[0].RetryDisposition != "retried_with_higher_max_completion_tokens" {
		t.Fatalf("attempts = %#v", receipts[0].Attempts)
	}
	if receipts[0].TotalTimeToFinishMS <= 0 {
		t.Fatalf("total time to finish = %d", receipts[0].TotalTimeToFinishMS)
	}
	if got, want := requestedTokens, []int{128, 512}; !equalInts(got, want) {
		t.Fatalf("max_tokens = %v, want %v", got, want)
	}
	if receipts[0].Attempts[1].ResponseHeaders["X-Trace-Id"][0] != "trace" {
		t.Fatalf("headers = %#v", receipts[0].Attempts[1].ResponseHeaders)
	}
	if _, ok := receipts[0].Attempts[1].ResponseHeaders["Set-Cookie"]; ok {
		t.Fatal("sensitive Set-Cookie header was persisted")
	}
	data, err := os.ReadFile(filepath.Join(dir, "acme-writer.json"))
	if err != nil || !json.Valid(data) {
		t.Fatalf("receipt: %v, valid=%v", err, json.Valid(data))
	}
}

func TestRunDoesNotRetryStop(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := `{"model":"acme/writer","provider":"Acme","choices":[{"message":{"content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	options := RunOptions{OutputDir: t.TempDir(), InitialTokens: 128, RetryTokens: 512, Concurrency: 1, Execute: true}
	_, _, err := Run(context.Background(), &Client{HTTPClient: client, BaseURL: "https://example.test/api", APIKey: "key"}, testRoster(), "source", "system", options)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestRunAppliesTimeoutAcrossLengthRetry(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			body := `{"model":"acme/writer","choices":[{"message":{"content":"partial"},"finish_reason":"length"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	options := RunOptions{OutputDir: t.TempDir(), InitialTokens: 128, RetryTokens: 512, Concurrency: 1, ModelTimeout: 20 * time.Millisecond, Execute: true}
	receipts, _, err := Run(context.Background(), &Client{HTTPClient: client, BaseURL: "https://example.test/api", APIKey: "key"}, testRoster(), "source", "system", options)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts[0].Attempts) != 2 || !strings.Contains(receipts[0].Attempts[1].Error, "context deadline exceeded") {
		t.Fatalf("attempts = %#v", receipts[0].Attempts)
	}
	if receipts[0].TotalTimeToFinishMS > 100 {
		t.Fatalf("model timeout was not shared across attempts: %dms", receipts[0].TotalTimeToFinishMS)
	}
}

func TestAutoRouterCapturesRoutedModelAndEnforcesPriceCeiling(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		if request.Provider == nil || request.Provider.MaxPrice.Prompt != 10 || request.Provider.MaxPrice.Completion != 10 {
			t.Fatalf("provider price ceiling = %#v", request.Provider)
		}
		body := `{"model":"google/gemini-3.5-flash","provider":"Google","choices":[{"message":{"content":"routed output"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30,"cost":0.0002}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	roster := testRoster()
	roster.Models[0] = Model{OpenRouterModelID: "openrouter/auto", LlamboSelector: "openrouter/openrouter/auto", DynamicRouting: true, RoutingMaxInputPer1M: 10, RoutingMaxOutputPer1M: 10, MaxCompletionTokens: 1024}
	options := RunOptions{OutputDir: t.TempDir(), InitialTokens: 128, RetryTokens: 512, Concurrency: 1, Execute: true}
	receipts, _, err := Run(context.Background(), &Client{HTTPClient: client, BaseURL: "https://example.test/api", APIKey: "key"}, roster, "source", "system", options)
	if err != nil {
		t.Fatal(err)
	}
	if got := receipts[0].ServedModel; got != "google/gemini-3.5-flash" {
		t.Fatalf("served model = %q", got)
	}
	if got := receipts[0].ServedProvider; got != "Google" {
		t.Fatalf("served provider = %q", got)
	}
}

func TestAutoRouterRequiresReportedCost(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"model":"google/gemini-3.5-flash","choices":[{"message":{"content":"output"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	model := Model{OpenRouterModelID: "openrouter/auto", DynamicRouting: true, RoutingMaxInputPer1M: 10, RoutingMaxOutputPer1M: 10}
	attempt := (&Client{HTTPClient: client, BaseURL: "https://example.test/api", APIKey: "key"}).Execute(context.Background(), model, "system", "source", 128, 1)
	if !strings.Contains(attempt.Error, "omitted usage.cost") {
		t.Fatalf("error = %q", attempt.Error)
	}
}

func TestRenderHTMLEscapesOutputAndIncludesPendingModels(t *testing.T) {
	t.Parallel()
	roster := testRoster()
	receipts := []Receipt{{ModelID: "acme/writer", Status: "complete", OutputText: `<script>alert("x")</script>`, TokensPerSecond: 12.5, TotalCostUSD: 0.25, TotalLatencyMS: 2000, TotalTimeToFinishMS: 2500, FinishReason: "stop", Attempts: []Attempt{{Attempt: 1}}}}
	var out bytes.Buffer
	if err := RenderHTML(&out, roster, receipts, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	if strings.Contains(html, "<script>alert") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("output was not escaped: %s", html)
	}
	if !strings.Contains(html, "openrouter/acme/writer") || !strings.Contains(html, "$0.250000") {
		t.Fatalf("missing comparison values: %s", html)
	}
	if !strings.Contains(html, "2.50s") {
		t.Fatalf("missing total time to finish: %s", html)
	}
	metadata := strings.Index(html, `class="metadata-row"`)
	article := strings.Index(html, `class="article-row"`)
	if metadata < 0 || article < metadata || !strings.Contains(html, `colspan="8"`) {
		t.Fatalf("model rows are not metadata followed by full-width article: %s", html)
	}
}

func TestRenderHTMLShowsAutoRouterSelection(t *testing.T) {
	t.Parallel()
	roster := testRoster()
	roster.Models[0] = Model{OpenRouterModelID: "openrouter/auto", LlamboSelector: "openrouter/openrouter/auto", DynamicRouting: true, RoutingMaxInputPer1M: 10, RoutingMaxOutputPer1M: 10, MaxCompletionTokens: 1024}
	receipts := []Receipt{{ModelID: "openrouter/auto", Status: "complete", ServedModel: "google/gemini-3.5-flash", ServedProvider: "Google", OutputText: "done"}}
	var out bytes.Buffer
	if err := RenderHTML(&out, roster, receipts, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if html := out.String(); !strings.Contains(html, "→ google/gemini-3.5-flash (Google)") {
		t.Fatalf("routed identity missing: %s", html)
	}
}

func TestRosterValidationRejectsAliasAndPriceViolation(t *testing.T) {
	t.Parallel()
	roster := testRoster()
	roster.Models[0].LlamboSelector = "openrouter/@preset/free"
	if err := roster.Validate(); err == nil {
		t.Fatal("expected selector validation error")
	}
	roster = testRoster()
	roster.Models[0].OutputPer1M = 11
	if err := roster.Validate(); err == nil {
		t.Fatal("expected pricing validation error")
	}
	roster = testRoster()
	roster.Models[0].Disabled = true
	if err := roster.Validate(); err == nil {
		t.Fatal("expected disabled-reason validation error")
	}
}

func testRoster() Roster {
	return Roster{
		SchemaVersion: 1,
		Locked:        true,
		Assets:        Assets{SourceInput: "source.md", SystemPrompt: "system.md", ComparisonPage: "writing.html", RawOutputDir: "results"},
		Pricing:       PricingPolicy{MaximumOutputPer1M: 10},
		Policy:        CampaignPolicy{Provider: "openrouter", ExactModelIDsRequired: true},
		Models:        []Model{{OpenRouterModelID: "acme/writer", LlamboSelector: "openrouter/acme/writer", InputPer1M: 1, OutputPer1M: 2, MaxCompletionTokens: 1024}},
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
