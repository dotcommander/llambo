package providers

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

const (
	defaultOpenAIBaseURL    = "https://api.openai.com/v1"
	defaultAnthropicBaseURL = "https://api.anthropic.com/v1"
	defaultGeminiBaseURL    = "https://generativelanguage.googleapis.com/v1beta"
)

// OpenAIClients holds initialized OpenAI clients and related resources
type OpenAIClients struct {
	Clients        map[string]whtypes.Provider // backend name -> dedicated wormhole provider
	Backends       []Backend                   // ordered list of backends
	CircuitBreaker *CircuitBreaker
	CostTracker    *CostTracker // tracks token usage and costs per provider
	RoutingMetrics *RoutingMetricsStore
	RouteEvents    *RouteEventLogger
	RoutingConfig  RoutingConfig
	KeyRotator     *KeyRotator       // manages multi-key rotation on 429
	configs        map[string]Config // immutable after construction
	logger         *slog.Logger
	mu             sync.RWMutex // protects Clients map
	generations    map[string]*clientGeneration
	limiters       map[string]chan struct{}
	sharedMetrics  bool
	closed         bool
	clientFactory  providerFactory

	// canaryPromoteOnce gates canary auto-promotion for THIS instance only.
	// Promotion is a one-way transition (Routing.Canary set nil after success),
	// so a second attempt is wasted disk IO that can race concurrent canary
	// requests. Per-instance (not package-global) so a fresh OpenAIClients can
	// promote again — across process lifetimes and across tests. Pointer +
	// lazy init (canaryOnce) keeps existing struct literals valid without
	// setting this field.
	canaryPromoteOnce *sync.Once
	canaryOnceInit    sync.Once
}

// canaryOnce returns this instance's canary-promotion gate, lazily allocating
// it so struct literals that omit canaryPromoteOnce stay valid. canaryOnceInit
// makes the allocation itself race-free.
func (oc *OpenAIClients) canaryOnce() *sync.Once {
	oc.canaryOnceInit.Do(func() {
		if oc.canaryPromoteOnce == nil {
			oc.canaryPromoteOnce = &sync.Once{}
		}
	})
	return oc.canaryPromoteOnce
}

// GetClient returns the wormhole provider for a backend (thread-safe).
func (oc *OpenAIClients) GetClient(backendName string) (whtypes.Provider, bool) {
	oc.mu.RLock()
	defer oc.mu.RUnlock()
	if oc.closed {
		return nil, false
	}
	c, ok := oc.Clients[backendName]
	return c, ok
}

// Cleanup releases optional telemetry resources.
func (oc *OpenAIClients) Cleanup() {
	if oc == nil {
		return
	}
	if !oc.sharedMetrics && oc.RoutingMetrics != nil {
		if err := oc.RoutingMetrics.Save(); err != nil {
			oc.loggerOrDefault().Warn("save routing metrics failed", "path", oc.RoutingMetrics.path, "error", err)
		}
	}
	if oc.RouteEvents != nil {
		if err := oc.RouteEvents.close(); err != nil {
			oc.loggerOrDefault().Warn("close route event log failed", "path", oc.RouteEvents.path, "error", err)
		}
	}
	oc.mu.Lock()
	defer oc.mu.Unlock()
	oc.closed = true
	for _, gen := range oc.generations {
		gen.retired = true
		oc.closeRetiredGenerationLocked(gen)
	}
	for name, client := range oc.Clients {
		if _, tracked := oc.generations[name]; !tracked && client != nil {
			_ = client.Close()
		}
	}
}

func (oc *OpenAIClients) loggerOrDefault() *slog.Logger {
	if oc.logger != nil {
		return oc.logger
	}
	return slog.Default()
}

// ErrKeyClientInit indicates that key rotation selected a new key but the
// provider client could not be constructed for it. This is a client-
// construction failure, not a network/quota failure, so it must NOT advance
// circuit-breaker state (see CircuitBreaker.RecordFailure).
var ErrKeyClientInit = errors.New("key rotation: client construction failed")

