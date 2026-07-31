package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dotcommander/llambo/providers"
)

type streamSanitizationRecorder struct {
	*httptest.ResponseRecorder
}

func (r *streamSanitizationRecorder) Flush() {}

func TestHandler_ChatCompletionStream_SanitizesUpstreamErrors(t *testing.T) {
	t.Parallel()
	server, stream := newHandlerStreamTestServer(t)
	stream.streamErr = providers.NewOpenAIError("upstream secret", http.StatusTooManyRequests, errors.New("429"))

	req := ChatCompletionRequest{Messages: []Message{{Role: "user", Content: "hello"}}, Stream: true}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	rr := &streamSanitizationRecorder{ResponseRecorder: httptest.NewRecorder()}
	server.handleChatCompletionStream(rr, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)), req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadGateway)
	}
	responseBody := rr.Body.String()
	if strings.Contains(responseBody, "upstream secret") {
		t.Fatalf("raw upstream error leaked: %s", responseBody)
	}
	if !strings.Contains(responseBody, "Upstream provider rate limit exceeded") {
		t.Fatalf("sanitized message missing: %s", responseBody)
	}
}
