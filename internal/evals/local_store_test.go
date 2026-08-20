package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func localStoreManifest(t *testing.T, limit int) (LocalRunManifest, LocalInputManifest) {
	t.Helper()
	in := LocalInputManifest{Suite: LocalSuiteTools, Version: "v1"}
	for i := 0; i < 10; i++ {
		expected := `{"name":"noop","arguments":{}}`
		if i >= 8 {
			expected = "null"
		}
		in.Cases = append(in.Cases, json.RawMessage(fmt.Sprintf(`{"id":"case-%d","prompt":"Do not call a tool.","tools":[{"name":"noop","parameters":{"type":"object"}}],"expected":%s}`, i+1, expected)))
	}
	m, err := NewLocalRunManifest("sealed.json", "input-hash", in, "groq/exact", "", "", limit, time.Minute, LocalPricing{Known: true}, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	return m, in
}

type recordingLocalExecutor struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (e *recordingLocalExecutor) ExecuteLocal(_ context.Context, _ LocalRunManifest, input json.RawMessage) (LocalCall, error) {
	e.mu.Lock()
	e.calls++
	n := e.calls
	e.mu.Unlock()
	if e.fail {
		return LocalCall{}, errors.New("provider interrupted")
	}
	return LocalCall{ServedModel: "groq/exact", Input: input, Output: json.RawMessage(`{"calls":[]}`), ObservedCostUSD: float64(n) / 100}, nil
}

func (e *recordingLocalExecutor) Count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func TestLocalRunStoreManifestFirstResumeAndFinalize(t *testing.T) {
	dir := t.TempDir()
	m, in := localStoreManifest(t, 2)
	store, err := OpenLocalRunStore(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatalf("manifest was not durable before calls: %v", err)
	}
	if err := store.AppendCall(LocalCall{CaseID: "1", RequestedModel: "groq/exact", ServedModel: "groq/exact", Input: in.Cases[0], Output: json.RawMessage(`{"calls":[]}`), ObservedCostUSD: .01, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenLocalRunStore(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	exec := &recordingLocalExecutor{}
	calls, err := RunStoredLocalEvaluation(context.Background(), m, in, exec, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || exec.Count() != 1 {
		t.Fatalf("resume calls=%d executor=%d, want 2 and 1", len(calls), exec.Count())
	}
	report, err := ScoreLocalCalls(m, calls)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Finalize(report, time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "partial" || math.Abs(receipt.ObservedCostUSD) > 1e-9 {
		t.Fatalf("receipt=%#v", receipt)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := VerifyLocalReceipt(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLocalRunStore(dir, m); err == nil {
		t.Fatal("completed receipt accepted for overwrite")
	}
}

func TestLocalRunStorePersistsFailedCallAndRejectsDifferentIdentity(t *testing.T) {
	dir := t.TempDir()
	m, in := localStoreManifest(t, 1)
	store, err := OpenLocalRunStore(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunStoredLocalEvaluation(context.Background(), m, in, &recordingLocalExecutor{fail: true}, store)
	if err == nil {
		t.Fatal("provider failure was lost")
	}
	if got := store.Calls(); len(got) != 1 || got[0].Error == "" {
		t.Fatalf("failed call not persisted: %#v", got)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	changed := m
	changed.Identity.RequestedModel = "groq/other"
	if _, err := OpenLocalRunStore(dir, changed); err == nil {
		t.Fatal("different identity resumed existing output")
	}
}

func TestLocalRunStoreOutputLock(t *testing.T) {
	dir := t.TempDir()
	m, _ := localStoreManifest(t, 1)
	first, err := OpenLocalRunStore(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := OpenLocalRunStore(dir, m); err == nil {
		t.Fatal("concurrent output owner acquired lock")
	}
}

func TestLocalRunStoreRefusesInDoubtRedispatch(t *testing.T) {
	dir := t.TempDir()
	m, in := localStoreManifest(t, 1)
	store, err := OpenLocalRunStore(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendIntent(LocalDispatchIntent{CaseID: "1", InputSHA256: localSHA(in.Cases[0]), At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenLocalRunStore(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exec := &recordingLocalExecutor{}
	if _, err := RunStoredLocalEvaluation(context.Background(), m, in, exec, store); err == nil || exec.Count() != 0 {
		t.Fatalf("in-doubt attempt redispatched: calls=%d err=%v", exec.Count(), err)
	}
}

func TestCampaignLedgerOwnedRecoveryConcurrencyAndUnknownCost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.json")
	ledger, err := OpenCampaignLedger(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	owner := CampaignReservationOwner{RunID: "short", RunIdentity: "full-identity", AttemptID: "attempt-1", OutputDir: "/runs/one"}
	first, err := ledger.Reserve(owner, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenCampaignLedger(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.Reserve(owner, 4, 4)
	if err != nil || recovered.At != first.At || len(reopened.Budget.Attempts) != 1 {
		t.Fatalf("recovery=%#v err=%v attempts=%d", recovered, err, len(reopened.Budget.Attempts))
	}
	otherOwner := owner
	otherOwner.AttemptID = "attempt-2"
	otherOwner.OutputDir = "/runs/two"
	if _, err := reopened.Reserve(otherOwner, 4, 4); err == nil {
		t.Fatal("duplicate concurrent identity admitted")
	}
	if err := reopened.RecordOwned(owner, nil, "partial"); err != nil {
		t.Fatal(err)
	}
	if math.Abs(reopened.Remaining()-6) > 1e-9 || reopened.Budget.Attempts[0].Status != campaignAttemptUnknownCost || reopened.Budget.Attempts[0].OutcomeStatus != "partial" {
		t.Fatalf("unknown cost did not retain reservation: %#v", reopened.Budget)
	}
	observed := 1.25
	if err := reopened.RecordOwned(owner, &observed, "partial"); err != nil {
		t.Fatal(err)
	}
	if math.Abs(reopened.Remaining()-8.75) > 1e-9 {
		t.Fatalf("known cost did not settle reservation: %.2f", reopened.Remaining())
	}
	if _, err := reopened.Reserve(otherOwner, 4, 4); err != nil {
		t.Fatal(err)
	}
	if err := reopened.RecordOwned(otherOwner, &observed, "partial"); err != nil {
		t.Fatal(err)
	}
	third := otherOwner
	third.AttemptID = "attempt-3"
	third.OutputDir = "/runs/three"
	if _, err := reopened.Reserve(third, 1, 1); err == nil {
		t.Fatal("third attempt for run identity admitted")
	}
}

func TestCampaignReservationOwnerIsStablePerOutputDirectory(t *testing.T) {
	first, err := NewCampaignReservationOwner("run", "identity", "relative-output")
	if err != nil {
		t.Fatal(err)
	}
	again, err := NewCampaignReservationOwner("run", "identity", "relative-output")
	if err != nil || first != again {
		t.Fatalf("stable owner first=%#v again=%#v err=%v", first, again, err)
	}
	other, err := NewCampaignReservationOwner("run", "identity", "another-output")
	if err != nil {
		t.Fatal(err)
	}
	if first.AttemptID == other.AttemptID || first.OutputDir == other.OutputDir {
		t.Fatalf("distinct output directory reused owner: %#v %#v", first, other)
	}
}
