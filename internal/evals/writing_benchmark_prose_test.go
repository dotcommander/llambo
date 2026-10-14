package evals

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestProseScreenAdapterFixtureAndStrictJudgment(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/prose-screen-fixtures.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	adapter := ProseScreenAdapter{}
	for _, line := range lines {
		var record WritingPromptRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if err := adapter.ValidateRecord(record); err != nil {
			t.Fatal(err)
		}
		criteria, err := adapter.Criteria(record)
		if err != nil || len(criteria) != 6 {
			t.Fatalf("criteria=%#v err=%v", criteria, err)
		}
		system, user, err := adapter.BuildJudgmentPrompt(record, "Useful prose.", criteria)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(system, "pre-screen") || !strings.Contains(user, "reader_profile") {
			t.Fatalf("prompt missing public contract: %s %s", system, user)
		}
	}
}
func TestProseScreenParserPreservesBoundEvidence(t *testing.T) {
	t.Parallel()
	adapter := ProseScreenAdapter{}
	response := proseInput().Prose
	record := WritingPromptRecord{BenchmarkID: adapter.ID(), ID: "case", Prompt: "Write prose.", SourceRecord: json.RawMessage(`{"prompt":"Write prose.","task":"Explain a deployment.","reader_profile":"An operator."}`)}
	results, err := adapter.ParseJudgment(string(mustProseJSON(t, validProseEvaluation())), response, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 6 || len(results[0].Evidence) != 1 || !results[0].Applicable {
		t.Fatalf("results=%#v", results)
	}
	if _, _, err := adapter.BuildJudgmentPrompt(record, response, nil); err != nil {
		t.Fatal(err)
	}
}

func TestProseScreenRejectsNonPublicOrDuplicateSourceFields(t *testing.T) {
	t.Parallel()
	adapter := ProseScreenAdapter{}
	for _, source := range []string{
		`{"prompt":"Write prose.","task":"Explain a deployment.","reader_profile":"An operator.","private_context":"hidden"}`,
		`{"prompt":"Write prose.","task":"Explain a deployment.","task":"Override it.","reader_profile":"An operator."}`,
		`{"prompt":"Write prose.","task":42,"reader_profile":"An operator."}`,
	} {
		record := WritingPromptRecord{BenchmarkID: adapter.ID(), ID: "case", Prompt: "Write prose.", SourceRecord: json.RawMessage(source)}
		if err := adapter.ValidateRecord(record); err == nil {
			t.Fatalf("accepted source_record %s", source)
		}
	}
}

func TestProseScreenSealedCalibrationPairsOrderOwningDimensions(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/prose-screen-calibration.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture proseCalibrationFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if !fixture.PublicOnly || len(fixture.Pairs) != 6 {
		t.Fatalf("fixture = %#v", fixture)
	}
	seen := map[string]bool{}
	adapter := ProseScreenAdapter{}
	for i, pair := range fixture.Pairs {
		assertProseCalibrationPair(t, pair)
		seen[pair.Dimension] = true
		assertProseCalibrationOrdering(t, adapter, pair, i)
	}
	for _, criterion := range proseScreenCriteria() {
		if !seen[criterion.ID] {
			t.Fatalf("missing calibrated owning dimension %q", criterion.ID)
		}
	}
}

func sealedCalibrationEvaluation(prose, defectiveDimension string, defectiveScore int) ProseEvaluation {
	span := []ProseEvidenceSpan{{Start: 0, End: len(prose)}}
	dimension := func(id string) ProseDimensionEvaluation {
		score := 5
		if id == defectiveDimension {
			score = defectiveScore
		}
		return ProseDimensionEvaluation{Score: score, Applicable: true, Rationale: "The sealed fixture supplies evidence for this dimension.", Evidence: span}
	}
	return ProseEvaluation{DepthAndDevelopment: dimension("depth_and_development"), StructuralCoherence: dimension("structural_coherence"), ProseCraft: dimension("prose_craft"), AntiRepetition: dimension("anti_repetition"), TargetReaderFit: dimension("target_reader_fit"), Mechanics: dimension("mechanics")}
}

type proseCalibrationFixture struct {
	PublicOnly bool `json:"public_only"`
	Pairs      []proseCalibrationPair
}

type proseCalibrationPair struct {
	Format        string `json:"format"`
	Dimension     string `json:"dimension"`
	Task          string `json:"task"`
	ReaderProfile string `json:"reader_profile"`
	Strong        string `json:"strong"`
	Defective     string `json:"defective"`
}

func assertProseCalibrationPair(t *testing.T, pair proseCalibrationPair) {
	t.Helper()
	if pair.Format == "" || pair.Dimension == "" || pair.Task == "" || pair.ReaderProfile == "" || pair.Strong == "" || pair.Defective == "" || pair.Strong == pair.Defective {
		t.Fatalf("invalid calibration pair %#v", pair)
	}
}

func assertProseCalibrationOrdering(t *testing.T, adapter ProseScreenAdapter, pair proseCalibrationPair, i int) {
	t.Helper()
	strong := sealedCalibrationEvaluation(pair.Strong, "", 5)
	defective := sealedCalibrationEvaluation(pair.Defective, pair.Dimension, 1)
	strongResults, err := adapter.ParseJudgment(string(mustProseJSON(t, strong)), pair.Strong, nil)
	if err != nil {
		t.Fatal(err)
	}
	defectiveResults, err := adapter.ParseJudgment(string(mustProseJSON(t, defective)), pair.Defective, nil)
	if err != nil {
		t.Fatal(err)
	}

	if proseCalibrationCriterion(t, defectiveResults, pair.Dimension).Score >= proseCalibrationCriterion(t, strongResults, pair.Dimension).Score {
		t.Fatalf("%s defective prose did not lower %s", pair.Format, pair.Dimension)
	}
	strongKey, weakKey := "strong-"+string(rune('a'+i)), "weak-"+string(rune('a'+i))
	generations := []WritingGenerationRecord{{Key: strongKey, Status: "success", Provider: "p", Model: "writer", PromptID: "strong"}, {Key: weakKey, Status: "success", Provider: "p", Model: "writer", PromptID: "weak"}}
	judgments := []WritingJudgmentRecord{{Status: "success", GenerationKey: strongKey, Results: strongResults}, {Status: "success", GenerationKey: weakKey, Results: defectiveResults}}
	report := BuildWritingRunReport(WritingRunManifest{Identity: WritingRunIdentity{BenchmarkID: adapter.ID()}}, adapter, generations, judgments, true)
	if len(report.Scores) != 1 || report.Scores[0].Judgments != 2 || report.ModelDispersion[0].Samples != 2 {
		t.Fatalf("report=%#v", report)
	}
}

func proseCalibrationCriterion(t *testing.T, results []WritingCriterionJudgment, dimension string) WritingCriterionJudgment {
	t.Helper()
	for _, result := range results {
		if result.CriterionID == dimension {
			return result
		}
	}
	t.Fatalf("missing %s", dimension)
	return WritingCriterionJudgment{}
}
