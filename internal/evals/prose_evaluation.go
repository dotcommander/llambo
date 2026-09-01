package evals

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	ProseEvaluationPromptVersion = "prose-evaluation-v2"
	ProseEvaluationSchemaName    = "ProseEvaluation"
)

var (
	ErrInvalidProseEvaluation = errors.New("invalid prose evaluation")
	//go:embed prompts/prose-evaluation-system.txt
	proseEvaluationSystemPrompt string
	//go:embed prompts/prose-evaluation-schema.json
	proseEvaluationSchema json.RawMessage
	proseRationalePattern = regexp.MustCompile(`^[^\s.!?](?:[^\r\n.!?]*[^\s.!?])?\.$`)
)

type ProseEvidenceSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}
type ProseDimensionEvaluation struct {
	Score      int                 `json:"score"`
	Applicable bool                `json:"applicable"`
	Rationale  string              `json:"rationale"`
	Evidence   []ProseEvidenceSpan `json:"evidence"`
}
type ProseEvaluation struct {
	DepthAndDevelopment ProseDimensionEvaluation `json:"depth_and_development"`
	StructuralCoherence ProseDimensionEvaluation `json:"structural_coherence"`
	ProseCraft          ProseDimensionEvaluation `json:"prose_craft"`
	AntiRepetition      ProseDimensionEvaluation `json:"anti_repetition"`
	TargetReaderFit     ProseDimensionEvaluation `json:"target_reader_fit"`
	Mechanics           ProseDimensionEvaluation `json:"mechanics"`
	Aggregate           ProseEvaluationAggregate `json:"-"`
}
type ProseEvaluationAggregate struct {
	Version              string  `json:"version"`
	Score                float64 `json:"score"`
	ApplicableDimensions int     `json:"applicable_dimensions"`
	MechanicsCapApplied  bool    `json:"mechanics_cap_applied"`
}
type ProseEvaluationInput struct {
	Task          string `json:"task"`
	ReaderProfile string `json:"reader_profile"`
	Prose         string `json:"prose"`
}
type ProseJudgeRequest struct {
	PromptVersion string
	SchemaName    string
	SystemPrompt  string
	UserPayload   json.RawMessage
	JSONSchema    json.RawMessage
}
type proseEvidenceWire struct {
	Start *int `json:"start"`
	End   *int `json:"end"`
}
type proseDimensionWire struct {
	Score      *int                 `json:"score"`
	Applicable *bool                `json:"applicable"`
	Rationale  *string              `json:"rationale"`
	Evidence   *[]proseEvidenceWire `json:"evidence"`
}
type proseEvaluationWire struct {
	DepthAndDevelopment *proseDimensionWire `json:"depth_and_development"`
	StructuralCoherence *proseDimensionWire `json:"structural_coherence"`
	ProseCraft          *proseDimensionWire `json:"prose_craft"`
	AntiRepetition      *proseDimensionWire `json:"anti_repetition"`
	TargetReaderFit     *proseDimensionWire `json:"target_reader_fit"`
	Mechanics           *proseDimensionWire `json:"mechanics"`
}

func ProseEvaluationSchema() json.RawMessage {
	return append(json.RawMessage(nil), proseEvaluationSchema...)
}

func (input ProseEvaluationInput) Validate() error {
	for _, field := range []struct{ name, value string }{{"task", input.Task}, {"reader profile", input.ReaderProfile}, {"prose", input.Prose}} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("prose %s must contain non-whitespace text", field.name)
		}
	}
	return nil
}
func NewProseJudgeRequest(input ProseEvaluationInput) (ProseJudgeRequest, error) {
	if err := input.Validate(); err != nil {
		return ProseJudgeRequest{}, err
	}
	if !json.Valid(proseEvaluationSchema) {
		return ProseJudgeRequest{}, fmt.Errorf("embedded prose evaluation schema is invalid JSON")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return ProseJudgeRequest{}, fmt.Errorf("encode prose judge payload: %w", err)
	}
	return ProseJudgeRequest{PromptVersion: ProseEvaluationPromptVersion, SchemaName: ProseEvaluationSchemaName, SystemPrompt: strings.TrimSpace(proseEvaluationSystemPrompt), UserPayload: payload, JSONSchema: ProseEvaluationSchema()}, nil
}

func DecodeProseEvaluation(raw []byte, prose string) (ProseEvaluation, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ProseEvaluation{}, fmt.Errorf("%w: response is empty", ErrInvalidProseEvaluation)
	}
	if err := rejectDuplicateProseJSONKeys(trimmed); err != nil {
		return ProseEvaluation{}, fmt.Errorf("%w: %v", ErrInvalidProseEvaluation, err)
	}
	var wire proseEvaluationWire
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return ProseEvaluation{}, fmt.Errorf("%w: parse strict JSON: %v", ErrInvalidProseEvaluation, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return ProseEvaluation{}, fmt.Errorf("%w: %v", ErrInvalidProseEvaluation, err)
	}
	evaluation, err := wire.evaluation()
	if err != nil {
		return ProseEvaluation{}, fmt.Errorf("%w: %v", ErrInvalidProseEvaluation, err)
	}
	if err := evaluation.Validate(prose); err != nil {
		return ProseEvaluation{}, fmt.Errorf("%w: %v", ErrInvalidProseEvaluation, err)
	}
	evaluation.Aggregate = ComputeProseEvaluationAggregate(evaluation)
	return evaluation, nil
}

