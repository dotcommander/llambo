package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func assertWarningRecord(t *testing.T, output []byte, message, path string) {
	t.Helper()

	var record map[string]any
	if err := json.Unmarshal(output, &record); err != nil {
		t.Fatalf("decode warning record: %v\noutput: %s", err, output)
	}
	if got := record["level"]; got != "WARN" {
		t.Fatalf("warning level = %v, want WARN", got)
	}
	if got := record["msg"]; got != message {
		t.Fatalf("warning message = %v, want %q", got, message)
	}
	if got := record["path"]; got != path {
		t.Fatalf("warning path = %v, want %q", got, path)
	}
	if got, ok := record["error"].(string); !ok || got == "" {
		t.Fatalf("warning error = %v, want non-empty string", record["error"])
	}
}

func TestRouteEventLoggerAndReader_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	logger := NewRouteEventLogger(path)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	if err := logger.Log(RouteEvent{Mode: "balanced", Intent: IntentChat, EstimatedTokens: 120, ChosenProvider: "openai", Success: true}); err != nil {
		t.Fatalf("log event 1: %v", err)
	}
	if err := logger.Log(RouteEvent{Mode: "cheapest", Intent: IntentCode, EstimatedTokens: 220, ChosenProvider: "openrouter", Success: false, Error: "timeout"}); err != nil {
		t.Fatalf("log event 2: %v", err)
	}

	events, err := ReadRouteEvents(path)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].ChosenProvider != "openai" || events[1].ChosenProvider != "openrouter" {
		t.Fatalf("unexpected providers: %+v", events)
	}
}

func TestReadRouteEvents_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := ReadRouteEvents("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestReadRouteEvents_SkipsBlankLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	content := "\n" +
		"{\"mode\":\"balanced\",\"intent\":\"chat\",\"chosen_provider\":\"openai\",\"success\":true}\n" +
		"\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write events file: %v", err)
	}

	events, err := ReadRouteEvents(path)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestRoutingMetricsStore_Record_ResetsDailyWindow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.json")

	store, err := NewRoutingMetricsStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	store.Providers["openai"] = &ProviderRuntimeMetrics{
		DailyWindow:   "2000-01-01",
		DailyRequests: 99,
		DailyTokens:   999,
		DailyCostUSD:  9.99,
	}

	store.Record("openai", 100*time.Millisecond, &LLMUsage{TotalTokens: 10, Cost: &LLMCost{TotalCost: 0.01}}, nil)
	snap := store.Snapshot()["openai"]
	if snap.DailyWindow == "2000-01-01" {
		t.Fatal("expected daily window reset to today")
	}
	if snap.DailyRequests != 1 {
		t.Fatalf("expected daily requests reset to 1, got %d", snap.DailyRequests)
	}
	if snap.DailyTokens != 10 {
		t.Fatalf("expected daily tokens reset to 10, got %d", snap.DailyTokens)
	}
	if snap.DailyCostUSD != 0.01 {
		t.Fatalf("expected daily cost reset to 0.01, got %f", snap.DailyCostUSD)
	}
}

func TestRoutingMetricsStore_LoadQuarantinesMalformedJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.json")
	if err := os.WriteFile(path, []byte(`{"providers":{}}}`), 0600); err != nil {
		t.Fatalf("write malformed metrics: %v", err)
	}

	store, err := NewRoutingMetricsStore(path)
	if err != nil {
		t.Fatalf("expected malformed metrics to fail open, got %v", err)
	}
	if len(store.Providers) != 0 {
		t.Fatalf("expected empty metrics after corrupt load, got %+v", store.Providers)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected original malformed metrics file to be quarantined, stat err=%v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	var quarantined []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "metrics.json.corrupt-") {
			quarantined = append(quarantined, entry.Name())
		}
	}
	if len(quarantined) != 1 {
		t.Fatalf("expected one quarantined metrics file, got %v", quarantined)
	}
}

