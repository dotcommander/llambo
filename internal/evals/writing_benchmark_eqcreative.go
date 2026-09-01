package evals

import (
	"fmt"
	"strings"
)

type EQCreativeLocalRubricAdapter struct{}

func (EQCreativeLocalRubricAdapter) ID() string      { return "eqbench-creative-v3" }
func (EQCreativeLocalRubricAdapter) Version() string { return "eqbench-creative-v3-local-rubric-v1" }
func (EQCreativeLocalRubricAdapter) ScoreIdentity() string {
	return "eqbench-creative-v3/local-rubric"
}
func (EQCreativeLocalRubricAdapter) GenerationSettings() WritingGenerationSettings {
	return WritingGenerationSettings{Temperature: 0.7, TemperatureSet: true, ExtraBody: map[string]any{"min_p": 0.1}}
}

func (a EQCreativeLocalRubricAdapter) ValidateRecord(record WritingPromptRecord) error {
	object, err := sourceObject(record.SourceRecord)
	if err != nil {
		return err
	}
	sourcePrompt := writingJSONFieldString(object, "writing_prompt", "prompt", "query")
	if sourcePrompt == "" {
		return fmt.Errorf("source_record has no writing prompt")
	}
	if sourcePrompt != strings.TrimSpace(record.Prompt) {
		return fmt.Errorf("normalized prompt does not match source_record")
	}
	return nil
}

func (EQCreativeLocalRubricAdapter) Criteria(WritingPromptRecord) ([]WritingCriterion, error) {
	return []WritingCriterion{
		{ID: "creativity", Description: "Originality, imaginative choices, and avoidance of generic or derivative phrasing."},
		{ID: "coherence", Description: "Narrative or rhetorical coherence, continuity, and intelligible progression."},
		{ID: "style", Description: "Prose craft, sentence-level control, rhythm, imagery, and effective use of language."},
		{ID: "voice", Description: "Distinctive and consistent voice, characterization, or point of view where applicable."},
		{ID: "impact", Description: "Emotional, dramatic, humorous, or intellectual impact appropriate to the prompt."},
		{ID: "instruction-adherence", Description: "Faithful fulfillment of the requested scenario, constraints, tone, and form."},
	}, nil
}
