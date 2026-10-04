package catalog

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dotcommander/llambo/internal/costs"
)

func PriceLabel(status CostStatus, input, output float64) string {
	switch status {
	case CostFree:
		return "free"
	case CostPaid:
		return fmt.Sprintf("paid $%.4g/$%.4g per 1M", input, output)
	default:
		return "unknown"
	}
}

func CostForModel(costMap map[string]costs.ModelCost, providerName, modelID string) (CostStatus, float64, float64) {
	return modelCostStatus(costMap, providerName, modelID)
}

func CostForEntry(costMap map[string]costs.ModelCost, providerName, modelID string, entry *ModelEntry) (CostStatus, float64, float64) {
	return modelCostStatusForEntry(costMap, providerName, modelID, entry)
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

func costAllowed(status CostStatus, output float64, opts SelectorOptions) bool {
	if opts.FreeOnly && status != CostFree {
		return false
	}
	return CostWithinCap(status, output, opts.MaxOutputCost, opts.IncludeUnknownCost)
}

func modelCostStatus(costMap map[string]costs.ModelCost, providerName, modelID string) (CostStatus, float64, float64) {
	return modelCostStatusForEntry(costMap, providerName, modelID, nil)
}

func modelCostStatusForEntry(costMap map[string]costs.ModelCost, providerName, modelID string, entry *ModelEntry) (CostStatus, float64, float64) {
	mc, ok := costMap[costs.Key(providerName, modelID)]
	if ok && mc.InputExplicit && mc.OutputExplicit && validPrice(mc.InputPer1M) && validPrice(mc.OutputPer1M) {
		if mc.InputPer1M == 0 && mc.OutputPer1M == 0 {
			return CostFree, mc.InputPer1M, mc.OutputPer1M
		}
		return CostPaid, mc.InputPer1M, mc.OutputPer1M
	}
	if status, input, output, ok := metadataCostStatus(entry); ok {
		return status, input, output
	}
	return CostUnknown, 0, 0
}

func metadataCostStatus(entry *ModelEntry) (CostStatus, float64, float64, bool) {
	if entry == nil {
		return CostUnknown, 0, 0, false
	}
	input, inputOK := pricePer1M(entry.Metadata.Pricing.Prompt)
	output, outputOK := pricePer1M(entry.Metadata.Pricing.Completion)
	if !inputOK || !outputOK {
		return CostUnknown, 0, 0, false
	}
	if input == 0 && output == 0 {
		return CostFree, input, output, true
	}
	return CostPaid, input, output, true
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

func isFree(costMap map[string]costs.ModelCost, providerName, modelID string) bool {
	status, _, _ := modelCostStatus(costMap, providerName, modelID)
	return status == CostFree
}

func validPrice(price float64) bool { return price >= 0 && !math.IsNaN(price) && !math.IsInf(price, 0) }
