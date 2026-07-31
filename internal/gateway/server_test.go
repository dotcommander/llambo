package gateway

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewarePreservesFlusher(t *testing.T) {
	t.Parallel()
	server := &Server{}
	handler := server.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "missing flusher", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected middleware to preserve http.Flusher, got status %d body=%q", rr.Code, rr.Body.String())
	}
}

func TestMiddlewareBearerAuthentication(t *testing.T) {
	t.Parallel()
	token := "test-gateway-token"
	server := &Server{authRequired: true, authTokenHash: sha256.Sum256([]byte(token))}
	handler := server.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tt := range []struct {
		name   string
		header string
		want   int
	}{
		{name: "missing", want: http.StatusUnauthorized},
		{name: "wrong", header: "Bearer wrong", want: http.StatusUnauthorized},
		{name: "valid", header: "Bearer " + token, want: http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/jobs", nil)
			req.Header.Set("Authorization", tt.header)
			handler.ServeHTTP(rr, req)
			if rr.Code != tt.want {
				t.Fatalf("status = %d, want %d", rr.Code, tt.want)
			}
		})
	}
}

func TestMiddlewareAllowsRequestsWhenAuthIsUnset(t *testing.T) {
	t.Parallel()
	server := &Server{}
	handler := server.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", nil)
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}

func TestMiddlewareCORSAllowlist(t *testing.T) {
	t.Parallel()
	const allowedOrigin = "http://127.0.0.1:3000"
	server := &Server{allowedOrigins: map[string]struct{}{allowedOrigin: {}}}
	handler := server.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("allowed preflight", func(t *testing.T) {
		t.Parallel()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/v1/jobs", nil)
		req.Header.Set("Origin", allowedOrigin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
		}
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
			t.Fatalf("allow origin = %q, want %q", got, allowedOrigin)
		}
		if got := rr.Header().Values("Vary"); len(got) != 1 || got[0] != "Origin" {
			t.Fatalf("Vary = %v, want [Origin]", got)
		}
	})

	t.Run("disallowed preflight", func(t *testing.T) {
		t.Parallel()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/v1/jobs", nil)
		req.Header.Set("Origin", "https://example.invalid")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusForbidden)
		}
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("unexpected allow origin %q", got)
		}
	})

	t.Run("disallowed method", func(t *testing.T) {
		t.Parallel()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/v1/jobs", nil)
		req.Header.Set("Origin", allowedOrigin)
		req.Header.Set("Access-Control-Request-Method", http.MethodDelete)
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusForbidden)
		}
	})
}

func TestNormalizeAllowedOriginsRejectsWildcard(t *testing.T) {
	t.Parallel()
	if _, err := normalizeAllowedOrigins([]string{"*"}); err == nil {
		t.Fatal("expected wildcard origin to be rejected")
	}
}