// RotateKey attempts to rotate to the next API key for a provider after a 429.
// Returns true if rotation succeeded and a new client was created.
//
// Kept for backward compatibility (e.g. the streaming path). Callers that
// must distinguish a client-construction failure from "all keys exhausted"
// should use RotateKeyWithError.
func (oc *OpenAIClients) RotateKey(provider string) bool {
	ok, _ := oc.RotateKeyWithError(provider)
	return ok
}

// RotateKeyWithError rotates to the next API key after a 429, distinguishing
// outcomes:
//   - (true, nil):  rotation succeeded, a new client is installed.
//   - (false, nil): no rotation possible (no rotator, single key, or all
//     keys exhausted) — caller should treat the original error.
//   - (false, err wrapping ErrKeyClientInit): a new key was selected but the
//     provider client could not be constructed. This is a
//     client-construction failure, not a quota failure, and
//     must not advance circuit-breaker state.
func (oc *OpenAIClients) RotateKeyWithError(provider string) (bool, error) {
	if oc.KeyRotator == nil {
		return false, nil
	}
	return oc.rotateKeyWithError(provider, oc.KeyRotator.GetKey(provider))
}

func (oc *OpenAIClients) rotateKeyWithError(provider, usedKey string) (bool, error) {
	return oc.rotateLeasedKeyWithError(provider, usedKey, nil)
}

func (oc *OpenAIClients) rotateKeyForResult(provider string, result chatRequestResult) (bool, error) {
	return oc.rotateLeasedKeyWithError(provider, result.clientKey, result.clientGeneration)
}

// rotateLeasedKeyWithError replaces exactly the generation that served the
// rate-limited request. It constructs a candidate before mutating key state,
// then commits only while that generation is still installed.
func (oc *OpenAIClients) rotateLeasedKeyWithError(provider, usedKey string, expected *clientGeneration) (bool, error) {
	if oc.KeyRotator == nil {
		return false, nil
	}

	// Only rotate if there are multiple keys
	if oc.KeyRotator.KeyCount(provider) <= 1 {
		return false, nil
	}

	oc.mu.RLock()
	current := oc.generations[provider]
	closed := oc.closed
	oc.mu.RUnlock()
	if closed || current == nil {
		return false, nil
	}
	if current.key != usedKey || (expected != nil && current != expected) {
		// Another request already installed a replacement. Retrying against it
		// is correct and, importantly, does not advance past that replacement.
		return true, nil
	}
	if expected == nil {
		expected = current
	}

	newKey, ok := oc.KeyRotator.NextKeyForRotation(provider, usedKey)
	if !ok || newKey == "" || newKey == usedKey {
		return false, nil
	}
	cfg := oc.configs[provider]

	client, err := oc.newClient(provider, cfg, newKey)
	if err != nil {
		return false, fmt.Errorf("%w: %s: %v", ErrKeyClientInit, provider, err)
	}

	oc.mu.Lock()
	if oc.closed || oc.generations[provider] != expected || expected.key != usedKey || !oc.KeyRotator.CommitRateLimit(provider, usedKey, newKey) {
		oc.mu.Unlock()
		_ = client.Close()
		if oc.closed {
			return false, nil
		}
		return true, nil
	}
	oc.Clients[provider] = client
	oc.generations[provider] = &clientGeneration{client: client, key: newKey}
	expected.retired = true
	oc.closeRetiredGenerationLocked(expected)
	oc.mu.Unlock()
	return true, nil
}

func (oc *OpenAIClients) newClient(provider string, cfg Config, key string) (whtypes.Provider, error) {
	if oc.clientFactory != nil {
		return oc.clientFactory(provider, cfg, key)
	}
	return createProviderForConfigWithKey(provider, cfg, key)
}

// GetHealthyBackends returns backends passing circuit breaker health check
func (oc *OpenAIClients) GetHealthyBackends() []Backend {
	var healthy []Backend
	for _, b := range oc.Backends {
		if oc.CircuitBreaker.IsHealthy(b.Name) {
			healthy = append(healthy, b)
		}
	}
	return healthy
}

