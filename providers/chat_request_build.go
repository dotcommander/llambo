package providers

import (
	"context"
	"encoding/json"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func buildTextRequest(ctx context.Context, cfg Config, systemPrompt, userContent string) whtypes.TextRequest {
	messages := []whtypes.Message{whtypes.NewUserMessage(userContent)}
	switch cfg.GetProviderType() {
	case "anthropic", "gemini":
	default:
		messages = append([]whtypes.Message{whtypes.NewSystemMessage(systemPrompt)}, messages...)
	}

	request := whtypes.TextRequest{
		BaseRequest: whtypes.BaseRequest{
			Model:           cfg.Model,
			ProviderOptions: buildProviderOptions(cfg, ctx),
		},
		Messages:     messages,
		SystemPrompt: systemPrompt,
	}
	request.ResponseFormat = ResponseFormatOverrideFromContext(ctx)

	maxTokens := cfg.MaxTokens
	temperature := cfg.Temperature
	temperatureSet := temperature > 0
	var topP *float64
	if ov := ChatRequestOverridesFromContext(ctx); ov != nil {
		if ov.MaxTokens != nil {
			maxTokens = *ov.MaxTokens
		}
		if ov.Temperature != nil {
			temperature = *ov.Temperature
			temperatureSet = true
		}
		topP = ov.TopP
	}

	if maxTokens > 0 {
		request.MaxTokens = intPtr(maxTokens)
	}
	if temperatureSet {
		request.Temperature = float32Ptr(float32(temperature))
	}
	if topP != nil && *topP > 0 {
		request.TopP = float32Ptr(float32(*topP))
	}

	stops := StopSequencesFromContext(ctx)
	if len(stops) > 0 {
		if len(stops) > maxNativeStopSequences {
			stops = stops[:maxNativeStopSequences]
		}
		request.Stop = stops
	}

	request.Tools = buildWormholeTools(ctx)
	request.ToolChoice = buildWormholeToolChoice(ctx)
	if structured, ok := ctx.Value(structuredRequestKey{}).(StructuredChatRequest); ok {
		request.Messages = cloneStructuredMessages(structured.Messages)
		request.SystemPrompt = structured.SystemPrompt
		request.Tools = append([]whtypes.Tool(nil), structured.Tools...)
		for i := range request.Tools {
			request.Tools[i].InputSchema = cloneJSONMap(request.Tools[i].InputSchema)
			if request.Tools[i].Function != nil {
				function := *request.Tools[i].Function
				function.Parameters = cloneJSONMap(function.Parameters)
				request.Tools[i].Function = &function
			}
		}
		if structured.ToolChoice != nil {
			choice := *structured.ToolChoice
			request.ToolChoice = &choice
		}
		request.Stop = append([]string(nil), structured.Stop...)
		request.ResponseFormat = cloneJSONValue(structured.ResponseFormat)
		if structured.Temperature != nil {
			request.Temperature = float32Ptr(float32(*structured.Temperature))
		}
		if structured.TopP != nil {
			request.TopP = float32Ptr(float32(*structured.TopP))
		}
		if structured.MaxTokens != nil {
			request.MaxTokens = intPtr(*structured.MaxTokens)
		}
	}
	return request
}

func buildProviderOptions(cfg Config, ctx context.Context) map[string]any {
	opts := ExtraBodyForModel(cfg)
	overrides := JSONOverridesFromContext(ctx)
	if metadata := RequestMetadataFromContext(ctx); len(metadata) > 0 {
		if opts == nil {
			opts = make(map[string]any, len(metadata)+len(overrides))
		}
		opts["metadata"] = metadata
	}
	for k, v := range overrides {
		if opts == nil {
			opts = make(map[string]any, len(overrides))
		}
		if k == "generationConfig" {
			if incoming, ok := v.(map[string]any); ok {
				merged := map[string]any{}
				if existing, ok := opts[k].(map[string]any); ok {
					merged = cloneJSONMap(existing)
				}
				for generationKey, generationValue := range incoming {
					merged[generationKey] = cloneJSONValue(generationValue)
				}
				opts[k] = merged
				continue
			}
		}
		opts[k] = cloneJSONValue(v)
	}
	if len(opts) == 0 {
		return nil
	}
	return opts
}

func buildWormholeTools(ctx context.Context) []whtypes.Tool {
	tools := ToolsFromContext(ctx)
	if len(tools) == 0 {
		return nil
	}
	out := make([]whtypes.Tool, 0, len(tools))
	for _, t := range tools {
		var schema map[string]any
		if len(t.InputSchema) > 0 && json.Valid(t.InputSchema) {
			_ = json.Unmarshal(t.InputSchema, &schema)
		}
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, *whtypes.NewTool(t.Name, t.Description, schema))
	}
	return out
}

func buildWormholeToolChoice(ctx context.Context) *whtypes.ToolChoice {
	choice := ToolChoiceFromContext(ctx)
	if choice == nil {
		return nil
	}
	switch choice.Type {
	case "none":
		return &whtypes.ToolChoice{Type: whtypes.ToolChoiceTypeNone}
	case "any":
		return &whtypes.ToolChoice{Type: whtypes.ToolChoiceTypeAny}
	case "tool":
		return &whtypes.ToolChoice{Type: whtypes.ToolChoiceTypeSpecific, ToolName: choice.Name}
	default:
		return &whtypes.ToolChoice{Type: whtypes.ToolChoiceTypeAuto}
	}
}
