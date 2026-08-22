package evals

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestLoadSourceResearchLedger(t *testing.T) {
	t.Parallel()
	ledger, err := LoadSourceResearchLedger()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(ledger.ScopedNulls), 66; got != want {
		t.Fatalf("scoped-null receipts=%d want %d", got, want)
	}
	if got, want := len(ledger.Sources), 3; got != want {
		t.Fatalf("sources=%d want %d", got, want)
	}
	if got := ledger.Sources[2]; got.ID != "writingbench-score-xlsx" || got.ArtifactKind != "normalized-identity-snapshot" || got.Verification != "sha256-verified" || got.Status != "normalized-frozen" {
		t.Fatalf("WritingBench source metadata=%#v", got)
	}
	digest, err := SourceResearchLedgerDigest(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if digest != sourceResearchLedgerDigest {
		t.Fatalf("canonical digest=%s want %s", digest, sourceResearchLedgerDigest)
	}
}

func TestDecodeSourceResearchLedgerRejectsMalformedData(t *testing.T) {
	t.Parallel()
	manifest, err := loadCoverageCampaignManifest()
	if err != nil {
		t.Fatal(err)
	}
	valid := string(sourceResearchLedgerJSON)
	tests := []struct {
		name string
		data string
		want string
	}{
		{"unknown field", strings.Replace(valid, `"schema_version": "llambo-source-ledger-v1",`, `"schema_version": "llambo-source-ledger-v1", "unexpected": true,`, 1), "unknown field"},
		{"invalid hash", strings.Replace(valid, "74d5049896f5c8de4c01d22d15357e5451209d215b7e6410b9169dc6cb30ffed", "not-a-sha", 1), "incomplete or invalid"},
		{"duplicate source", strings.Replace(valid, `"id": "lfm2.5-8b-a1b-card"`, `"id": "llambo-2-reference"`, 1), "duplicate source"},
		{"missing source reference", strings.Replace(valid, "\"supporting_source_ids\": [\n        \"lfm2.5-8b-a1b-card\"", "\"supporting_source_ids\": [\n        \"missing-source\"", 1), "unknown supporting source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeSourceResearchLedger([]byte(tt.data), manifest); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v want substring %q", err, tt.want)
			}
		})
	}
}

func TestSourceResearchLedgerRejectsReceiptCoverageMismatch(t *testing.T) {
	t.Parallel()
	manifest, ledger := testSourceResearchLedger(t)
	ledger.ScopedNulls = ledger.ScopedNulls[1:]
	if err := ValidateSourceResearchLedger(&ledger, manifest); err == nil || !strings.Contains(err.Error(), "scoped-null receipts") {
		t.Fatalf("missing receipt error=%v", err)
	}

	_, ledger = testSourceResearchLedger(t)
	ledger.ScopedNulls = append(ledger.ScopedNulls, ledger.ScopedNulls[0])
	if err := ValidateSourceResearchLedger(&ledger, manifest); err == nil || !strings.Contains(err.Error(), "scoped-null receipts") {
		t.Fatalf("extra receipt error=%v", err)
	}
}

func TestSourceResearchLedgerRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	manifest, ledger := testSourceResearchLedger(t)
	ledger.Claims[0].Confidence = math.NaN()
	if err := ValidateSourceResearchLedger(&ledger, manifest); err == nil || !strings.Contains(err.Error(), "incomplete or invalid") {
		t.Fatalf("non-finite confidence error=%v", err)
	}

	_, ledger = testSourceResearchLedger(t)
	ledger.Sources[0].Owner = "sk-this-is-not-a-source"
	if err := ValidateSourceResearchLedger(&ledger, manifest); err == nil || !strings.Contains(err.Error(), "incomplete or invalid") {
		t.Fatalf("secret-like source error=%v", err)
	}

	_, ledger = testSourceResearchLedger(t)
	for i := range ledger.Sources {
		if ledger.Sources[i].ID == "llambo-2-reference" {
			ledger.Sources[i].Verification = "metadata-only"
		}
	}
	if err := ValidateSourceResearchLedger(&ledger, manifest); err == nil || !strings.Contains(err.Error(), "incomplete or invalid") {
		t.Fatalf("invalid artifact verification error=%v", err)
	}

	_, ledger = testSourceResearchLedger(t)
	ledger.ScopedNulls[0].Category = "invented"
	if err := ValidateSourceResearchLedger(&ledger, manifest); err == nil || !strings.Contains(err.Error(), "not a non-present campaign cell") {
		t.Fatalf("invalid category error=%v", err)
	}
}

