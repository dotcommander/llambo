package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/dotcommander/llambo/providers"
)

type openaiCompatFetcher struct{}

func (openaiCompatFetcher) Fetch(ctx context.Context, cfg providers.Config) ([]UpstreamModel, string, error) {
	name := inferProviderName(cfg)
	key, err := apiKey(name, cfg)
	if err != nil {
		return nil, "", err
	}

	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if !strings.HasSuffix(base, "/v1") && !strings.HasSuffix(base, "/v4") {
		base += "/v1"
	}
	endpoint := base + "/models"

	headers := map[string]string{"Accept": "application/json"}
	if key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	for k, v := range cfg.ExtraHeaders {
		headers[k] = v
	}

	body, err := doGet(ctx, endpoint, headers)
	if err != nil {
		return nil, endpoint, err
	}

	var payload struct {
		Data []struct {
			ID                  string          `json:"id"`
			CanonicalSlug       string          `json:"canonical_slug"`
			Name                string          `json:"name"`
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
		m := UpstreamModel{ID: d.ID, OwnedBy: d.OwnedBy}
		if d.Created > 0 {
			m.UpstreamCreated = time.Unix(d.Created, 0).UTC()
		}
		m.Metadata = ModelMetadata{
			Name:                d.Name,
			CanonicalSlug:       d.CanonicalSlug,
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
	return out, endpoint, nil
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

	var payload struct {
		Models []struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			Description                string   `json:"description"`
			InputTokenLimit            int      `json:"inputTokenLimit"`
			OutputTokenLimit           int      `json:"outputTokenLimit"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, endpoint, fmt.Errorf("parse response: %w", err)
	}

	out := make([]UpstreamModel, 0, len(payload.Models))
	for _, m := range payload.Models {
		id := strings.TrimPrefix(m.Name, "models/")
		if id == "" {
			continue
		}
		out = append(out, UpstreamModel{
			ID: id,
			Metadata: ModelMetadata{
				Name:                       m.DisplayName,
				Description:                m.Description,
				ContextLength:              m.InputTokenLimit,
				InputTokenLimit:            m.InputTokenLimit,
				OutputTokenLimit:           m.OutputTokenLimit,
				SupportedGenerationMethods: append([]string(nil), m.SupportedGenerationMethods...),
			},
		})
	}
	return out, endpoint, nil
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
