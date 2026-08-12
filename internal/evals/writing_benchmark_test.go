package evals

import (
	"encoding/json"
	"strings"
	"testing"
)

func writingBenchFixture() WritingPromptRecord {
	return WritingPromptRecord{
		BenchmarkID: "writingbench", ID: "1", Prompt: "Write a launch note.",
		SourceRecord: json.RawMessage(`{"query":"Write a launch note.","domain1":"Professional","domain2":"Announcements","checklist":["clear audience","correct tone","specific details","strong structure","clean prose"]}`),
	}
}

func TestWritingAdaptersRejectPromptDrift(t *testing.T) {
	t.Parallel()
	record := writingBenchFixture()
	record.Prompt = "tampered"
	if err := (WritingBenchAdapter{}).ValidateRecord(record); err == nil {
		t.Fatal("expected source prompt mismatch")
	}
}

func TestWritingBenchAdapterCriteria(t *testing.T) {
	t.Parallel()
	adapter := WritingBenchAdapter{}
	criteria, err := adapter.Criteria(writingBenchFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(criteria) != 5 || criteria[0].ID != "checklist-1" {
		t.Fatalf("criteria = %#v", criteria)
	}
}

func TestWritingBenchAdapterCriteriaAcceptsExportedSchema(t *testing.T) {
	t.Parallel()
	record := writingBenchFixture()
	record.SourceRecord = json.RawMessage(`{"query":"Write a launch note.","domain1":"Professional","domain2":"Announcements","checklist":[{"name":"Audience","criteria_description":"Addresses the intended audience.","9-10":"Perfectly tailored."},{"criteria_description":"Uses the correct tone."},{"criteria_description":"Includes specific details."},{"criteria_description":"Has strong structure."},{"criteria_description":"Uses clean prose."}]}`)
	criteria, err := (WritingBenchAdapter{}).Criteria(record)
	if err != nil {
		t.Fatal(err)
	}
	if got := criteria[0].Description; !strings.Contains(got, "Audience: Addresses the intended audience.") || !strings.Contains(got, "9-10: Perfectly tailored.") {
		t.Fatalf("first criterion description = %q", got)
	}
}

func TestParseWritingJudgmentStrict(t *testing.T) {
	t.Parallel()
	score, reason, err := ParseWritingJudgment(`{"score":8,"reason":"clear and specific"}`)
	if err != nil || score != 8 || reason == "" {
		t.Fatalf("parse = %d, %q, %v", score, reason, err)
	}
	for _, raw := range []string{
		"```json\n{\"score\":8,\"reason\":\"ok\"}\n```",
		`{"score":11,"reason":"too high"}`,
		`{"score":8,"reason":""}`,
		`{"score":8,"reason":"ok","extra":true}`,
		`{"score":8,"reason":"ok"} {}`,
	} {
		if _, _, err := ParseWritingJudgment(raw); err == nil {
			t.Fatalf("expected strict parse failure for %q", raw)
		}
	}
}

func TestEQCreativeLocalRubricIdentity(t *testing.T) {
	t.Parallel()
	adapter := EQCreativeLocalRubricAdapter{}
	if got := adapter.ScoreIdentity(); got != "eqbench-creative-v3/local-rubric" {
		t.Fatalf("score identity = %q", got)
	}
	criteria, err := adapter.Criteria(WritingPromptRecord{})
	if err != nil || len(criteria) != 6 {
		t.Fatalf("criteria = %#v, %v", criteria, err)
	}
}
