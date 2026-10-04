package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/providers"
)

func (cliOpts *invocationOptions) pingTimeoutForProvider(_ string) time.Duration {
	return time.Duration(cliOpts.pingTimeout) * time.Second
}

func pingProvider(name string, cfg providers.Config, prompt string, timeout time.Duration) PingResult {
	return pingProviderContext(context.Background(), name, cfg, prompt, timeout)
}

func pingProviderContext(parent context.Context, name string, cfg providers.Config, prompt string, timeout time.Duration) PingResult {
	result := PingResult{
		Provider: name,
		Model:    cfg.Model,
		Headers:  make(map[string]string),
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	start := time.Now()

	// Use native Gemini API
	if providers.IsGeminiProvider(cfg) {
		apiKey := providers.GetAPIKey(name, cfg)
		if apiKey == "" && cfg.GetRequiresKey() {
			result.Error = "no API key configured"
			return result
		}
		client := newPingGeminiClient(apiKey, cfg.BaseURL, cfg.Model)
		resp, err := client.ChatStream(ctx, "", prompt, pingMaxTokens(cfg.MaxTokens))
		result.Latency = time.Since(start)
		if pingShouldFallbackToNonStreaming(err) {
			resp, err = client.Chat(ctx, "", prompt, pingMaxTokens(cfg.MaxTokens))
			result.Latency = time.Since(start)
		}

		if err != nil {
			result.Error = err.Error()
			return result
		}

		result.Success = true
		result.Response = resp.Content
		result.TokensIn = resp.TokensIn
		result.TokensOut = resp.TokensOut
		result.TTFB = resp.TTFB
		result.Generation = resp.Generation
		result.SpeedTokensPS = pingSpeedTokensPerSecond(resp.TokensOut, resp.Generation)
		return result
	}

	client, err := newPingOpenAIClientForConfig(name, cfg)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	maxTok := pingMaxTokens(cfg.MaxTokens)

	resp, err := client.ChatStream(ctx, "", prompt, maxTok)
	result.Latency = time.Since(start)
	if pingShouldFallbackToNonStreaming(err) {
		resp, err = client.Chat(ctx, "", prompt, maxTok)
		result.Latency = time.Since(start)
	}

	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Response = resp.Content
	result.TokensIn = resp.TokensIn
	result.TokensOut = resp.TokensOut
	result.TTFB = resp.TTFB
	result.Generation = resp.Generation
	result.SpeedTokensPS = pingSpeedTokensPerSecond(resp.TokensOut, resp.Generation)

	return result
}

func pingMaxTokens(configured int) int {
	const benchmarkMaxTokens = 256
	if configured <= 0 || configured > benchmarkMaxTokens {
		return benchmarkMaxTokens
	}
	return configured
}

func printResult(out io.Writer, r PingResult) {
	status := "✓"
	if !r.Success {
		status = "✗"
	}

	fmt.Fprintf(out, "%s %-10s %-35s %6dms", status, r.Provider, r.Model, r.Latency.Milliseconds())

	if r.Success {
		// Truncate response for display
		resp := r.Response
		if len(resp) > 50 {
			resp = resp[:50] + "..."
		}
		resp = strings.ReplaceAll(resp, "\n", " ")
		fmt.Fprintf(out, "  %q", resp)
		if r.TokensIn > 0 || r.TokensOut > 0 {
			fmt.Fprintf(out, " [%d→%d tok]", r.TokensIn, r.TokensOut)
		}
		if r.CostStatus != "" {
			fmt.Fprintf(out, " [%s]", pingCostLabel(r))
		}
	} else {
		fmt.Fprint(out, "  FAIL")
		if r.CostStatus != "" {
			fmt.Fprintf(out, " [%s]", pingCostLabel(r))
		}
	}
	fmt.Fprintln(out)
}

func pingCostLabel(r PingResult) string {
	label := catalog.PriceLabel(catalog.CostStatus(r.CostStatus), r.InputCostPer1M, r.OutputCostPer1M)
	if r.CostStatus != string(catalog.CostPaid) || (r.TokensIn == 0 && r.TokensOut == 0) {
		return label
	}
	input := float64(r.TokensIn) * r.InputCostPer1M / 1_000_000
	output := float64(r.TokensOut) * r.OutputCostPer1M / 1_000_000
	return fmt.Sprintf("%s, actual $%.6f", label, input+output)
}

func tokenizeWords(text string) []string {
	lower := strings.ToLower(text)
	var words []string
	var buf strings.Builder

	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '/' || r == '.' {
			buf.WriteRune(r)
		} else {
			if buf.Len() > 0 {
				words = append(words, buf.String())
				buf.Reset()
			}
		}
	}
	if buf.Len() > 0 {
		words = append(words, buf.String())
	}

	return words
}

func printSummary(out io.Writer, results []PingResult) {
	fmt.Fprintln(out, "\n"+strings.Repeat("─", 60))
	fmt.Fprintln(out, "SUMMARY")
	fmt.Fprintln(out, strings.Repeat("─", 60))

	var successful, failed int
	var totalLatency time.Duration
	var fastestProvider string
	var fastestLatency time.Duration

	for _, r := range results {
		if r.Success {
			successful++
			totalLatency += r.Latency
			if fastestLatency == 0 || r.Latency < fastestLatency {
				fastestLatency = r.Latency
				fastestProvider = r.Provider
			}
		} else {
			failed++
		}
	}

	fmt.Fprintf(out, "Total providers: %d (✓ %d, ✗ %d)\n", len(results), successful, failed)
	if successful > 0 {
		fmt.Fprintf(out, "Average latency: %dms\n", (totalLatency / time.Duration(successful)).Milliseconds())
		fmt.Fprintf(out, "Fastest: %s (%dms)\n", fastestProvider, fastestLatency.Milliseconds())
	}

	// Inefficiency analysis
	fmt.Fprintln(out, "\n"+strings.Repeat("─", 60))
	fmt.Fprintln(out, "INEFFICIENCY ANALYSIS")
	fmt.Fprintln(out, strings.Repeat("─", 60))

	for _, r := range results {
		if !r.Success {
			fmt.Fprintf(out, "⚠ %s: Failed - %s\n", r.Provider, r.Error)
			continue
		}

		var issues []string

		// Slow response (>5s)
		if r.Latency > 5*time.Second {
			issues = append(issues, fmt.Sprintf("slow response (%dms)", r.Latency.Milliseconds()))
		}

		// No token reporting
		if r.TokensIn == 0 && r.TokensOut == 0 {
			issues = append(issues, "no token usage reported")
		}

		// Empty response
		if strings.TrimSpace(r.Response) == "" {
			issues = append(issues, "empty response")
		}

		if len(issues) > 0 {
			fmt.Fprintf(out, "⚠ %s: %s\n", r.Provider, strings.Join(issues, ", "))
		}
	}
}

// Scalar helpers retain their signatures with independent default options.
func pingTimeoutForProvider(ignored0 string) time.Duration {
	return defaultInvocationOptions().pingTimeoutForProvider(ignored0)
}
