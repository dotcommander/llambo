package architecture_test

import (
	"os"
	"strings"
	"testing"
)

func TestModuleMapNamesCurrentOwners(t *testing.T) {
	content, err := os.ReadFile("module-docs.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(content)
	for _, want := range []string{
		"cmd/root_commands_*.go",
		"internal/gateway/server.go",
		"internal/evals/*.go",
		"providers/client_factory.go",
		"providers/chat_execution*.go",
		"providers/circuit_breaker.go",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("module map missing current owner %q", want)
		}
	}
	for _, stale := range []string{"Cobra", "github.com/openai/openai-go", "jobs.go"} {
		if strings.Contains(doc, stale) {
			t.Errorf("module map retains stale owner %q", stale)
		}
	}
}
