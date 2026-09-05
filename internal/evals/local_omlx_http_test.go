package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type localOMLXHTTPRoundTripper func(*http.Request) (*http.Response, error)

func (f localOMLXHTTPRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type localOMLXHTTPBody struct {
	reader  io.Reader
	closes  int
	onClose func()
}

func (b *localOMLXHTTPBody) Read(p []byte) (int, error) { return b.reader.Read(p) }

func (b *localOMLXHTTPBody) Close() error {
	b.closes++
	if b.onClose != nil {
		b.onClose()
	}
	return nil
}

type localOMLXHTTPReader func([]byte) (int, error)

func (r localOMLXHTTPReader) Read(p []byte) (int, error) { return r(p) }

func TestLocalOMLXHTTPBuildsAuthorizedBoundedRequest(t *testing.T) {
	t.Parallel()
	requestBody, err := json.Marshal(struct {
		Input string `json:"input"`
	}{Input: "hello"})
	require.NoError(t, err)
	responseBody, err := json.Marshal(struct {
		OK bool `json:"ok"`
	}{OK: true})
	require.NoError(t, err)
	body := &localOMLXHTTPBody{reader: bytes.NewReader(responseBody)}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "present")
	client := &http.Client{Transport: localOMLXHTTPRoundTripper(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "present", request.Context().Value(contextKey{}))
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v1/test", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		require.Equal(t, "Bearer secret", request.Header.Get("Authorization"))
		got, readErr := io.ReadAll(request.Body)
		require.NoError(t, readErr)
		require.Equal(t, requestBody, got)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: request}, nil
	})}

	result, err := executeLocalOMLXHTTP(ctx, localOMLXHTTPRequest{
		method:        http.MethodPost,
		endpoint:      "http://127.0.0.1:8000/v1/test",
		body:          bytes.NewReader(requestBody),
		contentType:   "application/json",
		apiKey:        "secret",
		client:        client,
		responseLimit: int64(len(responseBody)),
		buildError:    "build test request",
		requestError:  "request test endpoint",
	})
	require.NoError(t, err)
	require.Equal(t, responseBody, result.body)
	require.Zero(t, result.elapsed)
	require.Equal(t, 0, body.closes)
	require.NoError(t, result.response.Body.Close())
	require.Equal(t, 1, body.closes)
}

func TestLocalOMLXHTTPErrorsAndResponseOwnership(t *testing.T) {
	t.Parallel()
	readFailure := errors.New("read failure")
	buildDispatches := 0
	tests := []struct {
		name     string
		spec     localOMLXHTTPRequest
		want     string
		match    error
		body     *localOMLXHTTPBody
		dispatch *int
	}{
		{
			name: "build error does not dispatch",
			spec: localOMLXHTTPRequest{
				method: "\n", endpoint: "http://127.0.0.1:8000", responseLimit: 1,
				buildError: "build test request", requestError: "request test endpoint",
				client: &http.Client{Transport: localOMLXHTTPRoundTripper(func(*http.Request) (*http.Response, error) {
					buildDispatches++
					return nil, errors.New("should not dispatch")
				})},
			},
			want: "build test request:", dispatch: &buildDispatches,
		},
		{
			name: "transport cancellation retains identity",
			spec: localOMLXHTTPRequest{
				method: http.MethodPost, endpoint: "http://127.0.0.1:8000", responseLimit: 1,
				buildError: "build test request", requestError: "request test endpoint",
				client: &http.Client{Transport: localOMLXHTTPRoundTripper(func(*http.Request) (*http.Response, error) {
					return nil, context.Canceled
				})},
			},
			want: "request test endpoint:", match: context.Canceled,
		},
		{
			name: "bounded read failure closes once and retains identity",
			spec: localOMLXHTTPRequest{
				method: http.MethodPost, endpoint: "http://127.0.0.1:8000", responseLimit: 1,
				buildError: "build test request", requestError: "request test endpoint",
			},
			want: "read response:", match: readFailure,
			body: &localOMLXHTTPBody{reader: localOMLXHTTPReader(func([]byte) (int, error) { return 0, readFailure })},
		},
		{
			name: "response limit closes once",
			spec: localOMLXHTTPRequest{
				method: http.MethodPost, endpoint: "http://127.0.0.1:8000", responseLimit: 1,
				buildError: "build test request", requestError: "request test endpoint",
			},
			want: "response exceeds 1 bytes",
			body: &localOMLXHTTPBody{reader: bytes.NewReader([]byte("ab"))},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := tt.spec
			if tt.body != nil {
				spec.client = &http.Client{Transport: localOMLXHTTPRoundTripper(func(request *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: tt.body, Request: request}, nil
				})}
			}
			_, err := executeLocalOMLXHTTP(context.Background(), spec)
			require.Error(t, err)
			require.True(t, strings.HasPrefix(err.Error(), tt.want), err)
			if tt.match != nil {
				require.ErrorIs(t, err, tt.match)
			}
			if tt.body != nil {
				require.Equal(t, 1, tt.body.closes)
			}
			if tt.dispatch != nil {
				require.Zero(t, *tt.dispatch)
			}
		})
	}
}

