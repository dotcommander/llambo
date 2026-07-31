package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	whopenai "github.com/garyblankenship/wormhole/v3/providers/openai"
	"github.com/garyblankenship/wormhole/v3/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIEmbedding_Embed(t *testing.T) {
	t.Parallel()

	// noRetry disables wormhole's internal retry/backoff so the stubbed HTTP 500
	// in RequestError returns immediately instead of retrying with backoff (~7s).
	noRetry := 0

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		// Canned embeddings response: single vector [0.1,0.2,0.3], index 0.
		const body = `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":1,"total_tokens":1}}`

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)

		e := &OpenAIEmbedding{
			provider:   whopenai.New(types.ProviderConfig{APIKey: "test", BaseURL: srv.URL, MaxRetries: &noRetry}),
			model:      "text-embedding-3-small",
			dimensions: 3,
			batchSize:  100,
		}

		result, err := e.Embed(context.Background(), []string{"hello"})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Len(t, result[0], 3)
		assert.InDeltaSlice(t, []float32{0.1, 0.2, 0.3}, result[0], 1e-6)
	})

	t.Run("RequestError", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		e := &OpenAIEmbedding{
			provider:   whopenai.New(types.ProviderConfig{APIKey: "test", BaseURL: srv.URL, MaxRetries: &noRetry}),
			model:      "text-embedding-3-small",
			dimensions: 3,
			batchSize:  100,
		}

		result, err := e.Embed(context.Background(), []string{"hello"})
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "embedding request failed")
	})
}
