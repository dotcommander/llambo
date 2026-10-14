package evals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// LocalManifestInput contains the sealed identity and operator bounds of a run.
type LocalManifestInput struct {
	InputPath   string
	InputSHA256 string
	Input       LocalInputManifest
	Model       string
	Reference   string
	Feature     string
	Limit       int
	Timeout     time.Duration
	Pricing     LocalPricing
	CreatedAt   time.Time
}

// NewLocalRunManifest seals a run identity without executing provider work.
func NewLocalRunManifest(input LocalManifestInput) (LocalRunManifest, error) {
	limit := input.Limit
	if !input.Input.Suite.Valid() || strings.TrimSpace(input.Model) == "" || limit < 1 || input.Timeout <= 0 {
		return LocalRunManifest{}, errors.New("suite, exact model, positive limit, and timeout are required")
	}
	if err := input.Pricing.Validate(); err != nil {
		return LocalRunManifest{}, err
	}
	if limit > len(input.Input.Cases) {
		limit = len(input.Input.Cases)
	}
	m := LocalRunManifest{SchemaVersion: localEvalSchemaVersion, CreatedAt: input.CreatedAt.UTC(), InputPath: input.InputPath, Identity: LocalRunIdentity{Suite: input.Input.Suite, SuiteVersion: input.Input.Version, InputSHA256: input.InputSHA256, RequestedModel: input.Model, ReferenceModel: input.Reference, FeatureModel: input.Feature, Limit: limit, TimeoutSeconds: int(input.Timeout / time.Second), MaxOutputTokens: 32768, Pricing: input.Pricing}}
	b, _ := json.Marshal(m.Identity)
	h := sha256.Sum256(b)
	m.RunID = hex.EncodeToString(h[:8])
	return m, nil
}

// LocalRunIdentityHash returns the full hex digest of the manifest identity.
func LocalRunIdentityHash(m LocalRunManifest) string {
	b, _ := json.Marshal(m.Identity)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// SealLocalRunUseCase binds a use-case label into the identity and rederives
// the run ID.
func SealLocalRunUseCase(m *LocalRunManifest, useCase string) {
	m.Identity.LocalUseCase = strings.TrimSpace(useCase)
	m.RunID = LocalRunIdentityHash(*m)[:16]
}
