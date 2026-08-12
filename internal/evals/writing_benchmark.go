package evals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type WritingBenchmarkAdapter interface {
	ID() string
	Version() string
	ScoreIdentity() string
	GenerationSettings() WritingGenerationSettings
	ValidateRecord(WritingPromptRecord) error
	Criteria(WritingPromptRecord) ([]WritingCriterion, error)
}

func WritingAdapter(id string) (WritingBenchmarkAdapter, error) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "writingbench":
		return WritingBenchAdapter{}, nil
	case "eqbench-creative-v3":
		return EQCreativeLocalRubricAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported writing benchmark %q (supported: writingbench, eqbench-creative-v3)", id)
	}
}

func ValidateWritingPromptRecords(records []WritingPromptRecord, adapter WritingBenchmarkAdapter) error {
	if len(records) == 0 {
		return fmt.Errorf("writing prompt input is empty")
	}
	seen := make(map[string]struct{}, len(records))
	for i, record := range records {
		if record.BenchmarkID != adapter.ID() {
			return fmt.Errorf("prompt record %d uses benchmark %q, want %q", i+1, record.BenchmarkID, adapter.ID())
		}
		if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.Prompt) == "" {
			return fmt.Errorf("prompt record %d is missing id or prompt", i+1)
		}
		if _, ok := seen[record.ID]; ok {
			return fmt.Errorf("duplicate prompt id %q", record.ID)
		}
		seen[record.ID] = struct{}{}
		if err := adapter.ValidateRecord(record); err != nil {
			return fmt.Errorf("prompt %q: %w", record.ID, err)
		}
	}
	return nil
}

func BuildWritingJudgePrompt(record WritingPromptRecord, response string, criterion WritingCriterion) (string, string) {
	user := strings.NewReplacer(
		"{{PROMPT}}", record.Prompt,
		"{{RESPONSE}}", response,
		"{{CRITERION_ID}}", criterion.ID,
		"{{CRITERION}}", criterion.Description,
	).Replace(writingJudgeUserTemplate)
	return strings.TrimSpace(writingJudgeSystemPrompt), strings.TrimSpace(user)
}

func ParseWritingJudgment(raw string) (int, string, error) {
	var value struct {
		Score  int    `json:"score"`
		Reason string `json:"reason"`
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return 0, "", fmt.Errorf("parse strict judge JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return 0, "", err
	}
	if value.Score < 1 || value.Score > 10 {
		return 0, "", fmt.Errorf("judge score %d is outside 1..10", value.Score)
	}
	value.Reason = strings.TrimSpace(value.Reason)
	if value.Reason == "" {
		return 0, "", fmt.Errorf("judge reason is empty")
	}
	return value.Score, value.Reason, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("judge response contains trailing JSON")
		}
		return fmt.Errorf("parse judge response tail: %w", err)
	}
	return nil
}

func sourceObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("source_record is required")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("parse source_record: %w", err)
	}
	return object, nil
}
