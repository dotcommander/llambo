package evals

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const localAudioLimit = 32 << 20

type OMLXAudioAdapter struct {
	BaseURL             string
	APIKey              string
	Client              *http.Client
	VerifiedServedModel string
}

type ASRAudioInput struct {
	Case     ASRCase
	WAVPath  string
	WAV      []byte
	FileName string
}

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

func (a OMLXAudioAdapter) Transcribe(ctx context.Context, model string, input ASRAudioInput) (ASREvaluation, error) {
	if strings.TrimSpace(model) == "" || strings.TrimSpace(input.Case.ID) == "" || strings.TrimSpace(input.Case.Transcript) == "" {
		return ASREvaluation{}, fmt.Errorf("model, ASR case id, and transcript are required")
	}
	endpointModel, err := localProviderModel(model, "omlx")
	if err != nil {
		return ASREvaluation{}, err
	}
	wav, filename, err := loadSealedWAV(input)
	if err != nil {
		return ASREvaluation{}, err
	}
	if input.WAVPath != "" {
		got := fmt.Sprintf("%x", sha256.Sum256(wav))
		if input.Case.WAVSHA256 == "" || !strings.EqualFold(got, input.Case.WAVSHA256) {
			return ASREvaluation{}, fmt.Errorf("sealed WAV SHA-256 mismatch")
		}
	}
	metadata, err := ValidateWAV(wav)
	if err != nil {
		return ASREvaluation{}, fmt.Errorf("validate sealed WAV: %w", err)
	}
	root, err := validatedOMLXRoot(a.BaseURL)
	if err != nil {
		return ASREvaluation{}, err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("model", endpointModel); err != nil {
		return ASREvaluation{}, err
	}
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		return ASREvaluation{}, err
	}
	if _, err := part.Write(wav); err != nil {
		return ASREvaluation{}, err
	}
	if err := form.Close(); err != nil {
		return ASREvaluation{}, err
	}
	response, responseBody, elapsed, err := a.doHTTP(ctx, http.MethodPost, root+"/v1/audio/transcriptions", &body, form.FormDataContentType())
	if err != nil {
		return ASREvaluation{}, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ASREvaluation{}, fmt.Errorf("transcription HTTP %d: %s", response.StatusCode, boundedErrorText(responseBody))
	}
	var payload struct {
		Text     string `json:"text"`
		Model    string `json:"model"`
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return ASREvaluation{}, fmt.Errorf("parse transcription response: %w", err)
	}
	if strings.TrimSpace(payload.Text) == "" {
		return ASREvaluation{}, fmt.Errorf("transcription response is missing text")
	}
	served, err := localServedIdentity(model, firstNonEmpty(payload.Provider, response.Header.Get("X-Llambo-Provider")), firstNonEmpty(payload.Model, response.Header.Get("X-Llambo-Model")), a.VerifiedServedModel)
	if err != nil {
		return ASREvaluation{}, err
	}
	return ASREvaluation{ServedModel: served, Transcript: payload.Text, WER: WordErrorRate(input.Case.Transcript, payload.Text), CER: CharErrorRate(input.Case.Transcript, payload.Text), Latency: elapsed, RealTimeFactor: elapsed.Seconds() / metadata.DurationSeconds, InputWAV: metadata, OutputRawJSON: append(json.RawMessage(nil), responseBody...)}, nil
}

type TTSSpeechRequest struct {
	Model  string `json:"model"`
	Input  string `json:"input"`
	Voice  string `json:"voice,omitempty"`
	Format string `json:"response_format,omitempty"`
}

type TTSRoundTripper interface {
	TranscribeGeneratedAudio(context.Context, []byte) (string, error)
}

type OMLXTTSRoundTripper struct {
	Adapter OMLXAudioAdapter
	Model   string
}

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

type TTSEvaluation struct {
	ServedModel        string
	WAVBase64          string
	WAV                WAVMetadata
	Latency            time.Duration
	IntelligibilityWER *float64
}

func (a OMLXAudioAdapter) Synthesize(ctx context.Context, request TTSSpeechRequest, roundTrip TTSRoundTripper) (TTSEvaluation, error) {
	if strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.Input) == "" {
		return TTSEvaluation{}, fmt.Errorf("TTS model and input are required")
	}
	requestedModel := request.Model
	endpointModel, err := localProviderModel(requestedModel, "omlx")
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
	response, wav, elapsed, err := a.doHTTP(ctx, http.MethodPost, root+"/v1/audio/speech", bytes.NewReader(body), "application/json")
	if err != nil {
		return TTSEvaluation{}, err
	}
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

func (a OMLXAudioAdapter) doHTTP(ctx context.Context, method, endpoint string, body io.Reader, contentType string) (*http.Response, []byte, time.Duration, error) {
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
		return nil, nil, result.elapsed, err
	}
	defer result.response.Body.Close()
	return result.response, result.body, result.elapsed, nil
}

func validatedOMLXRoot(baseURL string) (string, error) {
	if err := ValidateOMLXLoopbackBaseURL(baseURL); err != nil {
		return "", err
	}
	return normalizeOMLXBaseURL(baseURL)
}

func loadSealedWAV(input ASRAudioInput) ([]byte, string, error) {
	if len(input.WAV) > 0 && strings.TrimSpace(input.WAVPath) != "" {
		return nil, "", fmt.Errorf("provide sealed WAV data or path, not both")
	}
	wav := input.WAV
	filename := input.FileName
	if input.WAVPath != "" {
		data, err := os.ReadFile(input.WAVPath)
		if err != nil {
			return nil, "", fmt.Errorf("read sealed WAV: %w", err)
		}
		wav = data
		if filename == "" {
			filename = filepath.Base(input.WAVPath)
		}
	}
	if len(wav) == 0 || len(wav) > localAudioLimit {
		return nil, "", fmt.Errorf("sealed WAV must be between 1 and %d bytes", localAudioLimit)
	}
	if filename == "" {
		filename = "sealed.wav"
	}
	return append([]byte(nil), wav...), filename, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
