package evals

import (
	"errors"
	"os"
	"testing"
)

func TestLocalLedgerAppendFailureDoesNotAdmitRecord(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		intent bool
	}{{name: "intent", intent: true}, {name: "call"}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			file, err := os.CreateTemp(t.TempDir(), "closed-ledger-*")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = file.Close() })
			if closeErr := file.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			store := newAppendBoundaryStore()
			if test.intent {
				store.intents = file
				err = store.AppendIntent(LocalDispatchIntent{CaseID: "1"})
			} else {
				store.calls = file
				err = store.AppendCall(LocalCall{CaseID: "1"})
			}
			if !errors.Is(err, os.ErrClosed) {
				t.Fatalf("closed ledger append = %v", err)
			}
			assertNoLocalRecordAdmitted(t, store)
		})
	}
}

func TestLocalLedgerSyncFailureDoesNotAdmitRecord(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		intent bool
	}{{name: "intent", intent: true}, {name: "call"}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
			store := newAppendBoundaryStore()
			if test.intent {
				store.intents = writer
				err = store.AppendIntent(LocalDispatchIntent{CaseID: "1"})
			} else {
				store.calls = writer
				err = store.AppendCall(LocalCall{CaseID: "1"})
			}
			if err == nil {
				t.Fatal("unsyncable ledger admitted a record")
			}
			assertNoLocalRecordAdmitted(t, store)
		})
	}
}

func newAppendBoundaryStore() *LocalRunStore {
	return &LocalRunStore{
		manifest:     LocalRunManifest{Identity: LocalRunIdentity{Limit: 1}},
		byCase:       make(map[string]LocalCall),
		intentByCase: make(map[string]LocalDispatchIntent),
	}
}

func assertNoLocalRecordAdmitted(t *testing.T, store *LocalRunStore) {
	t.Helper()
	if len(store.byCase) != 0 || len(store.intentByCase) != 0 {
		t.Fatal("failed durable append changed the in-memory index")
	}
}
