package modelsdev

import (
	"context"
	"encoding/json"
	"github.com/dotcommander/llambo/internal/costs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentFilteredSyncRetainsBothUpdates(t *testing.T) {
	t.Parallel()
	api := map[string]ProviderModels{"a": {Models: map[string]model{"A": {Cost: &modelCost{Input: 1, Output: 2}}}}, "b": {Models: map[string]model{"B": {Cost: &modelCost{Input: 3, Output: 4}}}}}
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		<-release
		_ = json.NewEncoder(w).Encode(api)
	}))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "prices.json")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, provider := range []string{" A ", "B"} {
		wg.Go(func() {
			_, _, err := RunSync(context.Background(), SyncOptions{ProviderToKey: map[string]string{"a": "a", "b": "b"}, Filter: []string{provider}, URL: server.URL, OutPath: path})
			errs <- err
		})
	}
	<-arrived
	<-arrived
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got costs.ModelsDevFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["a:a"].OutputPer1M != 2 || got["b:b"].OutputPer1M != 4 {
		t.Fatalf("lost filtered update: %+v", got)
	}
}

func TestIncompleteModelsDevPriceIsUnknown(t *testing.T) {
	t.Parallel()
	body := map[string]any{"p": map[string]any{"models": map[string]any{"missing": map[string]any{"cost": map[string]any{"input": 0}}, "negative": map[string]any{"cost": map[string]any{"input": -1, "output": 0}}, "free": map[string]any{"cost": map[string]any{"input": 0, "output": 0}}}}}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	api, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	prices := Normalize(api, map[string]string{"p": "p"}, nil)
	if len(prices) != 1 {
		t.Fatalf("invalid cost admitted: %+v", prices)
	}
	if _, ok := prices["p:free"]; !ok {
		t.Fatal("explicit free omitted")
	}
}
