package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

// A provider configured requires_key=false with no key at all (e.g. LM Studio
// on localhost) must issue requests without an Authorization header instead
// of failing client-side in wormhole's Bearer strategy.
func TestKeylessProviderSendsRequestWithoutAuthorization(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests int
	var authHeaders []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"1","object":"chat.completion","created":0,"model":"local-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		ProviderType: "openai",
		BaseURL:      srv.URL,
		Model:        "local-model",
		RequiresKey:  false,
	}
	client, err := createProviderForConfigWithKey("lmstudio", cfg, "")
	if err != nil {
		t.Fatalf("provider construction failed: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	resp, err := client.Text(context.Background(), whtypes.TextRequest{
		SystemPrompt: "be terse",
		Messages:     []whtypes.Message{whtypes.NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("Text() failed for keyless provider: %v", err)
	}
	if resp == nil || resp.Content() == "" {
		t.Fatalf("expected non-empty response content, got %+v", resp)
	}

	mu.Lock()
	defer mu.Unlock()
	if requests == 0 {
		t.Fatal("no request reached the local server")
	}
	for _, h := range authHeaders {
		if h != "" {
			t.Fatalf("Authorization header sent by keyless provider: %q", h)
		}
	}
}
