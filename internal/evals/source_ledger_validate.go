package evals

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
)

var (
	sourceLedgerID       = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	sourceLedgerRevision = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+-]{0,127}$`)
	sourceLedgerHash     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sourceLedgerClass    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	secretLikeLedgerData = regexp.MustCompile(`(?i)(?:\b(?:api[_ -]?key|authorization|bearer|password|secret)\b|\bsk-[a-z0-9_-]{8,}\b|\bghp_[a-z0-9]{20,}\b)`)
)

func ValidateSourceResearchLedger(ledger *SourceResearchLedger, manifest coverageCampaignManifest) error {
	if ledger == nil {
		return fmt.Errorf("source research ledger is nil")
	}
	normalizeSourceResearchLedger(ledger)
	return validateSourceResearchLedger(*ledger, &manifest)
}

func validateSourceResearchLedger(ledger SourceResearchLedger, manifest *coverageCampaignManifest) error {
	if ledger.SchemaVersion != "llambo-source-ledger-v1" {
		return fmt.Errorf("source research ledger has unsupported schema version %q", ledger.SchemaVersion)
	}
	if ledger.TargetVersion == "" {
		return fmt.Errorf("source research ledger has no target version")
	}
	if len(ledger.Sources) == 0 || len(ledger.Claims) == 0 || len(ledger.Searches) == 0 || len(ledger.ScopedNulls) == 0 {
		return fmt.Errorf("source research ledger requires sources, searches, and scoped-null receipts")
	}
	sourceIDs := make(map[string]struct{}, len(ledger.Sources))
	for i, source := range ledger.Sources {
		if err := validateResearchSource(source); err != nil {
			return fmt.Errorf("source %d: %w", i+1, err)
		}
		if _, exists := sourceIDs[source.ID]; exists {
			return fmt.Errorf("source research ledger has duplicate source %q", source.ID)
		}
		sourceIDs[source.ID] = struct{}{}
	}
	claimIDs := make(map[string]struct{}, len(ledger.Claims))
	for i, claim := range ledger.Claims {
		if _, exists := claimIDs[claim.ID]; exists {
			return fmt.Errorf("source research ledger has duplicate claim %q", claim.ID)
		}
		claimIDs[claim.ID] = struct{}{}
		if err := validateResearchClaim(claim, sourceIDs); err != nil {
			return fmt.Errorf("claim %d: %w", i+1, err)
		}
	}
	searchIDs := make(map[string]struct{}, len(ledger.Searches))
	for i, search := range ledger.Searches {
		if _, exists := searchIDs[search.ID]; exists {
			return fmt.Errorf("source research ledger has duplicate search %q", search.ID)
		}
		searchIDs[search.ID] = struct{}{}
		if err := validateResearchSearch(search, sourceIDs); err != nil {
			return fmt.Errorf("search %d: %w", i+1, err)
		}
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

func validateResearchSource(source ResearchSource) error {
	if !validLedgerID(source.ID) || !validLedgerURL(source.Locator) || !validLedgerText(source.Owner) || !validLedgerText(source.Title) || !validLedgerRevision(source.Revision) || !sourceLedgerHash.MatchString(source.ContentSHA256) || !validLedgerText(source.Methodology) || source.AccessedAt.IsZero() || !validArtifactVerification(source.ArtifactKind, source.Verification, source.Status) || !validLedgerClass(source.SourceClass) || !validIndependence(source.Independence) || !validSourceStatus(source.Status) {
		return fmt.Errorf("is incomplete or invalid")
	}
	return nil
}

func validateResearchClaim(claim ResearchClaim, sourceIDs map[string]struct{}) error {
	if !validLedgerID(claim.ID) || !validLedgerText(claim.Claim) || !validLedgerText(claim.ChangeCondition) || !finiteConfidence(claim.Confidence) {
		return fmt.Errorf("is incomplete or invalid")
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
		return fmt.Errorf("uses a source as both supporting and contradicting evidence")
	}
	return nil
}

func validateResearchSearch(search ResearchSearch, sourceIDs map[string]struct{}) error {
	if !validLedgerID(search.ID) || !validLedgerClass(search.SourceClass) || !validLedgerText(search.Query) || !validLedgerText(search.Result) || !validLedgerRevision(search.Revision) || !validLedgerText(search.NextSearchDiffers) {
		return fmt.Errorf("is incomplete or invalid")
	}
	return validateOptionalLedgerReferences(search.SourceIDs, sourceIDs, "search source")
}

func validateScopedNullReceipts(receipts []ScopedNullReceipt, manifest coverageCampaignManifest, sourceIDs map[string]struct{}) error {
	want := make(map[string]coverageCampaignCell)
	for _, target := range manifest.Targets {
		for _, category := range categorySpecs {
			cell := target.Cells[category.name]
			if cell.Status != "present" {
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
		if receipt.Status != cell.Status || receipt.Reason != cell.Reason || receipt.LastCheckedRevision != cell.LastCheckedRevision {
			return fmt.Errorf("scoped-null receipt %s does not match frozen campaign status, reason, or revision", key)
		}
		if !validCoverageStatus(receipt.Status) || receipt.Status == "present" || !validLedgerText(receipt.NextCheck) {
			return fmt.Errorf("scoped-null receipt %s is incomplete or invalid", key)
		}
		if !equalStringSlices(receipt.OrderedSourceClasses, orderedPrimaryClasses(receipt.Category)) {
			return fmt.Errorf("scoped-null receipt %s has an incomplete or reordered source ladder", key)
		}
		if err := validateRequiredLedgerReferences(receipt.SourceIDs, sourceIDs, "receipt source"); err != nil {
			return fmt.Errorf("scoped-null receipt %s: %w", key, err)
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
	return value == "frozen" || value == "normalized-frozen" || value == "superseded" || value == "unavailable"
}

func validArtifactVerification(kind, verification, status string) bool {
	switch kind {
	case "checked-in-raw-reference":
		return verification == "sha256-verified" && status == "frozen"
	case "normalized-snapshot":
		return verification == "metadata-only" && status == "normalized-frozen"
	case "normalized-identity-snapshot":
		return verification == "sha256-verified" && status == "normalized-frozen"
	default:
		return false
	}
}

func validateWritingBenchCampaignBlock(ledger SourceResearchLedger, manifest coverageCampaignManifest) error {
	snapshot, err := loadWritingBenchIdentitySnapshot()
	if err != nil {
		return fmt.Errorf("load WritingBench identity snapshot: %w", err)
	}
	if err := WritingBenchSnapshotProvesNoCampaignMatches(snapshot, manifest); err != nil {
		return err
	}
	var source *ResearchSource
	for i := range ledger.Sources {
		if ledger.Sources[i].ID == "writingbench-score-xlsx" {
			source = &ledger.Sources[i]
			break
		}
	}
	if source == nil || source.ArtifactKind != "normalized-identity-snapshot" || source.Verification != "sha256-verified" || source.Status != "normalized-frozen" || source.Locator != snapshot.Locator || source.Revision != snapshot.Revision || source.ContentSHA256 != snapshot.ContentSHA256 || !strings.Contains(source.Methodology, snapshot.Methodology) {
		return fmt.Errorf("WritingBench ledger source is not bound to the verified identity snapshot")
	}
	claimFound, searchFound := false, false
	for _, claim := range ledger.Claims {
		if claim.ID == "writingbench-target-boundary" && claim.Kind == "direct" && containsString(claim.SupportingIDs, source.ID) && strings.Contains(claim.Claim, "no matching target-family row") {
			claimFound = true
		}
	}
	for _, search := range ledger.Searches {
		if search.ID == "writing-primary-ladder" && search.Revision == snapshot.Revision && containsString(search.SourceIDs, source.ID) {
			searchFound = true
		}
	}
	if !claimFound || !searchFound {
		return fmt.Errorf("WritingBench no-match claim or search is not bound to the verified identity snapshot")
	}
	for _, target := range manifest.Targets {
		for _, receipt := range ledger.ScopedNulls {
			if receipt.ModelKey != target.Key || receipt.Category != "writing" {
				continue
			}
			if receipt.Status != "externally-blocked" || receipt.LastCheckedRevision != snapshot.Revision || !containsString(receipt.SourceIDs, source.ID) {
				return fmt.Errorf("WritingBench receipt for %q is not bound to the verified no-match snapshot", target.Key)
			}
		}
	}
	return nil
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
