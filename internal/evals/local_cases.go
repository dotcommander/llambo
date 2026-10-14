package evals

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DecodeLocalCases proves the expected suite shape before a provider call.
func DecodeLocalCases(in LocalInputManifest) ([]any, error) {
	decoded := make([]any, 0, len(in.Cases))
	caseIDs := make(map[string]struct{}, len(in.Cases))
	for i, raw := range in.Cases {
		target, err := newLocalCase(in.Suite)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return nil, fmt.Errorf("parse %s case %d: %w", in.Suite, i+1, err)
		}
		if err := validateLocalCase(target, i+1); err != nil {
			return nil, err
		}
		caseID := localCaseID(target)
		if _, exists := caseIDs[caseID]; exists {
			return nil, fmt.Errorf("%s case %d has duplicate id %q", in.Suite, i+1, caseID)
		}
		caseIDs[caseID] = struct{}{}
		decoded = append(decoded, target)
	}
	if err := validateLocalFixtureCardinality(in, decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func newLocalCase(suite LocalSuite) (any, error) {
	switch suite {
	case LocalSuiteTools:
		return new(ToolCase), nil
	case LocalSuiteASR:
		return new(ASRCase), nil
	case LocalSuiteTTS:
		return new(TTSCase), nil
	case LocalSuiteEmbeddings:
		return new(EmbeddingCase), nil
	case LocalSuiteAcceleration:
		return new(AccelerationCase), nil
	default:
		return nil, fmt.Errorf("unsupported local suite %q", suite)
	}
}

func validateLocalCase(value any, ordinal int) error {
	switch v := value.(type) {
	case *ToolCase:
		return validateLocalToolCase(v, ordinal)
	case *ASRCase:
		return validateLocalASRCase(v, ordinal)
	case *TTSCase:
		return validateLocalTTSCase(v, ordinal)
	case *EmbeddingCase:
		return validateLocalEmbeddingCase(v, ordinal)
	case *AccelerationCase:
		return validateLocalAccelerationCase(v, ordinal)
	}
	return nil
}

func validateLocalToolCase(v *ToolCase, ordinal int) error {
	if v.ID == "" || strings.TrimSpace(v.Prompt) == "" || len(v.Tools) == 0 {
		return fmt.Errorf("tool case %d requires id, prompt, and tools", ordinal)
	}
	toolNames := make(map[string]struct{}, len(v.Tools))
	for _, tool := range v.Tools {
		if strings.TrimSpace(tool.Name) == "" || !json.Valid(tool.Parameters) {
			return fmt.Errorf("tool case %d has an invalid tool definition", ordinal)
		}
		if _, exists := toolNames[tool.Name]; exists {
			return fmt.Errorf("tool case %d has duplicate tool %q", ordinal, tool.Name)
		}
		toolNames[tool.Name] = struct{}{}
	}
	if v.Expected != nil && (strings.TrimSpace(v.Expected.Name) == "" || !json.Valid(v.Expected.Arguments)) {
		return fmt.Errorf("tool case %d has an invalid expected call", ordinal)
	}
	return nil
}

func validateLocalASRCase(v *ASRCase, ordinal int) error {
	if v.ID == "" || strings.TrimSpace(v.Transcript) == "" || strings.TrimSpace(v.WAVPath) == "" || len(v.WAVSHA256) != 64 {
		return fmt.Errorf("ASR case %d requires id, transcript, and sealed WAV path", ordinal)
	}
	return nil
}

func validateLocalTTSCase(v *TTSCase, ordinal int) error {
	if v.ID == "" || strings.TrimSpace(v.Prompt) == "" {
		return fmt.Errorf("TTS case %d requires id and prompt", ordinal)
	}
	return nil
}

func validateLocalAccelerationCase(v *AccelerationCase, ordinal int) error {
	if v.ID == "" || strings.TrimSpace(v.Prompt) == "" || strings.TrimSpace(v.Oracle) == "" {
		return fmt.Errorf("acceleration case %d requires id, prompt, and oracle", ordinal)
	}
	return nil
}

func validateLocalEmbeddingCase(v *EmbeddingCase, ordinal int) error {
	if v.ID == "" || len(v.Documents) == 0 || len(v.Queries) == 0 {
		return fmt.Errorf("embedding case %d requires id, documents, and queries", ordinal)
	}
	documents, err := validateEmbeddingDocuments(v.Documents, ordinal)
	if err != nil {
		return err
	}
	return validateEmbeddingQueries(v.Queries, documents, ordinal)
}

func validateEmbeddingDocuments(values []EmbeddingText, ordinal int) (map[string]struct{}, error) {
	documents := make(map[string]struct{}, len(values))
	for _, document := range values {
		if strings.TrimSpace(document.ID) == "" || strings.TrimSpace(document.Text) == "" {
			return nil, fmt.Errorf("embedding case %d has invalid document", ordinal)
		}
		if _, exists := documents[document.ID]; exists {
			return nil, fmt.Errorf("embedding case %d has duplicate document %q", ordinal, document.ID)
		}
		documents[document.ID] = struct{}{}
	}
	return documents, nil
}

func validateEmbeddingQueries(values []EmbeddingQuery, documents map[string]struct{}, ordinal int) error {
	queries := make(map[string]struct{}, len(values))
	for _, query := range values {
		if strings.TrimSpace(query.ID) == "" || strings.TrimSpace(query.Text) == "" || len(query.Relevant) == 0 {
			return fmt.Errorf("embedding case %d has invalid query", ordinal)
		}
		if _, exists := queries[query.ID]; exists {
			return fmt.Errorf("embedding case %d has duplicate query %q", ordinal, query.ID)
		}
		queries[query.ID] = struct{}{}
		if err := validateEmbeddingRelevance(query, documents, ordinal); err != nil {
			return err
		}
	}
	return nil
}

func validateEmbeddingRelevance(query EmbeddingQuery, documents map[string]struct{}, ordinal int) error {
	seenRelevant := make(map[string]struct{}, len(query.Relevant))
	for _, relevant := range query.Relevant {
		if _, exists := documents[relevant]; !exists {
			return fmt.Errorf("embedding case %d query %q references unknown document %q", ordinal, query.ID, relevant)
		}
		if _, exists := seenRelevant[relevant]; exists {
			return fmt.Errorf("embedding case %d query %q has duplicate relevance %q", ordinal, query.ID, relevant)
		}
		seenRelevant[relevant] = struct{}{}
	}
	return nil
}

func localCaseID(value any) string {
	switch value := value.(type) {
	case *ToolCase:
		return value.ID
	case *ASRCase:
		return value.ID
	case *TTSCase:
		return value.ID
	case *EmbeddingCase:
		return value.ID
	case *AccelerationCase:
		return value.ID
	default:
		return ""
	}
}

func validateLocalFixtureCardinality(in LocalInputManifest, decoded []any) error {
	want := requiredLocalCaseCount(in.Suite)
	if len(decoded) != want {
		return fmt.Errorf("%s manifest requires exactly %d cases, got %d", in.Suite, want, len(decoded))
	}
	switch in.Suite {
	case LocalSuiteASR, LocalSuiteTTS, LocalSuiteAcceleration:
		// Fixed-case suites; cardinality is enforced by requiredLocalCaseCount.
	case LocalSuiteTools:
		required := 0
		for _, value := range decoded {
			if value.(*ToolCase).Expected != nil {
				required++
			}
		}
		if required != 8 {
			return fmt.Errorf("tools manifest requires exactly 8 required-call and 2 no-call cases, got %d required-call", required)
		}
	case LocalSuiteEmbeddings:
		fixture := decoded[0].(*EmbeddingCase)
		if len(fixture.Documents) != 60 || len(fixture.Queries) != 20 {
			return fmt.Errorf("embedding manifest requires exactly 60 documents and 20 queries, got %d and %d", len(fixture.Documents), len(fixture.Queries))
		}
	}
	return nil
}
