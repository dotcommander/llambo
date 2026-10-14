package evals

import (
	"errors"
	"fmt"
	"sync"
)

type writingBudget struct {
	mu       sync.Mutex
	max      float64
	spent    float64
	reserved float64
}

func newWritingBudget(max float64, store *WritingRunStore) *writingBudget {
	generations, judgments := store.Records()
	spent := 0.0
	for _, record := range generations {
		spent += writingPersistedAccountingCost(record.AccountingCostUSD, record.Usage)
	}
	for _, record := range judgments {
		spent += writingPersistedAccountingCost(record.AccountingCostUSD, record.Usage)
	}
	return &writingBudget{max: max, spent: spent}
}

func (b *writingBudget) reserve(estimate float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.max <= 0 {
		return errors.New("writing run cost limit must be positive")
	}
	if b.spent+b.reserved+estimate > b.max+1e-12 {
		return fmt.Errorf("writing run cost gate: estimated admission $%.6f would exceed $%.6f limit", b.spent+b.reserved+estimate, b.max)
	}
	b.reserved += estimate
	return nil
}

func (b *writingBudget) complete(estimate, actual float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reserved -= estimate
	if b.reserved < 0 {
		b.reserved = 0
	}
	b.spent += actual
}

func writingAccountingCost(usage WritingUsage, estimate float64) float64 {
	if usage.Known && (usage.CostKnown || (usage.CacheReadTokens == 0 && usage.CacheWriteTokens == 0)) {
		return usage.CostUSD
	}
	return estimate
}

func writingPersistedAccountingCost(accountingCost float64, usage WritingUsage) float64 {
	if accountingCost > 0 {
		return accountingCost
	}
	return usage.CostUSD
}
