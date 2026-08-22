package architecture_test

import (
	"os"
	"strings"
	"testing"
)

func TestRequestFlowDocumentsCurrentBoundaries(t *testing.T) {
	content, err := os.ReadFile("request-flow.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(content)
	for _, want := range []string{
		"## HTTP admission",
		"## Chat completion",
		"Server-Sent Events",
		"## Anthropic Messages",
		"## Embeddings",
		"POST /v1/jobs",
		"return HTTP 202",
		"The HTTP contract is polling",
		"Bearer authentication",
		"Wormhole result, classified by Llambo",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("request flow missing current contract %q", want)
		}
	}
	for _, stale := range []string{"Streaming Not Implemented", "OpenAI SDK", "ChatWithInfoContext()", "jobs.go:"} {
		if strings.Contains(doc, stale) {
			t.Errorf("request flow retains stale contract %q", stale)
		}
	}
}
