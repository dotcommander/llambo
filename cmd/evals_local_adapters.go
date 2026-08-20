package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

type commandToolChatClient struct {
	run      *promptRun
	provider string
	config   providers.Config
	timeout  time.Duration
}

func newCommandToolChatClient(errOut io.Writer, selector string, timeout time.Duration) (*commandToolChatClient, error) {
	providerName, model, ok := strings.Cut(strings.TrimSpace(selector), "/")
	if !ok || providerName == "" || model == "" {
		return nil, fmt.Errorf("tool model must be exact provider/model")
	}
	global, err := providers.LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	cfg, ok := global.Providers[providerName]
	if !ok || !cfg.Enabled {
		return nil, fmt.Errorf("provider %q is not enabled", providerName)
	}
	cfg.Model = model
	run, err := newPromptRun(errOut)
	if err != nil {
		return nil, err
	}
	return &commandToolChatClient{run: run, provider: providerName, config: cfg, timeout: timeout}, nil
}

func (c *commandToolChatClient) ToolChat(parent context.Context, request evals.ToolChatRequest) (evals.ToolChatResponse, error) {
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	tools := make([]providers.ToolDefinition, len(request.Tools))
	for i, tool := range request.Tools {
		tools[i] = providers.ToolDefinition{Name: tool.Name, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.Parameters...)}
	}
	choice := &providers.ToolChoice{Type: "auto"}
	if request.RequiredTool != "" {
		choice = &providers.ToolChoice{Type: "tool", Name: request.RequiredTool}
	}
	ctx = providers.WithTools(ctx, tools, choice)
	entry := providers.ProviderEntry{Name: c.provider, Config: c.config}
	release, err := c.run.acquireProvider(ctx, entry)
	if err != nil {
		return evals.ToolChatResponse{}, err
	}
	defer release()
	provider, err := providers.NewOpenAIWithSharedRoutingMetrics(map[string]providers.Config{entry.Name: entry.Config}, c.run.metrics)
	if err != nil {
		return evals.ToolChatResponse{}, err
	}
	defer provider.Shutdown()
	result, err := provider.ChatWithInfoContext(ctx, "Use tools exactly when required by the user request.", request.Messages[0].Content)
	response := evals.ToolChatResponse{Provider: result.Provider, Model: result.Model}
	if result.Usage != nil {
		response.Usage = evals.ToolChatUsage{InputTokens: result.Usage.PromptTokens, OutputTokens: result.Usage.CompletionTokens, Known: true}
	}
	for _, call := range result.ToolCalls {
		if !json.Valid([]byte(call.Arguments)) {
			return response, fmt.Errorf("tool %q returned invalid JSON arguments", call.Name)
		}
		response.ToolCalls = append(response.ToolCalls, evals.ToolCall{Name: call.Name, Arguments: json.RawMessage(call.Arguments)})
	}
	return response, err
}

func (c *commandToolChatClient) Close() {
	if c != nil && c.run != nil {
		c.run.close()
	}
}

func newTypedLocalExecutor(errOut io.Writer, suite evals.LocalSuite, model, referenceModel, featureModel, baseURL, settingsPath string, exclusiveApproved bool, timeout time.Duration, verified, verifiedReference string) (evals.LocalExecutor, func(), error) {
	switch suite {
	case evals.LocalSuiteTools:
		client, err := newCommandToolChatClient(errOut, model, timeout)
		if err != nil {
			return nil, nil, err
		}
		return evals.ToolLocalExecutor{Client: client}, client.Close, nil
	case evals.LocalSuiteEmbeddings:
		return evals.EmbeddingLocalExecutor{Adapter: evals.OMLXEmbeddingAdapter{BaseURL: baseURL, VerifiedServedModel: verified}}, func() {}, nil
	case evals.LocalSuiteASR:
		return evals.ASRLocalExecutor{Adapter: evals.OMLXAudioAdapter{BaseURL: baseURL, VerifiedServedModel: verified}}, func() {}, nil
	case evals.LocalSuiteTTS:
		if strings.TrimSpace(referenceModel) == "" || strings.TrimSpace(verifiedReference) == "" {
			return nil, nil, fmt.Errorf("TTS execution requires --reference-model naming the completed exact ASR model")
		}
		return evals.TTSLocalExecutor{
			Adapter: evals.OMLXAudioAdapter{BaseURL: baseURL, VerifiedServedModel: verified},
			RoundTrip: evals.OMLXTTSRoundTripper{
				Adapter: evals.OMLXAudioAdapter{BaseURL: baseURL, VerifiedServedModel: verifiedReference},
				Model:   referenceModel,
			},
		}, func() {}, nil
	case evals.LocalSuiteAcceleration:
		if !exclusiveApproved {
			return nil, nil, fmt.Errorf("acceleration requires --exclusive-omlx-settings-approved after separate approval")
		}
		if strings.TrimSpace(settingsPath) == "" || !strings.HasPrefix(featureModel, "omlx/") || !strings.HasPrefix(referenceModel, "omlx/") {
			return nil, nil, fmt.Errorf("acceleration requires --omlx-settings, exact --reference-model parent, and exact --feature-model helper")
		}
		parent := strings.TrimPrefix(referenceModel, "omlx/")
		if model != referenceModel {
			return nil, nil, fmt.Errorf("acceleration --model must equal the exact --reference-model parent")
		}
		client := &http.Client{Timeout: timeout}
		reload := func(runCtx context.Context) error { return postOMLXReload(runCtx, client, baseURL) }
		health := func(runCtx context.Context) error {
			_, err := evals.VerifyOMLXExactModel(runCtx, baseURL, "", parent, client)
			return err
		}
		guard := evals.OMLXSettingsGuard{Path: settingsPath, ParentModel: parent, Reload: reload, Health: health}
		return &evals.AccelerationLocalExecutor{Client: evals.OMLXAccelerationChatClient{BaseURL: baseURL, Client: client}, Guard: guard, Model: model, VerifiedModel: verified, MaxTokens: 32768}, func() {}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported local suite %q", suite)
	}
}

func postOMLXReload(ctx context.Context, client *http.Client, baseURL string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/admin/api/reload", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OMLX reload HTTP %d", response.StatusCode)
	}
	return nil
}
