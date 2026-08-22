package evals

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type IdentityTracker struct {
	requested string
	alias     bool
	served    string
}

func NewIdentityTracker(requested string) *IdentityTracker {
	return &IdentityTracker{requested: requested, alias: strings.Contains(requested, "@preset/") || strings.HasSuffix(requested, "-latest") || strings.HasSuffix(requested, "/local-model")}
}
func (t *IdentityTracker) Record(served string) error {
	served = strings.TrimSpace(served)
	provider, model, ok := strings.Cut(served, "/")
	requestedProvider, _, requestedOK := strings.Cut(t.requested, "/")
	if !ok || !requestedOK || provider == "" || model == "" {
		return fmt.Errorf("execution did not report served model identity")
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
func (t *IdentityTracker) Served() []string {
	if t.served == "" {
		return nil
	}
	return []string{t.served}
}

type LocalExecutor interface {
	ExecuteLocal(context.Context, LocalRunManifest, json.RawMessage) (LocalCall, error)
}
type LocalRunPlan struct {
	RunID                  string     `json:"run_id"`
	Suite                  LocalSuite `json:"suite"`
	Cases                  int        `json:"cases"`
	WorstCaseProviderCalls int        `json:"worst_case_provider_calls"`
	WorstCaseCostUSD       float64    `json:"worst_case_cost_usd"`
}

func PlanLocalRun(m LocalRunManifest, in LocalInputManifest) (LocalRunPlan, error) {
	if m.Identity.Suite != in.Suite || m.Identity.SuiteVersion != in.Version || m.Identity.Limit < 1 || m.Identity.Limit > len(in.Cases) {
		return LocalRunPlan{}, fmt.Errorf("manifest does not match sealed input")
	}
	if err := m.Identity.Pricing.Validate(); err != nil {
		return LocalRunPlan{}, err
	}
	if _, err := DecodeLocalCases(in); err != nil {
		return LocalRunPlan{}, err
	}
	if m.Identity.MaxOutputTokens < 1 {
		return LocalRunPlan{}, fmt.Errorf("positive maximum output tokens are required")
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

func RunLocalEvaluation(ctx context.Context, m LocalRunManifest, in LocalInputManifest, exec LocalExecutor) ([]LocalCall, error) {
	if exec == nil {
		return nil, fmt.Errorf("local executor is required")
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
		call.CaseID = fmt.Sprintf("%d", i+1)
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

func WriteLocalRunArtifacts(dir string, m LocalRunManifest, calls []LocalCall, report LocalReport, finished time.Time) (LocalReceipt, error) {
	if err := ensureNewLocalOutput(dir, m); err != nil {
		return LocalReceipt{}, err
	}
	if report.RunID == "" {
		report.RunID = m.RunID
	}
	if _, err := os.Stat(filepath.Join(dir, "receipt.json")); err == nil {
		return LocalReceipt{}, fmt.Errorf("completed output directory is immutable; use a new --output-dir")
	} else if !os.IsNotExist(err) {
		return LocalReceipt{}, err
	}
	if report.Suite == "" {
		report.Suite = m.Identity.Suite
	}
	report.CaseCount = len(calls)
	served := NewIdentityTracker(m.Identity.RequestedModel)
	for _, c := range calls {
		if c.Error == "" {
			if err := served.Record(c.ServedModel); err != nil {
				return LocalReceipt{}, err
			}
		}
		report.ObservedCostUSD += c.ObservedCostUSD
	}
	report.ServedModels = served.Served()
	if len(calls) != m.Identity.Limit || len(report.Errors) > 0 {
		report.Status = "partial"
	} else if report.Status == "" {
		report.Status = "complete_quality"
	}
	if err := writeJSON(filepath.Join(dir, "manifest.json"), m); err != nil {
		return LocalReceipt{}, err
	}
	if err := writeCalls(filepath.Join(dir, "calls.jsonl"), calls); err != nil {
		return LocalReceipt{}, err
	}
	if err := writeJSON(filepath.Join(dir, "report.json"), report); err != nil {
		return LocalReceipt{}, err
	}
	md := fmt.Sprintf("# Local evaluation %s\n\nStatus: %s\n\nScore (%s): %.6f\n", m.RunID, report.Status, report.ScoreIdentity, report.Score)
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(md), 0o644); err != nil {
		return LocalReceipt{}, err
	}
	hashes := map[string]string{}
	for _, name := range []string{"manifest.json", "calls.jsonl", "report.json", "report.md"} {
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			return LocalReceipt{}, e
		}
		hashes[name] = localSHA(b)
	}
	r := LocalReceipt{SchemaVersion: localEvalSchemaVersion, RunID: m.RunID, Status: report.Status, RequestedModel: m.Identity.RequestedModel, ServedModels: report.ServedModels, IdentitySHA256: LocalRunIdentityHash(m), InputSHA256: m.Identity.InputSHA256, Suite: m.Identity.Suite, SuiteVersion: m.Identity.SuiteVersion, CaseCount: len(calls), ObservedCostUSD: report.ObservedCostUSD, CreatedAt: m.CreatedAt, FinishedAt: finished.UTC(), ArtifactSHA256: hashes, Errors: report.Errors}
	if err := writeJSON(filepath.Join(dir, "receipt.json"), r); err != nil {
		return LocalReceipt{}, err
	}
	return r, nil
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}
func writeCalls(path string, calls []LocalCall) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, c := range calls {
		b, e := json.Marshal(c)
		if e != nil {
			return e
		}
		if _, e = w.Write(append(b, '\n')); e != nil {
			return e
		}
	}
	return w.Flush()
}
func VerifyLocalReceipt(dir string) error {
	b, e := os.ReadFile(filepath.Join(dir, "receipt.json"))
	if e != nil {
		return e
	}
	var r LocalReceipt
	if e = json.Unmarshal(b, &r); e != nil {
		return e
	}
	for name, want := range r.ArtifactSHA256 {
		got, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil || localSHA(got) != want {
			return fmt.Errorf("artifact hash mismatch: %s", name)
		}
	}
	return nil
}
func sortedStrings(v []string) []string { sort.Strings(v); return v }

// HTTPJSONLocalExecutor is deliberately generic: an endpoint receives the raw
// sealed case and returns a LocalCall-shaped JSON object. Provider adapters can
// be layered above it without allowing the local runner to infer protocols.
type HTTPJSONLocalExecutor struct {
	Endpoint string
	Client   *http.Client
}

func (e HTTPJSONLocalExecutor) ExecuteLocal(ctx context.Context, _ LocalRunManifest, input json.RawMessage) (LocalCall, error) {
	req, e2 := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint, strings.NewReader(string(input)))
	if e2 != nil {
		return LocalCall{}, e2
	}
	req.Header.Set("Content-Type", "application/json")
	c := e.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Minute}
	}
	res, e2 := c.Do(req)
	if e2 != nil {
		return LocalCall{}, e2
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return LocalCall{}, fmt.Errorf("local endpoint HTTP %d", res.StatusCode)
	}
	var call LocalCall
	e2 = json.NewDecoder(res.Body).Decode(&call)
	return call, e2
}