// CreateOpenAIClients creates OpenAI clients for all enabled backends.
// Each backend gets its own OpenAI client to ensure correct BaseURL routing.
// The callback is optional and will be called on circuit breaker events.
func CreateOpenAIClients(configs map[string]Config) (*OpenAIClients, error) {
	defaultRouting := RoutingConfig{}
	defaultRouting.ApplyDefaults()
	return CreateOpenAIClientsWithRouting(configs, defaultRouting, nil)
}

// CreateOpenAIClientsWithCallback creates OpenAI clients with an optional circuit breaker callback.
func CreateOpenAIClientsWithCallback(configs map[string]Config, callback CircuitBreakerCallback) (*OpenAIClients, error) {
	defaultRouting := RoutingConfig{}
	defaultRouting.ApplyDefaults()
	return CreateOpenAIClientsWithRouting(configs, defaultRouting, callback)
}

// CreateOpenAIClientsWithRouting creates OpenAI clients with intelligent routing settings.
func CreateOpenAIClientsWithRouting(configs map[string]Config, routing RoutingConfig, callback CircuitBreakerCallback) (*OpenAIClients, error) {
	return createOpenAIClientsWithRoutingMetrics(configs, routing, callback, nil)
}

func createOpenAIClientsWithRoutingMetrics(configs map[string]Config, routing RoutingConfig, callback CircuitBreakerCallback, sharedMetrics *RoutingMetricsStore) (*OpenAIClients, error) {
	return createOpenAIClientsWithRoutingMetricsAndFactory(configs, routing, callback, sharedMetrics, createProviderForConfigWithKey)
}

type providerFactory func(string, Config, string) (whtypes.Provider, error)

func createOpenAIClientsWithRoutingMetricsAndFactory(configs map[string]Config, routing RoutingConfig, callback CircuitBreakerCallback, sharedMetrics *RoutingMetricsStore, factory providerFactory) (*OpenAIClients, error) {
	clients := make(map[string]whtypes.Provider)
	gens := make(map[string]*clientGeneration)
	limiters := make(map[string]chan struct{})
	var backends []Backend
	var backendNames []string

	// Create key rotator for multi-key support
	keyRotator := NewKeyRotator(configs)

	// Create a dedicated client for each enabled backend
	for _, entry := range FilterEnabledProviders(configs) {
		client, err := factory(entry.Name, entry.Config, keyRotator.GetKey(entry.Name))
		if err != nil {
			closeProviderMap(clients)
			return nil, fmt.Errorf("provider init for %s: %w", entry.Name, err)
		}

		clients[entry.Name] = client
		gens[entry.Name] = &clientGeneration{client: client, key: keyRotator.GetKey(entry.Name)}
		limiters[entry.Name] = make(chan struct{}, entry.Config.GetWorkers())
		backendNames = append(backendNames, entry.Name)
		backends = append(backends, Backend{Name: entry.Name, Model: entry.Config.Model})
	}

	if len(clients) == 0 {
		return nil, fmt.Errorf("no backends enabled - check ~/.config/llambo/config.json and ensure at least one provider has \"enabled\": true")
	}

	routing.ApplyDefaults()
	metricsStore := sharedMetrics
	hasSharedMetrics := metricsStore != nil
	if metricsStore == nil {
		var err error
		metricsStore, err = NewRoutingMetricsStore(routing.MetricsPath)
		if err != nil {
			closeProviderMap(clients)
			return nil, fmt.Errorf("init routing metrics store: %w", err)
		}
	}

	return &OpenAIClients{
		Clients:        clients,
		Backends:       backends,
		CircuitBreaker: NewCircuitBreaker(backendNames, callback),
		CostTracker:    NewCostTracker(backendNames),
		RoutingMetrics: metricsStore,
		RouteEvents:    NewRouteEventLogger(routing.EventsPath),
		RoutingConfig:  routing,
		KeyRotator:     keyRotator,
		configs:        configs,
		logger:         slog.Default(),
		generations:    gens,
		limiters:       limiters,
		sharedMetrics:  hasSharedMetrics,
		clientFactory:  factory,
	}, nil
}

func closeProviderMap(clients map[string]whtypes.Provider) {
	for _, client := range clients {
		if client != nil {
			_ = client.Close()
		}
	}
}
