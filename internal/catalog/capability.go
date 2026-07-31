package catalog

import "strings"

// TextChatCapability reports whether a catalog model should be targeted with a
// plain text chat-completion style request. It is intentionally conservative:
// inventory can still keep non-chat models, but ping/prompt should not spend
// requests on embeddings, image/video generation, TTS, or alternate APIs.
func TextChatCapability(providerName, modelID string, entry *ModelEntry) (bool, string) {
	if entry != nil {
		if hasAny(entry.Metadata.SupportedGenerationMethods) && !metadataSupportsGenerationMethod(entry, "generateContent") {
			return false, "model does not support generateContent"
		}
		if hasAny(entry.Metadata.Architecture.InputModalities) && !metadataHasModality(entry.Metadata.Architecture.InputModalities, "text") {
			return false, "model does not accept text input"
		}
		if hasAny(entry.Metadata.Architecture.OutputModalities) && !metadataHasModality(entry.Metadata.Architecture.OutputModalities, "text") {
			return false, "model does not produce text output"
		}
	}

	if strings.EqualFold(providerName, "gemini") {
		if reason := geminiNonTextChatReason(modelID, entry); reason != "" {
			return false, reason
		}
	}

	return true, ""
}

func metadataSupportsGenerationMethod(entry *ModelEntry, method string) bool {
	if entry == nil {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(method))
	for _, got := range entry.Metadata.SupportedGenerationMethods {
		if strings.ToLower(strings.TrimSpace(got)) == want {
			return true
		}
	}
	return false
}

func metadataHasModality(modalities []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, got := range modalities {
		if strings.ToLower(strings.TrimSpace(got)) == want {
			return true
		}
	}
	return false
}

func hasAny(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func geminiNonTextChatReason(modelID string, entry *ModelEntry) string {
	haystack := strings.ToLower(modelID)
	if entry != nil {
		haystack += " " + strings.ToLower(entry.Metadata.Name)
		haystack += " " + strings.ToLower(entry.Metadata.Description)
	}

	rejections := []struct {
		needles []string
		reason  string
	}{
		{[]string{"embedding", "embedcontent"}, "embedding model"},
		{[]string{"imagen", " image", "-image", "nano banana"}, "image-generation model"},
		{[]string{"veo", "video"}, "video-generation model"},
		{[]string{"tts", "audio", "speech"}, "audio/TTS model"},
		{[]string{"lyria", "music"}, "music-generation model"},
		{[]string{"computer-use"}, "computer-use tool model"},
		{[]string{"deep-research", "antigravity", "omni"}, "Interactions API model"},
		{[]string{"aqa", "attributed question answering"}, "attributed-QA model"},
	}
	for _, rejection := range rejections {
		for _, needle := range rejection.needles {
			if strings.Contains(haystack, needle) {
				return rejection.reason
			}
		}
	}
	return ""
}
