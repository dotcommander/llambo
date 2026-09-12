package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dotcommander/llambo/providers"
)

type openaiCompatFetcher struct{}

// OpenAIModelsRequestSpec describes the OpenAI-compatible model-list request.
// Callers retain transport policy such as timeouts and response-size limits.
type OpenAIModelsRequestSpec struct {
	Endpoint string
	Headers  map[string]string
}

// BuildOpenAIModelsRequestSpec builds the shared OpenAI-compatible model-list
// endpoint and headers. Provider-specific headers intentionally apply last so
// they can override the defaults or authentication when required by a gateway.
func BuildOpenAIModelsRequestSpec(baseURL, apiKey string, extraHeaders map[string]string) OpenAIModelsRequestSpec {
	base := providers.NormalizeOpenAIBaseURL(baseURL)
	if base == "" {
		base = "/v1"
	}

	headers := map[string]string{"Accept": "application/json"}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	for k, v := range extraHeaders {
		headers[http.CanonicalHeaderKey(k)] = v
	}
	return OpenAIModelsRequestSpec{Endpoint: base + "/models", Headers: headers}
}

func decodeOpenAICompatibleModelData(body []byte) ([]json.RawMessage, error) {
	var payload struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return payload.Data, nil
}

// DecodeOpenAICompatibleModelIDs decodes model IDs without interpreting
// optional provider metadata. It is for callers whose established contract only
// needs a tolerant inventory of usable IDs.
func DecodeOpenAICompatibleModelIDs(body []byte) ([]string, error) {
	data, err := decodeOpenAICompatibleModelData(body)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(data))
	for _, raw := range data {
		var model struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &model); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if model.ID != "" {
			out = append(out, model.ID)
		}
	}
	return out, nil
}

// DecodeOpenAICompatibleModels decodes an OpenAI-compatible model list without
// changing upstream order. It retains all metadata currently used by catalog
// refresh; callers may project the result to a smaller representation.
func DecodeOpenAICompatibleModels(body []byte) ([]UpstreamModel, error) {
	data, err := decodeOpenAICompatibleModelData(body)
	if err != nil {
		return nil, err
	}

	out := make([]UpstreamModel, 0, len(data))
	for _, raw := range data {
		var d struct {
			ID                  string          `json:"id"`
			CanonicalSlug       string          `json:"canonical_slug"`
			Name                string          `json:"name"`
			Description         string          `json:"description"`
			Created             int64           `json:"created"`
			OwnedBy             string          `json:"owned_by"`
			ContextLength       int             `json:"context_length"`
			Architecture        architectureRaw `json:"architecture"`
			Pricing             ModelPricing    `json:"pricing"`
			TopProvider         topProviderRaw  `json:"top_provider"`
			SupportedParameters []string        `json:"supported_parameters"`
			DefaultParameters   map[string]any  `json:"default_parameters"`
			Reasoning           map[string]any  `json:"reasoning"`
			Benchmarks          map[string]any  `json:"benchmarks"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if d.ID == "" {
			continue
		}
		m := UpstreamModel{ID: d.ID, OwnedBy: d.OwnedBy}
		if d.Created > 0 {
			m.UpstreamCreated = time.Unix(d.Created, 0).UTC()
		}
		m.Metadata = ModelMetadata{
			Name:                d.Name,
			CanonicalSlug:       d.CanonicalSlug,
			Description:         d.Description,
			ContextLength:       d.ContextLength,
			SupportedParameters: append([]string(nil), d.SupportedParameters...),
			DefaultParameters:   d.DefaultParameters,
			Pricing:             d.Pricing,
			Architecture:        d.Architecture.model(),
			TopProvider:         d.TopProvider.model(),
			Reasoning:           d.Reasoning,
			Benchmarks:          d.Benchmarks,
		}
		out = append(out, m)
	}
	return out, nil
}

func (openaiCompatFetcher) Fetch(ctx context.Context, cfg providers.Config) ([]UpstreamModel, string, error) {
	name := inferProviderName(cfg)
	key, err := apiKey(name, cfg)
	if err != nil {
		return nil, "", err
	}

	spec := BuildOpenAIModelsRequestSpec(cfg.BaseURL, key, cfg.ExtraHeaders)
	body, err := doGet(ctx, spec.Endpoint, spec.Headers)
	if err != nil {
		return nil, spec.Endpoint, err
	}

	models, err := DecodeOpenAICompatibleModels(body)
	if err != nil {
		return nil, spec.Endpoint, err
	}
	return models, spec.Endpoint, nil
}

type architectureRaw struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Tokenizer        string   `json:"tokenizer"`
	InstructType     *string  `json:"instruct_type"`
}

func (a architectureRaw) model() ModelArchitecture {
	instructType := ""
	if a.InstructType != nil {
		instructType = *a.InstructType
	}
	return ModelArchitecture{
		Modality:         a.Modality,
		InputModalities:  append([]string(nil), a.InputModalities...),
		OutputModalities: append([]string(nil), a.OutputModalities...),
		Tokenizer:        a.Tokenizer,
		InstructType:     instructType,
	}
}

type topProviderRaw struct {
	ContextLength       *int `json:"context_length"`
	MaxCompletionTokens *int `json:"max_completion_tokens"`
	IsModerated         bool `json:"is_moderated"`
}

func (t topProviderRaw) model() ModelTopProvider {
	out := ModelTopProvider{IsModerated: t.IsModerated}
	if t.ContextLength != nil {
		out.ContextLength = *t.ContextLength
	}
	if t.MaxCompletionTokens != nil {
		out.MaxCompletionTokens = *t.MaxCompletionTokens
	}
	return out
}

type geminiFetcher struct{}

func decodeGeminiModelData(body []byte) ([]json.RawMessage, error) {
	var payload struct {
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return payload.Models, nil
}

func geminiModelID(name string) string {
	return strings.TrimPrefix(name, "models/")
}

// DecodeGeminiModelIDs decodes model IDs without interpreting optional Gemini
// metadata. It preserves the CLI's established tolerant inventory behavior.
func DecodeGeminiModelIDs(body []byte) ([]string, error) {
	data, err := decodeGeminiModelData(body)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(data))
	for _, raw := range data {
		var model struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &model); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if id := geminiModelID(model.Name); id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

// DecodeGeminiModels decodes a Gemini model list without changing upstream
// order. It retains the full catalog metadata and remains strict about its
// corresponding payload fields.
func DecodeGeminiModels(body []byte) ([]UpstreamModel, error) {
	data, err := decodeGeminiModelData(body)
	if err != nil {
		return nil, err
	}

	out := make([]UpstreamModel, 0, len(data))
	for _, raw := range data {
		var model struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			Description                string   `json:"description"`
			InputTokenLimit            int      `json:"inputTokenLimit"`
			OutputTokenLimit           int      `json:"outputTokenLimit"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		}
		if err := json.Unmarshal(raw, &model); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		id := geminiModelID(model.Name)
		if id == "" {
			continue
		}
		out = append(out, UpstreamModel{
			ID: id,
			Metadata: ModelMetadata{
				Name:                       model.DisplayName,
				Description:                model.Description,
				ContextLength:              model.InputTokenLimit,
				InputTokenLimit:            model.InputTokenLimit,
				OutputTokenLimit:           model.OutputTokenLimit,
				SupportedGenerationMethods: append([]string(nil), model.SupportedGenerationMethods...),
			},
		})
	}
	return out, nil
}

