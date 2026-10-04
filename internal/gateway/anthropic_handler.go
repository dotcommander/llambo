package gateway

import (
	"context"
	"encoding/json"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
	"net/http"
	"strings"

	"github.com/dotcommander/llambo/providers"
)

// handleAnthropicMessages handles POST /v1/messages.
// It translates Anthropic-compatible payloads to internal OpenAI-compatible flow.
func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	parsed, ok := s.parseAnthropicMessagesRequest(w, r)
	if !ok {
		return
	}

	if parsed.Translated.Stream {
		s.handleAnthropicMessagesStream(w, r, parsed)
		return
	}

	result, err := s.executeAnthropicMessages(r.Context(), parsed)
	if err != nil {
		_, message := sanitizeUpstreamError(err)
		writeAnthropicError(w, anthropicStatusFromError(err), parsed.RequestID, message)
		return
	}

	resp := buildAnthropicResponse(result, parsed.Original.StopSequences)
	writeJSONWithProviderHeaders(w, http.StatusOK, resp, result.Provider, result.Model)
}

func (s *Server) parseAnthropicMessagesRequest(w http.ResponseWriter, r *http.Request) (*anthropicParsedRequest, bool) {
	requestID := anthropicID("req")
	w.Header().Set("request-id", requestID)

	rawAnthropicVersion := strings.TrimSpace(r.Header.Get("anthropic-version"))
	if s.anthropicStrict && rawAnthropicVersion == "" {
		writeAnthropicError(w, http.StatusBadRequest, requestID, "anthropic-version header is required in strict mode")
		return nil, false
	}
	antVersion := rawAnthropicVersion
	if antVersion == "" {
		antVersion = defaultAnthropicVersion
	}
	w.Header().Set("anthropic-version", antVersion)

	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

	var req AnthropicMessagesRequest
	if err := decodeSingleJSON(r.Body, &req, s.anthropicStrict); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, requestID, "Invalid request body: "+err.Error())
		return nil, false
	}
	if len(req.StopSequences) > maxAnthropicStopSequences {
		writeAnthropicError(w, http.StatusBadRequest, requestID, "stop_sequences exceeds maximum of 256")
		return nil, false
	}
	req.StopSequences = normalizeStopSequences(req.StopSequences)

	parsedTools, toolChoice, err := parseAnthropicTooling(req.Tools, req.ToolChoice)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, requestID, err.Error())
		return nil, false
	}
	if err := validateAnthropicOptionalFields(req); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, requestID, err.Error())
		return nil, false
	}

	translated, err := translateAnthropicRequest(req)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, requestID, err.Error())
		return nil, false
	}

	if err := s.prepareChat(&translated); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, requestID, err.Error())
		return nil, false
	}
	translated.structured.Stop = req.StopSequences
	for _, tool := range parsedTools {
		var schema map[string]any
		_ = json.Unmarshal(tool.InputSchema, &schema)
		translated.structured.Tools = append(translated.structured.Tools, whtypes.Tool{Name: tool.Name, Description: tool.Description, InputSchema: schema})
	}
	if toolChoice != nil {
		kind := whtypes.ToolChoiceType(toolChoice.Type)
		if kind == "tool" {
			kind = whtypes.ToolChoiceTypeSpecific
		}
		translated.structured.ToolChoice = &whtypes.ToolChoice{Type: kind, ToolName: toolChoice.Name}
	}
	return &anthropicParsedRequest{
		RequestID:     requestID,
		Original:      req,
		Translated:    translated,
		Tools:         parsedTools,
		ToolChoice:    toolChoice,
		Metadata:      normalizeAnthropicMetadata(req.Metadata),
		JSONOverrides: anthropicJSONOverrides(req),
	}, true
}

func (s *Server) executeAnthropicMessages(parent context.Context, parsed *anthropicParsedRequest) (providers.ChatResult, error) {
	ctx, cancel := context.WithTimeout(parent, s.effectiveHandlerTimeout())
	defer cancel()
	ctx = parsed.enrichContext(ctx)

	return s.executeChat(ctx, parsed.Translated)
}
