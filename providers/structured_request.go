package providers

import (
	"context"
	"encoding/json"
	"fmt"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
	"reflect"
	"strings"
)

// StructuredChatRequest preserves the ordered conversation and wire options.
type StructuredChatRequest struct {
	Messages       []whtypes.Message
	Tools          []whtypes.Tool
	ToolChoice     *whtypes.ToolChoice
	Stop           []string
	ResponseFormat any
	SystemPrompt   string
	Temperature    *float64
	TopP           *float64
	MaxTokens      *int
}

// ResolvedTarget is an immutable exact-model admission constraint.
type ResolvedTarget struct {
	model     string
	providers map[string]bool
}

func (t ResolvedTarget) Model() string           { return t.model }
func (t ResolvedTarget) Allows(name string) bool { return t.model == "" || t.providers[name] }
func ResolveTarget(configs map[string]Config, model, embeddingDefault string) (ResolvedTarget, error) {
	if model == "" {
		return ResolvedTarget{}, nil
	}
	exact := model
	pinned := ""
	if prefix, rest, ok := strings.Cut(model, "/"); ok {
		if _, exists := configs[prefix]; exists {
			pinned = prefix
			exact = rest
		}
	}
	target := ResolvedTarget{model: exact, providers: make(map[string]bool)}
	_, embeddingBackend := selectEmbeddingProvider(configs)
	for name, cfg := range configs {
		if !cfg.Enabled || (pinned != "" && name != pinned) {
			continue
		}
		known := cfg.Model == exact || (embeddingDefault != "" && exact == embeddingDefault && name == embeddingBackend)
		for _, id := range cfg.Models {
			if id == exact {
				known = true
			}
		}
		if known {
			target.providers[name] = true
		}
	}
	if exact == "" || len(target.providers) == 0 {
		return ResolvedTarget{}, fmt.Errorf("unknown model target %q", model)
	}
	return target, nil
}

type structuredRequestKey struct{}

func structuredContext(ctx context.Context, r StructuredChatRequest) context.Context {
	return context.WithValue(ctx, structuredRequestKey{}, cloneStructuredRequest(r))
}
func (r StructuredChatRequest) projection() (string, string) {
	var b strings.Builder
	for _, m := range r.Messages {
		if m != nil {
			if text, ok := m.GetContent().(string); ok {
				b.WriteString(text)
				b.WriteByte('\n')
			}
		}
	}
	return r.SystemPrompt, b.String()
}
func cloneStructuredMessages(in []whtypes.Message) []whtypes.Message {
	out := make([]whtypes.Message, len(in))
	for i, m := range in {
		data, _ := json.Marshal(m)
		switch m.GetRole() {
		case whtypes.RoleSystem:
			v := &whtypes.SystemMessage{}
			_ = json.Unmarshal(data, v)
			out[i] = v
		case whtypes.RoleUser:
			v := &whtypes.UserMessage{}
			_ = json.Unmarshal(data, v)
			out[i] = v
		case whtypes.RoleAssistant:
			v := &whtypes.AssistantMessage{}
			_ = json.Unmarshal(data, v)
			out[i] = v
		case whtypes.RoleTool:
			v := &whtypes.ToolResultMessage{}
			_ = json.Unmarshal(data, v)
			out[i] = v
		}
	}
	return out
}
func (p *OpenAIProvider) ChatStructuredWithInfoContext(ctx context.Context, r StructuredChatRequest, target ResolvedTarget) (ChatResult, error) {
	if err := r.Validate(); err != nil {
		return ChatResult{}, err
	}
	ctx = structuredContext(ctx, r)
	system, user := r.projection()
	coordinator := p.coordinator().withTarget(target)
	plan, err := coordinator.plan(system, user, false)
	if err != nil {
		return ChatResult{}, err
	}
	outcome, err := coordinator.execute(ctx, plan, system, user, p.failoverCallback, p.executeChatAttempt)
	return p.chatResultFromOutcome(outcome), err
}
func (p *OpenAIProvider) ChatStructuredStreamWithInfoContext(ctx context.Context, r StructuredChatRequest, target ResolvedTarget, onChunk ChatStreamHandler) (ChatResult, error) {
	if err := r.Validate(); err != nil {
		return ChatResult{}, err
	}
	ctx = structuredContext(ctx, r)
	system, user := r.projection()
	coordinator := p.coordinator().withTarget(target)
	plan, err := coordinator.plan(system, user, false)
	if err != nil {
		return ChatResult{}, err
	}
	outcome, err := coordinator.executeStream(ctx, plan, system, user, onChunk, p.executeStreamAttempt)
	return p.chatResultFromOutcome(outcome), err
}
func (c executionCoordinator) withTarget(t ResolvedTarget) executionCoordinator {
	if t.model == "" {
		return c
	}
	configs := make(map[string]Config)
	for name, cfg := range c.configs {
		if t.Allows(name) {
			cfg.Model = t.model
			configs[name] = cfg
		}
	}
	c.configs = configs
	return c
}

func (r StructuredChatRequest) Validate() error {
	if len(r.Messages) == 0 {
		return fmt.Errorf("messages must not be empty")
	}
	for _, m := range r.Messages {
		if m == nil || (reflect.ValueOf(m).Kind() == reflect.Pointer && reflect.ValueOf(m).IsNil()) {
			return fmt.Errorf("nil message")
		}
		switch m.GetRole() {
		case whtypes.RoleSystem, whtypes.RoleUser, whtypes.RoleAssistant, whtypes.RoleTool:
		default:
			return fmt.Errorf("unsupported message role %q", m.GetRole())
		}
		if _, ok := m.GetContent().(string); !ok {
			return fmt.Errorf("unsupported message content")
		}
		if _, err := json.Marshal(m); err != nil {
			return fmt.Errorf("unsupported message shape: %w", err)
		}
	}
	return nil
}

func cloneStructuredRequest(r StructuredChatRequest) StructuredChatRequest {
	r.Messages = cloneStructuredMessages(r.Messages)
	r.Tools = append([]whtypes.Tool(nil), r.Tools...)
	for i := range r.Tools {
		r.Tools[i].InputSchema = cloneJSONMap(r.Tools[i].InputSchema)
		if r.Tools[i].Function != nil {
			f := *r.Tools[i].Function
			f.Parameters = cloneJSONMap(f.Parameters)
			r.Tools[i].Function = &f
		}
	}
	if r.ToolChoice != nil {
		v := *r.ToolChoice
		r.ToolChoice = &v
	}
	r.Stop = append([]string(nil), r.Stop...)
	r.ResponseFormat = cloneJSONValue(r.ResponseFormat)
	if r.Temperature != nil {
		v := *r.Temperature
		r.Temperature = &v
	}
	if r.TopP != nil {
		v := *r.TopP
		r.TopP = &v
	}
	if r.MaxTokens != nil {
		v := *r.MaxTokens
		r.MaxTokens = &v
	}
	return r
}
