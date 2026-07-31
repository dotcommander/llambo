package catalog

import "testing"

func TestTextChatCapability_GeminiTextModel(t *testing.T) {
	entry := &ModelEntry{Metadata: ModelMetadata{
		Name:                       "Gemini 2.5 Flash",
		SupportedGenerationMethods: []string{"generateContent", "countTokens"},
	}}

	ok, reason := TextChatCapability("gemini", "gemini-2.5-flash", entry)
	if !ok {
		t.Fatalf("TextChatCapability rejected text model: %s", reason)
	}
}

func TestTextChatCapability_RejectsKnownGeminiNonChatFamilies(t *testing.T) {
	tests := []struct {
		model string
		entry *ModelEntry
	}{
		{
			model: "gemini-embedding-001",
			entry: &ModelEntry{Metadata: ModelMetadata{
				SupportedGenerationMethods: []string{"embedContent", "countTokens"},
			}},
		},
		{model: "gemini-2.5-flash-image"},
		{model: "imagen-4.0-generate-001"},
		{model: "veo-3.1-generate-preview"},
		{model: "lyria-3-pro-preview"},
		{model: "gemini-2.5-pro-preview-tts"},
		{model: "gemini-2.5-computer-use-preview-10-2025"},
		{model: "deep-research-preview-04-2026"},
		{model: "aqa"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			ok, reason := TextChatCapability("gemini", tt.model, tt.entry)
			if ok {
				t.Fatalf("TextChatCapability accepted non-chat model %q", tt.model)
			}
			if reason == "" {
				t.Fatalf("TextChatCapability(%q) returned empty reason", tt.model)
			}
		})
	}
}

func TestTextChatCapability_RejectsNonTextModalities(t *testing.T) {
	entry := &ModelEntry{Metadata: ModelMetadata{
		Architecture: ModelArchitecture{
			InputModalities:  []string{"image"},
			OutputModalities: []string{"text"},
		},
	}}

	ok, reason := TextChatCapability("openrouter", "vision-only", entry)
	if ok {
		t.Fatal("TextChatCapability accepted model without text input")
	}
	if reason == "" {
		t.Fatal("TextChatCapability returned empty reason")
	}
}
