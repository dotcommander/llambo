package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestToolScoringCanonicalAndNoExtraCalls(t *testing.T) {
	t.Parallel()
	want := ToolCall{Name: "weather", Arguments: json.RawMessage(`{"city":"Richmond","units":"f"}`)}
	ok, err := ScoreToolCase(ToolCase{ID: "1", Expected: &want, Calls: []ToolCall{{Name: "weather", Arguments: json.RawMessage(`{ "units":"f", "city":"Richmond" }`)}}})
	if err != nil || !ok {
		t.Fatalf("canonical call = %v, %v", ok, err)
	}
	ok, err = ScoreToolCase(ToolCase{ID: "1", Expected: &want, Calls: []ToolCall{want, want}})
	if err != nil || ok {
		t.Fatalf("extra calls = %v, %v", ok, err)
	}
}
func TestTranscriptMetricsAndWAV(t *testing.T) {
	t.Parallel()
	if got := WordErrorRate("Hello, world!", "hello brave world"); math.Abs(got-.5) > 1e-9 {
		t.Fatalf("WER = %v", got)
	}
	if CharErrorRate("A", "B") != 1 {
		t.Fatal("CER")
	}
	wav := make([]byte, 48)
	copy(wav, "RIFF")
	copy(wav[8:], "WAVEfmt ")
	wav[16] = 16
	wav[20] = 1
	wav[22] = 1
	wav[24] = 0x40
	wav[25] = 0x1f
	wav[28] = 0x80
	wav[29] = 0x3e
	wav[34] = 16
	copy(wav[36:], "data")
	wav[40] = 4
	if _, err := ValidateWAV(wav); err != nil {
		t.Fatal(err)
	}
}
func TestRetrievalAndPairedScoring(t *testing.T) {
	t.Parallel()
	r, m, n, err := ScoreRetrieval([]RetrievalQuery{{ID: "q", Relevant: []string{"b"}}}, map[string]map[string]float64{"q": {"a": .9, "b": .8}})
	if err != nil || r != 0 || math.Abs(m-.5) > 1e-9 || math.Abs(n-1/math.Log2(3)) > 1e-9 {
		t.Fatalf("retrieval %.2f %.2f %.2f %v", r, m, n, err)
	}
	p, err := ScorePaired([]PairedResult{{true, 10, 10}}, []PairedResult{{true, 5, 20}})
	if err != nil || p.LatencyRatio != 2 || p.TokensPerSecondRatio != 2 {
		t.Fatalf("paired %#v %v", p, err)
	}
}
func TestLocalArtifactsAndReceiptHash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := LocalInputManifest{Suite: LocalSuiteTools, Version: "v1", Cases: []json.RawMessage{json.RawMessage(`{"id":"case-1"}`)}}
	m, err := NewLocalRunManifest("input", "abc", in, "groq/model", "", "", 1, time.Minute, LocalPricing{Known: true}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	r, err := WriteLocalRunArtifacts(dir, m, []LocalCall{{ServedModel: "groq/model"}}, LocalReport{ScoreIdentity: "tool exact", Score: 100}, time.Unix(2, 0))
	if err != nil || r.Status != "complete_quality" {
		t.Fatalf("receipt %#v %v", r, err)
	}
	if err := VerifyLocalReceipt(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyLocalReceipt(dir); err == nil {
		t.Fatal("corruption accepted")
	}
}
func TestIdentityAndCampaignBudget(t *testing.T) {
	t.Parallel()
	track := NewIdentityTracker("openrouter/@preset/free")
	if err := track.Record("openrouter/one"); err != nil {
		t.Fatal(err)
	}
	if err := track.Record("openrouter/two"); err == nil {
		t.Fatal("unstable alias accepted")
	}
	if err := NewIdentityTracker("openrouter/@preset/free").Record("openai/exact"); err == nil {
		t.Fatal("provider drift accepted")
	}
	if err := NewIdentityTracker("openrouter/@preset/free").Record("openrouter/@preset/free"); err == nil {
		t.Fatal("unresolved alias accepted")
	}
	l, err := OpenCampaignLedger(filepath.Join(t.TempDir(), "budget.json"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Admit("run", 6, 6); err != nil {
		t.Fatal(err)
	}
	if err := l.Admit("other", 5, 5); err == nil {
		t.Fatal("overbudget accepted")
	}
	if err := l.Record("run", 2, "complete_quality"); err != nil {
		t.Fatal(err)
	}
	if math.Abs(l.Remaining()-8) > 1e-9 {
		t.Fatalf("remaining %v", l.Remaining())
	}
}

type substitutingLocalExecutor struct{}

func (substitutingLocalExecutor) ExecuteLocal(_ context.Context, _ LocalRunManifest, _ json.RawMessage) (LocalCall, error) {
	return LocalCall{ServedModel: "groq/model", Input: json.RawMessage(`{"id":"easier","expected":null}`), Output: json.RawMessage(`{"calls":[]}`)}, nil
}

func TestLocalRunBindsRecordedInputAndCanaryIsPartial(t *testing.T) {
	t.Parallel()
	sealed := json.RawMessage(`{"id":"sealed","prompt":"Use weather.","tools":[{"name":"weather","parameters":{"type":"object"}}],"expected":{"name":"weather","arguments":{"city":"Richmond"}}}`)
	in := LocalInputManifest{Suite: LocalSuiteTools, Version: "v1", Cases: []json.RawMessage{sealed}}
	for i := 2; i <= 10; i++ {
		expected := `{"name":"weather","arguments":{"city":"Richmond"}}`
		if i >= 9 {
			expected = "null"
		}
		in.Cases = append(in.Cases, json.RawMessage(fmt.Sprintf(`{"id":"case-%d","prompt":"Use weather.","tools":[{"name":"weather","parameters":{"type":"object"}}],"expected":%s}`, i, expected)))
	}
	m, err := NewLocalRunManifest("input", "hash", in, "groq/model", "", "", 1, time.Minute, LocalPricing{Known: true}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	calls, err := RunLocalEvaluation(context.Background(), m, in, substitutingLocalExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if string(calls[0].Input) != string(sealed) {
		t.Fatalf("recorded input = %s", calls[0].Input)
	}
	report, err := ScoreLocalCalls(m, calls)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "partial" || report.Score != 0 {
		t.Fatalf("canary report = %#v", report)
	}
}
