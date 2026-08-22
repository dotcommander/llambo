package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalsNormalizedExportHelpAndNetworkBoundary(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"evals", "export", "normalized", "--help"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	for _, want := range []string{"cache-only normalized JSONL", "--output-dir", "deduplicated", "editorially ranked"} {
		if !strings.Contains(help, want) {
			t.Fatalf("export help missing %q:\n%s", want, help)
		}
	}

	for flag, args := range map[string][]string{
		"refresh":                      {"--refresh"},
		"refresh-official-model-cards": {"--refresh-official-model-cards"},
		"live-omlx":                    {"--live-omlx"},
	} {
		var rejectedOut, rejectedErr bytes.Buffer
		err := execute(context.Background(), append([]string{"evals", "export", "normalized"}, append(args, "--output-dir", filepath.Join(t.TempDir(), "dataset"))...), &rejectedOut, &rejectedErr)
		if err == nil || !strings.Contains(err.Error(), "--"+flag+" is not supported") {
			t.Fatalf("%s was accepted before cache work: %v", flag, err)
		}
	}
}
