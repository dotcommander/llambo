package evals

import (
	"errors"
	"fmt"
	"math"
)

func scoreEmbeddingVectors(c EmbeddingCase, served string, vectors [][]float64) (EmbeddingEvaluation, error) {
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
	metrics, err := ScoreRetrieval(retrievalQueries, scores)
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	return EmbeddingEvaluation{ServedModel: served, Documents: documents, Queries: queries, Scores: scores, RecallAt1: metrics.RecallAt1, MRRAt10: metrics.MRRAt10, NDCGAt10: metrics.NDCGAt10}, nil
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

// EmbeddingCosineScores computes cosine similarity between query and document
// vectors.
func EmbeddingCosineScores(documents, queries map[string][]float64) (map[string]map[string]float64, error) {
	if len(documents) == 0 || len(queries) == 0 {
		return nil, errors.New("documents and queries are required")
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
		return 0, errors.New("vectors must be nonempty and have matching dimensions")
	}
	var dot, an, bn float64
	for i := range a {
		if math.IsNaN(a[i]) || math.IsNaN(b[i]) || math.IsInf(a[i], 0) || math.IsInf(b[i], 0) {
			return 0, errors.New("vector contains a non-finite value")
		}
		dot += a[i] * b[i]
		an += a[i] * a[i]
		bn += b[i] * b[i]
	}
	if an == 0 || bn == 0 {
		return 0, errors.New("zero-norm vector")
	}
	return dot / math.Sqrt(an*bn), nil
}
