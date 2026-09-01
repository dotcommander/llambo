package evals

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func proseInput() ProseEvaluationInput {
	return ProseEvaluationInput{Task: "Explain a safe deployment.", ReaderProfile: "An on-call engineer.", Prose: "Verify the new instance before shifting traffic."}
}
func validProseEvaluation() ProseEvaluation {
	span := []ProseEvidenceSpan{{Start: 0, End: len(proseInput().Prose)}}
	d := func(score int) ProseDimensionEvaluation {
		return ProseDimensionEvaluation{Score: score, Applicable: true, Rationale: "The candidate provides clear evidence for this dimension.", Evidence: span}
	}
	return ProseEvaluation{DepthAndDevelopment: d(4), StructuralCoherence: d(4), ProseCraft: d(4), AntiRepetition: d(4), TargetReaderFit: d(4), Mechanics: d(5)}
}
func mustProseJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestProseEvaluationV2SchemaContract(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(ProseEvaluationSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	if schema["additionalProperties"] != false || schema["title"] != ProseEvaluationSchemaName {
		t.Fatalf("schema = %#v", schema)
	}
	required, _ := schema["required"].([]any)
	if len(required) != 6 {
		t.Fatalf("required = %#v", required)
	}
}
func TestDecodeProseEvaluationStrictAndBound(t *testing.T) {
	input := proseInput()
	want := validProseEvaluation()
	got, err := DecodeProseEvaluation(mustProseJSON(t, want), input.Prose)
	if err != nil {
		t.Fatal(err)
	}
	if got.Aggregate.Version != "prose-evaluation-aggregate-v2" || got.Aggregate.Score != 4.1 {
		t.Fatalf("aggregate = %#v", got.Aggregate)
	}
	if !reflect.DeepEqual(got.DepthAndDevelopment, want.DepthAndDevelopment) {
		t.Fatalf("got %#v", got)
	}
	for _, raw := range [][]byte{[]byte{}, []byte(`{"depth_and_development":{}}`), []byte(`{"depth_and_development":{},"depth_and_development":{}}`), append(mustProseJSON(t, want), []byte(` {}`)...), []byte(`{"aggregate":5}`)} {
		if _, err := DecodeProseEvaluation(raw, input.Prose); !errors.Is(err, ErrInvalidProseEvaluation) {
			t.Fatalf("error = %v for %s", err, raw)
		}
	}
	badSpan := want
	badSpan.Mechanics.Evidence = []ProseEvidenceSpan{{Start: 1, End: len(input.Prose) + 1}}
	if _, err := DecodeProseEvaluation(mustProseJSON(t, badSpan), input.Prose); !errors.Is(err, ErrInvalidProseEvaluation) {
		t.Fatal(err)
	}
	badRationale := want
	badRationale.ProseCraft.Rationale = "Two sentences. Are invalid."
	if _, err := DecodeProseEvaluation(mustProseJSON(t, badRationale), input.Prose); !errors.Is(err, ErrInvalidProseEvaluation) {
		t.Fatal(err)
	}
	nonApplicableMechanics := want
	nonApplicableMechanics.Mechanics.Applicable = false
	if _, err := DecodeProseEvaluation(mustProseJSON(t, nonApplicableMechanics), input.Prose); !errors.Is(err, ErrInvalidProseEvaluation) {
		t.Fatal(err)
	}
}
func TestDecodeProseEvaluationRejectsMissingRequiredZeroValueFields(t *testing.T) {
	base := mustProseJSON(t, validProseEvaluation())
	var value map[string]any
	if err := json.Unmarshal(base, &value); err != nil {
		t.Fatal(err)
	}
	for _, omission := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"applicable", func(v map[string]any) { delete(v["mechanics"].(map[string]any), "applicable") }},
		{"evidence start", func(v map[string]any) {
			evidence := v["prose_craft"].(map[string]any)["evidence"].([]any)
			delete(evidence[0].(map[string]any), "start")
		}},
		{"evidence end", func(v map[string]any) {
			evidence := v["target_reader_fit"].(map[string]any)["evidence"].([]any)
			delete(evidence[0].(map[string]any), "end")
		}},
	} {
		t.Run(omission.name, func(t *testing.T) {
			var copyValue map[string]any
			data, _ := json.Marshal(value)
			if err := json.Unmarshal(data, &copyValue); err != nil {
				t.Fatal(err)
			}
			omission.mutate(copyValue)
			raw, _ := json.Marshal(copyValue)
			if _, err := DecodeProseEvaluation(raw, proseInput().Prose); !errors.Is(err, ErrInvalidProseEvaluation) || !strings.Contains(err.Error(), "missing required") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
func TestDecodeProseEvaluationRejectsDuplicateFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/prose-evaluation-duplicate.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeProseEvaluation(raw, proseInput().Prose); !errors.Is(err, ErrInvalidProseEvaluation) || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error = %v", err)
	}
}
func TestNewProseJudgeRequestUsesPublicPayload(t *testing.T) {
	input := proseInput()
	request, err := NewProseJudgeRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	if request.PromptVersion != ProseEvaluationPromptVersion {
		t.Fatalf("request = %#v", request)
	}
	var payload ProseEvaluationInput
	if err := json.Unmarshal(request.UserPayload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload != input {
		t.Fatalf("payload=%#v", payload)
	}
}
func TestMechanicsCannotRescueAggregate(t *testing.T) {
	value := validProseEvaluation()
	value.Mechanics.Score = 1
	aggregate := ComputeProseEvaluationAggregate(value)
	if aggregate.Score != 1 || !aggregate.MechanicsCapApplied {
		t.Fatalf("aggregate=%#v", aggregate)
	}
}
