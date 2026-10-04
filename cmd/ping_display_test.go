package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestPingLatencyBucket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		d    time.Duration
		want int
	}{
		{"sub-second is fast", 444 * time.Millisecond, 0},
		{"exactly one second is normal", time.Second, 1},
		{"mid-range is normal", 2 * time.Second, 1},
		{"three seconds is slow", 3 * time.Second, 2},
		{"six seconds is slow", 6 * time.Second, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := pingLatencyBucket(tt.d); got != tt.want {
				t.Fatalf("pingLatencyBucket(%v) = %d, want %d", tt.d, got, tt.want)
			}
		})
	}
}

func TestPingDisplayWidths(t *testing.T) {
	t.Parallel()

	results := []PingResult{
		{Provider: "openrouter", Model: "anthropic/claude-haiku-4.5"},
		{Provider: "synthetic", Model: "hf:nvidia/NVIDIA-Nemotron-3-Super-120B-A12B-NVFP4"},
	}
	providerW, modelW := pingDisplayWidths(results)
	if providerW != len("openrouter") {
		t.Fatalf("providerW = %d, want %d", providerW, len("openrouter"))
	}
	if modelW != pingModelColMax {
		t.Fatalf("modelW = %d, want capped %d", modelW, pingModelColMax)
	}
}

func TestPingTruncateRunes(t *testing.T) {
	t.Parallel()

	if got := pingTruncateRunes("hello", 10); got != "hello" {
		t.Fatalf("short string mutated: %q", got)
	}
	truncated := pingTruncateRunes("天空是蓝色的，云朵飘过", 5)
	if !strings.HasSuffix(truncated, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", truncated)
	}
	// Truncation must not split a multi-byte rune.
	if !utf8.ValidString(truncated) {
		t.Fatalf("truncated string is not valid UTF-8: %q", truncated)
	}
}

func TestPingCollapseError(t *testing.T) {
	t.Parallel()

	multiline := "NETWORK_ERROR: HTTP 410: 410 Gone (type=about:blank\nURL: https://integrate.api.nvidia.com/v1/chat/completions\nResponse: {\"detail\":\"gone\"})"
	got := pingCollapseError(multiline)
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("collapsed error still contains whitespace: %q", got)
	}
	if !strings.HasPrefix(got, "NETWORK_ERROR: HTTP 410:") {
		t.Fatalf("unexpected collapse prefix: %q", got)
	}

	long := strings.Repeat("x", pingErrorMaxRunes+50)
	got = pingCollapseError(long)
	if n := len([]rune(got)); n != pingErrorMaxRunes {
		t.Fatalf("collapsed length = %d, want %d", n, pingErrorMaxRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
}

func TestPingPercentile(t *testing.T) {
	t.Parallel()

	if got := pingPercentile(nil, 0.95); got != 0 {
		t.Fatalf("empty percentile = %v, want 0", got)
	}

	sorted := []time.Duration{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
	for i := range sorted {
		sorted[i] *= time.Millisecond
	}

	if got := pingPercentile(sorted, 0.50); got != 500*time.Millisecond {
		t.Fatalf("p50 = %v, want 500ms", got)
	}
	if got := pingPercentile(sorted, 0.95); got != 1000*time.Millisecond {
		t.Fatalf("p95 = %v, want 1000ms", got)
	}
	if got := pingPercentile(sorted[:1], 0.95); got != 100*time.Millisecond {
		t.Fatalf("single-element p95 = %v, want 100ms", got)
	}
}

func TestPingTotalSpend(t *testing.T) {
	t.Parallel()

	results := []PingResult{
		{Success: true, CostStatus: "paid", TokensIn: 1000, TokensOut: 1000, InputCostPer1M: 1, OutputCostPer1M: 2},
		{Success: true, CostStatus: "free", TokensIn: 1000, TokensOut: 1000},
		{Success: false, CostStatus: "paid", TokensIn: 1000, TokensOut: 1000, InputCostPer1M: 1, OutputCostPer1M: 2},
		{Success: true, CostStatus: "paid", TokensIn: 0, TokensOut: 0, InputCostPer1M: 1, OutputCostPer1M: 2},
	}
	// Only the first result contributes: (1000*1 + 1000*2) / 1M = 0.003.
	if got := pingTotalSpend(results); got < 0.0029 || got > 0.0031 {
		t.Fatalf("pingTotalSpend = %f, want 0.003", got)
	}
}

func TestPrintResultsTableSmoke(t *testing.T) {
	t.Parallel()

	results := []PingResult{
		{Provider: "groq", Model: "openai/gpt-oss-120b", Success: true, Latency: 444 * time.Millisecond, Response: "The sky blushes pink.", TokensIn: 84, TokensOut: 63, CostStatus: "paid", InputCostPer1M: 0.15, OutputCostPer1M: 0.6},
		{Provider: "cerebras", Model: "gpt-oss-120b", Success: false, Latency: 386 * time.Millisecond, Error: "AUTH_ERROR: HTTP 401:\nWrong API Key", CostStatus: "paid"},
	}

	var buf bytes.Buffer
	printResultsTable(&buf, results)
	printSummary(&buf, results)
	out := buf.String()

	for _, want := range []string{"PROVIDER", "MODEL", "LATENCY", "RESULT", "✓", "✗", "FAIL", "SUMMARY", "INEFFICIENCY ANALYSIS", "84→63 tok", "paid"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Wrong API Key\nURL") {
		t.Fatalf("error body not collapsed to one line:\n%s", out)
	}
}
