package evals

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ProseScreenAdapter is a writer-candidate pre-screen. It intentionally does
// not make source, factual, semantic, or promotion claims.
type ProseScreenAdapter struct{}

func (ProseScreenAdapter) ID() string            { return "prose-screen" }
func (ProseScreenAdapter) Version() string       { return "prose-screen-v2" }
func (ProseScreenAdapter) ScoreIdentity() string { return "prose-screen/v2-pre-screen-only" }
func (ProseScreenAdapter) GenerationSettings() WritingGenerationSettings {
	return WritingGenerationSettings{Temperature: 0, TemperatureSet: true}
}
func (ProseScreenAdapter) Iterations(requested int) (int, error) {
	return singleWritingIteration("prose-screen", requested)
}
func (ProseScreenAdapter) JudgePromptVersion() string { return ProseEvaluationPromptVersion }
func (ProseScreenAdapter) buildRunReport(report WritingRunReport, generations []WritingGenerationRecord, judgments []WritingJudgmentRecord) WritingRunReport {
	report.PreScreenOnly = true
	return buildProseScreenRunReport(report, generations, judgments)
}

func (a ProseScreenAdapter) ValidateRecord(record WritingPromptRecord) error {
	object, err := proseScreenSourceObject(record.SourceRecord)
	if err != nil {
		return err
	}
	if sourcePrompt := writingJSONFieldString(object, "prompt"); sourcePrompt == "" || sourcePrompt != strings.TrimSpace(record.Prompt) {
		return fmt.Errorf("normalized prompt does not match source_record")
	}
	if writingJSONFieldString(object, "task") == "" {
		return fmt.Errorf("source_record has no public task")
	}
	if writingJSONFieldString(object, "reader_profile") == "" {
		return fmt.Errorf("source_record has no public reader_profile")
	}
	return nil
}
func (ProseScreenAdapter) Criteria(WritingPromptRecord) ([]WritingCriterion, error) {
	return proseScreenCriteria(), nil
}
func proseScreenCriteria() []WritingCriterion {
	return []WritingCriterion{
		{ID: "depth_and_development", Description: "Mechanisms, implications, and examples are adequately developed."},
		{ID: "structural_coherence", Description: "Sequence, headings, transitions, and paragraph progression are coherent."},
		{ID: "prose_craft", Description: "Language is precise, clear, concrete, and economical."},
		{ID: "anti_repetition", Description: "The candidate avoids repetition, recap padding, filler, and AI clichés."},
		{ID: "target_reader_fit", Description: "The candidate serves the stated public target reader."},
		{ID: "mechanics", Description: "Grammar, punctuation, formatting, and artifact-free presentation are sound."},
	}
}

// BuildJudgmentPrompt and ParseJudgment let the sealed writing runner preserve
// the v2 raw judgment while using the prose-specific strict decoder.
func (a ProseScreenAdapter) BuildJudgmentPrompt(record WritingPromptRecord, response string, _ []WritingCriterion) (string, string, error) {
	input, err := a.input(record, response)
	if err != nil {
		return "", "", err
	}
	request, err := NewProseJudgeRequest(input)
	if err != nil {
		return "", "", err
	}
	return request.SystemPrompt, string(request.UserPayload), nil
}
func (ProseScreenAdapter) JudgmentSettings(WritingRunManifest) (WritingGenerationSettings, error) {
	projected, err := projectGeminiProseSchema(ProseEvaluationSchema())
	if err != nil {
		return WritingGenerationSettings{}, fmt.Errorf("project prose screen schema: %w", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(projected, &schema); err != nil {
		return WritingGenerationSettings{}, fmt.Errorf("parse projected prose screen schema: %w", err)
	}
	return WritingGenerationSettings{Temperature: 0, TemperatureSet: true, ExtraBody: map[string]any{"generationConfig": map[string]any{"responseMimeType": "application/json", "responseSchema": schema}}}, nil
}
func (a ProseScreenAdapter) ParseJudgment(raw, response string, _ []WritingCriterion) ([]WritingCriterionJudgment, error) {
	evaluation, err := DecodeProseEvaluation([]byte(raw), response)
	if err != nil {
		return nil, err
	}
	results := make([]WritingCriterionJudgment, 0, len(evaluation.Dimensions()))
	for _, dimension := range evaluation.Dimensions() {
		results = append(results, WritingCriterionJudgment{CriterionID: dimension.ID, Criterion: proseCriterionDescription(dimension.ID), Score: dimension.Value.Score, Reason: dimension.Value.Rationale, Applicable: dimension.Value.Applicable, Evidence: append([]ProseEvidenceSpan(nil), dimension.Value.Evidence...)})
	}
	return results, nil
}
func (a ProseScreenAdapter) input(record WritingPromptRecord, response string) (ProseEvaluationInput, error) {
	object, err := proseScreenSourceObject(record.SourceRecord)
	if err != nil {
		return ProseEvaluationInput{}, err
	}
	return ProseEvaluationInput{Task: writingJSONFieldString(object, "task"), ReaderProfile: writingJSONFieldString(object, "reader_profile"), Prose: response}, nil
}

func proseScreenSourceObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if err := rejectDuplicateProseJSONKeys(raw); err != nil {
		return nil, fmt.Errorf("parse source_record: %w", err)
	}
	object, err := sourceObject(raw)
	if err != nil {
		return nil, err
	}
	unknown := make([]string, 0)
	for key := range object {
		switch key {
		case "prompt", "task", "reader_profile":
		default:
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("source_record contains non-public fields: %s", strings.Join(unknown, ", "))
	}
	for _, key := range []string{"prompt", "task", "reader_profile"} {
		rawValue, ok := object[key]
		if !ok {
			return nil, fmt.Errorf("source_record has no public %s", key)
		}
		var value string
		if err := json.Unmarshal(rawValue, &value); err != nil || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("source_record %s must be a non-empty string", key)
		}
	}
	return object, nil
}
func proseCriterionDescription(id string) string {
	for _, criterion := range proseScreenCriteria() {
		if criterion.ID == id {
			return criterion.Description
		}
	}
	return id
}

func proseAggregateFromJudgments(results []WritingCriterionJudgment) (ProseEvaluationAggregate, bool) {
	byID := make(map[string]WritingCriterionJudgment, len(results))
	for _, result := range results {
		byID[result.CriterionID] = result
	}
	value := ProseEvaluation{}
	for _, dimension := range value.Dimensions() {
		result, ok := byID[dimension.ID]
		if !ok {
			return ProseEvaluationAggregate{}, false
		}
		converted := ProseDimensionEvaluation{Score: result.Score, Applicable: result.Applicable, Rationale: result.Reason, Evidence: result.Evidence}
		switch dimension.ID {
		case "depth_and_development":
			value.DepthAndDevelopment = converted
		case "structural_coherence":
			value.StructuralCoherence = converted
		case "prose_craft":
			value.ProseCraft = converted
		case "anti_repetition":
			value.AntiRepetition = converted
		case "target_reader_fit":
			value.TargetReaderFit = converted
		case "mechanics":
			value.Mechanics = converted
		}
	}
	return ComputeProseEvaluationAggregate(value), true
}
