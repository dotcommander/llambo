package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

type closeTrackingProvider struct {
	fakeTextProvider
	closes atomic.Int32
}

func (p *closeTrackingProvider) Close() error {
	p.closes.Add(1)
	return nil
}

func newRotationTestClients(factory providerFactory) (*OpenAIClients, *clientGeneration) {
	const provider = "testprovider"
	cfg := Config{APIKeys: []string{"key-1", "key-2", "key-3"}}
	initial := &closeTrackingProvider{}
	gen := &clientGeneration{client: initial, key: "key-1"}
	return &OpenAIClients{
		Clients:       map[string]whtypes.Provider{provider: initial},
		KeyRotator:    NewKeyRotator(map[string]Config{provider: cfg}),
		configs:       map[string]Config{provider: cfg},
		generations:   map[string]*clientGeneration{provider: gen},
		clientFactory: factory,
	}, gen
}

func TestRotateKeyConstructionFailureLeavesGenerationAndKeyStateUnchanged(t *testing.T) {
	const provider = "testprovider"
	original := &closeTrackingProvider{}
	oc, gen := newRotationTestClients(func(string, Config, string) (whtypes.Provider, error) {
		return nil, errors.New("construction failed")
	})
	gen.client = original
	oc.Clients[provider] = original

	rotated, err := oc.rotateLeasedKeyWithError(provider, "key-1", gen)
	if rotated {
		t.Fatal("rotation unexpectedly succeeded")
	}
	if !errors.Is(err, ErrKeyClientInit) {
		t.Fatalf("rotation error = %v, want ErrKeyClientInit", err)
	}
	if got := oc.KeyRotator.GetKey(provider); got != "key-1" {
		t.Fatalf("current key = %q, want key-1", got)
	}
	if got := oc.generations[provider]; got != gen {
		t.Fatal("construction failure replaced the installed generation")
	}
	states := oc.KeyRotator.GetKeyStates(provider)
	if states[0].RateLimited {
		t.Fatal("construction failure marked the leased key rate-limited")
	}
}

func TestRotateKeyStaleCandidateCannotOverwriteNewerGeneration(t *testing.T) {
	const provider = "testprovider"
	firstK2Started := make(chan struct{})
	releaseFirstK2 := make(chan struct{})
	firstCandidate := &closeTrackingProvider{}
	secondCandidate := &closeTrackingProvider{}
	thirdCandidate := &closeTrackingProvider{}
	var k2Calls atomic.Int32

	oc, firstGen := newRotationTestClients(func(_ string, _ Config, key string) (whtypes.Provider, error) {
		switch key {
		case "key-2":
			if k2Calls.Add(1) == 1 {
				close(firstK2Started)
				<-releaseFirstK2
				return firstCandidate, nil
			}
			return secondCandidate, nil
		case "key-3":
			return thirdCandidate, nil
		default:
			t.Fatalf("unexpected candidate key %q", key)
			return nil, nil
		}
	})

	type rotationResult struct {
		rotated bool
		err     error
	}
	firstDone := make(chan rotationResult, 1)
	go func() {
		rotated, err := oc.rotateLeasedKeyWithError(provider, "key-1", firstGen)
		firstDone <- rotationResult{rotated: rotated, err: err}
	}()
	<-firstK2Started

	rotated, err := oc.rotateLeasedKeyWithError(provider, "key-1", firstGen)
	if err != nil || !rotated {
		t.Fatalf("second rotation = (%t, %v), want success", rotated, err)
	}
	secondGen := oc.generations[provider]
	if secondGen.key != "key-2" {
		t.Fatalf("second generation key = %q, want key-2", secondGen.key)
	}

	rotated, err = oc.rotateLeasedKeyWithError(provider, "key-2", secondGen)
	if err != nil || !rotated {
		t.Fatalf("third rotation = (%t, %v), want success", rotated, err)
	}
	close(releaseFirstK2)
	result := <-firstDone
	if result.err != nil || !result.rotated {
		t.Fatalf("stale first rotation = (%t, %v), want reusable installed generation", result.rotated, result.err)
	}

	if got := oc.generations[provider].key; got != "key-3" {
		t.Fatalf("installed generation key = %q, want key-3", got)
	}
	if got := oc.KeyRotator.GetKey(provider); got != "key-3" {
		t.Fatalf("rotator key = %q, want key-3", got)
	}
	if got := firstCandidate.closes.Load(); got != 1 {
		t.Fatalf("stale candidate closes = %d, want 1", got)
	}
}

