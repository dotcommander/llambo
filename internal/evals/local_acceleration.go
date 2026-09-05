package evals

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type AccelerationChatClient interface {
	AccelerationChat(context.Context, AccelerationChatRequest) (AccelerationChatResponse, error)
}

type AccelerationChatRequest struct {
	Model           string `json:"model"`
	Prompt          string `json:"prompt"`
	MaxOutputTokens int    `json:"max_output_tokens"`
}

type AccelerationChatResponse struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Content      string `json:"content"`
	OutputTokens int    `json:"output_tokens"`
}

// OMLXAccelerationChatClient is the direct, bounded OpenAI-compatible client
// used for the off/on conditions. It is kept separate from settings mutation.
type OMLXAccelerationChatClient struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func (c OMLXAccelerationChatClient) AccelerationChat(ctx context.Context, request AccelerationChatRequest) (AccelerationChatResponse, error) {
	if strings.TrimSpace(request.Prompt) == "" || request.MaxOutputTokens < 1 {
		return AccelerationChatResponse{}, fmt.Errorf("prompt and positive maximum output tokens are required")
	}
	model, err := localProviderModel(request.Model, "omlx")
	if err != nil {
		return AccelerationChatResponse{}, err
	}
	root, err := validatedOMLXRoot(c.BaseURL)
	if err != nil {
		return AccelerationChatResponse{}, err
	}
	body, err := json.Marshal(struct {
		Model     string            `json:"model"`
		Messages  []ToolChatMessage `json:"messages"`
		MaxTokens int               `json:"max_tokens"`
	}{Model: model, Messages: []ToolChatMessage{{Role: "user", Content: request.Prompt}}, MaxTokens: request.MaxOutputTokens})
	if err != nil {
		return AccelerationChatResponse{}, err
	}
	if len(body) > localAdapterResponseLimit {
		return AccelerationChatResponse{}, fmt.Errorf("acceleration request exceeds %d bytes", localAdapterResponseLimit)
	}
	httpResponse, err := executeLocalOMLXHTTP(ctx, localOMLXHTTPRequest{
		method:        http.MethodPost,
		endpoint:      root + "/v1/chat/completions",
		body:          bytes.NewReader(body),
		contentType:   "application/json",
		apiKey:        c.APIKey,
		client:        c.Client,
		responseLimit: localAdapterResponseLimit,
		buildError:    "build acceleration request",
		requestError:  "request acceleration chat",
	})
	if err != nil {
		return AccelerationChatResponse{}, err
	}
	response := httpResponse.response
	defer response.Body.Close()
	responseBody := httpResponse.body
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return AccelerationChatResponse{}, fmt.Errorf("acceleration chat HTTP %d: %s", response.StatusCode, boundedErrorText(responseBody))
	}
	var payload struct {
		Model   string `json:"model"`
		Choices []struct {
			Message ToolChatMessage `json:"message"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return AccelerationChatResponse{}, fmt.Errorf("parse acceleration response: %w", err)
	}
	if len(payload.Choices) != 1 || strings.TrimSpace(payload.Choices[0].Message.Content) == "" || payload.Usage.CompletionTokens < 1 {
		return AccelerationChatResponse{}, fmt.Errorf("acceleration response requires one nonempty choice and completion usage")
	}
	return AccelerationChatResponse{Provider: "omlx", Model: firstNonEmpty(payload.Model, response.Header.Get("X-Llambo-Model")), Content: payload.Choices[0].Message.Content, OutputTokens: payload.Usage.CompletionTokens}, nil
}

type AccelerationConditionRecord struct {
	CaseID      string       `json:"case_id"`
	MTPEnabled  bool         `json:"mtp_enabled"`
	Warmup      bool         `json:"warmup"`
	ServedModel string       `json:"served_model"`
	Result      PairedResult `json:"result"`
}

// EvaluateAccelerationCondition issues one direct exact-model chat call. The
// caller performs the guarded settings transition; this method deliberately
// does not mutate OMLX state itself.
func EvaluateAccelerationCondition(ctx context.Context, client AccelerationChatClient, request AccelerationChatRequest, c AccelerationCase, enabled, warmup bool, verifiedServedModel string) (AccelerationConditionRecord, error) {
	if client == nil || strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.Prompt) == "" || strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Oracle) == "" {
		return AccelerationConditionRecord{}, fmt.Errorf("client, model, prompt, case id, and oracle are required")
	}
	if request.MaxOutputTokens < 1 {
		return AccelerationConditionRecord{}, fmt.Errorf("positive maximum output tokens are required")
	}
	started := time.Now()
	response, err := client.AccelerationChat(ctx, request)
	latency := time.Since(started)
	if err != nil {
		return AccelerationConditionRecord{}, fmt.Errorf("acceleration chat: %w", err)
	}
	served, err := localServedIdentity(request.Model, response.Provider, response.Model, verifiedServedModel)
	if err != nil {
		return AccelerationConditionRecord{}, err
	}
	if response.OutputTokens < 1 || latency <= 0 {
		return AccelerationConditionRecord{}, fmt.Errorf("acceleration response requires positive output tokens and latency")
	}
	return AccelerationConditionRecord{CaseID: c.ID, MTPEnabled: enabled, Warmup: warmup, ServedModel: served, Result: PairedResult{AnswerCorrect: strings.TrimSpace(response.Content) == strings.TrimSpace(c.Oracle), LatencyMS: float64(latency) / float64(time.Millisecond), TokensPerSecond: float64(response.OutputTokens) / latency.Seconds()}}, nil
}

// OMLXSettingsGuard owns the only permitted persistent mutation in paired MTP
// evaluation. It restores the original bytes, hash, reload state, and health
// after both success and failure. Any third-party change while enabled fails
// closed without overwriting that actor's state.
type OMLXSettingsGuard struct {
	Path        string
	ParentModel string
	Reload      func(context.Context) error
	Health      func(context.Context) error
}

func (g OMLXSettingsGuard) WithMTPEnabled(ctx context.Context, enabled bool, run func(context.Context) error) (err error) {
	if strings.TrimSpace(g.Path) == "" || strings.TrimSpace(g.ParentModel) == "" || g.Reload == nil || g.Health == nil || run == nil {
		return fmt.Errorf("settings path, parent model, reload, health, and run are required")
	}
	original, err := os.ReadFile(g.Path)
	if err != nil {
		return fmt.Errorf("read model settings: %w", err)
	}
	info, err := os.Stat(g.Path)
	if err != nil {
		return fmt.Errorf("stat model settings: %w", err)
	}
	originalHash := bytesSHA256(original)
	changed, err := setMTPEnabled(original, g.ParentModel, enabled)
	if err != nil {
		return err
	}
	if bytesSHA256(changed) == originalHash {
		if err := g.Reload(ctx); err != nil {
			return fmt.Errorf("reload unchanged model settings: %w", err)
		}
		if err := g.Health(ctx); err != nil {
			return fmt.Errorf("verify OMLX health with unchanged MTP settings: %w", err)
		}
		if err := run(ctx); err != nil {
			return err
		}
		current, err := os.ReadFile(g.Path)
		if err != nil {
			return fmt.Errorf("read unchanged model settings after run: %w", err)
		}
		if bytesSHA256(current) != originalHash {
			return fmt.Errorf("model settings changed concurrently during guarded run")
		}
		return nil
	}
	current, err := os.ReadFile(g.Path)
	if err != nil {
		return fmt.Errorf("re-read model settings before mutation: %w", err)
	}
	if bytesSHA256(current) != originalHash {
		return fmt.Errorf("model settings changed concurrently before mutation")
	}
	if err := os.WriteFile(g.Path, changed, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write guarded model settings: %w", err)
	}
	changedHash := bytesSHA256(changed)
	defer func() {
		restoreCtx := context.WithoutCancel(ctx)
		current, readErr := os.ReadFile(g.Path)
		if readErr != nil {
			err = errors.Join(err, fmt.Errorf("read model settings for restore: %w", readErr))
			return
		}
		if bytesSHA256(current) != changedHash {
			err = errors.Join(err, fmt.Errorf("model settings changed concurrently during guarded run; refusing overwrite"))
			return
		}
		if writeErr := os.WriteFile(g.Path, original, info.Mode().Perm()); writeErr != nil {
			err = errors.Join(err, fmt.Errorf("restore model settings: %w", writeErr))
			return
		}
		restored, verifyErr := os.ReadFile(g.Path)
		if verifyErr != nil || bytesSHA256(restored) != originalHash {
			if verifyErr != nil {
				err = errors.Join(err, fmt.Errorf("verify restored model settings: %w", verifyErr))
			} else {
				err = errors.Join(err, fmt.Errorf("restored model settings hash mismatch"))
			}
			return
		}
		if reloadErr := g.Reload(restoreCtx); reloadErr != nil {
			err = errors.Join(err, fmt.Errorf("reload restored model settings: %w", reloadErr))
		}
		if healthErr := g.Health(restoreCtx); healthErr != nil {
			err = errors.Join(err, fmt.Errorf("verify OMLX health after restore: %w", healthErr))
		}
	}()
	if err := g.Reload(ctx); err != nil {
		return fmt.Errorf("reload guarded model settings: %w", err)
	}
	if err := g.Health(ctx); err != nil {
		return fmt.Errorf("verify OMLX health after MTP change: %w", err)
	}
	return run(ctx)
}

// AccelerationLocalExecutor runs one excluded warm-up for each off/on condition,
// then returns one paired observation for every sealed case. Settings mutation
// and restoration are owned entirely by Guard.
type AccelerationLocalExecutor struct {
	Client        AccelerationChatClient
	Guard         OMLXSettingsGuard
	Model         string
	VerifiedModel string
	MaxTokens     int
	mu            sync.Mutex
	warmed        map[bool]bool
}

func (e *AccelerationLocalExecutor) ExecuteLocal(ctx context.Context, _ LocalRunManifest, raw json.RawMessage) (LocalCall, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var c AccelerationCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return LocalCall{}, err
	}
	if e.Client == nil || e.MaxTokens < 1 {
		return LocalCall{}, fmt.Errorf("acceleration client and positive token limit are required")
	}
	if e.warmed == nil {
		e.warmed = make(map[bool]bool, 2)
	}
	results := make(map[bool]AccelerationConditionRecord, 2)
	for _, enabled := range []bool{false, true} {
		err := e.Guard.WithMTPEnabled(ctx, enabled, func(runCtx context.Context) error {
			request := AccelerationChatRequest{Model: e.Model, Prompt: c.Prompt, MaxOutputTokens: e.MaxTokens}
			if !e.warmed[enabled] {
				if _, err := EvaluateAccelerationCondition(runCtx, e.Client, request, c, enabled, true, e.VerifiedModel); err != nil {
					return fmt.Errorf("excluded warm-up: %w", err)
				}
				e.warmed[enabled] = true
			}
			record, err := EvaluateAccelerationCondition(runCtx, e.Client, request, c, enabled, false, e.VerifiedModel)
			if err != nil {
				return err
			}
			results[enabled] = record
			return nil
		})
		if err != nil {
			return LocalCall{}, err
		}
	}
	out, err := json.Marshal(struct {
		Off []PairedResult `json:"off"`
		On  []PairedResult `json:"on"`
	}{[]PairedResult{results[false].Result}, []PairedResult{results[true].Result}})
	if err != nil {
		return LocalCall{}, err
	}
	return LocalCall{ServedModel: results[true].ServedModel, Output: out}, nil
}

func bytesSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func setMTPEnabled(data []byte, parent string, enabled bool) ([]byte, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse model settings: %w", err)
	}
	if root == nil {
		return nil, fmt.Errorf("parse model settings: root object is required")
	}
	if raw, ok := root[parent]; ok {
		updated, err := setMTPInModelObject(raw, enabled)
		if err != nil {
			return nil, fmt.Errorf("update model %q: %w", parent, err)
		}
		root[parent] = updated
		return json.Marshal(root)
	}
	rawModels, ok := root["models"]
	if !ok {
		return nil, fmt.Errorf("model settings do not contain parent %q", parent)
	}
	var byID map[string]json.RawMessage
	if json.Unmarshal(rawModels, &byID) == nil && byID != nil {
		raw, found := byID[parent]
		if !found {
			return nil, fmt.Errorf("model settings do not contain parent %q", parent)
		}
		updated, err := setMTPInModelObject(raw, enabled)
		if err != nil {
			return nil, fmt.Errorf("update model %q: %w", parent, err)
		}
		byID[parent] = updated
		encoded, err := json.Marshal(byID)
		if err != nil {
			return nil, err
		}
		root["models"] = encoded
		return json.Marshal(root)
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(rawModels, &entries); err != nil {
		return nil, fmt.Errorf("parse model settings list: %w", err)
	}
	found := 0
	for _, entry := range entries {
		id := ""
		for _, key := range []string{"id", "model", "name"} {
			if raw := entry[key]; raw != nil {
				_ = json.Unmarshal(raw, &id)
				if id != "" {
					break
				}
			}
		}
		if id != parent {
			continue
		}
		found++
		entry["vlm_mtp_enabled"] = json.RawMessage(fmt.Sprintf("%t", enabled))
	}
	if found != 1 {
		return nil, fmt.Errorf("model settings contain %d entries for parent %q", found, parent)
	}
	var err error
	root["models"], err = json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	return json.Marshal(root)
}

func setMTPInModelObject(raw json.RawMessage, enabled bool) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("parse model settings object: %w", err)
	}
	if object == nil {
		return nil, fmt.Errorf("model settings must be an object")
	}
	object["vlm_mtp_enabled"] = json.RawMessage(fmt.Sprintf("%t", enabled))
	updated, err := json.Marshal(object)
	return updated, err
}
