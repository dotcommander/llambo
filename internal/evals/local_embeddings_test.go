package evals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOMLXEmbeddingAdapterValidatesIndexedFiniteVectorsAndScores(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Error("unexpected embedding request")
		}
		var request struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != "embed" || len(request.Input) != 3 {
			t.Error("bad embedding payload")
		}
		_, _ = w.Write([]byte(`{"model":"embed","data":[{"index":2,"embedding":[0,1]},{"index":0,"embedding":[1,0]},{"index":1,"embedding":[1,0]}]}`))
	}))
	t.Cleanup(server.Close)
	result, err := (OMLXEmbeddingAdapter{BaseURL: server.URL, Client: server.Client()}).Evaluate(context.Background(), "omlx/embed", EmbeddingCase{ID: "retrieval", Documents: []EmbeddingText{{ID: "d1", Text: "alpha"}, {ID: "d2", Text: "beta"}}, Queries: []EmbeddingQuery{{ID: "q1", Text: "alpha", Relevant: []string{"d1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.ServedModel != "omlx/embed" || result.RecallAt1 != 1 || result.NDCGAt10 != 1 {
		t.Fatalf("unexpected embedding evaluation: %#v", result)
	}
}

func TestIndexedEmbeddingVectorsRejectsInconsistentAndDuplicateIndexes(t *testing.T) {
	t.Parallel()
	_, err := indexedEmbeddingVectors([]struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	}{{0, []float64{1}}, {0, []float64{2}}}, 2)
	if err == nil {
		t.Fatal("duplicate index accepted")
	}
	if _, err := EmbeddingCosineScores(map[string][]float64{"d": {0, 0}}, map[string][]float64{"q": {1, 0}}); err == nil {
		t.Fatal("zero norm accepted")
	}
}