func TestRoutingMetricsStore_LoadWarnsWhenQuarantineFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.json")
	if err := os.WriteFile(path, []byte(`{"providers":{}}}`), 0600); err != nil {
		t.Fatalf("write malformed metrics: %v", err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	renameErr := errors.New("forced quarantine rename failure")
	store, err := newRoutingMetricsStore(path, logger, func(string, string) error {
		return renameErr
	})
	if err != nil {
		t.Fatalf("expected malformed metrics to fail open, got %v", err)
	}
	if len(store.Providers) != 0 {
		t.Fatalf("expected empty metrics after corrupt load, got %+v", store.Providers)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected original file to remain after failed quarantine: %v", err)
	}
	assertWarningRecord(t, logs.Bytes(), "quarantine corrupt routing metrics file failed", path)
}

func TestRoutingMetricsStore_RecordWarnsWhenPeriodicSaveFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("block metrics directory creation"), 0600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	path := filepath.Join(parentFile, "metrics.json")

	var logs bytes.Buffer
	store := &RoutingMetricsStore{
		path:      path,
		interval:  time.Second,
		logger:    slog.New(slog.NewJSONHandler(&logs, nil)),
		rename:    os.Rename,
		Providers: make(map[string]*ProviderRuntimeMetrics),
	}
	store.Record("openai", time.Millisecond, nil, nil)

	if got := store.Snapshot()["openai"].Requests; got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	assertWarningRecord(t, logs.Bytes(), "save routing metrics failed", path)
}

func TestRouteEventLogger_CloseWarnsOnError(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	logger := NewRouteEventLogger(path)
	if err := logger.Log(RouteEvent{Mode: "balanced", Intent: IntentChat, Success: true}); err != nil {
		t.Fatalf("log event: %v", err)
	}
	if err := logger.file.Close(); err != nil {
		t.Fatalf("pre-close event file: %v", err)
	}
	var logs bytes.Buffer
	logger.logger = slog.New(slog.NewJSONHandler(&logs, nil))

	logger.Close()
	assertWarningRecord(t, logs.Bytes(), "close route event log failed", path)
	logger.Close()
}

func TestOpenAIClients_CleanupWarnsWhenRouteEventCloseFails(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	routeEvents := NewRouteEventLogger(path)
	if err := routeEvents.Log(RouteEvent{Mode: "balanced", Intent: IntentChat, Success: true}); err != nil {
		t.Fatalf("log event: %v", err)
	}
	if err := routeEvents.file.Close(); err != nil {
		t.Fatalf("pre-close event file: %v", err)
	}

	var logs bytes.Buffer
	clients := &OpenAIClients{
		RouteEvents: routeEvents,
		logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	clients.Cleanup()

	assertWarningRecord(t, logs.Bytes(), "close route event log failed", path)
}

func TestOpenAIClients_CleanupWarnsWhenMetricsSaveFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("block metrics directory creation"), 0600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	path := filepath.Join(parentFile, "metrics.json")

	var logs bytes.Buffer
	clients := &OpenAIClients{
		RoutingMetrics: &RoutingMetricsStore{path: path, Providers: make(map[string]*ProviderRuntimeMetrics)},
		logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	clients.Cleanup()

	assertWarningRecord(t, logs.Bytes(), "save routing metrics failed", path)
}

func TestExecutionCoordinatorWarnsWhenRouteEventWriteFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("block event directory creation"), 0600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	path := filepath.Join(parentFile, "events.jsonl")

	var logs bytes.Buffer
	coordinator := executionCoordinator{oc: &OpenAIClients{
		RouteEvents: NewRouteEventLogger(path),
		logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	}}
	coordinator.logRouteOutcome(IntentChat, 1, "planned", "chosen", "model", nil, time.Millisecond, false, "failed", nil, false)

	assertWarningRecord(t, logs.Bytes(), "write route event failed", path)
}
