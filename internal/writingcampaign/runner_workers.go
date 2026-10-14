package writingcampaign

import (
	"context"
	"sort"
	"strings"
	"sync"
)

func completedCampaignReceipts(existing []Receipt, content campaignContent) map[string]Receipt {
	completed := make(map[string]Receipt, len(existing))
	for _, receipt := range existing {
		if receipt.Status == "complete" && receipt.SourceSHA256 == content.sourceHash && receipt.SystemSHA256 == content.systemHash {
			completed[receipt.ModelID] = receipt
		}
	}
	return completed
}

// runCampaignWorkers owns bounded workers; closing jobs terminates every worker.
func runCampaignWorkers(ctx context.Context, client *Client, models []Model, input campaignExecution) []Receipt {
	jobs := make(chan Model)
	results := make(chan Receipt, len(models))
	var workers sync.WaitGroup
	for range input.options.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for model := range jobs {
				receipt := generateCampaignReceipt(ctx, client, model, input)
				results <- receipt
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, model := range models {
			if receipt, ok := input.completed[model.OpenRouterModelID]; ok {
				results <- receipt
				continue
			}
			select {
			case jobs <- model:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	receipts := make([]Receipt, 0, len(models))
	for receipt := range results {
		receipts = append(receipts, receipt)
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].ModelID < receipts[j].ModelID })
	return receipts
}

func generateCampaignReceipt(ctx context.Context, client *Client, model Model, input campaignExecution) Receipt {
	receipt := runModel(ctx, client, modelRun{model: model, content: input.content, options: input.options})
	if err := writeReceipt(input.options.OutputDir, receipt); err != nil {
		receipt.Status = "write_failed"
		if len(receipt.Attempts) == 0 {
			receipt.Attempts = []Attempt{{Attempt: 1, Error: err.Error()}}
		} else {
			receipt.Attempts[len(receipt.Attempts)-1].Error = strings.TrimSpace(receipt.Attempts[len(receipt.Attempts)-1].Error + "; " + err.Error())
		}
	}
	return receipt
}
