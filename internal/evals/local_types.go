package evals

// Local evaluation contracts intentionally live outside the LLAMBO-2 scorer. They
// describe sealed, task-specific evidence only and are never catalog imports.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LocalSuite string

const (
	LocalSuiteTools        LocalSuite = "tools"
	LocalSuiteASR          LocalSuite = "asr"
	LocalSuiteTTS          LocalSuite = "tts"
	LocalSuiteEmbeddings   LocalSuite = "embeddings"
	LocalSuiteAcceleration LocalSuite = "acceleration"
	localEvalSchemaVersion            = 1
)

func (s LocalSuite) Valid() bool {
	switch s {
	case LocalSuiteTools, LocalSuiteASR, LocalSuiteTTS, LocalSuiteEmbeddings, LocalSuiteAcceleration:
		return true
	}
	return false
}

// LocalInputManifest is immutable input supplied by the caller. Cases remain
// raw so each adapter owns strict, suite-specific validation.
type LocalInputManifest struct {
	Suite   LocalSuite        `json:"suite"`
	Version string            `json:"version"`
	Cases   []json.RawMessage `json:"cases"`
}

func LoadLocalInputManifest(path string, suite LocalSuite) (LocalInputManifest, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LocalInputManifest{}, "", fmt.Errorf("read local evaluation input: %w", err)
	}
	if len(data) == 0 || len(data) > 16<<20 {
		return LocalInputManifest{}, "", fmt.Errorf("local evaluation input must be between 1 and %d bytes", 16<<20)
	}
	var in LocalInputManifest
	if err := json.Unmarshal(data, &in); err != nil {
		return LocalInputManifest{}, "", fmt.Errorf("parse local evaluation input: %w", err)
	}
	if !suite.Valid() || in.Suite != suite || strings.TrimSpace(in.Version) == "" || len(in.Cases) == 0 {
		return LocalInputManifest{}, "", fmt.Errorf("sealed input must contain matching suite, version, and cases")
	}
	for i, c := range in.Cases {
		if !json.Valid(c) {
			return LocalInputManifest{}, "", fmt.Errorf("case %d is not JSON", i+1)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(c, &object); err != nil || object == nil {
			return LocalInputManifest{}, "", fmt.Errorf("case %d must be a JSON object", i+1)
		}
	}
	h := sha256.Sum256(data)
	return in, hex.EncodeToString(h[:]), nil
}

