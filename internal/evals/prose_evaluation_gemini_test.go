package evals

import (
	"strings"
	"testing"
)

func TestProjectGeminiProseSchemaPreservesSupportedConstraints(t *testing.T) {
	projected, err := projectGeminiProseSchema(ProseEvaluationSchema())
	if err != nil {
		t.Fatal(err)
	}
	for _, constraint := range []string{"depth_and_development", "applicable", "evidence", "minimum", "maximum", "required"} {
		if !strings.Contains(string(projected), constraint) {
			t.Fatalf("projected schema lost %q: %s", constraint, projected)
		}
	}
	for _, unsupported := range []string{"$schema", "$id", "definitions", "additionalProperties", "$ref"} {
		if strings.Contains(string(projected), unsupported) {
			t.Fatalf("projected schema retained %q: %s", unsupported, projected)
		}
	}
}
