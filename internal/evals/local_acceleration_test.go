package evals

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type accelerationClientFunc func(context.Context, AccelerationChatRequest) (AccelerationChatResponse, error)

func (f accelerationClientFunc) AccelerationChat(ctx context.Context, request AccelerationChatRequest) (AccelerationChatResponse, error) {
	return f(ctx, request)
}

func TestEvaluateAccelerationConditionCapturesPairedMetrics(t *testing.T) {
	t.Parallel()
	record, err := EvaluateAccelerationCondition(context.Background(), accelerationClientFunc(func(_ context.Context, request AccelerationChatRequest) (AccelerationChatResponse, error) {
		if request.Model != "omlx/parent" {
			t.Fatal("wrong model")
		}
		return AccelerationChatResponse{Provider: "omlx", Model: "parent", Content: "42", OutputTokens: 2}, nil
	}), AccelerationChatRequest{Model: "omlx/parent", Prompt: "answer", MaxOutputTokens: 32}, AccelerationCase{ID: "trace", Oracle: "42"}, true, false, "")
	if err != nil || !record.Result.AnswerCorrect || record.Result.TokensPerSecond <= 0 || !record.MTPEnabled {
		t.Fatalf("record %#v, err %v", record, err)
	}
}

func TestOMLXAccelerationChatClientUsesExactEndpointModel(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != "parent" {
			t.Error("wrong direct acceleration request")
		}
		_, _ = w.Write([]byte(`{"model":"parent","choices":[{"message":{"content":"42"}}],"usage":{"completion_tokens":2}}`))
	}))
	t.Cleanup(server.Close)
	response, err := (OMLXAccelerationChatClient{BaseURL: server.URL, Client: server.Client()}).AccelerationChat(context.Background(), AccelerationChatRequest{Model: "omlx/parent", Prompt: "answer", MaxOutputTokens: 32})
	if err != nil || response.Provider != "omlx" || response.Model != "parent" || response.Content != "42" {
		t.Fatalf("response %#v, err %v", response, err)
	}
}

func TestOMLXSettingsGuardRestoresOriginalBytesOnFailure(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "model_settings.json")
	original := []byte("{\n  \"models\": {\"parent\": {\"vlm_mtp_enabled\": false, \"keep\": 1}}\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	reloads, health := 0, 0
	guard := OMLXSettingsGuard{Path: path, ParentModel: "parent", Reload: func(context.Context) error { reloads++; return nil }, Health: func(context.Context) error { health++; return nil }}
	err := guard.WithMTPEnabled(context.Background(), true, func(context.Context) error {
		changed, err := os.ReadFile(path)
		if err != nil || bytesSHA256(changed) == bytesSHA256(original) {
			t.Fatal("settings were not changed")
		}
		return errors.New("run failure")
	})
	if err == nil {
		t.Fatal("run failure lost")
	}
	restored, err := os.ReadFile(path)
	if err != nil || string(restored) != string(original) || reloads != 2 || health != 2 {
		t.Fatalf("restored=%q reloads=%d health=%d err=%v", restored, reloads, health, err)
	}
}

func TestOMLXSettingsGuardRefusesConcurrentOverwrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "model_settings.json")
	if err := os.WriteFile(path, []byte(`{"parent":{"vlm_mtp_enabled":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := OMLXSettingsGuard{Path: path, ParentModel: "parent", Reload: func(context.Context) error { return nil }, Health: func(context.Context) error { return nil }}
	err := guard.WithMTPEnabled(context.Background(), true, func(context.Context) error {
		return os.WriteFile(path, []byte(`{"other":"actor"}`), 0o600)
	})
	if err == nil {
		t.Fatal("concurrent writer was overwritten")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != `{"other":"actor"}` {
		t.Fatalf("unexpected state %q, %v", got, readErr)
	}
}

func TestAccelerationLocalExecutorWarmsEachConditionOnceAndRestores(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "model_settings.json")
	original := []byte("{\n  \"models\": {\"parent\": {\"vlm_mtp_enabled\": false}}\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := accelerationClientFunc(func(_ context.Context, _ AccelerationChatRequest) (AccelerationChatResponse, error) {
		calls++
		time.Sleep(time.Microsecond)
		return AccelerationChatResponse{Provider: "omlx", Model: "parent", Content: "ok", OutputTokens: 2}, nil
	})
	executor := &AccelerationLocalExecutor{
		Client: client, Model: "omlx/parent", VerifiedModel: "omlx/parent", MaxTokens: 32,
		Guard: OMLXSettingsGuard{Path: path, ParentModel: "parent", Reload: func(context.Context) error { return nil }, Health: func(context.Context) error { return nil }},
	}
	for _, id := range []string{"one", "two"} {
		raw, _ := json.Marshal(AccelerationCase{ID: id, Prompt: "p", Oracle: "ok"})
		if _, err := executor.ExecuteLocal(context.Background(), LocalRunManifest{}, raw); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 6 {
		t.Fatalf("calls = %d, want two warm-ups plus four measured calls", calls)
	}
	restored, err := os.ReadFile(path)
	if err != nil || string(restored) != string(original) {
		t.Fatalf("settings not restored: %q, %v", restored, err)
	}
}
