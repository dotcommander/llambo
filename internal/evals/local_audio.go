package evals

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const localAudioLimit = 32 << 20

// OMLXAudioAdapter calls a local OMLX server's audio endpoints.
type OMLXAudioAdapter struct {
	BaseURL             string
	APIKey              string
	Client              *http.Client
	VerifiedServedModel string
}

// ASRAudioInput carries one WAV payload for transcription.
type ASRAudioInput struct {
	Case     ASRCase
	WAVPath  string
	WAV      []byte
	FileName string
}

// ASREvaluation is the scored outcome of one transcription.
type ASREvaluation struct {
	ServedModel    string
	Transcript     string
	WER            float64
	CER            float64
	Latency        time.Duration
	RealTimeFactor float64
	InputWAV       WAVMetadata
	OutputRawJSON  json.RawMessage
}

// Transcribe performs a bounded audio request and validates the served identity.
func (a OMLXAudioAdapter) Transcribe(ctx context.Context, model string, input ASRAudioInput) (ASREvaluation, error) {
	if strings.TrimSpace(model) == "" || strings.TrimSpace(input.Case.ID) == "" || strings.TrimSpace(input.Case.Transcript) == "" {
		return ASREvaluation{}, errors.New("model, ASR case id, and transcript are required")
	}
	endpointModel, err := localOMLXModel(model)
	if err != nil {
		return ASREvaluation{}, err
	}
	audio, err := sealedTranscriptionAudio(input)
	if err != nil {
		return ASREvaluation{}, err
	}
	root, err := validatedOMLXRoot(a.BaseURL)
	if err != nil {
		return ASREvaluation{}, err
	}
	body, contentType, err := transcriptionForm(endpointModel, audio)
	if err != nil {
		return ASREvaluation{}, err
	}
	httpResult, err := a.doHTTP(ctx, http.MethodPost, root+"/v1/audio/transcriptions", body, contentType)
	if err != nil {
		return ASREvaluation{}, err
	}
	response, responseBody, elapsed := httpResult.response, httpResult.body, httpResult.elapsed
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ASREvaluation{}, fmt.Errorf("transcription HTTP %d: %s", response.StatusCode, boundedErrorText(responseBody))
	}
	var payload struct {
		Text     string `json:"text"`
		Model    string `json:"model"`
		Provider string `json:"provider"`
	}
	if err = json.Unmarshal(responseBody, &payload); err != nil {
		return ASREvaluation{}, fmt.Errorf("parse transcription response: %w", err)
	}
	if strings.TrimSpace(payload.Text) == "" {
		return ASREvaluation{}, errors.New("transcription response is missing text")
	}
	served, err := localServedIdentity(model, firstNonEmpty(payload.Provider, response.Header.Get("X-Llambo-Provider")), firstNonEmpty(payload.Model, response.Header.Get("X-Llambo-Model")), a.VerifiedServedModel)
	if err != nil {
		return ASREvaluation{}, err
	}
	return ASREvaluation{ServedModel: served, Transcript: payload.Text, WER: WordErrorRate(input.Case.Transcript, payload.Text), CER: CharErrorRate(input.Case.Transcript, payload.Text), Latency: elapsed, RealTimeFactor: elapsed.Seconds() / audio.metadata.DurationSeconds, InputWAV: audio.metadata, OutputRawJSON: append(json.RawMessage(nil), responseBody...)}, nil
}

// TTSSpeechRequest is the OMLX speech-synthesis request body.
type TTSSpeechRequest struct {
	Model  string `json:"model"`
	Input  string `json:"input"`
	Voice  string `json:"voice,omitempty"`
	Format string `json:"response_format,omitempty"`
}

// TTSRoundTripper transcribes synthesized audio for round-trip scoring.
type TTSRoundTripper interface {
	TranscribeGeneratedAudio(context.Context, []byte) (string, error)
}

// OMLXTTSRoundTripper scores TTS output by transcribing it with ASR.
type OMLXTTSRoundTripper struct {
	Adapter OMLXAudioAdapter
	Model   string
}

