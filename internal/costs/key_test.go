package costs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeyCanonicalizesProviderAndModel(t *testing.T) {
	t.Parallel()

	want := "zai:glm-4.7"
	if got := Key("ZAI", "GLM-4.7"); got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}
	if got := Key("zai", "glm-4.7"); got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}
}

func TestLoadUsesCanonicalKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "model-costs.csv")
	csv := "provider,model,input_per_1m_usd,output_per_1m_usd,notes\n" +
		"zai,GLM-4.7,1.4,4.4,\n"
	if err := os.WriteFile(path, []byte(csv), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := m[Key("zai", "GLM-4.7")]; !ok {
		t.Fatalf("Load() missing canonical key %q", Key("zai", "GLM-4.7"))
	}
	if _, ok := m[Key("ZAI", "glm-4.7")]; !ok {
		t.Fatalf("Load() lookup with mixed-case inputs missing key %q", Key("ZAI", "glm-4.7"))
	}
}
