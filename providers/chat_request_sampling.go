package providers

import (
	"context"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func applyTextSampling(request *whtypes.TextRequest, ctx context.Context, cfg Config) {
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
	if temperatureSet && !rejectsGeminiTemperature(cfg) {
		request.Temperature = float32Ptr(float32(temperature))
	}
	if topP != nil && *topP > 0 {
		request.TopP = float32Ptr(float32(*topP))
	}
}

func applyStructuredTextRequest(request *whtypes.TextRequest, cfg Config, structured StructuredChatRequest) {
	request.Messages = cloneStructuredMessages(structured.Messages)
	request.SystemPrompt = structured.SystemPrompt
	request.Tools = cloneStructuredTools(structured.Tools)
	if structured.ToolChoice != nil {
		choice := *structured.ToolChoice
		request.ToolChoice = &choice
	}
	request.Stop = append([]string(nil), structured.Stop...)
	request.ResponseFormat = cloneJSONValue(structured.ResponseFormat)
	if structured.Temperature != nil && !rejectsGeminiTemperature(cfg) {
		request.Temperature = float32Ptr(float32(*structured.Temperature))
	}
	if structured.TopP != nil {
		request.TopP = float32Ptr(float32(*structured.TopP))
	}
	if structured.MaxTokens != nil {
		request.MaxTokens = intPtr(*structured.MaxTokens)
	}
}

func cloneStructuredTools(tools []whtypes.Tool) []whtypes.Tool {
	cloned := append([]whtypes.Tool(nil), tools...)
	for i := range cloned {
		cloned[i].InputSchema = cloneJSONMap(cloned[i].InputSchema)
		if cloned[i].Function == nil {
			continue
		}
		function := *cloned[i].Function
		function.Parameters = cloneJSONMap(function.Parameters)
		cloned[i].Function = &function
	}
	return cloned
}
