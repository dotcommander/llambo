package evals

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

//go:embed testdata/omlx-coverage-source-ledger-v1.json
var sourceResearchLedgerJSON []byte

// sourceResearchLedgerContentDigest detects changes to the embedded source.
const sourceResearchLedgerContentDigest = "e6d3f68bf9eb440c9c18b1925da5242ff2a47ac849c4dd1f865d9a0a120fea3b"

// sourceResearchLedgerDigest pins the campaign's canonical research state. Updating a
// source, claim, search, or scoped-null receipt requires an intentional review
// of this immutable input rather than silently changing blocked coverage.
const sourceResearchLedgerDigest = "f4bdcdb4225ef831c3ee2cace73443dd85a2685dfe189d2c97c5220d7dc1b437"

type SourceResearchLedger struct {
	SchemaVersion string              `json:"schema_version"`
	TargetVersion string              `json:"target_version"`
	Sources       []ResearchSource    `json:"sources"`
	Claims        []ResearchClaim     `json:"claims"`
	Searches      []ResearchSearch    `json:"searches"`
	ScopedNulls   []ScopedNullReceipt `json:"scoped_null_receipts"`
}

type ResearchSource struct {
	ID            string    `json:"id"`
	Locator       string    `json:"locator"`
	Owner         string    `json:"owner"`
	Title         string    `json:"title"`
	Revision      string    `json:"revision"`
	ContentSHA256 string    `json:"content_sha256"`
	Methodology   string    `json:"methodology"`
	AccessedAt    time.Time `json:"accessed_at"`
	ArtifactKind  string    `json:"artifact_kind"`
	Verification  string    `json:"verification"`
	SourceClass   string    `json:"source_class"`
	Independence  string    `json:"independence"`
	Status        string    `json:"status"`
}

type ResearchClaim struct {
	ID               string   `json:"id"`
	Claim            string   `json:"claim"`
	SupportingIDs    []string `json:"supporting_source_ids"`
	ContradictingIDs []string `json:"contradicting_source_ids,omitempty"`
	Kind             string   `json:"kind"`
	Confidence       float64  `json:"confidence"`
	ChangeCondition  string   `json:"change_condition"`
}

type ResearchSearch struct {
	ID                string   `json:"id"`
	SourceClass       string   `json:"source_class"`
	Query             string   `json:"query"`
	Result            string   `json:"result"`
	Revision          string   `json:"revision"`
	SourceIDs         []string `json:"source_ids,omitempty"`
	NextSearchDiffers string   `json:"next_search_differs"`
}

type ScopedNullReceipt struct {
	ModelKey             string   `json:"model_key"`
	Category             string   `json:"category"`
	OrderedSourceClasses []string `json:"ordered_source_classes"`
	Reason               string   `json:"reason"`
	Status               string   `json:"status"`
	LastCheckedRevision  string   `json:"last_checked_revision"`
	SourceIDs            []string `json:"source_ids"`
	NextCheck            string   `json:"next_check"`
}

// LoadSourceResearchLedger returns the embedded, digest-pinned campaign
// evidence. It contains source and search provenance only; it never supplies a
// score observation.
func LoadSourceResearchLedger() (SourceResearchLedger, error) {
	if sourceResearchLedgerDigest == "" {
		return SourceResearchLedger{}, fmt.Errorf("source research ledger digest is not pinned")
	}
	if got := sourceLedgerBytesDigest(sourceResearchLedgerJSON); got != sourceResearchLedgerContentDigest {
		return SourceResearchLedger{}, fmt.Errorf("source research ledger content digest %s does not match pinned %s", got, sourceResearchLedgerContentDigest)
	}
	manifest, err := loadCoverageCampaignManifest()
	if err != nil {
		return SourceResearchLedger{}, err
	}
	ledger, err := DecodeSourceResearchLedger(sourceResearchLedgerJSON, manifest)
	if err != nil {
		return SourceResearchLedger{}, err
	}
	if got, err := SourceResearchLedgerDigest(ledger); err != nil {
		return SourceResearchLedger{}, err
	} else if got != sourceResearchLedgerDigest {
		return SourceResearchLedger{}, fmt.Errorf("source research ledger canonical digest %s does not match pinned %s", got, sourceResearchLedgerDigest)
	}
	return ledger, nil
}

