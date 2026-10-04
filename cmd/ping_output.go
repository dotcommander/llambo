package cmd

import (
	"context"
	"strings"
	"time"
	"unicode"

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

// Scalar helpers retain their signatures with independent default options.
func pingTimeoutForProvider(ignored0 string) time.Duration {
	return defaultInvocationOptions().pingTimeoutForProvider(ignored0)
}
