package evals

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
)

// Repeated string literals consolidated by goconst.
const (
	normalizedFrozen = "normalized-frozen"
	present1         = "present"
	sha256Verified   = "sha256-verified"
)

var (
	sourceLedgerID       = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	sourceLedgerRevision = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+-]{0,127}$`)
	sourceLedgerHash     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sourceLedgerClass    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	secretLikeLedgerData = regexp.MustCompile(`(?i)(?:\b(?:api[_ -]?key|authorization|bearer|password|secret)\b|\bsk-[a-z0-9_-]{8,}\b|\bghp_[a-z0-9]{20,}\b)`)
)

// ValidateSourceResearchLedger checks the ledger against the campaign
// manifest's admitted sources and claims.
func ValidateSourceResearchLedger(ledger *SourceResearchLedger, manifest coverageCampaignManifest) error {
	if ledger == nil {
		return errors.New("source research ledger is nil")
	}
	normalizeSourceResearchLedger(ledger)
	return validateResearchLedgerContent(*ledger, &manifest)
}

func validateResearchLedgerContent(ledger SourceResearchLedger, manifest *coverageCampaignManifest) error {
	if ledger.SchemaVersion != "llambo-source-ledger-v1" {
		return fmt.Errorf("source research ledger has unsupported schema version %q", ledger.SchemaVersion)
	}
	if ledger.TargetVersion == "" {
		return errors.New("source research ledger has no target version")
	}
	if len(ledger.Sources) == 0 || len(ledger.Claims) == 0 || len(ledger.Searches) == 0 || len(ledger.ScopedNulls) == 0 {
		return errors.New("source research ledger requires sources, searches, and scoped-null receipts")
	}
	sourceIDs, err := validateSourceLedgerIdentities(ledger)
	if err != nil {
		return err
	}
	if manifest == nil {
		return nil
	}
	if ledger.TargetVersion != manifest.TargetVersion {
		return fmt.Errorf("source research ledger target version %q does not match campaign %q", ledger.TargetVersion, manifest.TargetVersion)
	}
	if err := validateScopedNullReceipts(ledger.ScopedNulls, *manifest, sourceIDs); err != nil {
		return err
	}
	return validateWritingBenchCampaignBlock(ledger, *manifest)
}

func validateResearchClaim(claim ResearchClaim, sourceIDs map[string]struct{}) error {
	if !validLedgerID(claim.ID) || !validLedgerText(claim.Claim) || !validLedgerText(claim.ChangeCondition) || !finiteConfidence(claim.Confidence) {
		return errors.New("is incomplete or invalid")
	}
	if claim.Kind != "direct" && claim.Kind != "computed" && claim.Kind != "inferred" {
		return fmt.Errorf("has invalid kind %q", claim.Kind)
	}
	if err := validateRequiredLedgerReferences(claim.SupportingIDs, sourceIDs, "supporting source"); err != nil {
		return err
	}
	if err := validateOptionalLedgerReferences(claim.ContradictingIDs, sourceIDs, "contradicting source"); err != nil {
		return err
	}
	if overlapStrings(claim.SupportingIDs, claim.ContradictingIDs) {
		return errors.New("uses a source as both supporting and contradicting evidence")
	}
	return nil
}

func validateResearchSearch(search ResearchSearch, sourceIDs map[string]struct{}) error {
	if !validLedgerID(search.ID) || !validLedgerClass(search.SourceClass) || !validLedgerText(search.Query) || !validLedgerText(search.Result) || !validLedgerRevision(search.Revision) || !validLedgerText(search.NextSearchDiffers) {
		return errors.New("is incomplete or invalid")
	}
	return validateOptionalLedgerReferences(search.SourceIDs, sourceIDs, "search source")
}

func validateScopedNullReceipts(receipts []ScopedNullReceipt, manifest coverageCampaignManifest, sourceIDs map[string]struct{}) error {
	want := make(map[string]coverageCampaignCell)
	for _, target := range manifest.Targets {
		for _, category := range categorySpecs {
			cell := target.Cells[category.name]
			if cell.Status != present1 {
				want[coverageCellKey(target.Key, category.name)] = cell
			}
		}
	}
	if len(receipts) != len(want) {
		return fmt.Errorf("source research ledger has %d scoped-null receipts, want %d", len(receipts), len(want))
	}
	seen := make(map[string]struct{}, len(receipts))
	for i, receipt := range receipts {
		key := coverageCellKey(receipt.ModelKey, receipt.Category)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("source research ledger has duplicate scoped-null receipt %s", key)
		}
		seen[key] = struct{}{}
		cell, expected := want[key]
		if !expected {
			return fmt.Errorf("scoped-null receipt %d is not a non-present campaign cell: %s", i+1, key)
		}
		if err := validateScopedNullCellReceipt(receipt, cell, sourceIDs); err != nil {
			return err
		}
	}
	for key := range want {
		if _, ok := seen[key]; !ok {
			return fmt.Errorf("source research ledger is missing scoped-null receipt %s", key)
		}
	}
	return nil
}

func orderedPrimaryClasses(category string) []string {
	for _, spec := range categorySpecs {
		if spec.name == category {
			legacy := legacyCategorySpec(spec.name)
			classes := make([]string, 0, len(legacy.primaries))
			for _, primary := range legacy.primaries {
				classes = append(classes, primary.benchmark)
			}
			return classes
		}
	}
	return nil
}

func coverageCellKey(modelKey, category string) string { return modelKey + "\x00" + category }

func validLedgerID(value string) bool {
	return sourceLedgerID.MatchString(value) && !secretLikeLedgerData.MatchString(value)
}

func validLedgerRevision(value string) bool {
	return sourceLedgerRevision.MatchString(value) && !secretLikeLedgerData.MatchString(value)
}

func validLedgerClass(value string) bool {
	return sourceLedgerClass.MatchString(value) && !secretLikeLedgerData.MatchString(value)
}

func validLedgerURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" && !secretLikeLedgerData.MatchString(value)
}

func validLedgerText(value string) bool {
	return strings.TrimSpace(value) == value && value != "" && !secretLikeLedgerData.MatchString(value)
}

func validIndependence(value string) bool {
	return value == "benchmark-owner" || value == "first-party" || value == "derived"
}

func validSourceStatus(value string) bool {
	return value == "frozen" || value == normalizedFrozen || value == "superseded" || value == unavailable1
}

func validArtifactVerification(kind, verification, status string) bool {
	switch kind {
	case "checked-in-raw-reference":
		return verification == sha256Verified && status == "frozen"
	case "normalized-snapshot":
		return verification == "metadata-only" && status == normalizedFrozen
	case "normalized-identity-snapshot":
		return verification == sha256Verified && status == normalizedFrozen
	default:
		return false
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func finiteConfidence(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func validateRequiredLedgerReferences(ids []string, known map[string]struct{}, label string) error {
	if len(ids) == 0 {
		return fmt.Errorf("has no %ss", label)
	}
	return validateOptionalLedgerReferences(ids, known, label)
}

func validateOptionalLedgerReferences(ids []string, known map[string]struct{}, label string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			return fmt.Errorf("has duplicate %s %q", label, id)
		}
		seen[id] = struct{}{}
		if _, exists := known[id]; !exists {
			return fmt.Errorf("references unknown %s %q", label, id)
		}
	}
	return nil
}

func overlapStrings(left, right []string) bool {
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; ok {
			return true
		}
	}
	return false
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