func TestLocalOMLXHTTPClosesResponseOnReadPanic(t *testing.T) {
	t.Parallel()
	body := &localOMLXHTTPBody{reader: localOMLXHTTPReader(func([]byte) (int, error) { panic("read failed") })}
	client := &http.Client{Transport: localOMLXHTTPRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: request}, nil
	})}
	require.PanicsWithValue(t, "read failed", func() {
		_, _ = executeLocalOMLXHTTP(context.Background(), localOMLXHTTPRequest{
			method: http.MethodPost, endpoint: "http://127.0.0.1:8000", responseLimit: 1, client: client,
		})
	})
	require.Equal(t, 1, body.closes)
}

func TestOMLXAdapterHTTPPreservesTransportAndStatusErrors(t *testing.T) {
	t.Parallel()
	offline := errors.New("offline")
	tests := []struct {
		name   string
		invoke func(*http.Client) error
		prefix string
		status string
	}{
		{
			name: "embeddings",
			invoke: func(client *http.Client) error {
				_, err := (OMLXEmbeddingAdapter{BaseURL: "http://127.0.0.1:8000", Client: client}).Evaluate(context.Background(), "omlx/embed", EmbeddingCase{ID: "case", Documents: []EmbeddingText{{ID: "d", Text: "document"}}, Queries: []EmbeddingQuery{{ID: "q", Text: "query", Relevant: []string{"d"}}}})
				return err
			},
			prefix: "request embeddings:", status: "embeddings HTTP 503: unavailable",
		},
		{
			name: "acceleration",
			invoke: func(client *http.Client) error {
				_, err := (OMLXAccelerationChatClient{BaseURL: "http://127.0.0.1:8000", Client: client}).AccelerationChat(context.Background(), AccelerationChatRequest{Model: "omlx/model", Prompt: "prompt", MaxOutputTokens: 1})
				return err
			},
			prefix: "request acceleration chat:", status: "acceleration chat HTTP 503: unavailable",
		},
		{
			name: "audio",
			invoke: func(client *http.Client) error {
				_, err := (OMLXAudioAdapter{BaseURL: "http://127.0.0.1:8000", Client: client}).Synthesize(context.Background(), TTSSpeechRequest{Model: "omlx/model", Input: "prompt"}, nil)
				return err
			},
			prefix: "request audio endpoint:", status: "speech HTTP 503: unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			offlineClient := &http.Client{Transport: localOMLXHTTPRoundTripper(func(*http.Request) (*http.Response, error) {
				return nil, offline
			})}
			err := tt.invoke(offlineClient)
			require.ErrorIs(t, err, offline)
			require.True(t, strings.HasPrefix(err.Error(), tt.prefix), err)

			statusClient := &http.Client{Transport: localOMLXHTTPRoundTripper(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unavailable")), Request: request}, nil
			})}
			require.EqualError(t, tt.invoke(statusClient), tt.status)
		})
	}
}

func TestOMLXAdapterHTTPPreservesResponseCloseTiming(t *testing.T) {
	t.Parallel()
	accelerationJSON, err := json.Marshal(struct {
		Choices []struct {
			Message ToolChatMessage `json:"message"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}{Choices: []struct {
		Message ToolChatMessage `json:"message"`
	}{{Message: ToolChatMessage{Content: "answer"}}}, Usage: struct {
		CompletionTokens int `json:"completion_tokens"`
	}{CompletionTokens: 1}})
	require.NoError(t, err)

	t.Run("audio closes before served identity", func(t *testing.T) {
		t.Parallel()
		headers := http.Header{"X-Llambo-Provider": {"omlx"}, "X-Llambo-Model": {"before-close"}}
		body := &localOMLXHTTPBody{reader: bytes.NewReader(validTestWAV()), onClose: func() { headers.Set("X-Llambo-Model", "closed-model") }}
		client := &http.Client{Transport: localOMLXHTTPRoundTripper(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: headers, Body: body, Request: request}, nil
		})}
		result, callErr := (OMLXAudioAdapter{BaseURL: "http://127.0.0.1:8000", Client: client}).Synthesize(context.Background(), TTSSpeechRequest{Model: "omlx/closed-model", Input: "prompt"}, nil)
		require.NoError(t, callErr)
		require.Equal(t, "omlx/closed-model", result.ServedModel)
		require.Equal(t, 1, body.closes)
	})

	t.Run("acceleration closes after response interpretation", func(t *testing.T) {
		t.Parallel()
		headers := http.Header{"X-Llambo-Model": {"before-close"}}
		body := &localOMLXHTTPBody{reader: bytes.NewReader(accelerationJSON), onClose: func() { headers.Set("X-Llambo-Model", "after-close") }}
		client := &http.Client{Transport: localOMLXHTTPRoundTripper(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: headers, Body: body, Request: request}, nil
		})}
		result, callErr := (OMLXAccelerationChatClient{BaseURL: "http://127.0.0.1:8000", Client: client}).AccelerationChat(context.Background(), AccelerationChatRequest{Model: "omlx/before-close", Prompt: "prompt", MaxOutputTokens: 1})
		require.NoError(t, callErr)
		require.Equal(t, "before-close", result.Model)
		require.Equal(t, "after-close", headers.Get("X-Llambo-Model"))
		require.Equal(t, 1, body.closes)
	})
}
