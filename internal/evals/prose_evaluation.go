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
)

// Repeated string literals consolidated by goconst.
const (
	antiRepetition      = "anti_repetition"
	depthAndDevelopment = "depth_and_development"
	mechanics1          = "mechanics"
	proseCraft          = "prose_craft"
	structuralCoherence = "structural_coherence"
	targetReaderFit     = "target_reader_fit"
)

// Prose judge prompt and schema identity for the v2 six-dimension contract.
const (
	ProseEvaluationPromptVersion = "prose-evaluation-v2"
	ProseEvaluationSchemaName    = "ProseEvaluation"
)

// ErrInvalidProseEvaluation wraps every judge-response validation failure.
var (
	ErrInvalidProseEvaluation = errors.New("invalid prose evaluation")
	//go:embed prompts/prose-evaluation-system.txt
	proseEvaluationSystemPrompt string
	//go:embed prompts/prose-evaluation-schema.json
	proseEvaluationSchema json.RawMessage
	proseRationalePattern = regexp.MustCompile(`^[^\s.!?](?:[^\r\n.!?]*[^\s.!?])?\.$`)
)

// ProseEvidenceSpan is a half-open byte range in the evaluated prose cited
// as dimension evidence.
type ProseEvidenceSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ProseDimensionEvaluation is the judge verdict for one dimension.
type ProseDimensionEvaluation struct {
	Score      int                 `json:"score"`
	Applicable bool                `json:"applicable"`
	Rationale  string              `json:"rationale"`
	Evidence   []ProseEvidenceSpan `json:"evidence"`
}

// ProseEvaluation is the decoded six-dimension judge verdict.
type ProseEvaluation struct {
	DepthAndDevelopment ProseDimensionEvaluation `json:"depth_and_development"`
	StructuralCoherence ProseDimensionEvaluation `json:"structural_coherence"`
	ProseCraft          ProseDimensionEvaluation `json:"prose_craft"`
	AntiRepetition      ProseDimensionEvaluation `json:"anti_repetition"`
	TargetReaderFit     ProseDimensionEvaluation `json:"target_reader_fit"`
	Mechanics           ProseDimensionEvaluation `json:"mechanics"`
	Aggregate           ProseEvaluationAggregate `json:"-"`
}

// ProseEvaluationAggregate is the derived aggregate score with its mechanics
// cap outcome.
type ProseEvaluationAggregate struct {
	Version              string  `json:"version"`
	Score                float64 `json:"score"`
	ApplicableDimensions int     `json:"applicable_dimensions"`
	MechanicsCapApplied  bool    `json:"mechanics_cap_applied"`
}

// ProseEvaluationInput is the judge payload: task, reader profile, and prose.
type ProseEvaluationInput struct {
	Task          string `json:"task"`
	ReaderProfile string `json:"reader_profile"`
	Prose         string `json:"prose"`
}

// ProseJudgeRequest is a fully assembled structured-output judge call.
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

// ProseEvaluationSchema returns the embedded judge JSON schema.
func ProseEvaluationSchema() json.RawMessage {
	return append(json.RawMessage(nil), proseEvaluationSchema...)
}

// Validate requires non-whitespace task, reader profile, and prose.
func (input ProseEvaluationInput) Validate() error {
	for _, field := range []struct{ name, value string }{{task1, input.Task}, {"reader profile", input.ReaderProfile}, {"prose", input.Prose}} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("prose %s must contain non-whitespace text", field.name)
		}
	}
	return nil
}

// NewProseJudgeRequest validates input and assembles the judge request with
// the pinned prompt version and schema.
func NewProseJudgeRequest(input ProseEvaluationInput) (ProseJudgeRequest, error) {
	if err := input.Validate(); err != nil {
		return ProseJudgeRequest{}, err
	}
	if !json.Valid(proseEvaluationSchema) {
		return ProseJudgeRequest{}, errors.New("embedded prose evaluation schema is invalid JSON")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return ProseJudgeRequest{}, fmt.Errorf("encode prose judge payload: %w", err)
	}
	return ProseJudgeRequest{PromptVersion: ProseEvaluationPromptVersion, SchemaName: ProseEvaluationSchemaName, SystemPrompt: strings.TrimSpace(proseEvaluationSystemPrompt), UserPayload: payload, JSONSchema: ProseEvaluationSchema()}, nil
}

