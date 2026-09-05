package providers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewOpenAIWithSharedRoutingMetricsRetainsMetricsOwnership(t *testing.T) {
	t.Parallel()

	metricsPath := filepath.Join(t.TempDir(), "metrics.json")
	metrics, err := NewRoutingMetricsStore(metricsPath)
	require.NoError(t, err)
	provider, err := NewOpenAIWithSharedRoutingMetrics(testOpenAIProviderConfigs(), metrics)
	require.NoError(t, err)
	t.Cleanup(provider.Shutdown)
	require.Same(t, metrics, provider.GetOpenAIClients().RoutingMetrics)
	provider.Shutdown()
	_, err = os.Stat(metricsPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestNewOpenAIWithRoutingCallbacksPropagatesConstructionInputs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	routing := RoutingConfig{
		Mode:        string(RoutingModeFastest),
		MetricsPath: filepath.Join(dir, "metrics.json"),
		EventsPath:  filepath.Join(dir, "events.jsonl"),
	}
	var circuitEvents []CircuitBreakerEvent
	var failoverEvents []FailoverEvent
	provider, err := NewOpenAIWithRoutingCallbacks(testOpenAIProviderConfigs(), routing, func(event CircuitBreakerEvent) {
		circuitEvents = append(circuitEvents, event)
	}, func(event FailoverEvent) {
		failoverEvents = append(failoverEvents, event)
	})
	require.NoError(t, err)
	t.Cleanup(provider.Shutdown)

	require.Equal(t, DefaultMaxTokens+321, provider.MaxTokens())
	require.Equal(t, string(RoutingModeFastest), provider.GetOpenAIClients().RoutingConfig.Mode)
	provider.GetOpenAIClients().CircuitBreaker.RecordFailure("test", errors.New("failure"))
	provider.GetOpenAIClients().CircuitBreaker.RecordFailure("test", errors.New("failure"))
	provider.GetOpenAIClients().CircuitBreaker.RecordFailure("test", errors.New("failure"))
	require.Len(t, circuitEvents, 1)
	event := circuitEvents[0]
	require.Equal(t, "test", event.Backend)
	require.Equal(t, "disabled", event.EventType)
	provider.failoverCallback(FailoverEvent{FromBackend: "test", ToBackend: "fallback"})
	require.Len(t, failoverEvents, 1)
	failover := failoverEvents[0]
	require.Equal(t, "fallback", failover.ToBackend)
	provider.Shutdown()
	_, err = os.Stat(routing.MetricsPath)
	require.NoError(t, err)
}

func TestNewOpenAIConstructionPropagatesEmptyBackendErrors(t *testing.T) {
	t.Parallel()

	metrics, err := NewRoutingMetricsStore(filepath.Join(t.TempDir(), "metrics.json"))
	require.NoError(t, err)
	_, err = NewOpenAIWithSharedRoutingMetrics(map[string]Config{}, metrics)
	require.ErrorContains(t, err, "no backends enabled")
	_, err = NewOpenAIWithRoutingCallbacks(map[string]Config{}, RoutingConfig{MetricsPath: filepath.Join(t.TempDir(), "metrics.json")}, nil, nil)
	require.ErrorContains(t, err, "no backends enabled")
}

func testOpenAIProviderConfigs() map[string]Config {
	return map[string]Config{
		"test": {
			BaseURL:      "http://127.0.0.1:1",
			Model:        "test-model",
			Enabled:      true,
			RequiresKey:  false,
			MaxTokens:    DefaultMaxTokens + 321,
			ProviderType: "openai",
		},
	}
}
