package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// RunStoredLocalEvaluation resumes only a matching, determinate sealed attempt.
func RunStoredLocalEvaluation(ctx context.Context, manifest LocalRunManifest, in LocalInputManifest, exec LocalExecutor, store *LocalRunStore) ([]LocalCall, error) {
	if exec == nil || store == nil {
		return nil, errors.New("local executor and run store are required")
	}
	if LocalRunIdentityHash(manifest) != LocalRunIdentityHash(store.manifest) {
		return nil, errors.New("run store does not belong to manifest identity")
	}
	if _, err := PlanLocalRun(manifest, in); err != nil {
		return nil, err
	}
	tracker := NewIdentityTracker(manifest.Identity.RequestedModel)
	if err := store.validateStoredLocalCalls(manifest, in, tracker); err != nil {
		return store.Calls(), err
	}
	if err := store.executeMissingLocalCases(ctx, manifest, in, exec, tracker); err != nil {
		return store.Calls(), err
	}
	return store.Calls(), nil
}

func (store *LocalRunStore) validateStoredLocalCalls(manifest LocalRunManifest, in LocalInputManifest, tracker *IdentityTracker) error {
	for _, call := range store.Calls() {
		caseIndex, _ := strconv.Atoi(call.CaseID)
		if call.RequestedModel != manifest.Identity.RequestedModel {
			return fmt.Errorf("stored case %s has a different requested model", call.CaseID)
		}
		if !bytes.Equal(call.Input, in.Cases[caseIndex-1]) {
			return fmt.Errorf("stored case %s does not match sealed input", call.CaseID)
		}
		if call.Error == "" {
			if err := tracker.Record(call.ServedModel); err != nil {
				return err
			}
		}
	}
	return nil
}

func (store *LocalRunStore) executeMissingLocalCases(ctx context.Context, manifest LocalRunManifest, in LocalInputManifest, exec LocalExecutor, tracker *IdentityTracker) error {
	for i := 0; i < manifest.Identity.Limit; i++ {
		caseID := strconv.Itoa(i + 1)
		if _, exists := store.byCase[caseID]; exists {
			continue
		}
		if _, inDoubt := store.intentByCase[caseID]; inDoubt {
			if store.intentByCase[caseID].InputSHA256 != localSHA(in.Cases[i]) {
				return fmt.Errorf("case %s dispatch intent does not match sealed input", caseID)
			}
			return fmt.Errorf("case %s has a dispatch intent without a durable result; attempt is in doubt and will not be retried", caseID)
		}
		if err := store.executeStoredLocalCase(ctx, manifest, localCaseDispatch{caseID: caseID, raw: in.Cases[i]}, exec, tracker); err != nil {
			return err
		}
	}
	return nil
}

type localCaseDispatch struct {
	caseID string
	raw    json.RawMessage
}

func (store *LocalRunStore) executeStoredLocalCase(ctx context.Context, manifest LocalRunManifest, dispatch localCaseDispatch, exec LocalExecutor, tracker *IdentityTracker) error {
	if err := store.AppendIntent(LocalDispatchIntent{CaseID: dispatch.caseID, InputSHA256: localSHA(dispatch.raw), At: time.Now().UTC()}); err != nil {
		return err
	}
	call, err := exec.ExecuteLocal(ctx, manifest, dispatch.raw)
	call.CaseID = dispatch.caseID
	call.RequestedModel = manifest.Identity.RequestedModel
	call.Input = append(json.RawMessage(nil), dispatch.raw...)
	call.At = time.Now().UTC()
	if priceErr := ApplyLocalCallPricing(manifest.Identity.Pricing, &call); priceErr != nil {
		return priceErr
	}
	return store.admitExecutedLocalCall(call, err, tracker)
}

func (store *LocalRunStore) admitExecutedLocalCall(call LocalCall, executionErr error, tracker *IdentityTracker) error {
	if executionErr != nil {
		return store.appendFailedLocalCall(call, executionErr)
	}
	if call.Error == "" {
		if err := tracker.Record(call.ServedModel); err != nil {
			return store.appendFailedLocalCall(call, err)
		}
	}
	return store.AppendCall(call)
}

func (store *LocalRunStore) appendFailedLocalCall(call LocalCall, cause error) error {
	call.Error = cause.Error()
	if appendErr := store.AppendCall(call); appendErr != nil {
		return errors.Join(cause, appendErr)
	}
	return cause
}