func (geminiFetcher) Fetch(ctx context.Context, cfg providers.Config) ([]UpstreamModel, string, error) {
	name := inferProviderName(cfg)
	key, err := apiKey(name, cfg)
	if err != nil {
		return nil, "", err
	}

	base := strings.TrimSuffix(cfg.BaseURL, "/")
	base = strings.TrimSuffix(base, "/v1beta")
	endpoint := base + "/v1beta/models"
	requestURL := endpoint
	if key != "" {
		requestURL += "?key=" + url.QueryEscape(key)
	}

	body, err := doGet(ctx, requestURL, map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, endpoint, err
	}

	models, err := DecodeGeminiModels(body)
	if err != nil {
		return nil, endpoint, err
	}
	return models, endpoint, nil
}

type anthropicFetcher struct{}

func (anthropicFetcher) Fetch(ctx context.Context, cfg providers.Config) ([]UpstreamModel, string, error) {
	name := inferProviderName(cfg)
	key, err := apiKey(name, cfg)
	if err != nil {
		return nil, "", err
	}

	base := strings.TrimSuffix(cfg.BaseURL, "/")
	endpoint := base + "/v1/models"

	headers := map[string]string{
		"Accept":            "application/json",
		"anthropic-version": "2023-06-01",
	}
	if key != "" {
		headers["x-api-key"] = key
	}

	body, err := doGet(ctx, endpoint, headers)
	if err != nil {
		return nil, endpoint, err
	}

	var payload struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			CreatedAt   string `json:"created_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, endpoint, fmt.Errorf("parse response: %w", err)
	}

	out := make([]UpstreamModel, 0, len(payload.Data))
	for _, d := range payload.Data {
		if d.ID == "" {
			continue
		}
		m := UpstreamModel{ID: d.ID}
		if d.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, d.CreatedAt); err == nil {
				m.UpstreamCreated = t.UTC()
			}
		}
		out = append(out, m)
	}
	return out, endpoint, nil
}
