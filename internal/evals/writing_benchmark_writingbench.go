package evals

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type WritingBenchAdapter struct{}

func (WritingBenchAdapter) ID() string            { return "writingbench" }
func (WritingBenchAdapter) Version() string       { return "writingbench-checklist-v2" }
func (WritingBenchAdapter) ScoreIdentity() string { return "writingbench/local-checklist" }
func (WritingBenchAdapter) GenerationSettings() WritingGenerationSettings {
	return WritingGenerationSettings{}
}

func (a WritingBenchAdapter) ValidateRecord(record WritingPromptRecord) error {
	object, err := sourceObject(record.SourceRecord)
	if err != nil {
		return err
	}
	if sourcePrompt := writingJSONFieldString(object, "prompt", "query", "question", "instruction", "input"); sourcePrompt == "" || sourcePrompt != strings.TrimSpace(record.Prompt) {
		return fmt.Errorf("normalized prompt does not match source_record")
	}
	criteria, err := a.Criteria(record)
	if err != nil {
		return err
	}
	if len(criteria) != 5 {
		return fmt.Errorf("checklist has %d criteria, want exactly 5", len(criteria))
	}
	return nil
}

func (WritingBenchAdapter) Criteria(record WritingPromptRecord) ([]WritingCriterion, error) {
	object, err := sourceObject(record.SourceRecord)
	if err != nil {
		return nil, err
	}
	raw, ok := object["checklist"]
	if !ok {
		return nil, fmt.Errorf("source_record has no checklist")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parse checklist: %w", err)
	}
	criteria := make([]WritingCriterion, 0, len(items))
	for i, item := range items {
		description := ""
		if err := json.Unmarshal(item, &description); err != nil {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(item, &fields); err != nil {
				return nil, fmt.Errorf("parse checklist item %d: %w", i+1, err)
			}
			description = renderWritingBenchCriterion(fields)
		}
		description = strings.TrimSpace(description)
		if description == "" {
			return nil, fmt.Errorf("checklist item %d is empty", i+1)
		}
		criteria = append(criteria, WritingCriterion{ID: "checklist-" + strconv.Itoa(i+1), Description: description})
	}
	return criteria, nil
}

func renderWritingBenchCriterion(fields map[string]json.RawMessage) string {
	name := writingJSONFieldString(fields, "name")
	description := writingJSONFieldString(fields, "criterion", "criteria", "criteria_description", "description", "text", "content")
	parts := make([]string, 0, 6)
	if name != "" && description != "" {
		parts = append(parts, name+": "+description)
	} else {
		parts = append(parts, name+description)
	}
	for _, band := range []string{"1-2", "3-4", "5-6", "7-8", "9-10"} {
		if value := writingJSONFieldString(fields, band); value != "" {
			parts = append(parts, band+": "+value)
		}
	}
	return strings.Join(parts, "\n")
}