func (wire proseEvaluationWire) evaluation() (ProseEvaluation, error) {
	convert := func(name string, value *proseDimensionWire) (ProseDimensionEvaluation, error) {
		if value == nil || value.Score == nil || value.Applicable == nil || value.Rationale == nil || value.Evidence == nil {
			return ProseDimensionEvaluation{}, fmt.Errorf("%s has a missing required field", name)
		}
		evidence := make([]ProseEvidenceSpan, len(*value.Evidence))
		for i, span := range *value.Evidence {
			if span.Start == nil || span.End == nil {
				return ProseDimensionEvaluation{}, fmt.Errorf("%s evidence %d has a missing required field", name, i+1)
			}
			evidence[i] = ProseEvidenceSpan{Start: *span.Start, End: *span.End}
		}
		return ProseDimensionEvaluation{Score: *value.Score, Applicable: *value.Applicable, Rationale: *value.Rationale, Evidence: evidence}, nil
	}
	depth, err := convert("depth_and_development", wire.DepthAndDevelopment)
	if err != nil {
		return ProseEvaluation{}, err
	}
	structure, err := convert("structural_coherence", wire.StructuralCoherence)
	if err != nil {
		return ProseEvaluation{}, err
	}
	craft, err := convert("prose_craft", wire.ProseCraft)
	if err != nil {
		return ProseEvaluation{}, err
	}
	repetition, err := convert("anti_repetition", wire.AntiRepetition)
	if err != nil {
		return ProseEvaluation{}, err
	}
	reader, err := convert("target_reader_fit", wire.TargetReaderFit)
	if err != nil {
		return ProseEvaluation{}, err
	}
	mechanics, err := convert("mechanics", wire.Mechanics)
	if err != nil {
		return ProseEvaluation{}, err
	}
	return ProseEvaluation{DepthAndDevelopment: depth, StructuralCoherence: structure, ProseCraft: craft, AntiRepetition: repetition, TargetReaderFit: reader, Mechanics: mechanics}, nil
}
func (e ProseEvaluation) Dimensions() []struct {
	ID    string
	Value ProseDimensionEvaluation
} {
	return []struct {
		ID    string
		Value ProseDimensionEvaluation
	}{{"depth_and_development", e.DepthAndDevelopment}, {"structural_coherence", e.StructuralCoherence}, {"prose_craft", e.ProseCraft}, {"anti_repetition", e.AntiRepetition}, {"target_reader_fit", e.TargetReaderFit}, {"mechanics", e.Mechanics}}
}
func (e ProseEvaluation) Validate(prose string) error {
	if strings.TrimSpace(prose) == "" {
		return fmt.Errorf("candidate prose is empty")
	}
	for _, dimension := range e.Dimensions() {
		v := dimension.Value
		if dimension.ID == "mechanics" && !v.Applicable {
			return fmt.Errorf("mechanics must be applicable")
		}
		if v.Score < 1 || v.Score > 5 {
			return fmt.Errorf("%s score must be an integer from 1 through 5, got %d", dimension.ID, v.Score)
		}
		if strings.TrimSpace(v.Rationale) != v.Rationale || len(v.Rationale) > 240 || !proseRationalePattern.MatchString(v.Rationale) {
			return fmt.Errorf("%s rationale must be one concise trimmed sentence", dimension.ID)
		}
		if len(v.Evidence) == 0 {
			return fmt.Errorf("%s requires at least one evidence span", dimension.ID)
		}
		for _, span := range v.Evidence {
			if span.Start < 0 || span.End <= span.Start || span.End > len(prose) || !utf8.ValidString(prose[span.Start:span.End]) {
				return fmt.Errorf("%s has an invalid evidence span [%d,%d)", dimension.ID, span.Start, span.End)
			}
		}
	}
	return nil
}

// ComputeProseEvaluationAggregate is Go-owned. Mechanics caps the result so artifacts cannot be offset by fluent prose.
func ComputeProseEvaluationAggregate(e ProseEvaluation) ProseEvaluationAggregate {
	weights := map[string]float64{"depth_and_development": .20, "structural_coherence": .20, "prose_craft": .20, "anti_repetition": .15, "target_reader_fit": .15, "mechanics": .10}
	total, weight, applicable := 0.0, 0.0, 0
	for _, dimension := range e.Dimensions() {
		if dimension.Value.Applicable {
			w := weights[dimension.ID]
			total += float64(dimension.Value.Score) * w
			weight += w
			applicable++
		}
	}
	result := ProseEvaluationAggregate{Version: "prose-evaluation-aggregate-v2", ApplicableDimensions: applicable}
	if weight == 0 {
		return result
	}
	result.Score = math.Round((total/weight)*1000) / 1000
	if e.Mechanics.Applicable && float64(e.Mechanics.Score) < result.Score {
		result.Score = float64(e.Mechanics.Score)
		result.MechanicsCapApplied = true
	}
	return result
}
func rejectDuplicateProseJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := walkProseJSONValue(decoder); err != nil {
		return fmt.Errorf("parse JSON structure: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("response contains trailing JSON")
		}
		return fmt.Errorf("parse JSON tail: %w", err)
	}
	return nil
}
func walkProseJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate field %q", key)
			}
			seen[key] = struct{}{}
			if err := walkProseJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("object has invalid closing delimiter %q", end)
		}
	case '[':
		for decoder.More() {
			if err := walkProseJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("array has invalid closing delimiter %q", end)
		}
	default:
		return fmt.Errorf("unexpected delimiter %q", delim)
	}
	return nil
}
