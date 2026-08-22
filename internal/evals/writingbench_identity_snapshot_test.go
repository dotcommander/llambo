package evals

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLoadWritingBenchIdentitySnapshot(t *testing.T) {
	t.Parallel()
	snapshot, err := loadWritingBenchIdentitySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RowCount != 54 || len(snapshot.Rows) != 54 {
		t.Fatalf("rows=%d count=%d", len(snapshot.Rows), snapshot.RowCount)
	}
	digest, err := WritingBenchIdentitySnapshotDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if digest != writingBenchIdentitySnapshotDigest {
		t.Fatalf("digest=%s want %s", digest, writingBenchIdentitySnapshotDigest)
	}
	for i := 1; i < len(snapshot.Rows); i++ {
		if snapshot.Rows[i-1].NormalizedIdentity >= snapshot.Rows[i].NormalizedIdentity {
			t.Fatalf("rows not deterministically sorted at %d", i)
		}
	}
}

func TestDecodeWritingBenchIdentitySnapshotRejectsInvalidInventory(t *testing.T) {
	t.Parallel()
	snapshot, err := loadWritingBenchIdentitySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*WritingBenchIdentitySnapshot)
		want   string
	}{
		{"duplicate key", func(s *WritingBenchIdentitySnapshot) { s.Rows[1] = s.Rows[0] }, "duplicate key"},
		{"ambiguous identity", func(s *WritingBenchIdentitySnapshot) {
			s.Rows[1].Name = strings.Replace(s.Rows[0].Name, "-", " ", 1)
			s.Rows[1].Key = "writingbench:" + canonicalKey(s.Rows[1].Name)
			s.Rows[1].Organization = s.Rows[0].Organization
			s.Rows[1].NormalizedName = s.Rows[0].NormalizedName
			s.Rows[1].OrganizationFamily = s.Rows[0].OrganizationFamily
			s.Rows[1].NormalizedIdentity = s.Rows[0].NormalizedIdentity
		}, "ambiguous identity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			copy := snapshot
			copy.Rows = append([]WritingBenchIdentitySnapshotRow(nil), snapshot.Rows...)
			tt.mutate(&copy)
			data, err := json.Marshal(copy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeWritingBenchIdentitySnapshot(data); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v want %q", err, tt.want)
			}
		})
	}
	if _, err := DecodeWritingBenchIdentitySnapshot([]byte(`{"schema_version":"writingbench-identity-snapshot-v1","unexpected":true}`)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error=%v", err)
	}
}

func TestWritingBenchSnapshotRejectsCampaignMatch(t *testing.T) {
	t.Parallel()
	manifest, err := loadCoverageCampaignManifest()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := loadWritingBenchIdentitySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	manifest.Targets[0].WritingIdentity = coverageCampaignWritingIdentity{Name: snapshot.Rows[0].Name, Organization: snapshot.Rows[0].Organization}
	if err := WritingBenchSnapshotProvesNoCampaignMatches(snapshot, manifest); err == nil || !strings.Contains(err.Error(), "campaign match") {
		t.Fatalf("match error=%v", err)
	}
}
