package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// EmbeddingEvaluation is the scored outcome of one embedding case.
type EmbeddingEvaluation struct {
	ServedModel string
	Documents   map[string][]float64
	Queries     map[string][]float64
	Scores      map[string]map[string]float64
	RecallAt1   float64
	MRRAt10     float64
	NDCGAt10    float64
}

// Evaluate executes and scores a bounded retrieval case against the verified OMLX model.
func (a OMLXEmbeddingAdapter) Evaluate(ctx context.Context, model string, c EmbeddingCase) (EmbeddingEvaluation, error) {
	prepared, err := a.prepareRequest(model, c)
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	httpResponse, err := executeLocalOMLXHTTP(ctx, localOMLXHTTPRequest{
		method:        http.MethodPost,
		endpoint:      prepared.root + "/v1/embeddings",
		body:          bytes.NewReader(prepared.body),
		contentType:   applicationJSON,
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
	defer func() { _ = response.Body.Close() }()
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
	if err = json.Unmarshal(responseBody, &payload); err != nil {
		return EmbeddingEvaluation{}, fmt.Errorf("parse embeddings response: %w", err)
	}
	served, err := localServedIdentity(model, "omlx", payload.Model, a.VerifiedServedModel)
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	vectors, err := indexedEmbeddingVectors(payload.Data, prepared.inputCount)
	if err != nil {
		return EmbeddingEvaluation{}, err
	}
	return scoreEmbeddingVectors(c, served, vectors)
}

type preparedEmbeddingRequest struct {
	root       string
	body       []byte
	inputCount int
}

func (a OMLXEmbeddingAdapter) prepareRequest(model string, c EmbeddingCase) (preparedEmbeddingRequest, error) {
	if strings.TrimSpace(model) == "" || strings.TrimSpace(c.ID) == "" || len(c.Documents) == 0 || len(c.Queries) == 0 {
		return preparedEmbeddingRequest{}, errors.New("model, case id, documents, and queries are required")
	}
	endpointModel, err := localOMLXModel(model)
	if err != nil {
		return preparedEmbeddingRequest{}, err
	}
	root, err := normalizeOMLXBaseURL(a.BaseURL)
	if err != nil {
		return preparedEmbeddingRequest{}, err
	}
	if err = ValidateOMLXLoopbackBaseURL(a.BaseURL); err != nil {
		return preparedEmbeddingRequest{}, err
	}
	inputs, err := embeddingRequestTexts(c)
	if err != nil {
		return preparedEmbeddingRequest{}, err
	}
	body, err := json.Marshal(struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{Model: endpointModel, Input: inputs})
	if err != nil {
		return preparedEmbeddingRequest{}, err
	}
	if len(body) > localAdapterResponseLimit {
		return preparedEmbeddingRequest{}, fmt.Errorf("embedding request exceeds %d bytes", localAdapterResponseLimit)
	}
	return preparedEmbeddingRequest{root: root, body: body, inputCount: len(inputs)}, nil
}

func embeddingRequestTexts(c EmbeddingCase) ([]string, error) {
	inputs := make([]string, 0, len(c.Documents)+len(c.Queries))
	for i, d := range c.Documents {
		if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Text) == "" {
			return nil, fmt.Errorf("document %d requires id and text", i+1)
		}
		inputs = append(inputs, d.Text)
	}
	for i, q := range c.Queries {
		if strings.TrimSpace(q.ID) == "" || strings.TrimSpace(q.Text) == "" || len(q.Relevant) == 0 {
			return nil, fmt.Errorf("query %d requires id, text, and relevance", i+1)
		}
		inputs = append(inputs, q.Text)
	}
	return inputs, nil
}

func readBoundedLocalBody(body io.Reader, limit int64) ([]byte, error) {
	if limit < 1 {
		return nil, errors.New("positive response limit is required")
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