func TestSourceResearchLedgerCanonicalNormalization(t *testing.T) {
	t.Parallel()
	_, ledger := testSourceResearchLedger(t)
	first, err := CanonicalSourceResearchLedger(ledger)
	if err != nil {
		t.Fatal(err)
	}
	ledger.Sources[0], ledger.Sources[1] = ledger.Sources[1], ledger.Sources[0]
	ledger.Searches[0], ledger.Searches[len(ledger.Searches)-1] = ledger.Searches[len(ledger.Searches)-1], ledger.Searches[0]
	ledger.ScopedNulls[0], ledger.ScopedNulls[len(ledger.ScopedNulls)-1] = ledger.ScopedNulls[len(ledger.ScopedNulls)-1], ledger.ScopedNulls[0]
	second, err := CanonicalSourceResearchLedger(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("canonical normalization drifted\nfirst=%s\nsecond=%s", first, second)
	}
}

func TestSourceResearchLedgerDerivesExactRuntimeUnresolvedCells(t *testing.T) {
	t.Parallel()
	manifest, ledger := testSourceResearchLedger(t)
	allUnresolved, err := buildCoverageCampaign(nil)
	if err != nil {
		t.Fatal(err)
	}
	active, err := DeriveActiveScopedNullReceipts(ledger, *allUnresolved)
	if err != nil || len(active) != 66 {
		t.Fatalf("all active receipts=%d err=%v", len(active), err)
	}
	if err := ValidateActiveScopedNullReceipts(active, *allUnresolved); err != nil {
		t.Fatal(err)
	}

	// A runtime-present score removes one active receipt. Keeping historical
	// receipts in the active list is rejected as an extra, not silently ignored.
	rows := []ReportModel{{Key: manifest.Targets[0].Key, LlamboScores: map[string]*LlamboScore{"reasoning": {Score: 50}}}}
	withPresent, err := buildCoverageCampaign(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateActiveScopedNullReceipts(active, *withPresent); err == nil {
		t.Fatal("historical receipt for runtime-present cell accepted as active")
	}
	activeWithPresent, err := DeriveActiveScopedNullReceipts(ledger, *withPresent)
	if err != nil || len(activeWithPresent) != 65 {
		t.Fatalf("active after runtime-present score=%d err=%v", len(activeWithPresent), err)
	}
	if err := ValidateActiveScopedNullReceipts(activeWithPresent, *withPresent); err != nil {
		t.Fatal(err)
	}

	// If that score becomes missing again, the old active list is now incomplete.
	if err := ValidateActiveScopedNullReceipts(activeWithPresent, *allUnresolved); err == nil {
		t.Fatal("active list missing newly unresolved cell accepted")
	}
	ledger.ScopedNulls = ledger.ScopedNulls[1:]
	if _, err := DeriveActiveScopedNullReceipts(ledger, *allUnresolved); err == nil {
		t.Fatal("missing runtime unresolved receipt accepted")
	}
}

func TestSourceResearchLedgerDerivesWritingBlocksFromIdentitySnapshot(t *testing.T) {
	t.Parallel()
	_, ledger := testSourceResearchLedger(t)
	campaign, err := buildCoverageCampaign(nil)
	if err != nil {
		t.Fatal(err)
	}
	active, err := DeriveActiveScopedNullReceipts(ledger, *campaign)
	if err != nil {
		t.Fatal(err)
	}
	writing := 0
	for _, receipt := range active {
		if receipt.Category != "writing" {
			continue
		}
		writing++
		if receipt.Status != "externally-blocked" || receipt.LastCheckedRevision != "d9338ce9b09792ea7167279fee7ccc1910e28c5d" || !containsString(receipt.SourceIDs, "writingbench-score-xlsx") {
			t.Fatalf("writing receipt=%#v", receipt)
		}
	}
	if writing != 11 {
		t.Fatalf("writing blocks=%d want 11", writing)
	}
}

func testSourceResearchLedger(t *testing.T) (coverageCampaignManifest, SourceResearchLedger) {
	t.Helper()
	manifest, err := loadCoverageCampaignManifest()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := DecodeSourceResearchLedger(sourceResearchLedgerJSON, manifest)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, ledger
}

func TestSourceResearchLedgerJSONRoundTripRemainsStrict(t *testing.T) {
	t.Parallel()
	manifest, ledger := testSourceResearchLedger(t)
	data, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSourceResearchLedger(data, manifest); err != nil {
		t.Fatal(err)
	}
}
