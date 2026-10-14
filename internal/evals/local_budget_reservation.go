package evals

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// Reserve atomically admits one uniquely owned attempt. Repeating the exact
// owner is crash recovery, not a second admission. A different owner cannot
// reserve the same run identity while an earlier reservation remains active.
func (l *CampaignLedger) Reserve(owner CampaignReservationOwner, worstCase, maxRun float64) (CampaignAttempt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	unlock, err := l.lockAndReload()
	if err != nil {
		return CampaignAttempt{}, err
	}
	defer unlock()
	if validationErr := owner.validate(); validationErr != nil {
		return CampaignAttempt{}, validationErr
	}
	owner.OutputDir = filepath.Clean(owner.OutputDir)
	if !validCost(worstCase) || worstCase < 0 || !validCost(maxRun) || maxRun <= 0 {
		return CampaignAttempt{}, errors.New("finite non-negative worst-case and positive run budget are required")
	}
	if worstCase > maxRun+1e-12 {
		return CampaignAttempt{}, fmt.Errorf("worst-case run estimate $%.6f exceeds --max-run-cost $%.6f", worstCase, maxRun)
	}
	recovered, err := recoverCampaignAttempt(l.Budget.Attempts, owner, worstCase)
	if err != nil {
		return CampaignAttempt{}, err
	}
	if recovered != nil {
		return *recovered, nil
	}
	if err := l.checkCampaignAdmission(owner, worstCase); err != nil {
		return CampaignAttempt{}, err
	}
	now := time.Now().UTC()
	attempt := CampaignAttempt{RunID: owner.RunID, RunIdentity: owner.RunIdentity, AttemptID: owner.AttemptID, OutputDir: owner.OutputDir, WorstCaseCostUSD: worstCase, Status: campaignAttemptAdmitted, At: now, UpdatedAt: now}
	l.Budget.Attempts = append(l.Budget.Attempts, attempt)
	l.Budget.ReservedCostUSD += worstCase
	if err := l.save(); err != nil {
		return CampaignAttempt{}, err
	}
	return attempt, nil
}

func recoverCampaignAttempt(attempts []CampaignAttempt, owner CampaignReservationOwner, worstCase float64) (*CampaignAttempt, error) {
	for _, attempt := range attempts {
		if owner.matches(attempt) {
			if attempt.WorstCaseCostUSD != worstCase {
				return nil, errors.New("recovered reservation worst-case differs from recorded attempt")
			}
			if campaignAttemptHoldsReservation(attempt) {
				return &attempt, nil
			}
			return nil, fmt.Errorf("attempt %q is already finalized", owner.AttemptID)
		}
	}
	return nil, nil
}

func (l *CampaignLedger) checkCampaignAdmission(owner CampaignReservationOwner, worstCase float64) error {
	for _, attempt := range l.Budget.Attempts {
		if owner.sameIdentity(attempt) && campaignAttemptHoldsReservation(attempt) {
			return fmt.Errorf("run identity already has an active reservation in %s", attempt.OutputDir)
		}
	}
	count := 0
	for _, attempt := range l.Budget.Attempts {
		if owner.sameIdentity(attempt) {
			count++
		}
	}
	if count >= 2 {
		return fmt.Errorf("maximum two attempts already recorded for run identity %s", owner.RunIdentity)
	}
	remaining := l.Budget.MaxCostUSD - l.Budget.ObservedCostUSD - l.Budget.ReservedCostUSD
	if worstCase > remaining+1e-12 {
		return fmt.Errorf("worst-case run estimate $%.6f exceeds campaign remaining $%.6f", worstCase, remaining)
	}
	return nil
}
