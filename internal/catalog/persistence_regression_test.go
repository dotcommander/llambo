package catalog

import (
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
	"math"
	"reflect"
	"testing"
)

func TestGeneralCandidatesUnionConfiguredExactIDs(t *testing.T) {
	t.Parallel()
	cat := &Catalog{Providers: map[string]*ProviderCatalog{"p": {Models: map[string]*ModelEntry{"remote": {}, "Case/Model": {}}}}}
	cfg := providers.Config{Model: "Case/Model", Models: []string{"case/model", "configured"}}
	got := candidateModels(cat, "p", cfg, "all", "all")
	want := []string{"Case/Model", "case/model", "configured", "remote"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates=%v want=%v", got, want)
	}
}

func TestInvalidPriceCannotBeFreeOrPaid(t *testing.T) {
	t.Parallel()
	for _, value := range []float64{-1, math.NaN(), math.Inf(1)} {
		cm := map[string]costs.ModelCost{"p:m": {InputPer1M: value, OutputPer1M: 0, InputExplicit: true, OutputExplicit: true}}
		if status, _, _ := CostForModel(cm, "p", "m"); status != CostUnknown {
			t.Fatalf("invalid price status=%s", status)
		}
	}
	for _, raw := range []string{"NaN", "Inf", "-1", "1e999"} {
		if _, ok := pricePer1M(raw); ok {
			t.Fatalf("invalid metadata accepted: %s", raw)
		}
	}
}

func TestFiniteNonnegativeMetadataPricesRemainValid(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"0", "0.01", "1e300"} {
		if _, ok := pricePer1M(raw); !ok {
			t.Fatalf("finite nonnegative price rejected: %s", raw)
		}
	}
}
