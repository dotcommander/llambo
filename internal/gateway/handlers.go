package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dotcommander/llambo/providers"
)

const defaultAnthropicVersion = "2023-06-01"

// handleChatCompletion handles POST /v1/chat/completions
func (s *Server) handleChatCompletion(w http.ResponseWriter, r *http.Request) {
	// Limit request body size to prevent memory exhaustion
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

	var req ChatCompletionRequest
	if err := decodeSingleJSON(r.Body, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body: "+err.Error())
		return
	}

	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "Messages array is required")
		return
	}

	// Extract system and user prompts from messages
	systemPrompt, userContent := ExtractPrompts(req.Messages)

	if req.Stream {
		s.handleChatCompletionStream(w, r, req)
		return
	}

	// Create request-scoped timeout
	ctx, cancel := context.WithTimeout(r.Context(), HandlerTimeout)
	defer cancel()
	ctx = providers.WithChatRequestOverrides(ctx, req.MaxTokens, req.Temperature, req.TopP)
	ctx = providers.WithJSONOverrides(ctx, chatJSONOverrides(req))
	ctx = providers.WithResponseFormatOverride(ctx, responseFormatOverride(req.ResponseFormat))

	start := time.Now()
	result, err := s.provider.ChatWithInfoContext(ctx, systemPrompt, userContent)
	if err != nil {
		writeErrorDetail(w, http.StatusBadGateway, upstreamErrorDetail(err))
		return
	}

	meta := &LlamboMeta{
		Backend:    result.Provider,
		Model:      result.Model,
		DurationMs: time.Since(start).Milliseconds(),
	}

	// Populate usage if available
	var usage *Usage
	if result.Usage != nil {
		usage = &Usage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
		}
		meta.PromptTokens = result.Usage.PromptTokens
		meta.CompletionTokens = result.Usage.CompletionTokens
		meta.TotalTokens = result.Usage.TotalTokens
		if result.Usage.Cost != nil {
			meta.CostUSD = result.Usage.Cost.TotalCost
		}
	}
	if result.Route != nil {
		meta.RoutingMode = result.Route.Mode
		meta.RoutingIntent = string(result.Route.Intent)
		meta.RoutingReason = result.Route.Reason
	}

	resp := ChatCompletionResponse{
		ID:      generateID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   result.Model,
		Choices: []Choice{{
			Index:        0,
			Message:      &Message{Role: "assistant", Content: result.Content},
			FinishReason: normalizeOpenAIFinishReason(result.FinishReason),
		}},
		Usage:   usage,
		XLlambo: meta,
	}

	writeJSONWithProviderHeaders(w, http.StatusOK, resp, result.Provider, result.Model)
}

func chatJSONOverrides(req ChatCompletionRequest) map[string]any {
	if req.ResponseFormat == nil {
		return nil
	}
	generationConfig := responseFormatGenerationConfig(req)
	switch strings.TrimSpace(strings.ToLower(req.ResponseFormat.Type)) {
	case "json_object":
		return map[string]any{"generationConfig": generationConfig}
	case "json_schema":
		if schema := responseFormatSchema(req.ResponseFormat.JSONSchema); len(schema) > 0 {
			generationConfig["responseSchema"] = schema
		}
		return map[string]any{"generationConfig": generationConfig}
	default:
		return nil
	}
}

func responseFormatOverride(responseFormat *ResponseFormat) any {
	if responseFormat == nil {
		return nil
	}
	override := map[string]any{"type": responseFormat.Type}
	if len(responseFormat.JSONSchema) == 0 || string(responseFormat.JSONSchema) == "null" {
		return override
	}
	var schema any
	if err := json.Unmarshal(responseFormat.JSONSchema, &schema); err != nil {
		return override
	}
	override["json_schema"] = schema
	return override
}

func responseFormatGenerationConfig(req ChatCompletionRequest) map[string]any {
	generationConfig := map[string]any{
		"responseMimeType": "application/json",
	}
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		generationConfig["maxOutputTokens"] = *req.MaxTokens
	}
	if req.Temperature != nil && *req.Temperature > 0 {
		generationConfig["temperature"] = *req.Temperature
	}
	if req.TopP != nil && *req.TopP > 0 {
		generationConfig["topP"] = *req.TopP
	}
	return generationConfig
}

func responseFormatSchema(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var wrapped struct {
		Schema map[string]any `json:"schema"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Schema) > 0 {
		return wrapped.Schema
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err == nil {
		return schema
	}
	return nil
}

// handleEmbeddings handles POST /v1/embeddings
func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if s.embedder == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "Embedding provider not configured")
		return
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

	var req EmbeddingRequest
	if err := decodeSingleJSON(r.Body, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body: "+err.Error())
		return
	}

	if len(req.Input) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "Input array is required")
		return
	}

	embeddings, err := s.embedder.Embed(r.Context(), req.Input)
	if err != nil {
		errType, message := sanitizeUpstreamError(err)
		writeError(w, http.StatusBadGateway, errType, message)
		return
	}

	data := make([]EmbeddingData, len(embeddings))
	for i, emb := range embeddings {
		data[i] = EmbeddingData{
			Object:    "embedding",
			Index:     i,
			Embedding: emb,
		}
	}

	resp := EmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  s.embedder.ModelName(),
		Usage: Usage{
			PromptTokens: len(req.Input) * TokenEstimationMultiplier,
			TotalTokens:  len(req.Input) * TokenEstimationMultiplier,
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleListModels handles GET /v1/models
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	var models []ModelInfo

	for _, entry := range providers.FilterEnabledProviders(s.configs) {
		models = append(models, ModelInfo{
			ID:      entry.Config.Model,
			Object:  "model",
			OwnedBy: entry.Name,
		})
	}

	resp := ModelsResponse{
		Object: "list",
		Data:   models,
	}

	writeJSON(w, http.StatusOK, resp)
}
