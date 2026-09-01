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

type writingJudgmentAdapter interface {
	BuildJudgmentPrompt(WritingPromptRecord, string, []WritingCriterion) (string, string, error)
	ParseJudgment(string, string, []WritingCriterion) ([]WritingCriterionJudgment, error)
}

type writingJudgePromptVersioner interface{ JudgePromptVersion() string }
type writingJudgmentSettingsAdapter interface {
	JudgmentSettings(WritingRunManifest) (WritingGenerationSettings, error)
}

func WritingAdapter(id string) (WritingBenchmarkAdapter, error) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "writingbench":
		return WritingBenchAdapter{}, nil
	case "eqbench-creative-v3":
		return EQCreativeLocalRubricAdapter{}, nil
	case "prose-screen":
		return ProseScreenAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported writing benchmark %q (supported: writingbench, eqbench-creative-v3, prose-screen)", id)
	}
}

func buildWritingAdapterJudgmentPrompt(adapter WritingBenchmarkAdapter, record WritingPromptRecord, response string, criteria []WritingCriterion) (string, string, error) {
	if specialized, ok := adapter.(writingJudgmentAdapter); ok {
		return specialized.BuildJudgmentPrompt(record, response, criteria)
	}
	return BuildCombinedWritingJudgePrompt(record, response, criteria)
}
func parseWritingAdapterJudgment(adapter WritingBenchmarkAdapter, raw, response string, criteria []WritingCriterion) ([]WritingCriterionJudgment, error) {
	if specialized, ok := adapter.(writingJudgmentAdapter); ok {
		return specialized.ParseJudgment(raw, response, criteria)
	}
	return ParseCombinedWritingJudgment(raw, criteria)
}
func writingAdapterJudgePromptVersion(adapter WritingBenchmarkAdapter) string {
	if versioned, ok := adapter.(writingJudgePromptVersioner); ok {
		return versioned.JudgePromptVersion()
	}
	return WritingJudgePromptVersion
}
func writingAdapterJudgmentSettings(adapter WritingBenchmarkAdapter, manifest WritingRunManifest) (WritingGenerationSettings, error) {
	if specialized, ok := adapter.(writingJudgmentSettingsAdapter); ok {
		return specialized.JudgmentSettings(manifest)
	}
	return CombinedWritingJudgeSettings(manifest.Identity.JudgeThinkingLevel)
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

func BuildCombinedWritingJudgePrompt(record WritingPromptRecord, response string, criteria []WritingCriterion) (string, string, error) {
	encoded, err := json.Marshal(criteria)
	if err != nil {
		return "", "", fmt.Errorf("encode writing criteria: %w", err)
	}
	user := strings.NewReplacer(
		"{{PROMPT}}", record.Prompt,
		"{{RESPONSE}}", response,
		"{{CRITERIA}}", string(encoded),
	).Replace(writingCombinedJudgeUserTemplate)
	return strings.TrimSpace(writingCombinedJudgeSystemPrompt), strings.TrimSpace(user), nil
}

func ParseCombinedWritingJudgment(raw string, criteria []WritingCriterion) ([]WritingCriterionJudgment, error) {
	var value struct {
		Results []struct {
			CriterionID string `json:"criterion_id"`
			Score       int    `json:"score"`
			Reason      string `json:"reason"`
		} `json:"results"`
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("parse strict combined judge JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	expected := make(map[string]WritingCriterion, len(criteria))
	for _, criterion := range criteria {
		expected[criterion.ID] = criterion
	}
	if len(value.Results) != len(expected) {
		return nil, fmt.Errorf("combined judge returned %d results, want %d", len(value.Results), len(expected))
	}
	seen := make(map[string]struct{}, len(value.Results))
	out := make([]WritingCriterionJudgment, 0, len(value.Results))
	for _, result := range value.Results {
		criterion, ok := expected[result.CriterionID]
		if !ok {
			return nil, fmt.Errorf("combined judge returned unexpected criterion %q", result.CriterionID)
		}
		if _, ok := seen[result.CriterionID]; ok {
			return nil, fmt.Errorf("combined judge duplicated criterion %q", result.CriterionID)
		}
		if result.Score < 1 || result.Score > 10 {
			return nil, fmt.Errorf("judge score %d is outside 1..10", result.Score)
		}
		result.Reason = strings.TrimSpace(result.Reason)
		if result.Reason == "" {
			return nil, fmt.Errorf("combined judge criterion %q has an empty reason", result.CriterionID)
		}
		seen[result.CriterionID] = struct{}{}
		out = append(out, WritingCriterionJudgment{CriterionID: result.CriterionID, Criterion: criterion.Description, Score: result.Score, Reason: result.Reason})
	}
	return out, nil
}

func CombinedWritingJudgeSettings(thinkingLevel string) (WritingGenerationSettings, error) {
	var schema map[string]any
	if err := json.Unmarshal(writingCombinedJudgeSchema, &schema); err != nil {
		return WritingGenerationSettings{}, fmt.Errorf("parse embedded combined judge schema: %w", err)
	}
	return WritingGenerationSettings{ExtraBody: map[string]any{
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"responseSchema":   schema,
			"thinkingConfig":   map[string]any{"thinkingLevel": thinkingLevel},
		},
	}}, nil
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
