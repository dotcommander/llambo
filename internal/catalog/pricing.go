package catalog

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dotcommander/llambo/internal/costs"
)

// Repeated string literals consolidated by goconst.
const (
	free1 = "free"
)

// costQuote is a resolved pricing classification with per-1M token rates.
// known distinguishes a resolved quote from an unknown-cost fallback.
type costQuote struct {
	status      CostStatus
	inputPer1M  float64
	outputPer1M float64
	known       bool
}

// PriceLabel renders a human-readable price description for a model.
func PriceLabel(status CostStatus, input, output float64) string {
	switch status {
	case CostFree:
		return free1
	case CostPaid:
		return fmt.Sprintf("paid $%.4g/$%.4g per 1M", input, output)
	case CostUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// CostForModel returns the pricing status and per-million rates for a model.
func CostForModel(costMap map[string]costs.ModelCost, providerName, modelID string) (status CostStatus, inputPer1M, outputPer1M float64) {
	return modelCostStatusForEntry(costMap, providerName, modelID, nil).scalars()
}

// CostForEntry is CostForModel honoring a catalog entry's pinned pricing.
func CostForEntry(costMap map[string]costs.ModelCost, providerName, modelID string, entry *ModelEntry) (status CostStatus, inputPer1M, outputPer1M float64) {
	return modelCostStatusForEntry(costMap, providerName, modelID, entry).scalars()
}

// scalars unpacks the quote for the exported scalar-returning API.
func (q costQuote) scalars() (status CostStatus, inputPer1M, outputPer1M float64) {
	return q.status, q.inputPer1M, q.outputPer1M
}

// CostWithinCap reports whether a model's output cost is within maxOutputCost
// (USD per 1M tokens). maxOutputCost<=0 disables the cap. Unknown cost passes
// only when includeUnknown is true.
func CostWithinCap(status CostStatus, output, maxOutputCost float64, includeUnknown bool) bool {
	if maxOutputCost <= 0 {
		return true
	}
	if status == CostUnknown {
		return includeUnknown
	}
	return !(status == CostPaid && output > maxOutputCost)
}

// costAllowed applies the selector's free-only and output-cost budget bounds.
func costAllowed(quote costQuote, opts SelectorOptions) bool {
	if opts.FreeOnly && quote.status != CostFree {
		return false
	}
	return CostWithinCap(quote.status, quote.outputPer1M, opts.MaxOutputCost, opts.IncludeUnknownCost)
}

// modelCostStatusForEntry resolves pricing from the explicit cost map first,
// then from catalog metadata pricing.
func modelCostStatusForEntry(costMap map[string]costs.ModelCost, providerName, modelID string, entry *ModelEntry) costQuote {
	mc, known := costMap[costs.Key(providerName, modelID)]
	if quote, ok := explicitCostQuote(mc, known); ok {
		return quote
	}
	if quote := metadataCostQuote(entry); quote.known {
		return quote
	}
	return costQuote{status: CostUnknown}
}

// explicitCostQuote converts a cost-map row when both sides are explicitly and
// validly priced.
func explicitCostQuote(mc costs.ModelCost, ok bool) (costQuote, bool) {
	if !ok || !mc.InputExplicit || !mc.OutputExplicit || !validPrice(mc.InputPer1M) || !validPrice(mc.OutputPer1M) {
		return costQuote{}, false
	}
	status := CostPaid
	if mc.InputPer1M == 0 && mc.OutputPer1M == 0 {
		status = CostFree
	}
	return costQuote{status: status, inputPer1M: mc.InputPer1M, outputPer1M: mc.OutputPer1M, known: true}, true
}

// metadataCostQuote resolves pricing from catalog metadata price labels.
func metadataCostQuote(entry *ModelEntry) costQuote {
	if entry == nil {
		return costQuote{}
	}
	input, inputOK := pricePer1M(entry.Metadata.Pricing.Prompt)
	output, outputOK := pricePer1M(entry.Metadata.Pricing.Completion)
	if !inputOK || !outputOK {
		return costQuote{}
	}
	status := CostPaid
	if input == 0 && output == 0 {
		status = CostFree
	}
	return costQuote{status: status, inputPer1M: input, outputPer1M: output, known: true}
}

func pricePer1M(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	price, err := strconv.ParseFloat(raw, 64)
	if err != nil || !validPrice(price) || !validPrice(price*1_000_000) {
		return 0, false
	}
	return price * 1_000_000, true
}

func validPrice(price float64) bool { return price >= 0 && !math.IsNaN(price) && !math.IsInf(price, 0) }
