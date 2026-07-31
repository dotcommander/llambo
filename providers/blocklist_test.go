package providers

import "testing"

func TestBlocklistCaseInsensitive(t *testing.T) {
	t.Parallel()
	bl := NewBlocklist([]string{"OpenAI:O3-Mini", "synthetic:hf:zai-org/glm-4.7"})

	if !bl.Blocked("openai", "o3-mini") {
		t.Fatal("expected lower-case provider:model to be blocked")
	}
	if !bl.Blocked("OPENAI", "O3-MINI") {
		t.Fatal("expected upper-case provider:model to be blocked")
	}
	if !bl.Blocked("synthetic", "hf:zai-org/glm-4.7") {
		t.Fatal("expected model id with internal colon to be blocked")
	}
	if bl.Blocked("openai", "gpt-4o") {
		t.Fatal("non-listed model must not be blocked")
	}
	if NewBlocklist(nil).Blocked("openai", "o3-mini") {
		t.Fatal("empty blocklist must block nothing")
	}
}