// TranscribeGeneratedAudio transcribes one synthesized WAV.
func (r OMLXTTSRoundTripper) TranscribeGeneratedAudio(ctx context.Context, wav []byte) (string, error) {
	result, err := r.Adapter.Transcribe(ctx, r.Model, ASRAudioInput{
		Case: ASRCase{ID: "tts-round-trip", Transcript: "round trip transcription"},
		WAV:  wav,
	})
	if err != nil {
		return "", err
	}
	return result.Transcript, nil
}

// TTSEvaluation is the scored outcome of one synthesis round trip.
type TTSEvaluation struct {
	ServedModel        string
	WAVBase64          string
	WAV                WAVMetadata
	Latency            time.Duration
	IntelligibilityWER *float64
}

// Synthesize generates speech and scores it through the round-tripper.
func (a OMLXAudioAdapter) Synthesize(ctx context.Context, request TTSSpeechRequest, roundTrip TTSRoundTripper) (TTSEvaluation, error) {
	if strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.Input) == "" {
		return TTSEvaluation{}, errors.New("TTS model and input are required")
	}
	requestedModel := request.Model
	endpointModel, err := localOMLXModel(requestedModel)
	if err != nil {
		return TTSEvaluation{}, err
	}
	root, err := validatedOMLXRoot(a.BaseURL)
	if err != nil {
		return TTSEvaluation{}, err
	}
	if request.Format == "" {
		request.Format = "wav"
	}
	request.Model = endpointModel
	body, err := json.Marshal(request)
	if err != nil {
		return TTSEvaluation{}, err
	}
	if len(body) > localAdapterResponseLimit {
		return TTSEvaluation{}, fmt.Errorf("speech request exceeds %d bytes", localAdapterResponseLimit)
	}
	httpResult, err := a.doHTTP(ctx, http.MethodPost, root+"/v1/audio/speech", bytes.NewReader(body), applicationJSON)
	if err != nil {
		return TTSEvaluation{}, err
	}
	response, wav, elapsed := httpResult.response, httpResult.body, httpResult.elapsed
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return TTSEvaluation{}, fmt.Errorf("speech HTTP %d: %s", response.StatusCode, boundedErrorText(wav))
	}
	metadata, err := ValidateWAV(wav)
	if err != nil {
		return TTSEvaluation{}, fmt.Errorf("validate generated WAV: %w", err)
	}
	served, err := localServedIdentity(requestedModel, response.Header.Get("X-Llambo-Provider"), response.Header.Get("X-Llambo-Model"), a.VerifiedServedModel)
	if err != nil {
		return TTSEvaluation{}, err
	}
	result := TTSEvaluation{ServedModel: served, WAVBase64: base64.StdEncoding.EncodeToString(wav), WAV: metadata, Latency: elapsed}
	if roundTrip != nil {
		transcript, err := roundTrip.TranscribeGeneratedAudio(ctx, wav)
		if err != nil {
			return TTSEvaluation{}, fmt.Errorf("round-trip ASR: %w", err)
		}
		wer := WordErrorRate(request.Input, transcript)
		result.IntelligibilityWER = &wer
	}
	return result, nil
}

func (a OMLXAudioAdapter) doHTTP(ctx context.Context, method, endpoint string, body io.Reader, contentType string) (localOMLXHTTPResponse, error) {
	result, err := executeLocalOMLXHTTP(ctx, localOMLXHTTPRequest{
		method:         method,
		endpoint:       endpoint,
		body:           body,
		contentType:    contentType,
		apiKey:         a.APIKey,
		client:         a.Client,
		responseLimit:  localAudioLimit,
		buildError:     "build audio request",
		requestError:   "request audio endpoint",
		measureLatency: true,
	})
	if err != nil {
		return localOMLXHTTPResponse{elapsed: result.elapsed}, err
	}
	defer func() { _ = result.response.Body.Close() }()
	return result, nil
}

func validatedOMLXRoot(baseURL string) (string, error) {
	if err := ValidateOMLXLoopbackBaseURL(baseURL); err != nil {
		return "", err
	}
	return normalizeOMLXBaseURL(baseURL)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
