package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dotcommander/llambo/internal/writingcampaign"
)

func selectCampaignRoster(roster *writingcampaign.Roster, selectedModel string) error {
	if strings.TrimSpace(selectedModel) != "" {
		return selectExactCampaignModel(roster, selectedModel)
	}
	active := roster.Models[:0]
	for _, model := range roster.Models {
		if !model.Disabled {
			active = append(active, model)
		}
	}
	if len(active) == 0 {
		return errors.New("locked writing roster has no enabled models")
	}
	roster.Models = active
	return nil
}

func selectExactCampaignModel(roster *writingcampaign.Roster, selectedModel string) error {
	selected := make([]writingcampaign.Model, 0, 1)
	for _, model := range roster.Models {
		if model.OpenRouterModelID == selectedModel {
			if model.Disabled {
				return fmt.Errorf("model %q is disabled: %s", selectedModel, model.DisabledReason)
			}
			selected = append(selected, model)
			break
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("model %q is not in the locked writing roster", selectedModel)
	}
	roster.Models = selected
	return nil
}

func campaignReceiptError(receipts []writingcampaign.Receipt, outputDir string) error {
	failed := 0
	for _, receipt := range receipts {
		if receipt.Status != "complete" {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("campaign completed with %d failed model receipts; see %s", failed, outputDir)
	}
	return nil
}
