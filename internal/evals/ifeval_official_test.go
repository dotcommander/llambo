package evals

import "testing"

func TestOfficialIFEvalSnapshotAndIdentityMerge(t *testing.T) {
	snapshot, err := loadOfficialIFEvalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Models) != 8 || snapshot.CommitSHA != "b9aebfcbe28b6cb374042f495d733037550ab146" || snapshot.ContentSHA != "10950a8975d3f43c4b51a74def690603ac82be6c53a2d8eb509e6e31177b6efc" {
		t.Fatalf("unexpected official IFEval provenance: %#v", snapshot)
	}
	base := []Model{{Key: "aa:lfm2-5-8b-a1b", Name: "LFM2.5-8B-A1B", Organization: "Liquid AI", AA: &ArtificialMetrics{}}}
	merged := mergeWritingBenchModels(base, snapshot.Models)
	result := merged[0].Benchmarks["ifeval-official"]
	if result.Score == nil || *result.Score != 91.84 || result.Identity != IdentityMatchExact || result.SourceID != "ifeval-official" || result.SourceClass != string(SourceFirstPartyResult) || result.EvidenceGrade != "first_party" || result.SourceRevision != snapshot.CommitSHA {
		t.Fatalf("official IFEval identity did not attach exactly: %#v", merged[0])
	}
}

func TestInstructionIndexRemainsPreferredOverIFEval(t *testing.T) {
	index, ifeval := 25.0, 91.84
	model := Model{LLMStats: &LLMStatsMetrics{Indexes: map[string]Index{"instruction_following": {Conservative: index}}}, Benchmarks: map[string]BenchmarkResult{"ifeval-official": {Score: &ifeval, Identity: IdentityMatchExact}}}
	reference := FormulaReference{Metrics: map[string][]float64{"llm_instruction_general": {0, 25, 50}, "ifeval_official": {82.23, 91.84}}}
	score := scoreCategory(model, categorySpecs[2], reference)
	if score == nil || score.Primary == nil || score.Primary.Benchmark != "instruction-following-index" {
		t.Fatalf("fallback displaced preferred instruction index: %#v", score)
	}
}
