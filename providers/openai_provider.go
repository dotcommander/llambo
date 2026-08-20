package providers

import (
	"context"
	"strings"
	"time"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

// FailoverEvent represents a failover attempt
type FailoverEvent struct {
	FromBackend string
	ToBackend   string
	ToModel     string
	Error       error
}

// FailoverCallback is called on failover events
type FailoverCallback func(event FailoverEvent)

// NormalizeModelName strips common provider prefixes from model names
func NormalizeModelName(model string) string {
	model = strings.TrimPrefix(model, "mistralai/")
	model = strings.TrimPrefix(model, "anthropic/")
	model = strings.TrimPrefix(model, "hf:zai-org/")
	model = strings.TrimPrefix(model, "hf:")
	model = strings.TrimPrefix(model, "openai/")
	model = strings.TrimPrefix(model, "google/")
	return model
}

// OpenAIProvider wraps OpenAI clients for unified multi-provider access
// Each backend gets its own client to ensure correct BaseURL routing
type OpenAIProvider struct {
	oc               *OpenAIClients    // holds clients, backends, circuit breaker
	configs          map[string]Config // provider name -> config
	maxTokens        int
	requestCounter   uint64 // for round-robin load balancing
	failoverCallback FailoverCallback
}

// NewOpenAI creates an OpenAI-backed provider
// Each backend gets its own OpenAI client to ensure correct BaseURL routing
func NewOpenAI(configs map[string]Config) (*OpenAIProvider, error) {
	defaultRouting := RoutingConfig{}
	defaultRouting.ApplyDefaults()
	return NewOpenAIWithRoutingCallbacks(configs, defaultRouting, nil, nil)
}

// NewOpenAIWithSharedRoutingMetrics creates a provider using default routing
// and an externally owned routing metrics store. The caller is responsible for
// saving the store; Shutdown only closes provider resources.
func NewOpenAIWithSharedRoutingMetrics(configs map[string]Config, metrics *RoutingMetricsStore) (*OpenAIProvider, error) {
	defaultRouting := RoutingConfig{}
	defaultRouting.ApplyDefaults()
	oc, err := createOpenAIClientsWithRoutingMetrics(configs, defaultRouting, nil, metrics)
	if err != nil {
		return nil, err
	}
	return &OpenAIProvider{
		oc:        oc,
		configs:   configs,
		maxTokens: MaxTokensFromConfigs(configs, DefaultMaxTokens),
	}, nil
}

// NewOpenAIWithCallbacks creates an OpenAI provider with optional event callbacks
func NewOpenAIWithCallbacks(configs map[string]Config, cbCallback CircuitBreakerCallback, failoverCallback FailoverCallback) (*OpenAIProvider, error) {
	defaultRouting := RoutingConfig{}
	defaultRouting.ApplyDefaults()
	return NewOpenAIWithRoutingCallbacks(configs, defaultRouting, cbCallback, failoverCallback)
}

// NewOpenAIWithRoutingCallbacks creates an OpenAI provider with routing config and optional callbacks.
func NewOpenAIWithRoutingCallbacks(configs map[string]Config, routing RoutingConfig, cbCallback CircuitBreakerCallback, failoverCallback FailoverCallback) (*OpenAIProvider, error) {
	oc, err := CreateOpenAIClientsWithRouting(configs, routing, cbCallback)
	if err != nil {
		return nil, err
	}

	return &OpenAIProvider{
		oc:               oc,
		configs:          configs,
		maxTokens:        MaxTokensFromConfigs(configs, DefaultMaxTokens),
		failoverCallback: failoverCallback,
	}, nil
}

// SetFailoverCallback sets the callback for failover events
func (p *OpenAIProvider) SetFailoverCallback(callback FailoverCallback) {
	p.failoverCallback = callback
}

func (p *OpenAIProvider) Name() string {
	return "openai"
}

func (p *OpenAIProvider) MaxTokens() int {
	return p.maxTokens
}

// providerInfo holds provider details for load balancing
type providerInfo struct {
	name   string
	cfg    Config
	weight int // workers = weight for load distribution
}

// Chat sends a request through OpenAI with automatic failover.
// ctx must be non-nil; a per-request timeout (DefaultRequestTimeout)
// is layered on top.
func (p *OpenAIProvider) Chat(ctx context.Context, systemPrompt, userContent string) (string, error) {
	result, err := p.ChatWithInfo(ctx, systemPrompt, userContent)
	return result.Content, err
}

// ChatWithInfo sends a request using weighted round-robin load balancing.
// ctx must be non-nil; a per-request timeout (DefaultRequestTimeout)
// is layered on top.
func (p *OpenAIProvider) ChatWithInfo(ctx context.Context, systemPrompt, userContent string) (ChatResult, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultRequestTimeout)
	defer cancel()
	return p.ChatWithInfoContext(ctx, systemPrompt, userContent)
}

