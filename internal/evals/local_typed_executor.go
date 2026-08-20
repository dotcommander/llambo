package evals

import (
	"context"
	"encoding/json"
	"fmt"
)

type ToolLocalExecutor struct {
	Client              ToolChatClient
	VerifiedServedModel string
}

func (e ToolLocalExecutor) ExecuteLocal(ctx context.Context, manifest LocalRunManifest, raw json.RawMessage) (LocalCall, error) {
	var c ToolCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return LocalCall{}, err
	}
	required := ""
	if c.Expected != nil {
		required = c.Expected.Name
	}
	call, _, err := EvaluateToolCase(ctx, e.Client, ToolEvaluationRequest{Case: c, VerifiedServedModel: e.VerifiedServedModel, Chat: ToolChatRequest{Model: manifest.Identity.RequestedModel, Messages: []ToolChatMessage{{Role: "user", Content: c.Prompt}}, Tools: c.Tools, RequiredTool: required}})
	return call, err
}

type EmbeddingLocalExecutor struct{ Adapter OMLXEmbeddingAdapter }

func (e EmbeddingLocalExecutor) ExecuteLocal(ctx context.Context, manifest LocalRunManifest, raw json.RawMessage) (LocalCall, error) {
	var c EmbeddingCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return LocalCall{}, err
	}
	result, err := e.Adapter.Evaluate(ctx, manifest.Identity.RequestedModel, c)
	if err != nil {
		return LocalCall{}, err
	}
	output, err := json.Marshal(struct {
		Scores                       map[string]map[string]float64 `json:"scores"`
		RecallAt1, MRRAt10, NDCGAt10 float64
	}{result.Scores, result.RecallAt1, result.MRRAt10, result.NDCGAt10})
	if err != nil {
		return LocalCall{}, err
	}
	return LocalCall{ServedModel: result.ServedModel, Output: output}, nil
}

type ASRLocalExecutor struct{ Adapter OMLXAudioAdapter }

func (e ASRLocalExecutor) ExecuteLocal(ctx context.Context, manifest LocalRunManifest, raw json.RawMessage) (LocalCall, error) {
	var c ASRCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return LocalCall{}, err
	}
	result, err := e.Adapter.Transcribe(ctx, manifest.Identity.RequestedModel, ASRAudioInput{Case: c, WAVPath: c.WAVPath})
	if err != nil {
		return LocalCall{}, err
	}
	output, err := json.Marshal(struct {
		Transcript                          string `json:"transcript"`
		WER, CER, LatencyMS, RealTimeFactor float64
	}{result.Transcript, result.WER, result.CER, float64(result.Latency.Milliseconds()), result.RealTimeFactor})
	if err != nil {
		return LocalCall{}, err
	}
	return LocalCall{ServedModel: result.ServedModel, Output: output}, nil
}

type TTSLocalExecutor struct {
	Adapter   OMLXAudioAdapter
	RoundTrip TTSRoundTripper
}

func (e TTSLocalExecutor) ExecuteLocal(ctx context.Context, manifest LocalRunManifest, raw json.RawMessage) (LocalCall, error) {
	var c TTSCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return LocalCall{}, err
	}
	result, err := e.Adapter.Synthesize(ctx, TTSSpeechRequest{Model: manifest.Identity.RequestedModel, Input: c.Prompt, Format: "wav"}, e.RoundTrip)
	if err != nil {
		return LocalCall{}, err
	}
	output, err := json.Marshal(struct {
		WAVBase64          string   `json:"wav_base64"`
		SampleRate         uint32   `json:"sample_rate"`
		Channels           uint16   `json:"channels"`
		DurationSeconds    float64  `json:"duration_seconds"`
		IntelligibilityWER *float64 `json:"intelligibility_wer,omitempty"`
	}{result.WAVBase64, result.WAV.SampleRate, result.WAV.Channels, result.WAV.DurationSeconds, result.IntelligibilityWER})
	if err != nil {
		return LocalCall{}, err
	}
	return LocalCall{ServedModel: result.ServedModel, Output: output}, nil
}

type UnsupportedAccelerationExecutor struct{}

func (UnsupportedAccelerationExecutor) ExecuteLocal(context.Context, LocalRunManifest, json.RawMessage) (LocalCall, error) {
	return LocalCall{}, fmt.Errorf("acceleration execution requires a separately authorized exclusive OMLX settings window")
}
