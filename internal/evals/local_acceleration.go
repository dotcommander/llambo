package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Repeated string literals consolidated by goconst.
const (
	applicationJSON = "application/json"
)

// AccelerationChatClient performs one acceleration probe chat call.
type AccelerationChatClient interface {
	AccelerationChat(context.Context, AccelerationChatRequest) (AccelerationChatResponse, error)
}

// AccelerationChatRequest is one acceleration probe request body.
type AccelerationChatRequest struct {
	Model           string `json:"model"`
	Prompt          string `json:"prompt"`
	MaxOutputTokens int    `json:"max_output_tokens"`
}

// AccelerationChatResponse is the acceleration probe outcome.
type AccelerationChatResponse struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Content      string `json:"content"`
	OutputTokens int    `json:"output_tokens"`
}

// OMLXAccelerationChatClient is the direct, bounded OpenAI-compatible client
// used for the off/on conditions. It is kept separate from settings mutation.
type OMLXAccelerationChatClient struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

// AccelerationChat executes one bounded chat call against the OMLX server.
func (c OMLXAccelerationChatClient) AccelerationChat(ctx context.Context, request AccelerationChatRequest) (AccelerationChatResponse, error) {
	if strings.TrimSpace(request.Prompt) == "" || request.MaxOutputTokens < 1 {
		return AccelerationChatResponse{}, errors.New("prompt and positive maximum output tokens are required")
	}
	model, err := localOMLXModel(request.Model)
	if err != nil {
		return AccelerationChatResponse{}, err
	}
	root, err := validatedOMLXRoot(c.BaseURL)
	if err != nil {
		return AccelerationChatResponse{}, err
	}
	body, err := json.Marshal(struct {
		Model     string            `json:"model"`
		Messages  []ToolChatMessage `json:"messages"`
		MaxTokens int               `json:"max_tokens"`
	}{Model: model, Messages: []ToolChatMessage{{Role: "user", Content: request.Prompt}}, MaxTokens: request.MaxOutputTokens})
	if err != nil {
		return AccelerationChatResponse{}, err
	}
	if len(body) > localAdapterResponseLimit {
		return AccelerationChatResponse{}, fmt.Errorf("acceleration request exceeds %d bytes", localAdapterResponseLimit)
	}
	httpResponse, err := executeLocalOMLXHTTP(ctx, localOMLXHTTPRequest{
		method:        http.MethodPost,
		endpoint:      root + "/v1/chat/completions",
		body:          bytes.NewReader(body),
		contentType:   applicationJSON,
		apiKey:        c.APIKey,
		client:        c.Client,
		responseLimit: localAdapterResponseLimit,
		buildError:    "build acceleration request",
		requestError:  "request acceleration chat",
	})
	if err != nil {
		return AccelerationChatResponse{}, err
	}
	response := httpResponse.response
	defer func() { _ = response.Body.Close() }()
	responseBody := httpResponse.body
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return AccelerationChatResponse{}, fmt.Errorf("acceleration chat HTTP %d: %s", response.StatusCode, boundedErrorText(responseBody))
	}
	var payload struct {
		Model   string `json:"model"`
		Choices []struct {
			Message ToolChatMessage `json:"message"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return AccelerationChatResponse{}, fmt.Errorf("parse acceleration response: %w", err)
	}
	if len(payload.Choices) != 1 || strings.TrimSpace(payload.Choices[0].Message.Content) == "" || payload.Usage.CompletionTokens < 1 {
		return AccelerationChatResponse{}, errors.New("acceleration response requires one nonempty choice and completion usage")
	}
	return AccelerationChatResponse{Provider: "omlx", Model: firstNonEmpty(payload.Model, response.Header.Get("X-Llambo-Model")), Content: payload.Choices[0].Message.Content, OutputTokens: payload.Usage.CompletionTokens}, nil
}

// AccelerationConditionRecord is one ledgered off/on probe with its result.
type AccelerationConditionRecord struct {
	CaseID      string       `json:"case_id"`
	MTPEnabled  bool         `json:"mtp_enabled"`
	Warmup      bool         `json:"warmup"`
	ServedModel string       `json:"served_model"`
	Result      PairedResult `json:"result"`
}

// AccelerationObservation binds one exact-model probe to its paired condition.
type AccelerationObservation struct {
	Request             AccelerationChatRequest
	Case                AccelerationCase
	MTPEnabled          bool
	Warmup              bool
	VerifiedServedModel string
}

// EvaluateAccelerationCondition issues one direct exact-model chat call. The
// caller performs the guarded settings transition; this method deliberately
// does not mutate OMLX state itself.
func EvaluateAccelerationCondition(ctx context.Context, client AccelerationChatClient, observation AccelerationObservation) (AccelerationConditionRecord, error) {
	request, c := observation.Request, observation.Case
	if client == nil || strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.Prompt) == "" || strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Oracle) == "" {
		return AccelerationConditionRecord{}, errors.New("client, model, prompt, case id, and oracle are required")
	}
	if request.MaxOutputTokens < 1 {
		return AccelerationConditionRecord{}, errors.New("positive maximum output tokens are required")
	}
	started := time.Now()
	response, err := client.AccelerationChat(ctx, request)
	latency := time.Since(started)
	if err != nil {
		return AccelerationConditionRecord{}, fmt.Errorf("acceleration chat: %w", err)
	}
	served, err := localServedIdentity(request.Model, response.Provider, response.Model, observation.VerifiedServedModel)
	if err != nil {
		return AccelerationConditionRecord{}, err
	}
	if response.OutputTokens < 1 || latency <= 0 {
		return AccelerationConditionRecord{}, errors.New("acceleration response requires positive output tokens and latency")
	}
	return AccelerationConditionRecord{CaseID: c.ID, MTPEnabled: observation.MTPEnabled, Warmup: observation.Warmup, ServedModel: served, Result: PairedResult{AnswerCorrect: strings.TrimSpace(response.Content) == strings.TrimSpace(c.Oracle), LatencyMS: float64(latency) / float64(time.Millisecond), TokensPerSecond: float64(response.OutputTokens) / latency.Seconds()}}, nil
}

// AccelerationLocalExecutor runs one excluded warm-up for each off/on condition,
// then returns one paired observation for every sealed case. Settings mutation
// and restoration are owned entirely by Guard.
type AccelerationLocalExecutor struct {
	Client        AccelerationChatClient
	Guard         OMLXSettingsGuard
	Model         string
	VerifiedModel string
	MaxTokens     int
	mu            sync.Mutex
	warmed        map[bool]bool
}

// ExecuteLocal runs the guarded off/on acceleration pair for one case.
func (e *AccelerationLocalExecutor) ExecuteLocal(ctx context.Context, _ LocalRunManifest, raw json.RawMessage) (LocalCall, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var c AccelerationCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return LocalCall{}, err
	}
	if e.Client == nil || e.MaxTokens < 1 {
		return LocalCall{}, errors.New("acceleration client and positive token limit are required")
	}
	if e.warmed == nil {
		e.warmed = make(map[bool]bool, 2)
	}
	results := make(map[bool]AccelerationConditionRecord, 2)
	for _, enabled := range []bool{false, true} {
		err := e.Guard.WithMTPEnabled(ctx, enabled, func(runCtx context.Context) error {
			request := AccelerationChatRequest{Model: e.Model, Prompt: c.Prompt, MaxOutputTokens: e.MaxTokens}
			if !e.warmed[enabled] {
				if _, err := EvaluateAccelerationCondition(runCtx, e.Client, AccelerationObservation{Request: request, Case: c, MTPEnabled: enabled, Warmup: true, VerifiedServedModel: e.VerifiedModel}); err != nil {
					return fmt.Errorf("excluded warm-up: %w", err)
				}
				e.warmed[enabled] = true
			}
			record, err := EvaluateAccelerationCondition(runCtx, e.Client, AccelerationObservation{Request: request, Case: c, MTPEnabled: enabled, Warmup: false, VerifiedServedModel: e.VerifiedModel})
			if err != nil {
				return err
			}
			results[enabled] = record
			return nil
		})
		if err != nil {
			return LocalCall{}, err
		}
	}
	out, err := json.Marshal(struct {
		Off []PairedResult `json:"off"`
		On  []PairedResult `json:"on"`
	}{[]PairedResult{results[false].Result}, []PairedResult{results[true].Result}})
	if err != nil {
		return LocalCall{}, err
	}
	return LocalCall{ServedModel: results[true].ServedModel, Output: out}, nil
}
