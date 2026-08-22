package architecture

import (
	"os"
	"strings"
	"testing"
)

func readDoc(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestArchitectureIndexMapsCurrentPackages(t *testing.T) {
	doc := readDoc(t, "../README.md")
	for _, want := range []string{"cmd/", "internal/gateway/", "internal/evals/", "providers/", "wormhole"} {
		if !strings.Contains(doc, want) {
			t.Errorf("architecture index missing %q", want)
		}
	}
}

func TestArchitectureOverviewDefinesBoundariesAndSafety(t *testing.T) {
	doc := readDoc(t, "../architecture.md")
	for _, want := range []string{
		"## Boundaries",
		"cmd/ (Kong command and startup wiring)",
		"internal/gateway/ (HTTP protocol and job lifecycle)",
		"wormhole provider instances",
		"Non-loopback binds require `--allow-remote`",
		"`gateway.auth_token_env`",
		"## State and concurrency",
		"## Shutdown",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("architecture overview missing %q", want)
		}
	}
	for _, stale := range []string{"Cobra", "github.com/openai/openai-go", "CreateOpenAIClients"} {
		if strings.Contains(doc, stale) {
			t.Errorf("architecture overview retains stale contract %q", stale)
		}
	}
}