func DecodeSourceResearchLedger(data []byte, manifest coverageCampaignManifest) (SourceResearchLedger, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var ledger SourceResearchLedger
	if err := decoder.Decode(&ledger); err != nil {
		return SourceResearchLedger{}, fmt.Errorf("decode source research ledger: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return SourceResearchLedger{}, fmt.Errorf("decode source research ledger: %w", err)
	}
	if err := ValidateSourceResearchLedger(&ledger, manifest); err != nil {
		return SourceResearchLedger{}, err
	}
	return ledger, nil
}

// DeriveActiveScopedNullReceipts selects the exact current unresolved cells
// from immutable historical receipts and verifies their current campaign
// status, reason, and last checked revision.
func DeriveActiveScopedNullReceipts(ledger SourceResearchLedger, campaign CoverageCampaign) ([]ScopedNullReceipt, error) {
	receipts := make(map[string]struct{}, len(ledger.ScopedNulls))
	byCell := make(map[string]ScopedNullReceipt, len(ledger.ScopedNulls))
	for _, receipt := range ledger.ScopedNulls {
		key := coverageCellKey(receipt.ModelKey, receipt.Category)
		receipts[key] = struct{}{}
		byCell[key] = receipt
	}
	active := make([]ScopedNullReceipt, 0, len(campaign.Unresolved))
	for _, unresolved := range campaign.Unresolved {
		key := coverageCellKey(unresolved.ModelKey, unresolved.Category)
		if _, ok := receipts[key]; !ok {
			return nil, fmt.Errorf("source research ledger has no scoped-null receipt for runtime unresolved %s", key)
		}
		receipt := byCell[key]
		if receipt.Status != unresolved.Status || receipt.Reason != unresolved.Reason || receipt.LastCheckedRevision != unresolved.LastCheckedRevision {
			return nil, fmt.Errorf("source research ledger scoped-null receipt does not match runtime unresolved %s", key)
		}
		active = append(active, receipt)
	}
	return active, ValidateActiveScopedNullReceipts(active, campaign)
}

// ValidateActiveScopedNullReceipts requires an exact active runtime list.
// Historical receipts for runtime-present cells are intentionally excluded.
func ValidateActiveScopedNullReceipts(receipts []ScopedNullReceipt, campaign CoverageCampaign) error {
	if len(receipts) != len(campaign.Unresolved) {
		return fmt.Errorf("active scoped-null receipts=%d want exactly %d runtime unresolved cells", len(receipts), len(campaign.Unresolved))
	}
	want := make(map[string]CoverageCampaignCell, len(campaign.Unresolved))
	for _, unresolved := range campaign.Unresolved {
		want[coverageCellKey(unresolved.ModelKey, unresolved.Category)] = unresolved
	}
	seen := make(map[string]struct{}, len(receipts))
	for _, receipt := range receipts {
		key := coverageCellKey(receipt.ModelKey, receipt.Category)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("active scoped-null receipts have duplicate %s", key)
		}
		seen[key] = struct{}{}
		unresolved, ok := want[key]
		if !ok {
			return fmt.Errorf("active scoped-null receipt is not runtime unresolved: %s", key)
		}
		if receipt.Status != unresolved.Status || receipt.Reason != unresolved.Reason || receipt.LastCheckedRevision != unresolved.LastCheckedRevision {
			return fmt.Errorf("active scoped-null receipt does not match runtime unresolved %s", key)
		}
	}
	return nil
}

// ValidateCoverageCampaignResearchLedger is retained for callers that need a
// single validation entry point; it derives, rather than treats history as,
// the active receipt list.
func ValidateCoverageCampaignResearchLedger(ledger SourceResearchLedger, campaign CoverageCampaign) error {
	_, err := DeriveActiveScopedNullReceipts(ledger, campaign)
	return err
}

func CanonicalSourceResearchLedger(ledger SourceResearchLedger) ([]byte, error) {
	normalized := cloneSourceResearchLedger(ledger)
	normalizeSourceResearchLedger(&normalized)
	if err := validateSourceResearchLedger(normalized, nil); err != nil {
		return nil, err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("marshal source research ledger: %w", err)
	}
	return append(data, '\n'), nil
}

func cloneSourceResearchLedger(ledger SourceResearchLedger) SourceResearchLedger {
	clone := ledger
	clone.Sources = append([]ResearchSource(nil), ledger.Sources...)
	clone.Claims = append([]ResearchClaim(nil), ledger.Claims...)
	clone.Searches = append([]ResearchSearch(nil), ledger.Searches...)
	clone.ScopedNulls = append([]ScopedNullReceipt(nil), ledger.ScopedNulls...)
	for i := range clone.Claims {
		clone.Claims[i].SupportingIDs = append([]string(nil), clone.Claims[i].SupportingIDs...)
		clone.Claims[i].ContradictingIDs = append([]string(nil), clone.Claims[i].ContradictingIDs...)
	}
	for i := range clone.Searches {
		clone.Searches[i].SourceIDs = append([]string(nil), clone.Searches[i].SourceIDs...)
	}
	for i := range clone.ScopedNulls {
		clone.ScopedNulls[i].OrderedSourceClasses = append([]string(nil), clone.ScopedNulls[i].OrderedSourceClasses...)
		clone.ScopedNulls[i].SourceIDs = append([]string(nil), clone.ScopedNulls[i].SourceIDs...)
	}
	return clone
}

func SourceResearchLedgerDigest(ledger SourceResearchLedger) (string, error) {
	data, err := CanonicalSourceResearchLedger(ledger)
	if err != nil {
		return "", err
	}
	return sourceLedgerBytesDigest(data), nil
}

func sourceLedgerBytesDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func normalizeSourceResearchLedger(ledger *SourceResearchLedger) {
	sort.Slice(ledger.Sources, func(i, j int) bool { return ledger.Sources[i].ID < ledger.Sources[j].ID })
	sort.Slice(ledger.Claims, func(i, j int) bool { return ledger.Claims[i].ID < ledger.Claims[j].ID })
	sort.Slice(ledger.Searches, func(i, j int) bool { return ledger.Searches[i].ID < ledger.Searches[j].ID })
	sort.Slice(ledger.ScopedNulls, func(i, j int) bool {
		left, right := coverageCellKey(ledger.ScopedNulls[i].ModelKey, ledger.ScopedNulls[i].Category), coverageCellKey(ledger.ScopedNulls[j].ModelKey, ledger.ScopedNulls[j].Category)
		return left < right
	})
	for i := range ledger.Claims {
		sort.Strings(ledger.Claims[i].SupportingIDs)
		sort.Strings(ledger.Claims[i].ContradictingIDs)
	}
	for i := range ledger.Searches {
		sort.Strings(ledger.Searches[i].SourceIDs)
	}
	for i := range ledger.ScopedNulls {
		sort.Strings(ledger.ScopedNulls[i].SourceIDs)
	}
}