// DecodeProseEvaluation strictly decodes a judge response, validates evidence
// spans against the prose, and computes the aggregate.
func DecodeProseEvaluation(raw []byte, prose string) (ProseEvaluation, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ProseEvaluation{}, fmt.Errorf("%w: response is empty", ErrInvalidProseEvaluation)
	}
	if err := rejectDuplicateProseJSONKeys(trimmed); err != nil {
		return ProseEvaluation{}, fmt.Errorf("%w: %w", ErrInvalidProseEvaluation, err)
	}
	var wire proseEvaluationWire
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return ProseEvaluation{}, fmt.Errorf("%w: parse strict JSON: %w", ErrInvalidProseEvaluation, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return ProseEvaluation{}, fmt.Errorf("%w: %w", ErrInvalidProseEvaluation, err)
	}
	evaluation, err := wire.evaluation()
	if err != nil {
		return ProseEvaluation{}, fmt.Errorf("%w: %w", ErrInvalidProseEvaluation, err)
	}
	if err := evaluation.Validate(prose); err != nil {
		return ProseEvaluation{}, fmt.Errorf("%w: %w", ErrInvalidProseEvaluation, err)
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
	depth, err := convert(depthAndDevelopment, wire.DepthAndDevelopment)
	if err != nil {
		return ProseEvaluation{}, err
	}
	structure, err := convert(structuralCoherence, wire.StructuralCoherence)
	if err != nil {
		return ProseEvaluation{}, err
	}
	craft, err := convert(proseCraft, wire.ProseCraft)
	if err != nil {
		return ProseEvaluation{}, err
	}
	repetition, err := convert(antiRepetition, wire.AntiRepetition)
	if err != nil {
		return ProseEvaluation{}, err
	}
	reader, err := convert(targetReaderFit, wire.TargetReaderFit)
	if err != nil {
		return ProseEvaluation{}, err
	}
	mechanics, err := convert(mechanics1, wire.Mechanics)
	if err != nil {
		return ProseEvaluation{}, err
	}
	return ProseEvaluation{DepthAndDevelopment: depth, StructuralCoherence: structure, ProseCraft: craft, AntiRepetition: repetition, TargetReaderFit: reader, Mechanics: mechanics}, nil
}

// Dimensions lists the six dimension verdicts in stable order.
func (e ProseEvaluation) Dimensions() []struct {
	ID    string
	Value ProseDimensionEvaluation
} {
	return []struct {
		ID    string
		Value ProseDimensionEvaluation
	}{{depthAndDevelopment, e.DepthAndDevelopment}, {structuralCoherence, e.StructuralCoherence}, {proseCraft, e.ProseCraft}, {antiRepetition, e.AntiRepetition}, {targetReaderFit, e.TargetReaderFit}, {mechanics1, e.Mechanics}}
}

// Validate checks evaluation structure and citation spans against the source prose.
func (e ProseEvaluation) Validate(prose string) error {
	if strings.TrimSpace(prose) == "" {
		return errors.New("candidate prose is empty")
	}
	for _, dimension := range e.Dimensions() {
		if err := validateProseDimension(dimension.ID, dimension.Value, prose); err != nil {
			return err
		}
	}
	return nil
}

// ComputeProseEvaluationAggregate is Go-owned. Mechanics caps the result so artifacts cannot be offset by fluent prose.
func ComputeProseEvaluationAggregate(e ProseEvaluation) ProseEvaluationAggregate {
	weights := map[string]float64{depthAndDevelopment: .20, structuralCoherence: .20, proseCraft: .20, antiRepetition: .15, targetReaderFit: .15, mechanics1: .10}
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
			return errors.New("response contains trailing JSON")
		}
		return fmt.Errorf("parse JSON tail: %w", err)
	}
	return nil
}