// ChatWithInfoContext sends a request with explicit context control
func (p *OpenAIProvider) ChatWithInfoContext(ctx context.Context, systemPrompt, userContent string) (ChatResult, error) {
	plan, err := p.coordinator().plan(systemPrompt, userContent, false)
	if err != nil {
		return ChatResult{}, err
	}
	outcome, err := p.coordinator().execute(ctx, plan, systemPrompt, userContent, p.failoverCallback, p.executeChatAttempt)
	if err != nil {
		return ChatResult{}, err
	}
	return p.chatResultFromOutcome(outcome), nil
}

// ChatStreamWithInfoContext streams response deltas and returns final metadata.
func (p *OpenAIProvider) ChatStreamWithInfoContext(ctx context.Context, systemPrompt, userContent string, onChunk ChatStreamHandler) (ChatResult, error) {
	plan, err := p.coordinator().plan(systemPrompt, userContent, false)
	if err != nil {
		return ChatResult{}, err
	}
	outcome, err := p.coordinator().executeStream(ctx, plan, systemPrompt, userContent, onChunk, p.executeStreamAttempt)
	if err != nil {
		return ChatResult{}, err
	}
	return p.chatResultFromOutcome(outcome), nil
}

// collectEnabledProviders gathers enabled and healthy providers with weights
func (p *OpenAIProvider) collectEnabledProviders() ([]providerInfo, int) {
	return p.coordinator().collectEnabledProviders()
}

// selectWeightedProvider uses weighted round-robin to pick a provider
func (p *OpenAIProvider) selectWeightedProvider(enabled []providerInfo, totalWeight int) providerInfo {
	return p.coordinator().selectWeightedProvider(enabled, totalWeight)
}

func decisionReasonForProvider(decision *RouteDecision, provider string) (string, bool) {
	if decision == nil {
		return "", false
	}
	for _, c := range decision.Candidates {
		if c.Provider == provider {
			return c.Reason, true
		}
	}
	return "", false
}

func decisionCandidates(decision *RouteDecision) []CandidateScore {
	if decision == nil || len(decision.Candidates) == 0 {
		return nil
	}
	out := make([]CandidateScore, len(decision.Candidates))
	copy(out, decision.Candidates)
	return out
}

func (p *OpenAIProvider) coordinator() executionCoordinator {
	return newExecutionCoordinator(p.oc, p.configs, &p.requestCounter)
}

func (p *OpenAIProvider) executeChatAttempt(ctx context.Context, info providerInfo, systemPrompt, userContent string) (chatRequestResult, error) {
	return p.chatRequestWithKeyRotation(ctx, info.name, info.cfg, systemPrompt, userContent)
}

func (p *OpenAIProvider) executeStreamAttempt(ctx context.Context, info providerInfo, systemPrompt, userContent string, onChunk ChatStreamHandler) (chatRequestResult, bool, error) {
	withMetadata := func(chunk ChatStreamChunk) error {
		if chunk.Provider == "" {
			chunk.Provider = info.name
		}
		if chunk.Model == "" {
			chunk.Model = info.cfg.Model
		}
		if onChunk == nil {
			return nil
		}
		return onChunk(chunk)
	}
	return p.chatStreamRequestWithKeyRotation(ctx, info.name, info.cfg, systemPrompt, userContent, withMetadata)
}

func (p *OpenAIProvider) chatResultFromOutcome(outcome chatExecutionOutcome) ChatResult {
	// Wormhole's response Provider identifies the wire protocol implementation
	// (for example "openai"), not Llambo's configured backend. Preserve the
	// routed backend here while still using the provider-reported served model.
	providerName := outcome.provider.name
	modelName := outcome.result.actualModel
	if modelName == "" {
		modelName = outcome.provider.cfg.Model
	}
	return ChatResult{
		Content:      outcome.result.content,
		Provider:     providerName,
		Model:        modelName,
		Usage:        outcome.result.usage,
		FinishReason: outcome.result.finishReason,
		ToolCalls:    outcome.result.toolCalls,
		Route:        outcome.decision,
	}
}

// chatRequestWithKeyRotation attempts a chat request, rotating keys on 429 errors
func (p *OpenAIProvider) chatRequestWithKeyRotation(ctx context.Context, backendName string, cfg Config, systemPrompt, userContent string) (chatRequestResult, error) {
	return executeChatAttemptWithKeyRotation(ctx, backendName, cfg, systemPrompt, userContent, p.rotateKeyWithErrorForResult, p.chatRequest)
}

