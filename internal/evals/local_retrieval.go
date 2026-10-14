package evals

import (
	"errors"
	"math"
	"sort"
)

// RetrievalQuery is one ranked-retrieval query with relevance judgments.
type RetrievalQuery struct {
	ID       string   `json:"id"`
	Relevant []string `json:"relevant"`
}

// RetrievalMetrics bundles the averaged rank metrics for a retrieval case.
type RetrievalMetrics struct {
	RecallAt1 float64
	MRRAt10   float64
	NDCGAt10  float64
}

// ScoreRetrieval averages the existing per-query rank metrics.
func ScoreRetrieval(queries []RetrievalQuery, scores map[string]map[string]float64) (RetrievalMetrics, error) {
	if len(queries) == 0 {
		return RetrievalMetrics{}, errors.New("no retrieval queries")
	}
	var total RetrievalMetrics
	for _, q := range queries {
		if q.ID == "" || len(q.Relevant) == 0 {
			return RetrievalMetrics{}, errors.New("invalid retrieval query")
		}
		metrics := scoreRetrievalQuery(q, scores)
		total.RecallAt1 += metrics.recall1
		total.MRRAt10 += metrics.mrr10
		total.NDCGAt10 += metrics.ndcg10
	}
	n := float64(len(queries))
	return RetrievalMetrics{RecallAt1: total.RecallAt1 / n, MRRAt10: total.MRRAt10 / n, NDCGAt10: total.NDCGAt10 / n}, nil
}

type retrievalQueryMetrics struct {
	recall1, mrr10, ndcg10 float64
}

func scoreRetrievalQuery(q RetrievalQuery, scores map[string]map[string]float64) retrievalQueryMetrics {
	var metrics retrievalQueryMetrics
	ranked := rankScores(scores[q.ID])
	rel := map[string]bool{}
	for _, id := range q.Relevant {
		rel[id] = true
	}
	if len(ranked) > 0 && rel[ranked[0]] {
		metrics.recall1++
	}
	dcg := 0.0
	first := firstRank(ranked, rel)
	for i, id := range ranked {
		if i == 10 {
			break
		}
		if rel[id] {
			dcg += 1 / math.Log2(float64(i)+2)
			if first == i {
				metrics.mrr10 += 1 / float64(i+1)
			}
		}
	}
	ideal := 0.0
	for i := 0; i < min(10, len(rel)); i++ {
		ideal += 1 / math.Log2(float64(i)+2)
	}
	if ideal > 0 {
		metrics.ndcg10 += dcg / ideal
	}
	return metrics
}

func firstRank(ids []string, rel map[string]bool) int {
	for i, id := range ids {
		if rel[id] {
			return i
		}
	}
	return -1
}
func rankScores(m map[string]float64) []string {
	ids := make([]string, 0, len(m))
	for id, v := range m {
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return m[ids[i]] > m[ids[j]] || (m[ids[i]] == m[ids[j]] && ids[i] < ids[j]) })
	return ids
}
