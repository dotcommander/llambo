package evals

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// localOMLXHTTPRequest describes one bounded direct OMLX request. Adapters own
// validation, response interpretation, and successful response-body closure.
type localOMLXHTTPRequest struct {
	method         string
	endpoint       string
	body           io.Reader
	contentType    string
	apiKey         string
	client         *http.Client
	responseLimit  int64
	buildError     string
	requestError   string
	measureLatency bool
}

// localOMLXHTTPResponse retains the original response and its already bounded
// body. On success its Body remains open for the adapter to close at its
// existing ownership point; bounded-read failures close it before returning.
type localOMLXHTTPResponse struct {
	response *http.Response
	body     []byte
	elapsed  time.Duration
}

func executeLocalOMLXHTTP(ctx context.Context, spec localOMLXHTTPRequest) (localOMLXHTTPResponse, error) {
	request, err := http.NewRequestWithContext(ctx, spec.method, spec.endpoint, spec.body)
	if err != nil {
		return localOMLXHTTPResponse{}, fmt.Errorf("%s: %w", spec.buildError, err)
	}
	request.Header.Set("Content-Type", spec.contentType)
	if spec.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+spec.apiKey)
	}
	client := spec.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}

	var started time.Time
	if spec.measureLatency {
		started = time.Now()
	}
	response, err := client.Do(request)
	result := localOMLXHTTPResponse{}
	if spec.measureLatency {
		result.elapsed = time.Since(started)
	}
	if err != nil {
		return result, fmt.Errorf("%s: %w", spec.requestError, err)
	}
	callerOwnsBody := false
	defer func() {
		if !callerOwnsBody {
			_ = response.Body.Close()
		}
	}()
	result.response = response
	result.body, err = readBoundedLocalBody(response.Body, spec.responseLimit)
	if err != nil {
		return localOMLXHTTPResponse{elapsed: result.elapsed}, err
	}
	callerOwnsBody = true
	return result, nil
}