func (p *OpenAIProvider) chatStreamRequestWithKeyRotation(ctx context.Context, backendName string, cfg Config, systemPrompt, userContent string, onChunk ChatStreamHandler) (chatRequestResult, bool, error) {
	result, emitted, err := p.chatStreamRequest(ctx, backendName, cfg, systemPrompt, userContent, onChunk)
	if err == nil {
		return result, emitted, nil
	}

	if !emitted && IsRateLimitError(err) && p.rotateKeyForResult(backendName, result) {
		return p.chatStreamRequest(ctx, backendName, cfg, systemPrompt, userContent, onChunk)
	}

	return result, emitted, err
}

// chatRequestResult holds content and usage from a chat request
type chatRequestResult struct {
	content          string
	usage            *LLMUsage
	finishReason     string
	toolCalls        []ToolCall
	actualProvider   string
	actualModel      string
	duration         time.Duration
	clientKey        string
	clientGeneration *clientGeneration
}

// chatRequest uses Chat Completions API with per-backend client
func (p *OpenAIProvider) chatRequest(ctx context.Context, backendName string, cfg Config, systemPrompt, userContent string) (chatRequestResult, error) {
	start := time.Now()
	lease, err := p.oc.acquireClient(ctx, backendName)
	if err != nil {
		return finalizeChatRequest(backendName, cfg, start, "", nil, "", nil, err)
	}
	defer p.oc.releaseAttempt(backendName, lease)

	content, usage, finishReason, toolCalls, identity, err := executeChatRequestWithIdentity(ctx, lease.Client(), cfg, systemPrompt, userContent, DefaultChatConfig)
	result, err := finalizeChatRequest(backendName, cfg, start, content, usage, finishReason, toolCalls, err)
	result.actualProvider = identity.provider
	result.actualModel = identity.model
	result.clientKey = lease.keyForRotation()
	result.clientGeneration = lease.generationForRotation()
	return result, err
}

func (p *OpenAIProvider) chatStreamRequest(ctx context.Context, backendName string, cfg Config, systemPrompt, userContent string, onChunk ChatStreamHandler) (chatRequestResult, bool, error) {
	start := time.Now()
	lease, err := p.oc.acquireClient(ctx, backendName)
	if err != nil {
		result, err := finalizeChatRequest(backendName, cfg, start, "", nil, "", nil, err)
		return result, false, err
	}
	defer p.oc.releaseAttempt(backendName, lease)

	content, usage, finishReason, toolCalls, emitted, identity, err := executeChatStreamRequestWithIdentity(ctx, lease.Client(), cfg, systemPrompt, userContent, onChunk, DefaultChatConfig)
	result, reqErr := finalizeChatRequest(backendName, cfg, start, content, usage, finishReason, toolCalls, err)
	result.actualProvider = identity.provider
	result.actualModel = identity.model
	result.clientKey = lease.keyForRotation()
	result.clientGeneration = lease.generationForRotation()
	return result, emitted, reqErr
}

func (p *OpenAIProvider) rotateKeyForResult(backend string, result chatRequestResult) bool {
	rotated, _ := p.rotateKeyWithErrorForResult(backend, result)
	return rotated
}

func (p *OpenAIProvider) rotateKeyWithErrorForResult(backend string, result chatRequestResult) (bool, error) {
	return p.oc.rotateLeasedKeyWithError(backend, result.clientKey, result.clientGeneration)
}

// GetOpenAIClients returns the underlying OpenAIClients for sharing with BackendQueue
func (p *OpenAIProvider) GetOpenAIClients() *OpenAIClients {
	return p.oc
}

func (p *OpenAIProvider) Shutdown() {
	p.oc.Cleanup()
}

// Backend represents one provider endpoint
type Backend struct {
	Name  string
	Model string
}

// createMockOpenAIClients creates mock clients for testing (no real API calls)
func createMockOpenAIClients(backends []string, configs map[string]Config) *OpenAIClients {
	var backendList []Backend
	for _, name := range backends {
		cfg := configs[name]
		backendList = append(backendList, Backend{
			Name:  name,
			Model: cfg.Model,
		})
	}

	return &OpenAIClients{
		Clients:        make(map[string]whtypes.Provider), // zero clients - tests must use methods that don't call real APIs
		Backends:       backendList,
		CircuitBreaker: NewCircuitBreaker(backends, nil),
		CostTracker:    NewCostTracker(backends),
		RoutingConfig:  RoutingConfig{Mode: "balanced"},
	}
}
