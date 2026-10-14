package evals

// Local evaluation contracts intentionally live outside the LLAMBO-2 scorer. They
// describe sealed, task-specific evidence only and are never catalog imports.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalSuite names one sealed local evaluation suite.
type LocalSuite string

// Suite identifiers for local evaluations.
const (
	LocalSuiteTools        LocalSuite = "tools"
	LocalSuiteASR          LocalSuite = "asr"
	LocalSuiteTTS          LocalSuite = "tts"
	LocalSuiteEmbeddings   LocalSuite = "embeddings"
	LocalSuiteAcceleration LocalSuite = "acceleration"
	localEvalSchemaVersion            = 1
)

// Valid reports whether the suite is one of the supported identifiers.
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

// LoadLocalInputManifest reads and structurally validates a sealed local
// evaluation input, returning the manifest and the input file digest.
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
		return LocalInputManifest{}, "", errors.New("sealed input must contain matching suite, version, and cases")
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

// LocalPricing is the explicit per-million pricing required before any
// provider call.
type LocalPricing struct {
	InputPer1M  float64 `json:"input_per_1m"`
	OutputPer1M float64 `json:"output_per_1m"`
	Known       bool    `json:"known"`
}

// Validate enforces known, finite, non-negative pricing.
func (p LocalPricing) Validate() error {
	if !p.Known || math.IsNaN(p.InputPer1M) || math.IsNaN(p.OutputPer1M) || math.IsInf(p.InputPer1M, 0) || math.IsInf(p.OutputPer1M, 0) || p.InputPer1M < 0 || p.OutputPer1M < 0 {
		return errors.New("known non-negative pricing is required")
	}
	return nil
}

// LocalRunIdentity seals the preconditions of a local run: suite, input
// digest, model selectors, and budget bounds.
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

// LocalRunManifest is the durable start marker for a local run, keyed by a
// hash of its identity.
type LocalRunManifest struct {
	SchemaVersion int              `json:"schema_version"`
	RunID         string           `json:"run_id"`
	CreatedAt     time.Time        `json:"created_at"`
	InputPath     string           `json:"input_path"`
	Identity      LocalRunIdentity `json:"identity"`
}

// LocalCall is one ledgered provider call with served identity and usage.
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
		return errors.New("local call is required")
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

// LocalReport is the scored outcome of a local run.
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

// LocalReceipt is the sealed completion proof with artifact digests.
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
		return errors.New("--output-dir must be explicit")
	}
	if err := os.MkdirAll(path, 0o750); err != nil { //nolint:gosec // reviewed directory mode
		return err
	}
	existing, err := os.ReadFile(filepath.Join(path, manifestJSON))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var old LocalRunManifest
	if json.Unmarshal(existing, &old) != nil || LocalRunIdentityHash(old) != LocalRunIdentityHash(manifest) {
		return errors.New("output directory belongs to a different run identity")
	}
	for _, name := range []string{callsJsonl, reportJSON1, reportMd, "receipt.json"} {
		if _, err := os.Stat(filepath.Join(path, name)); err == nil {
			return fmt.Errorf("output directory already contains %s; preserve it and use a new --output-dir", name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
