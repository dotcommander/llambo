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

//go:embed testdata/writingbench-identity-v1.json
var writingBenchIdentitySnapshotJSON []byte

const writingBenchIdentitySnapshotContentDigest = "2249760d9a472ed6ad3710075885b8c9429733b84284a3236c1e1515063401c7"
const writingBenchIdentitySnapshotDigest = "dcbf675dc25f1d5ef2484367eb4cba5677d1f71fbd72015041df416ab717e7c6"

type WritingBenchIdentitySnapshot struct {
	SchemaVersion string                            `json:"schema_version"`
	FetchedAt     time.Time                         `json:"fetched_at"`
	Locator       string                            `json:"locator"`
	Revision      string                            `json:"revision"`
	CommitSHA     string                            `json:"commit_sha"`
	ContentSHA256 string                            `json:"content_sha256"`
	Methodology   string                            `json:"methodology"`
	RowCount      int                               `json:"row_count"`
	Rows          []WritingBenchIdentitySnapshotRow `json:"rows"`
}

type WritingBenchIdentitySnapshotRow struct {
	Key                string `json:"key"`
	Name               string `json:"name"`
	Organization       string `json:"organization"`
	NormalizedName     string `json:"normalized_name"`
	OrganizationFamily string `json:"organization_family"`
	NormalizedIdentity string `json:"normalized_identity"`
}

func loadWritingBenchIdentitySnapshot() (WritingBenchIdentitySnapshot, error) {
	if got := writingBenchIdentitySnapshotBytesDigest(writingBenchIdentitySnapshotJSON); got != writingBenchIdentitySnapshotContentDigest {
		return WritingBenchIdentitySnapshot{}, fmt.Errorf("WritingBench identity snapshot content digest %s does not match pinned %s", got, writingBenchIdentitySnapshotContentDigest)
	}
	decoder := json.NewDecoder(bytes.NewReader(writingBenchIdentitySnapshotJSON))
	decoder.DisallowUnknownFields()
	var snapshot WritingBenchIdentitySnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return WritingBenchIdentitySnapshot{}, fmt.Errorf("decode WritingBench identity snapshot: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return WritingBenchIdentitySnapshot{}, fmt.Errorf("decode WritingBench identity snapshot: %w", err)
	}
	if err := validateWritingBenchIdentitySnapshot(snapshot); err != nil {
		return WritingBenchIdentitySnapshot{}, err
	}
	if got, err := WritingBenchIdentitySnapshotDigest(snapshot); err != nil {
		return WritingBenchIdentitySnapshot{}, err
	} else if got != writingBenchIdentitySnapshotDigest {
		return WritingBenchIdentitySnapshot{}, fmt.Errorf("WritingBench identity snapshot canonical digest %s does not match pinned %s", got, writingBenchIdentitySnapshotDigest)
	}
	return snapshot, nil
}

func DecodeWritingBenchIdentitySnapshot(data []byte) (WritingBenchIdentitySnapshot, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var snapshot WritingBenchIdentitySnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return WritingBenchIdentitySnapshot{}, fmt.Errorf("decode WritingBench identity snapshot: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return WritingBenchIdentitySnapshot{}, fmt.Errorf("decode WritingBench identity snapshot: %w", err)
	}
	if err := validateWritingBenchIdentitySnapshot(snapshot); err != nil {
		return WritingBenchIdentitySnapshot{}, err
	}
	return snapshot, nil
}

func WritingBenchIdentitySnapshotDigest(snapshot WritingBenchIdentitySnapshot) (string, error) {
	normalized := snapshot
	normalized.Rows = append([]WritingBenchIdentitySnapshotRow(nil), snapshot.Rows...)
	sort.Slice(normalized.Rows, func(i, j int) bool {
		if normalized.Rows[i].NormalizedIdentity == normalized.Rows[j].NormalizedIdentity {
			return normalized.Rows[i].Key < normalized.Rows[j].Key
		}
		return normalized.Rows[i].NormalizedIdentity < normalized.Rows[j].NormalizedIdentity
	})
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal WritingBench identity snapshot: %w", err)
	}
	return writingBenchIdentitySnapshotBytesDigest(append(data, '\n')), nil
}

func WritingBenchSnapshotProvesNoCampaignMatches(snapshot WritingBenchIdentitySnapshot, manifest coverageCampaignManifest) error {
	identities := make(map[string]struct{}, len(snapshot.Rows))
	for _, row := range snapshot.Rows {
		identities[row.NormalizedIdentity] = struct{}{}
	}
	for _, target := range manifest.Targets {
		identity, ok := externalIdentity(Model{Name: target.WritingIdentity.Name, Organization: target.WritingIdentity.Organization})
		if !ok {
			return fmt.Errorf("campaign target %q has no conservative WritingBench identity", target.Key)
		}
		if _, found := identities[identity]; found {
			return fmt.Errorf("WritingBench identity snapshot has a campaign match for %q", target.Key)
		}
	}
	return nil
}

func validateWritingBenchIdentitySnapshot(snapshot WritingBenchIdentitySnapshot) error {
	if snapshot.SchemaVersion != "writingbench-identity-snapshot-v1" || snapshot.FetchedAt.IsZero() || !validLedgerURL(snapshot.Locator) || !validLedgerRevision(snapshot.Revision) || !validLedgerRevision(snapshot.CommitSHA) || !sourceLedgerHash.MatchString(snapshot.ContentSHA256) || !validLedgerText(snapshot.Methodology) || snapshot.RowCount != 54 || len(snapshot.Rows) != snapshot.RowCount {
		return fmt.Errorf("WritingBench identity snapshot is incomplete or invalid")
	}
	keys, identities := make(map[string]struct{}, len(snapshot.Rows)), make(map[string]struct{}, len(snapshot.Rows))
	for i, row := range snapshot.Rows {
		identity, ok := externalIdentity(Model{Name: row.Name, Organization: row.Organization})
		if !ok || row.Key != "writingbench:"+canonicalKey(row.Name) || row.NormalizedName != normalizeIdentityText(row.Name, true) || row.OrganizationFamily != canonicalOrganizationFamily(row.Organization) || row.NormalizedIdentity != identity {
			return fmt.Errorf("WritingBench identity snapshot row %d is invalid", i+1)
		}
		if _, exists := keys[row.Key]; exists {
			return fmt.Errorf("WritingBench identity snapshot has duplicate key %q", row.Key)
		}
		if _, exists := identities[row.NormalizedIdentity]; exists {
			return fmt.Errorf("WritingBench identity snapshot has ambiguous identity %q", row.NormalizedIdentity)
		}
		keys[row.Key], identities[row.NormalizedIdentity] = struct{}{}, struct{}{}
		if i > 0 && (snapshot.Rows[i-1].NormalizedIdentity > row.NormalizedIdentity || snapshot.Rows[i-1].NormalizedIdentity == row.NormalizedIdentity && snapshot.Rows[i-1].Key >= row.Key) {
			return fmt.Errorf("WritingBench identity snapshot rows are not sorted")
		}
	}
	return nil
}

func writingBenchIdentitySnapshotBytesDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
