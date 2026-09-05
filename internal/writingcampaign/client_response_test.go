package writingcampaign

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type clientResponsePolicyChoice struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

type clientResponsePolicyFixture struct {
	Model    string                       `json:"model"`
	Provider string                       `json:"provider"`
	Choices  []clientResponsePolicyChoice `json:"choices"`
	Usage    struct {
		PromptTokens     int      `json:"prompt_tokens"`
		CompletionTokens int      `json:"completion_tokens"`
		TotalTokens      int      `json:"total_tokens"`
		Cost             *float64 `json:"cost"`
	} `json:"usage"`
}

type clientResponsePolicyBody struct {
	reader io.Reader
	closes int
}

func (b *clientResponsePolicyBody) Read(p []byte) (int, error) { return b.reader.Read(p) }

func (b *clientResponsePolicyBody) Close() error {
	b.closes++
	return nil
}

func TestClientResponsePolicy(t *testing.T) {
	t.Parallel()
	zero := 0.0
	reportedCost := 0.25
	choice := func(content, finish string) clientResponsePolicyChoice {
		var value clientResponsePolicyChoice
		value.Message.Content = content
		value.FinishReason = finish
		return value
	}
	fixture := func(model, provider, content, finish string, cost *float64) clientResponsePolicyFixture {
		value := clientResponsePolicyFixture{Model: model, Provider: provider, Choices: []clientResponsePolicyChoice{choice(content, finish)}}
		value.Usage.PromptTokens = 2
		value.Usage.CompletionTokens = 3
		value.Usage.TotalTokens = 5
		value.Usage.Cost = cost
		return value
	}
	noChoices := fixture("acme/exact", "Acme", "output", "stop", &reportedCost)
	noChoices.Choices = nil
	multipleChoices := fixture("acme/exact", "Acme", "one", "stop", &reportedCost)
	multipleChoices.Choices = append(multipleChoices.Choices, choice("two", "stop"))

	tests := []struct {
		name         string
		model        Model
		response     clientResponsePolicyFixture
		wantError    string
		wantServed   string
		wantProvider string
		wantOutput   string
		wantFinish   string
		wantCost     float64
		wantTokens   int
	}{
		{
			name:         "exact identity trims metadata but preserves raw output and falls back to configured cost",
			model:        Model{OpenRouterModelID: "acme/exact", InputPer1M: 1_000_000, OutputPer1M: 2_000_000},
			response:     fixture(" acme/exact ", " Acme ", "  raw output  ", " stop ", nil),
			wantServed:   "acme/exact",
			wantProvider: "Acme",
			wantOutput:   "  raw output  ",
			wantFinish:   "stop",
			wantCost:     8,
			wantTokens:   5,
		},
		{
			name:         "exact mismatch is rejected",
			model:        Model{OpenRouterModelID: "acme/exact"},
			response:     fixture("other/model", "Acme", "output", "stop", nil),
			wantError:    "served model \"other/model\" does not match requested exact model \"acme/exact\"",
			wantServed:   "other/model",
			wantProvider: "Acme",
			wantOutput:   "output",
			wantFinish:   "stop",
			wantTokens:   5,
		},
		{
			name:         "empty exact identity is rejected",
			model:        Model{OpenRouterModelID: "acme/exact"},
			response:     fixture("  ", "Acme", "output", "stop", nil),
			wantError:    "served model \"\" does not match requested exact model \"acme/exact\"",
			wantProvider: "Acme",
			wantOutput:   "output",
			wantFinish:   "stop",
			wantTokens:   5,
		},
		{
			name:         "dynamic routing accepts routed identity and explicit zero cost",
			model:        Model{OpenRouterModelID: "openrouter/auto", DynamicRouting: true},
			response:     fixture("google/routed", "Google", "output", "stop", &zero),
			wantServed:   "google/routed",
			wantProvider: "Google",
			wantOutput:   "output",
			wantFinish:   "stop",
			wantTokens:   5,
		},
		{
			name:         "explicit zero cost wins exact model fallback pricing",
			model:        Model{OpenRouterModelID: "acme/exact", InputPer1M: 1_000_000, OutputPer1M: 2_000_000},
			response:     fixture("acme/exact", "Acme", "output", "stop", &zero),
			wantServed:   "acme/exact",
			wantProvider: "Acme",
			wantOutput:   "output",
			wantFinish:   "stop",
			wantTokens:   5,
		},
		{
			name:         "reported cost overrides configured exact model pricing",
			model:        Model{OpenRouterModelID: "acme/exact", InputPer1M: 1_000_000, OutputPer1M: 2_000_000},
			response:     fixture("acme/exact", "Acme", "output", "stop", &reportedCost),
			wantServed:   "acme/exact",
			wantProvider: "Acme",
			wantOutput:   "output",
			wantFinish:   "stop",
			wantCost:     reportedCost,
			wantTokens:   5,
		},
		{
			name:         "dynamic routing requires reported cost",
			model:        Model{OpenRouterModelID: "openrouter/auto", DynamicRouting: true},
			response:     fixture("google/routed", "Google", "output", "stop", nil),
			wantError:    "auto-router response omitted usage.cost",
			wantServed:   "google/routed",
			wantProvider: "Google",
			wantOutput:   "output",
			wantFinish:   "stop",
			wantTokens:   5,
		},
		{
			name:         "dynamic identity error replaces missing cost error",
			model:        Model{OpenRouterModelID: "openrouter/auto", DynamicRouting: true},
			response:     fixture(" ", "Google", "output", "stop", nil),
			wantError:    "auto-router response omitted the routed model identity",
			wantProvider: "Google",
			wantOutput:   "output",
			wantFinish:   "stop",
			wantTokens:   5,
		},
		{
			name:         "unknown exact price remains zero",
			model:        Model{OpenRouterModelID: "acme/exact"},
			response:     fixture("acme/exact", "Acme", "output", "stop", nil),
			wantServed:   "acme/exact",
			wantProvider: "Acme",
			wantOutput:   "output",
			wantFinish:   "stop",
			wantTokens:   5,
		},
		{
			name:      "no choices returns before projected usage",
			model:     Model{OpenRouterModelID: "acme/exact", InputPer1M: 1_000_000},
			response:  noChoices,
			wantError: "expected one response choice, got 0",
		},
		{
			name:      "multiple choices return before projected usage",
			model:     Model{OpenRouterModelID: "acme/exact", InputPer1M: 1_000_000},
			response:  multipleChoices,
			wantError: "expected one response choice, got 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tt.response)
			require.NoError(t, err)
			body := &clientResponsePolicyBody{reader: bytes.NewReader(raw)}
			started := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
			times := []time.Time{started, started.Add(1500 * time.Millisecond)}
			nowIndex := 0
			client := &Client{
				HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					require.Equal(t, http.MethodPost, request.Method)
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Trace-ID": {"trace"}}, Body: body, Request: request}, nil
				})},
				BaseURL: "https://example.test/api",
				APIKey:  "key",
				Now: func() time.Time {
					value := times[nowIndex]
					nowIndex++
					return value
				},
			}

			result := client.Execute(context.Background(), tt.model, "system", "source", 128, 1)
			require.Equal(t, tt.wantError, result.Error)
			require.Equal(t, tt.wantServed, result.ServedModel)
			require.Equal(t, tt.wantProvider, result.ServedProvider)
			require.Equal(t, tt.wantOutput, result.OutputText)
			require.Equal(t, tt.wantFinish, result.FinishReason)
			wantUsage := Usage{}
			if tt.wantTokens != 0 {
				wantUsage = Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: tt.wantTokens, CostUSD: tt.wantCost, TokensPerSecond: 2}
			}
			require.Equal(t, wantUsage, result.Usage)
			require.Equal(t, http.StatusOK, result.HTTPStatus)
			require.Equal(t, []string{"trace"}, result.ResponseHeaders["X-Trace-Id"])
			require.JSONEq(t, string(raw), string(result.RawResponse))
			require.Equal(t, int64(1500), result.LatencyMS)
			require.Equal(t, 1, body.closes)
		})
	}
}
