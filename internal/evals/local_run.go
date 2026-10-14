package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Repeated string literals consolidated by goconst.
const (
	completeQuality = "complete_quality"
	partial1        = "partial"
)

const reportMd = "report.md"

const manifestJSON = "manifest.json"

// IdentityTracker enforces that every call serves the exact requested model
// and that aliases resolve stably.
type IdentityTracker struct {
	requested string
	alias     bool
	served    string
}

// NewIdentityTracker classifies the requested selector as alias or exact.
func NewIdentityTracker(requested string) *IdentityTracker {
	return &IdentityTracker{requested: requested, alias: strings.Contains(requested, "@preset/") || strings.HasSuffix(requested, "-latest") || strings.HasSuffix(requested, "/local-model")}
}

// Record validates one served identity against the request.
func (t *IdentityTracker) Record(served string) error {
	served = strings.TrimSpace(served)
	provider, model, ok := strings.Cut(served, "/")
	requestedProvider, _, requestedOK := strings.Cut(t.requested, "/")
	if !ok || !requestedOK || provider == "" || model == "" {
		return errors.New("execution did not report served model identity")
	}
	if provider != requestedProvider {
		return fmt.Errorf("served provider %q differs from requested provider %q", provider, requestedProvider)
	}
	if t.alias && NewIdentityTracker(served).alias {
		return fmt.Errorf("alias %q did not resolve to an exact served model", t.requested)
	}
	if !t.alias && served != t.requested {
		return fmt.Errorf("served model %q differs from requested exact model %q", served, t.requested)
	}
	if t.served != "" && t.served != served {
		return fmt.Errorf("unstable alias: resolved both %q and %q", t.served, served)
	}
	t.served = served
	return nil
}

// Served returns the distinct served identities, or nil.
func (t *IdentityTracker) Served() []string {
	if t.served == "" {
		return nil
	}
	return []string{t.served}
}

// LocalExecutor executes one sealed case for a suite.
type LocalExecutor interface {
	ExecuteLocal(context.Context, LocalRunManifest, json.RawMessage) (LocalCall, error)
}

// LocalRunPlan is the provider-free plan for a local run.
type LocalRunPlan struct {
	RunID                  string     `json:"run_id"`
	Suite                  LocalSuite `json:"suite"`
	Cases                  int        `json:"cases"`
	WorstCaseProviderCalls int        `json:"worst_case_provider_calls"`
	WorstCaseCostUSD       float64    `json:"worst_case_cost_usd"`
}

// PlanLocalRun verifies the manifest matches the sealed input and computes
// worst-case call counts and cost.
func PlanLocalRun(m LocalRunManifest, in LocalInputManifest) (LocalRunPlan, error) {
	if m.Identity.Suite != in.Suite || m.Identity.SuiteVersion != in.Version || m.Identity.Limit < 1 || m.Identity.Limit > len(in.Cases) {
		return LocalRunPlan{}, errors.New("manifest does not match sealed input")
	}
	if err := m.Identity.Pricing.Validate(); err != nil {
		return LocalRunPlan{}, err
	}
	if _, err := DecodeLocalCases(in); err != nil {
		return LocalRunPlan{}, err
	}
	if m.Identity.MaxOutputTokens < 1 {
		return LocalRunPlan{}, errors.New("positive maximum output tokens are required")
	}
	plan := LocalRunPlan{RunID: m.RunID, Suite: in.Suite, Cases: m.Identity.Limit, WorstCaseProviderCalls: m.Identity.Limit}
	if in.Suite == LocalSuiteTTS && m.Identity.ReferenceModel != "" {
		plan.WorstCaseProviderCalls *= 2 // synthesis plus ASR intelligibility round trip
	}
	if in.Suite == LocalSuiteAcceleration {
		plan.WorstCaseProviderCalls = 2*m.Identity.Limit + 2 // off/on per case plus one excluded warm-up per condition
	}
	for _, raw := range in.Cases[:m.Identity.Limit] {
		callCost := float64(len(raw))*m.Identity.Pricing.InputPer1M/1_000_000 + float64(m.Identity.MaxOutputTokens)*m.Identity.Pricing.OutputPer1M/1_000_000
		if in.Suite == LocalSuiteAcceleration {
			callCost *= 2
		}
		plan.WorstCaseCostUSD += callCost
	}
	if in.Suite == LocalSuiteAcceleration {
		raw := in.Cases[0]
		plan.WorstCaseCostUSD += 2 * (float64(len(raw))*m.Identity.Pricing.InputPer1M/1_000_000 + float64(m.Identity.MaxOutputTokens)*m.Identity.Pricing.OutputPer1M/1_000_000)
	}
	return plan, nil
}

// RunLocalEvaluation executes all planned cases, enforcing identity, budget,
// and ledger durability.
func RunLocalEvaluation(ctx context.Context, m LocalRunManifest, in LocalInputManifest, exec LocalExecutor) ([]LocalCall, error) {
	if exec == nil {
		return nil, errors.New("local executor is required")
	}
	if _, err := PlanLocalRun(m, in); err != nil {
		return nil, err
	}
	t := NewIdentityTracker(m.Identity.RequestedModel)
	calls := make([]LocalCall, 0, m.Identity.Limit)
	for i := 0; i < m.Identity.Limit; i++ {
		call, err := exec.ExecuteLocal(ctx, m, in.Cases[i])
		if err != nil {
			return calls, err
		}
		call.CaseID = strconv.Itoa(i + 1)
		call.RequestedModel = m.Identity.RequestedModel
		call.Input = append(json.RawMessage(nil), in.Cases[i]...)
		call.At = time.Now().UTC()
		if err := ApplyLocalCallPricing(m.Identity.Pricing, &call); err != nil {
			return calls, err
		}
		if err := t.Record(call.ServedModel); err != nil {
			return calls, err
		}
		calls = append(calls, call)
	}
	return calls, nil
}

// HTTPJSONLocalExecutor is deliberately generic: an endpoint receives the raw
// sealed case and returns a LocalCall-shaped JSON object. Provider adapters can
// be layered above it without allowing the local runner to infer protocols.
// HTTPJSONLocalExecutor executes cases by posting their raw JSON to a chat
// completion endpoint.
type HTTPJSONLocalExecutor struct {
	Endpoint string
	Client   *http.Client
}

// ExecuteLocal posts one sealed case and ledgers the response.
func (e HTTPJSONLocalExecutor) ExecuteLocal(ctx context.Context, _ LocalRunManifest, input json.RawMessage) (LocalCall, error) {
	req, e2 := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint, strings.NewReader(string(input)))
	if e2 != nil {
		return LocalCall{}, e2
	}
	req.Header.Set("Content-Type", applicationJSON)
	c := e.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Minute}
	}
	res, e2 := c.Do(req)
	if e2 != nil {
		return LocalCall{}, e2
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return LocalCall{}, fmt.Errorf("local endpoint HTTP %d", res.StatusCode)
	}
	var call LocalCall
	e2 = json.NewDecoder(res.Body).Decode(&call)
	return call, e2
}
