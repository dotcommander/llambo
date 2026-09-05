package evals

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFetchWritingEvidenceHTTPSetsUserAgentAndClonesHeaders(t *testing.T) {
	responseHeaders := http.Header{"Etag": {`"revision-1"`}}
	responseBody := &trackedReadCloser{Reader: strings.NewReader("body")}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got, want := request.Header.Get("User-Agent"), "llambo-evals/1"; got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: responseHeaders, Body: responseBody, Request: request}, nil
	})}

	body, headers, err := fetchWritingEvidenceHTTP(context.Background(), client, "https://example.test/evidence", "test evidence", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(body), "body"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if got, want := headers.Get("ETag"), `"revision-1"`; got != want {
		t.Fatalf("ETag = %q, want %q", got, want)
	}
	headers.Set("ETag", `"changed"`)
	if got, want := responseHeaders.Get("ETag"), `"revision-1"`; got != want {
		t.Fatalf("response headers mutated through clone: %q, want %q", got, want)
	}
	if !responseBody.closed {
		t.Fatal("response body was not closed")
	}
}

func TestFetchWritingEvidenceHTTPRejectsNonSuccessBeforeReadingBody(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTeapot, Header: make(http.Header), Body: io.NopCloser(errReader{err: errors.New("body was read")}), Request: request}, nil
	})}

	_, _, err := fetchWritingEvidenceHTTP(context.Background(), client, "https://example.test/evidence", "test evidence", 4)
	if got, want := errString(err), "GET https://example.test/evidence: HTTP 418"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestFetchWritingEvidenceHTTPReadErrorUsesSourceLabel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(errReader{err: errors.New("read failure")}), Request: request}, nil
	})}

	_, _, err := fetchWritingEvidenceHTTP(context.Background(), client, "https://example.test/evidence", "WritingBench score.xlsx", 4)
	if got, want := errString(err), "read WritingBench score.xlsx: read failure"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestFetchWritingEvidenceHTTPEnforcesExactByteBoundary(t *testing.T) {
	for _, tt := range []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "exact limit", body: "four"},
		{name: "one byte over", body: "five!", wantErr: "test evidence exceeds 4 bytes"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tt.body)), Request: request}, nil
			})}

			body, _, err := fetchWritingEvidenceHTTP(context.Background(), client, "https://example.test/evidence", "test evidence", 4)
			if tt.wantErr != "" {
				if got := errString(err); got != tt.wantErr {
					t.Fatalf("error = %q, want %q", got, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(body), tt.body; got != want {
				t.Fatalf("body = %q, want %q", got, want)
			}
		})
	}
}

type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}

type trackedReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackedReadCloser) Close() error {
	r.closed = true
	return nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
