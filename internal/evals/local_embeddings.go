package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
)

// A sealed retrieval case can return 80 high-dimensional float vectors. Keep
// the request bound small, but allow a bounded response large enough for those
// JSON-encoded vectors.
const localEmbeddingResponseLimit = 32 << 20

// OMLXEmbeddingAdapter directly speaks the bounded OpenAI-compatible OMLX
// embedding endpoint. It is intentionally not a generic local executor.
type OMLXEmbeddingAdapter struct {
	BaseURL             string
	APIKey              string
	Client              *http.Client
	VerifiedServedModel string
}

type EmbeddingEvaluation struct {
	ServedModel string
	Documents   map[string][]float64
	Queries     map[string][]float64
	Scores      map[string]map[string]float64
	RecallAt1   float64
	MRRAt10     float64
	NDCGAt10    float64
}

func (a OMLXEmbeddingAdapter) Evaluate(ctx context.Context, model string, c EmbeddingCase) (EmbeddingEvaluation, error) {
	if strings.TrimSpace(model) == "" || strings.TrimSpace(c.ID) == "" || len(c.Documents) == 0 || len(c.Queries) == 0 {
		return EmbeddingEvaluation{}, fmt.Errorf("model, case id, documents, and queries are required")
	}
	endpointModel, err := localProviderModel(model, "omlx")
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	root, err := normalizeOMLXBaseURL(a.BaseURL)
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	if err := ValidateOMLXLoopbackBaseURL(a.BaseURL); err != nil {
		return EmbeddingEvaluation{}, err
	}
	inputs := make([]string, 0, len(c.Documents)+len(c.Queries))
	for i, d := range c.Documents {
		if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Text) == "" {
			return EmbeddingEvaluation{}, fmt.Errorf("document %d requires id and text", i+1)
		}
		inputs = append(inputs, d.Text)
	}
	for i, q := range c.Queries {
		if strings.TrimSpace(q.ID) == "" || strings.TrimSpace(q.Text) == "" || len(q.Relevant) == 0 {
			return EmbeddingEvaluation{}, fmt.Errorf("query %d requires id, text, and relevance", i+1)
		}
		inputs = append(inputs, q.Text)
	}
	body, err := json.Marshal(struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{Model: endpointModel, Input: inputs})
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	if len(body) > localAdapterResponseLimit {
		return EmbeddingEvaluation{}, fmt.Errorf("embedding request exceeds %d bytes", localAdapterResponseLimit)
	}
	httpResponse, err := executeLocalOMLXHTTP(ctx, localOMLXHTTPRequest{
		method:        http.MethodPost,
		endpoint:      root + "/v1/embeddings",
		body:          bytes.NewReader(body),
		contentType:   "application/json",
		apiKey:        a.APIKey,
		client:        a.Client,
		responseLimit: localEmbeddingResponseLimit,
		buildError:    "build embeddings request",
		requestError:  "request embeddings",
	})
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	response := httpResponse.response
	defer response.Body.Close()
	responseBody := httpResponse.body
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return EmbeddingEvaluation{}, fmt.Errorf("embeddings HTTP %d: %s", response.StatusCode, boundedErrorText(responseBody))
	}
	var payload struct {
		Model string `json:"model"`
		Data  []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return EmbeddingEvaluation{}, fmt.Errorf("parse embeddings response: %w", err)
	}
	served, err := localServedIdentity(model, "omlx", payload.Model, a.VerifiedServedModel)
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	vectors, err := indexedEmbeddingVectors(payload.Data, len(inputs))
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	documents := make(map[string][]float64, len(c.Documents))
	queries := make(map[string][]float64, len(c.Queries))
	for i, d := range c.Documents {
		documents[d.ID] = vectors[i]
	}
	for i, q := range c.Queries {
		queries[q.ID] = vectors[len(c.Documents)+i]
	}
	scores, err := EmbeddingCosineScores(documents, queries)
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	retrievalQueries := make([]RetrievalQuery, len(c.Queries))
	for i, q := range c.Queries {
		retrievalQueries[i] = RetrievalQuery{ID: q.ID, Relevant: q.Relevant}
	}
	recall, mrr, ndcg, err := ScoreRetrieval(retrievalQueries, scores)
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	return EmbeddingEvaluation{ServedModel: served, Documents: documents, Queries: queries, Scores: scores, RecallAt1: recall, MRRAt10: mrr, NDCGAt10: ndcg}, nil
}

func indexedEmbeddingVectors(data []struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}, want int) ([][]float64, error) {
	if len(data) != want {
		return nil, fmt.Errorf("embedding response count %d, want %d", len(data), want)
	}
	vectors := make([][]float64, want)
	dimension := 0
	for _, item := range data {
		if item.Index < 0 || item.Index >= want || vectors[item.Index] != nil {
			return nil, fmt.Errorf("embedding response has invalid or duplicate index %d", item.Index)
		}
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("embedding %d is empty", item.Index)
		}
		if dimension == 0 {
			dimension = len(item.Embedding)
		}
		if len(item.Embedding) != dimension {
			return nil, fmt.Errorf("embedding %d has dimension %d, want %d", item.Index, len(item.Embedding), dimension)
		}
		for _, value := range item.Embedding {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("embedding %d contains a non-finite value", item.Index)
			}
		}
		vectors[item.Index] = append([]float64(nil), item.Embedding...)
	}
	for i, vector := range vectors {
		if vector == nil {
			return nil, fmt.Errorf("embedding response omits index %d", i)
		}
	}
	return vectors, nil
}

func EmbeddingCosineScores(documents, queries map[string][]float64) (map[string]map[string]float64, error) {
	if len(documents) == 0 || len(queries) == 0 {
		return nil, fmt.Errorf("documents and queries are required")
	}
	scores := make(map[string]map[string]float64, len(queries))
	for queryID, query := range queries {
		row := make(map[string]float64, len(documents))
		for documentID, document := range documents {
			score, err := cosineSimilarity(query, document)
			if err != nil {
				return nil, fmt.Errorf("%s against %s: %w", queryID, documentID, err)
			}
			row[documentID] = score
		}
		scores[queryID] = row
	}
	return scores, nil
}

func cosineSimilarity(a, b []float64) (float64, error) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, fmt.Errorf("vectors must be nonempty and have matching dimensions")
	}
	var dot, an, bn float64
	for i := range a {
		if math.IsNaN(a[i]) || math.IsNaN(b[i]) || math.IsInf(a[i], 0) || math.IsInf(b[i], 0) {
			return 0, fmt.Errorf("vector contains a non-finite value")
		}
		dot += a[i] * b[i]
		an += a[i] * a[i]
		bn += b[i] * b[i]
	}
	if an == 0 || bn == 0 {
		return 0, fmt.Errorf("zero-norm vector")
	}
	return dot / math.Sqrt(an*bn), nil
}

func readBoundedLocalBody(body io.Reader, limit int64) ([]byte, error) {
	if limit < 1 {
		return nil, fmt.Errorf("positive response limit is required")
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func boundedErrorText(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 512 {
		return text[:512]
	}
	return text
}
