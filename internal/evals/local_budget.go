package evals

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	campaignAttemptAdmitted    = "admitted"
	campaignAttemptUnknownCost = "unknown_cost"
)

// CampaignAttempt is an immutable admission record except for its observed
// cost and terminal status. AttemptID and OutputDir make the reservation
// recoverable by precisely the process that owns an output directory.
type CampaignAttempt struct {
	RunID            string    `json:"run_id"`
	RunIdentity      string    `json:"run_identity,omitempty"`
	AttemptID        string    `json:"attempt_id,omitempty"`
	OutputDir        string    `json:"output_dir,omitempty"`
	WorstCaseCostUSD float64   `json:"worst_case_cost_usd"`
	ObservedCostUSD  float64   `json:"observed_cost_usd"`
	Status           string    `json:"status"`
	OutcomeStatus    string    `json:"outcome_status,omitempty"`
	At               time.Time `json:"at"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
}

type CampaignBudget struct {
	SchemaVersion   int               `json:"schema_version"`
	MaxCostUSD      float64           `json:"max_cost_usd"`
	ObservedCostUSD float64           `json:"observed_cost_usd"`
	ReservedCostUSD float64           `json:"reserved_cost_usd"`
	Attempts        []CampaignAttempt `json:"attempts"`
}

// CampaignReservationOwner is stable across a restarted process. RunIdentity
// should be a full input/config/model identity hash; RunID remains present for
// concise reporting and compatibility with older ledgers.
type CampaignReservationOwner struct {
	RunID       string
	RunIdentity string
	AttemptID   string
	OutputDir   string
}

// NewCampaignReservationOwner derives a restart-stable attempt id from an
// exact run identity and the canonical output directory. A fresh directory is
// therefore a fresh attempt, while reopening the same directory recovers its
// reservation without allowing a duplicate concurrent authorization.
func NewCampaignReservationOwner(runID, runIdentity, outputDir string) (CampaignReservationOwner, error) {
	if strings.TrimSpace(outputDir) == "" {
		return CampaignReservationOwner{}, fmt.Errorf("output directory is required")
	}
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		return CampaignReservationOwner{}, fmt.Errorf("canonicalize output directory: %w", err)
	}
	abs = filepath.Clean(abs)
	owner := CampaignReservationOwner{RunID: runID, RunIdentity: runIdentity, OutputDir: abs}
	owner.AttemptID = localSHA([]byte(runIdentity + "\x00" + abs))[:16]
	if err := owner.validate(); err != nil {
		return CampaignReservationOwner{}, err
	}
	return owner, nil
}

func (o CampaignReservationOwner) validate() error {
	if strings.TrimSpace(o.RunID) == "" || strings.TrimSpace(o.RunIdentity) == "" || strings.TrimSpace(o.AttemptID) == "" || strings.TrimSpace(o.OutputDir) == "" {
		return fmt.Errorf("run id, run identity, attempt id, and output directory are required")
	}
	return nil
}

func (o CampaignReservationOwner) matches(a CampaignAttempt) bool {
	return a.RunID == o.RunID && a.RunIdentity == o.RunIdentity && a.AttemptID == o.AttemptID && a.OutputDir == o.OutputDir
}

func (o CampaignReservationOwner) sameIdentity(a CampaignAttempt) bool {
	return a.RunIdentity == o.RunIdentity
}

type CampaignLedger struct {
	path string
	mu   sync.Mutex
	// Budget is refreshed under the cross-process lock before every mutation.
	Budget CampaignBudget
}

func OpenCampaignLedger(path string, maxCost float64) (*CampaignLedger, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("campaign ledger path is required")
	}
	l := &CampaignLedger{path: path}
	unlock, err := lockCampaignPath(path)
	if err != nil {
		return nil, err
	}
	defer unlock()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if !validCost(maxCost) || maxCost <= 0 {
			return nil, fmt.Errorf("positive campaign maximum is required")
		}
		l.Budget = CampaignBudget{SchemaVersion: 2, MaxCostUSD: maxCost}
		if err := l.save(); err != nil {
			return nil, err
		}
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &l.Budget); err != nil {
		return nil, fmt.Errorf("parse campaign ledger: %w", err)
	}
	if !validCost(l.Budget.MaxCostUSD) || l.Budget.MaxCostUSD <= 0 {
		return nil, fmt.Errorf("invalid campaign budget")
	}
	if maxCost > 0 && math.Abs(maxCost-l.Budget.MaxCostUSD) > 1e-12 {
		return nil, fmt.Errorf("campaign maximum differs from existing ledger")
	}
	return l, nil
}

func (l *CampaignLedger) Remaining() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	// This is advisory. Admission always reloads under the process lock.
	return l.Budget.MaxCostUSD - l.Budget.ObservedCostUSD - l.Budget.ReservedCostUSD
}

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
	if err := owner.validate(); err != nil {
		return CampaignAttempt{}, err
	}
	owner.OutputDir = filepath.Clean(owner.OutputDir)
	if !validCost(worstCase) || worstCase < 0 || !validCost(maxRun) || maxRun <= 0 {
		return CampaignAttempt{}, fmt.Errorf("finite non-negative worst-case and positive run budget are required")
	}
	if worstCase > maxRun+1e-12 {
		return CampaignAttempt{}, fmt.Errorf("worst-case run estimate $%.6f exceeds --max-run-cost $%.6f", worstCase, maxRun)
	}
	for _, attempt := range l.Budget.Attempts {
		if owner.matches(attempt) {
			if attempt.WorstCaseCostUSD != worstCase {
				return CampaignAttempt{}, fmt.Errorf("recovered reservation worst-case differs from recorded attempt")
			}
			if campaignAttemptHoldsReservation(attempt) {
				return attempt, nil
			}
			return CampaignAttempt{}, fmt.Errorf("attempt %q is already finalized", owner.AttemptID)
		}
	}
	for _, attempt := range l.Budget.Attempts {
		if owner.sameIdentity(attempt) && campaignAttemptHoldsReservation(attempt) {
			return CampaignAttempt{}, fmt.Errorf("run identity already has an active reservation in %s", attempt.OutputDir)
		}
	}
	count := 0
	for _, attempt := range l.Budget.Attempts {
		if owner.sameIdentity(attempt) {
			count++
		}
	}
	if count >= 2 {
		return CampaignAttempt{}, fmt.Errorf("maximum two attempts already recorded for run identity %s", owner.RunIdentity)
	}
	remaining := l.Budget.MaxCostUSD - l.Budget.ObservedCostUSD - l.Budget.ReservedCostUSD
	if worstCase > remaining+1e-12 {
		return CampaignAttempt{}, fmt.Errorf("worst-case run estimate $%.6f exceeds campaign remaining $%.6f", worstCase, remaining)
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

// RecordOwned durably closes an owned reservation when observed is known. A
// nil observed cost records an unknown-cost outcome and deliberately retains
// its worst-case reservation, so a provider accounting gap cannot free budget
// for another paid call.
func (l *CampaignLedger) RecordOwned(owner CampaignReservationOwner, observed *float64, status string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	unlock, err := l.lockAndReload()
	if err != nil {
		return err
	}
	defer unlock()
	if err := owner.validate(); err != nil {
		return err
	}
	owner.OutputDir = filepath.Clean(owner.OutputDir)
	if strings.TrimSpace(status) == "" {
		return fmt.Errorf("attempt status is required")
	}
	if observed != nil && (!validCost(*observed) || *observed < 0) {
		return fmt.Errorf("observed cost must be finite and non-negative")
	}
	for i := range l.Budget.Attempts {
		attempt := &l.Budget.Attempts[i]
		if !owner.matches(*attempt) {
			continue
		}
		if !campaignAttemptHoldsReservation(*attempt) {
			return fmt.Errorf("attempt %q has no active reservation", owner.AttemptID)
		}
		attempt.UpdatedAt = time.Now().UTC()
		if observed == nil {
			attempt.Status = campaignAttemptUnknownCost
			attempt.OutcomeStatus = status
			return l.save()
		}
		l.Budget.ReservedCostUSD -= attempt.WorstCaseCostUSD
		if l.Budget.ReservedCostUSD < 0 && l.Budget.ReservedCostUSD > -1e-9 {
			l.Budget.ReservedCostUSD = 0
		}
		attempt.ObservedCostUSD = *observed
		attempt.Status = status
		attempt.OutcomeStatus = ""
		l.Budget.ObservedCostUSD += *observed
		return l.save()
	}
	return fmt.Errorf("no owned reservation for attempt %q", owner.AttemptID)
}

func campaignAttemptHoldsReservation(a CampaignAttempt) bool {
	return a.Status == campaignAttemptAdmitted || a.Status == campaignAttemptUnknownCost
}

// Admit and Record retain the original command-facing API. New callers must
// use Reserve and RecordOwned, which bind admission to an output directory and
// stable attempt id.
func (l *CampaignLedger) Admit(runID string, worstCase, maxRun float64) error {
	_, err := l.Reserve(CampaignReservationOwner{RunID: runID, RunIdentity: runID, AttemptID: "legacy-" + runID, OutputDir: "legacy-" + runID}, worstCase, maxRun)
	return err
}

func (l *CampaignLedger) Record(runID string, observed float64, status string) error {
	return l.RecordOwned(CampaignReservationOwner{RunID: runID, RunIdentity: runID, AttemptID: "legacy-" + runID, OutputDir: "legacy-" + runID}, &observed, status)
}

func (l *CampaignLedger) lockAndReload() (func(), error) {
	unlock, err := lockCampaignPath(l.path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		unlock()
		return nil, err
	}
	if err := json.Unmarshal(data, &l.Budget); err != nil {
		unlock()
		return nil, fmt.Errorf("reload campaign ledger: %w", err)
	}
	return unlock, nil
}

func lockCampaignPath(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}, nil
}

func (l *CampaignLedger) save() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(l.Budget, "", "  ")
	if err != nil {
		return err
	}
	return writeDurableLocalFile(l.path, append(b, '\n'), 0o600)
}

func validCost(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
