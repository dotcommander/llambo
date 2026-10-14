package evals

import "testing"

func assertFetchedSourceModels(t *testing.T, result Result) {
	t.Helper()
	if len(result.Models) != 2 {
		t.Fatalf("unexpected merged models: %#v", result.Models)
	}
	if len(result.ReferenceModels) != 17 || sourceModelCounts(result.ReferenceModels)["writingbench"] != 1 || sourceModelCounts(result.ReferenceModels)["eqbench_creative_v3"] != 1 || sourceModelCounts(result.ReferenceModels)["ifeval_official"] != 8 {
		t.Fatalf("source-native drift population was not preserved: %#v", result.ReferenceModels)
	}
	models := make(map[string]Model, len(result.Models))
	for _, model := range result.Models {
		models[model.Key] = model
	}
	if models["gpt-oss-20b-high"].AA == nil || models["gpt-oss-20b-high"].LLMStats == nil {
		t.Fatalf("safe cross-source variant was not merged: %#v", models["gpt-oss-20b-high"])
	}
	if models["gpt-oss-20b-high"].IdentityMatch != IdentityMatchNormalized || models["closed-llm"].IdentityMatch != IdentityMatchUnmatched {
		t.Fatalf("unexpected identity status: %#v", models)
	}
	closed := models["closed-llm"]
	if closed.Open == nil || *closed.Open {
		t.Fatalf("closed LLM was not retained: %#v", closed)
	}
}

func assertFetchedSourceProvenance(t *testing.T, result Result) {
	t.Helper()
	if len(result.Sources) != 14 || !fetchSourceCachesMatch(t, result.Sources, map[string]string{"LLM Stats": "fetched", "LLM Stats frozen cohorts": "unavailable", "Artificial Analysis": "fetched", "WritingBench": "fetched", "EQ-Bench Creative v3": "fetched", "Lech Mazur Creative Story-Writing": "fetched", "Arena Creative Writing": "fetched", "Official IFEval": "frozen"}) {
		t.Fatalf("unexpected source status: %#v", result.Sources)
	}
	if sourceStatusNamed(t, result.Sources, "WritingBench").ContentSHA == "" || sourceStatusNamed(t, result.Sources, "WritingBench").Methodology == "" || sourceStatusNamed(t, result.Sources, "EQ-Bench Creative v3").CommitSHA == "" || sourceStatusNamed(t, result.Sources, "Official IFEval").Version == "" || sourceStatusNamed(t, result.Sources, "Official IFEval").ContentSHA == "" {
		t.Fatalf("snapshot provenance was not surfaced in source status: %#v", result.Sources)
	}
}