// DecodeLocalCases proves that every sealed case has the adapter's expected
// JSON shape before a provider call. Callers can type-assert the returned
// values to ToolCase, ASRCase, TTSCase, EmbeddingCase, or AccelerationCase.
func DecodeLocalCases(in LocalInputManifest) ([]any, error) {
	decoded := make([]any, 0, len(in.Cases))
	caseIDs := make(map[string]struct{}, len(in.Cases))
	for i, raw := range in.Cases {
		var target any
		switch in.Suite {
		case LocalSuiteTools:
			target = new(ToolCase)
		case LocalSuiteASR:
			target = new(ASRCase)
		case LocalSuiteTTS:
			target = new(TTSCase)
		case LocalSuiteEmbeddings:
			target = new(EmbeddingCase)
		case LocalSuiteAcceleration:
			target = new(AccelerationCase)
		default:
			return nil, fmt.Errorf("unsupported local suite %q", in.Suite)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return nil, fmt.Errorf("parse %s case %d: %w", in.Suite, i+1, err)
		}
		switch v := target.(type) {
		case *ToolCase:
			if v.ID == "" || strings.TrimSpace(v.Prompt) == "" || len(v.Tools) == 0 {
				return nil, fmt.Errorf("tool case %d requires id, prompt, and tools", i+1)
			}
			toolNames := make(map[string]struct{}, len(v.Tools))
			for _, tool := range v.Tools {
				if strings.TrimSpace(tool.Name) == "" || !json.Valid(tool.Parameters) {
					return nil, fmt.Errorf("tool case %d has an invalid tool definition", i+1)
				}
				if _, exists := toolNames[tool.Name]; exists {
					return nil, fmt.Errorf("tool case %d has duplicate tool %q", i+1, tool.Name)
				}
				toolNames[tool.Name] = struct{}{}
			}
			if v.Expected != nil && (strings.TrimSpace(v.Expected.Name) == "" || !json.Valid(v.Expected.Arguments)) {
				return nil, fmt.Errorf("tool case %d has an invalid expected call", i+1)
			}
		case *ASRCase:
			if v.ID == "" || strings.TrimSpace(v.Transcript) == "" || strings.TrimSpace(v.WAVPath) == "" || len(v.WAVSHA256) != 64 {
				return nil, fmt.Errorf("ASR case %d requires id, transcript, and sealed WAV path", i+1)
			}
		case *TTSCase:
			if v.ID == "" || strings.TrimSpace(v.Prompt) == "" {
				return nil, fmt.Errorf("TTS case %d requires id and prompt", i+1)
			}
		case *EmbeddingCase:
			if v.ID == "" || len(v.Documents) == 0 || len(v.Queries) == 0 {
				return nil, fmt.Errorf("embedding case %d requires id, documents, and queries", i+1)
			}
			documents := make(map[string]struct{}, len(v.Documents))
			for _, document := range v.Documents {
				if strings.TrimSpace(document.ID) == "" || strings.TrimSpace(document.Text) == "" {
					return nil, fmt.Errorf("embedding case %d has invalid document", i+1)
				}
				if _, exists := documents[document.ID]; exists {
					return nil, fmt.Errorf("embedding case %d has duplicate document %q", i+1, document.ID)
				}
				documents[document.ID] = struct{}{}
			}
			queries := make(map[string]struct{}, len(v.Queries))
			for _, query := range v.Queries {
				if strings.TrimSpace(query.ID) == "" || strings.TrimSpace(query.Text) == "" || len(query.Relevant) == 0 {
					return nil, fmt.Errorf("embedding case %d has invalid query", i+1)
				}
				if _, exists := queries[query.ID]; exists {
					return nil, fmt.Errorf("embedding case %d has duplicate query %q", i+1, query.ID)
				}
				queries[query.ID] = struct{}{}
				seenRelevant := make(map[string]struct{}, len(query.Relevant))
				for _, relevant := range query.Relevant {
					if _, exists := documents[relevant]; !exists {
						return nil, fmt.Errorf("embedding case %d query %q references unknown document %q", i+1, query.ID, relevant)
					}
					if _, exists := seenRelevant[relevant]; exists {
						return nil, fmt.Errorf("embedding case %d query %q has duplicate relevance %q", i+1, query.ID, relevant)
					}
					seenRelevant[relevant] = struct{}{}
				}
			}
		case *AccelerationCase:
			if v.ID == "" || strings.TrimSpace(v.Prompt) == "" || strings.TrimSpace(v.Oracle) == "" {
				return nil, fmt.Errorf("acceleration case %d requires id, prompt, and oracle", i+1)
			}
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

type LocalPricing struct {
	InputPer1M  float64 `json:"input_per_1m"`
	OutputPer1M float64 `json:"output_per_1m"`
	Known       bool    `json:"known"`
}

func (p LocalPricing) Validate() error {
	if !p.Known || math.IsNaN(p.InputPer1M) || math.IsNaN(p.OutputPer1M) || math.IsInf(p.InputPer1M, 0) || math.IsInf(p.OutputPer1M, 0) || p.InputPer1M < 0 || p.OutputPer1M < 0 {
		return fmt.Errorf("known non-negative pricing is required")
	}
	return nil
}

type LocalRunIdentity struct {
	Suite           LocalSuite   `json:"suite"`
	SuiteVersion    string       `json:"suite_version"`
	InputSHA256     string       `json:"input_sha256"`
	RequestedModel  string       `json:"requested_model"`
	ReferenceModel  string       `json:"reference_model,omitempty"`
	FeatureModel    string       `json:"feature_model,omitempty"`
	Limit           int          `json:"limit"`
	TimeoutSeconds  int          `json:"timeout_seconds"`
	MaxOutputTokens int          `json:"max_output_tokens"`
	Pricing         LocalPricing `json:"pricing"`
	LocalUseCase    string       `json:"local_use_case,omitempty"`
}
type LocalRunManifest struct {
	SchemaVersion int              `json:"schema_version"`
	RunID         string           `json:"run_id"`
	CreatedAt     time.Time        `json:"created_at"`
	InputPath     string           `json:"input_path"`
	Identity      LocalRunIdentity `json:"identity"`
}

func NewLocalRunManifest(inputPath, inputHash string, in LocalInputManifest, model, reference, feature string, limit int, timeout time.Duration, pricing LocalPricing, now time.Time) (LocalRunManifest, error) {
	if !in.Suite.Valid() || strings.TrimSpace(model) == "" || limit < 1 || timeout <= 0 {
		return LocalRunManifest{}, fmt.Errorf("suite, exact model, positive limit, and timeout are required")
	}
	if err := pricing.Validate(); err != nil {
		return LocalRunManifest{}, err
	}
	if limit > len(in.Cases) {
		limit = len(in.Cases)
	}
	m := LocalRunManifest{SchemaVersion: localEvalSchemaVersion, CreatedAt: now.UTC(), InputPath: inputPath, Identity: LocalRunIdentity{Suite: in.Suite, SuiteVersion: in.Version, InputSHA256: inputHash, RequestedModel: model, ReferenceModel: reference, FeatureModel: feature, Limit: limit, TimeoutSeconds: int(timeout / time.Second), MaxOutputTokens: 32768, Pricing: pricing}}
	b, _ := json.Marshal(m.Identity)
	h := sha256.Sum256(b)
	m.RunID = hex.EncodeToString(h[:8])
	return m, nil
}
func LocalRunIdentityHash(m LocalRunManifest) string {
	b, _ := json.Marshal(m.Identity)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func SealLocalRunUseCase(m *LocalRunManifest, useCase string) {
	m.Identity.LocalUseCase = strings.TrimSpace(useCase)
	m.RunID = LocalRunIdentityHash(*m)[:16]
}

type LocalCall struct {
	CaseID         string          `json:"case_id"`
	RequestedModel string          `json:"requested_model"`
	ServedModel    string          `json:"served_model"`
	Input          json.RawMessage `json:"input"`
	Output         json.RawMessage `json:"output"`
	Error          string          `json:"error,omitempty"`
	InputTokens    int             `json:"input_tokens,omitempty"`
	OutputTokens   int             `json:"output_tokens,omitempty"`
	// UsageKnown distinguishes a provider-reported zero-token response from
	// omitted usage. It is deliberately persisted so campaign settlement can
	// fail closed after a restart rather than treating missing usage as free.
	UsageKnown        bool      `json:"usage_known"`
	LatencyMS         int64     `json:"latency_ms,omitempty"`
	ObservedCostUSD   float64   `json:"observed_cost_usd,omitempty"`
	ObservedCostKnown bool      `json:"observed_cost_known"`
	At                time.Time `json:"at"`
}

// ApplyLocalCallPricing derives a durable observed cost from the sealed run
// price and provider-reported token usage. Unknown paid usage intentionally
// remains unknown: callers must retain their worst-case campaign reservation.
func ApplyLocalCallPricing(pricing LocalPricing, call *LocalCall) error {
	if call == nil {
		return fmt.Errorf("local call is required")
	}
	if err := pricing.Validate(); err != nil {
		return err
	}
	if pricing.InputPer1M == 0 && pricing.OutputPer1M == 0 {
		call.ObservedCostUSD = 0
		call.ObservedCostKnown = true
		return nil
	}
	if call.Error != "" || !call.UsageKnown || call.InputTokens < 0 || call.OutputTokens < 0 {
		call.ObservedCostKnown = false
		return nil
	}
	call.ObservedCostUSD = float64(call.InputTokens)*pricing.InputPer1M/1_000_000 + float64(call.OutputTokens)*pricing.OutputPer1M/1_000_000
	call.ObservedCostKnown = true
	return nil
}

// ObservedLocalCost returns nil when any paid call has no authoritative usage.
// A known explicit zero price is still safely settled as zero.
func ObservedLocalCost(pricing LocalPricing, calls []LocalCall) (*float64, error) {
	if err := pricing.Validate(); err != nil {
		return nil, err
	}
	observed := 0.0
	for _, call := range calls {
		if !call.ObservedCostKnown {
			return nil, nil
		}
		observed += call.ObservedCostUSD
	}
	return &observed, nil
}

type LocalReport struct {
	RunID           string             `json:"run_id"`
	Suite           LocalSuite         `json:"suite"`
	Status          string             `json:"status"`
	ScoreIdentity   string             `json:"score_identity"`
	Score           float64            `json:"score"`
	Metrics         map[string]float64 `json:"metrics"`
	CaseCount       int                `json:"case_count"`
	ServedModels    []string           `json:"served_models"`
	ObservedCostUSD float64            `json:"observed_cost_usd"`
	Errors          []string           `json:"errors,omitempty"`
}
type LocalReceipt struct {
	SchemaVersion   int               `json:"schema_version"`
	RunID           string            `json:"run_id"`
	Status          string            `json:"status"`
	RequestedModel  string            `json:"requested_model"`
	ServedModels    []string          `json:"served_models"`
	IdentitySHA256  string            `json:"identity_sha256"`
	InputSHA256     string            `json:"input_sha256"`
	Suite           LocalSuite        `json:"suite"`
	SuiteVersion    string            `json:"suite_version"`
	CaseCount       int               `json:"case_count"`
	ObservedCostUSD float64           `json:"observed_cost_usd"`
	CreatedAt       time.Time         `json:"created_at"`
	FinishedAt      time.Time         `json:"finished_at"`
	ArtifactSHA256  map[string]string `json:"artifact_sha256"`
	Errors          []string          `json:"errors,omitempty"`
}

func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
func localSHA(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func ensureNewLocalOutput(path string, manifest LocalRunManifest) error {
	if path == "" || filepath.Clean(path) == "." {
		return fmt.Errorf("--output-dir must be explicit")
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	existing, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var old LocalRunManifest
	if json.Unmarshal(existing, &old) != nil || LocalRunIdentityHash(old) != LocalRunIdentityHash(manifest) {
		return fmt.Errorf("output directory belongs to a different run identity")
	}
	for _, name := range []string{"calls.jsonl", "report.json", "report.md", "receipt.json"} {
		if _, err := os.Stat(filepath.Join(path, name)); err == nil {
			return fmt.Errorf("output directory already contains %s; preserve it and use a new --output-dir", name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