func TestRotateKeyCandidateClosesWhenCleanupWins(t *testing.T) {
	const provider = "testprovider"
	constructionStarted := make(chan struct{})
	allowConstruction := make(chan struct{})
	candidate := &closeTrackingProvider{}
	oc, initialGen := newRotationTestClients(func(string, Config, string) (whtypes.Provider, error) {
		close(constructionStarted)
		<-allowConstruction
		return candidate, nil
	})
	initial := initialGen.client.(*closeTrackingProvider)

	done := make(chan struct {
		rotated bool
		err     error
	}, 1)
	go func() {
		rotated, err := oc.rotateLeasedKeyWithError(provider, "key-1", initialGen)
		done <- struct {
			rotated bool
			err     error
		}{rotated, err}
	}()
	<-constructionStarted
	oc.Cleanup()
	close(allowConstruction)
	result := <-done
	if result.err != nil || result.rotated {
		t.Fatalf("rotation after cleanup = (%t, %v), want fenced failure", result.rotated, result.err)
	}
	if got := candidate.closes.Load(); got != 1 {
		t.Fatalf("post-cleanup candidate closes = %d, want 1", got)
	}
	if got := initial.closes.Load(); got != 1 {
		t.Fatalf("installed generation closes = %d, want 1", got)
	}
	if got := oc.KeyRotator.GetKey(provider); got != "key-1" {
		t.Fatalf("rotator key = %q, want key-1", got)
	}
}

func TestCreateOpenAIClientsRollsBackPartiallyConstructedClients(t *testing.T) {
	first := &closeTrackingProvider{}
	configs := map[string]Config{
		"alpha": {Enabled: true, Priority: 1, RequiresKey: false},
		"beta":  {Enabled: true, Priority: 2, RequiresKey: false},
	}
	_, err := createOpenAIClientsWithRoutingMetricsAndFactory(configs, RoutingConfig{}, nil, nil, func(name string, _ Config, _ string) (whtypes.Provider, error) {
		if name == "beta" {
			return nil, errors.New("beta construction failed")
		}
		return first, nil
	})
	if err == nil {
		t.Fatal("factory construction unexpectedly succeeded")
	}
	if got := first.closes.Load(); got != 1 {
		t.Fatalf("partially constructed client closes = %d, want 1", got)
	}
}

type baseURLProvider interface {
	GetBaseURL() string
}

func TestCreateProviderForConfigWithKeyUsesEffectiveBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      Config
		expected string
	}{
		{
			name:     "openai default",
			cfg:      Config{Model: "gpt-test", ProviderType: "openai", RequiresKey: true},
			expected: defaultOpenAIBaseURL,
		},
		{
			name:     "openai compatible normalizes version suffix",
			cfg:      Config{BaseURL: "https://openrouter.ai/api", Model: "gpt-test", ProviderType: "openai", RequiresKey: true},
			expected: "https://openrouter.ai/api/v1",
		},
		{
			name:     "anthropic default",
			cfg:      Config{Model: "claude-test", ProviderType: "anthropic", RequiresKey: true},
			expected: defaultAnthropicBaseURL,
		},
		{
			name:     "gemini default",
			cfg:      Config{Model: "gemini-test", ProviderType: "gemini", RequiresKey: true},
			expected: defaultGeminiBaseURL,
		},
		{
			name:     "gemini preserves native version path",
			cfg:      Config{BaseURL: "https://generativelanguage.googleapis.com/v1beta/", Model: "gemini-test", ProviderType: "gemini", RequiresKey: true},
			expected: "https://generativelanguage.googleapis.com/v1beta",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := createProviderForConfigWithKey("test-provider", tt.cfg, "test-key")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })

			withBaseURL, ok := client.(baseURLProvider)
			require.True(t, ok)
			require.Equal(t, tt.expected, withBaseURL.GetBaseURL())
		})
	}
}

func TestCreateProviderForConfigWithKeyGeminiPreservesAPIKeyQuery(t *testing.T) {
	t.Parallel()

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		require.Equal(t, "/models/gemini-test:generateContent", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{
				"finishReason": "STOP",
				"content": map[string]any{
					"parts": []map[string]string{{"text": "OK"}},
				},
			}},
		})
	}))
	t.Cleanup(server.Close)

	client, err := createProviderForConfigWithKey("gemini", Config{
		BaseURL:      server.URL,
		Model:        "gemini-test",
		ProviderType: "gemini",
		RequiresKey:  true,
	}, "test-api-key")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	resp, err := client.Text(context.Background(), whtypes.TextRequest{
		BaseRequest: whtypes.BaseRequest{Model: "gemini-test"},
		Messages:    []whtypes.Message{whtypes.NewUserMessage("ping")},
	})
	require.NoError(t, err)
	require.Equal(t, "OK", resp.Text)
	require.Equal(t, "key=test-api-key", gotQuery)
}
