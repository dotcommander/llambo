package modelsdev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchReturnsBody(t *testing.T) {
	t.Parallel()

	want := []byte(`{"models":[]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(want)
	}))
	t.Cleanup(srv.Close)

	got, err := Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("Fetch() body = %q, want %q", got, want)
	}
}

func TestFetchReturnsErrorForNonOK(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	_, err := Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want non-nil")
	}
}
