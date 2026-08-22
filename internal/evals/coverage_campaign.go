package evals

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed testdata/omlx-coverage-campaign-v1.json
var coverageCampaignJSON []byte

type coverageCampaignManifest struct {
	TargetVersion string                   `json:"target_version"`
	Targets       []coverageCampaignTarget `json:"targets"`
	InactiveOrNew []CoverageCampaignNote   `json:"inactive_or_new"`
}

type coverageCampaignTarget struct {
	Key             string                          `json:"key"`
	WritingIdentity coverageCampaignWritingIdentity `json:"writing_identity"`
	Cells           map[string]coverageCampaignCell `json:"cells"`
}

type coverageCampaignWritingIdentity struct {
	Name         string `json:"name"`
	Organization string `json:"organization"`
}

type coverageCampaignCell struct {
	Status              string `json:"status"`
	Reason              string `json:"reason"`
	LastCheckedRevision string `json:"last_checked_revision"`
}

// CoverageCampaign is an explicit bounded research ledger. It is deliberately
// separate from canonical ranking rows: projected OMLX artifacts can be
// campaign targets, but remain projections in the score matrix.
type CoverageCampaign struct {
	TargetVersion string                 `json:"target_version"`
	TargetModels  int                    `json:"target_models"`
	TotalCells    int                    `json:"total_cells"`
	PresentCells  int                    `json:"present_cells"`
	Unresolved    []CoverageCampaignCell `json:"unresolved_cells,omitempty"`
	Status        string                 `json:"status"`
	InactiveOrNew []CoverageCampaignNote `json:"inactive_or_new,omitempty"`
}

type CoverageCampaignCell struct {
	ModelKey            string `json:"model_key"`
	Category            string `json:"category"`
	Status              string `json:"status"`
	Reason              string `json:"reason,omitempty"`
	LastCheckedRevision string `json:"last_checked_revision,omitempty"`
}

type CoverageCampaignNote struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

func loadCoverageCampaignManifest() (coverageCampaignManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(coverageCampaignJSON))
	decoder.DisallowUnknownFields()
	var manifest coverageCampaignManifest
	if err := decoder.Decode(&manifest); err != nil {
		return coverageCampaignManifest{}, fmt.Errorf("decode coverage campaign manifest: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return coverageCampaignManifest{}, fmt.Errorf("decode coverage campaign manifest: %w", err)
	}
	manifest.TargetVersion = strings.TrimSpace(manifest.TargetVersion)
	if manifest.TargetVersion == "" || len(manifest.Targets) != 11 {
		return coverageCampaignManifest{}, fmt.Errorf("coverage campaign must declare a target version and exactly 11 targets")
	}
	seen := make(map[string]struct{}, len(manifest.Targets))
	for i := range manifest.Targets {
		target := &manifest.Targets[i]
		target.Key = strings.TrimSpace(target.Key)
		if target.Key == "" || target.WritingIdentity.Name == "" || target.WritingIdentity.Organization == "" {
			return coverageCampaignManifest{}, fmt.Errorf("coverage campaign target %d has no key", i+1)
		}
		if _, ok := externalIdentity(Model{Name: target.WritingIdentity.Name, Organization: target.WritingIdentity.Organization}); !ok {
			return coverageCampaignManifest{}, fmt.Errorf("coverage campaign target %q has invalid WritingBench identity", target.Key)
		}
		if _, ok := seen[target.Key]; ok {
			return coverageCampaignManifest{}, fmt.Errorf("coverage campaign has duplicate target %q", target.Key)
		}
		seen[target.Key] = struct{}{}
		if len(target.Cells) != len(categorySpecs) {
			return coverageCampaignManifest{}, fmt.Errorf("coverage campaign target %q has %d cells, want %d", target.Key, len(target.Cells), len(categorySpecs))
		}
		for _, category := range categorySpecs {
			cell, ok := target.Cells[category.name]
			if !ok || !validCoverageStatus(cell.Status) || strings.TrimSpace(cell.Reason) == "" || strings.TrimSpace(cell.LastCheckedRevision) == "" {
				return coverageCampaignManifest{}, fmt.Errorf("coverage campaign target %q category %q is incomplete", target.Key, category.name)
			}
		}
	}
	sort.Slice(manifest.Targets, func(i, j int) bool { return manifest.Targets[i].Key < manifest.Targets[j].Key })
	return manifest, nil
}

func validCoverageStatus(status string) bool {
	switch status {
	case "present", "research-pending", "source-absent", "identity-ambiguous", "inapplicable", "externally-blocked":
		return true
	default:
		return false
	}
}

func buildCoverageCampaign(rows []ReportModel) (*CoverageCampaign, error) {
	manifest, err := loadCoverageCampaignManifest()
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]ReportModel, len(rows))
	for _, row := range rows {
		byKey[row.Key] = row
	}
	campaign := &CoverageCampaign{TargetVersion: manifest.TargetVersion, TargetModels: len(manifest.Targets), TotalCells: len(manifest.Targets) * len(categorySpecs), InactiveOrNew: append([]CoverageCampaignNote(nil), manifest.InactiveOrNew...)}
	for _, target := range manifest.Targets {
		row, found := byKey[target.Key]
		for _, category := range categorySpecs {
			cell := target.Cells[category.name]
			if found && row.LlamboScores[category.name] != nil {
				campaign.PresentCells++
				continue
			}
			campaign.Unresolved = append(campaign.Unresolved, CoverageCampaignCell{ModelKey: target.Key, Category: category.name, Status: cell.Status, Reason: cell.Reason, LastCheckedRevision: cell.LastCheckedRevision})
		}
	}
	if len(campaign.Unresolved) == 0 {
		campaign.Status = "complete"
	} else {
		campaign.Status = "blocked"
	}
	sort.Slice(campaign.Unresolved, func(i, j int) bool {
		if campaign.Unresolved[i].ModelKey == campaign.Unresolved[j].ModelKey {
			return campaign.Unresolved[i].Category < campaign.Unresolved[j].Category
		}
		return campaign.Unresolved[i].ModelKey < campaign.Unresolved[j].ModelKey
	})
	ledger, err := LoadSourceResearchLedger()
	if err != nil {
		return nil, err
	}
	activeReceipts, err := DeriveActiveScopedNullReceipts(ledger, *campaign)
	if err != nil {
		return nil, err
	}
	if err := ValidateActiveScopedNullReceipts(activeReceipts, *campaign); err != nil {
		return nil, err
	}
	return campaign, nil
}
