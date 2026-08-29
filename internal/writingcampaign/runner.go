package writingcampaign

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type RunOptions struct {
	RosterPath    string
	SourcePath    string
	SystemPath    string
	OutputDir     string
	HTMLPath      string
	InitialTokens int
	RetryTokens   int
	Concurrency   int
	ModelTimeout  time.Duration
	Execute       bool
}

type Plan struct {
	Models               int  `json:"models"`
	InitialMaxTokens     int  `json:"initial_max_completion_tokens"`
	LengthRetryMaxTokens int  `json:"length_retry_max_completion_tokens"`
	Execute              bool `json:"execute"`
}

func BuildPlan(roster Roster, _, _ string, options RunOptions) (Plan, error) {
	if options.InitialTokens < 1 || options.RetryTokens < options.InitialTokens {
		return Plan{}, fmt.Errorf("token limits must be positive and retry limit must not be lower than initial limit")
	}
	if options.Concurrency < 1 || options.Concurrency > 8 {
		return Plan{}, fmt.Errorf("concurrency must be between 1 and 8")
	}
	return Plan{Models: len(roster.Models), InitialMaxTokens: options.InitialTokens, LengthRetryMaxTokens: options.RetryTokens, Execute: options.Execute}, nil
}

func Run(ctx context.Context, client *Client, roster Roster, source, systemPrompt string, options RunOptions) ([]Receipt, Plan, error) {
	plan, err := BuildPlan(roster, source, systemPrompt, options)
	if err != nil {
		return nil, Plan{}, err
	}
	if !options.Execute {
		receipts, err := LoadReceipts(options.OutputDir)
		return receipts, plan, err
	}
	if strings.TrimSpace(client.APIKey) == "" {
		return nil, plan, fmt.Errorf("OpenRouter API key is required")
	}
	if err := os.MkdirAll(options.OutputDir, 0o755); err != nil {
		return nil, plan, fmt.Errorf("create output directory: %w", err)
	}
	sourceHash := contentHash(source)
	systemHash := contentHash(systemPrompt)
	existing, err := LoadReceipts(options.OutputDir)
	if err != nil {
		return nil, plan, err
	}
	completed := make(map[string]Receipt, len(existing))
	for _, receipt := range existing {
		if receipt.Status == "complete" && receipt.SourceSHA256 == sourceHash && receipt.SystemSHA256 == systemHash {
			completed[receipt.ModelID] = receipt
		}
	}
	jobs := make(chan Model)
	results := make(chan Receipt, len(roster.Models))
	var workers sync.WaitGroup
	for range options.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for model := range jobs {
				receipt := runModel(ctx, client, model, source, systemPrompt, sourceHash, systemHash, options)
				if err := writeReceipt(options.OutputDir, receipt); err != nil {
					receipt.Status = "write_failed"
					if len(receipt.Attempts) == 0 {
						receipt.Attempts = []Attempt{{Attempt: 1, Error: err.Error()}}
					} else {
						receipt.Attempts[len(receipt.Attempts)-1].Error = strings.TrimSpace(receipt.Attempts[len(receipt.Attempts)-1].Error + "; " + err.Error())
					}
				}
				results <- receipt
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, model := range roster.Models {
			if receipt, ok := completed[model.OpenRouterModelID]; ok {
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
	receipts := make([]Receipt, 0, len(roster.Models))
	for receipt := range results {
		receipts = append(receipts, receipt)
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].ModelID < receipts[j].ModelID })
	if err := ctx.Err(); err != nil {
		return receipts, plan, err
	}
	return receipts, plan, nil
}

func runModel(ctx context.Context, client *Client, model Model, source, systemPrompt, sourceHash, systemHash string, options RunOptions) Receipt {
	if options.ModelTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.ModelTimeout)
		defer cancel()
	}
	receipt := Receipt{SchemaVersion: ReceiptSchemaVersion, ModelID: model.OpenRouterModelID, LlamboSelector: model.LlamboSelector, Status: "failed", SourceSHA256: sourceHash, SystemSHA256: systemHash, CreatedAt: time.Now().UTC()}
	initial := min(options.InitialTokens, model.MaxCompletionTokens)
	first := client.Execute(ctx, model, systemPrompt, source, initial, 1)
	receipt.Attempts = append(receipt.Attempts, first)
	if first.Error == "" && strings.EqualFold(first.FinishReason, "length") {
		retry := min(options.RetryTokens, model.MaxCompletionTokens)
		if retry > initial {
			receipt.RetryDisposition = "retried_with_higher_max_completion_tokens"
			receipt.Attempts = append(receipt.Attempts, client.Execute(ctx, model, systemPrompt, source, retry, 2))
		} else {
			receipt.RetryDisposition = "length_retry_unavailable_at_model_limit"
		}
	}
	final := receipt.Attempts[len(receipt.Attempts)-1]
	receipt.FinalAttempt = final.Attempt
	receipt.OutputText = final.OutputText
	receipt.ServedModel = final.ServedModel
	receipt.ServedProvider = final.ServedProvider
	receipt.FinishReason = final.FinishReason
	for _, attempt := range receipt.Attempts {
		receipt.TotalLatencyMS += attempt.LatencyMS
		receipt.TotalCostUSD += attempt.Usage.CostUSD
	}
	if len(receipt.Attempts) > 0 {
		duration := final.CompletedAt.Sub(receipt.Attempts[0].StartedAt)
		receipt.TotalTimeToFinishMS = duration.Milliseconds()
		if duration > 0 && receipt.TotalTimeToFinishMS == 0 {
			receipt.TotalTimeToFinishMS = 1
		}
	}
	receipt.TokensPerSecond = final.Usage.TokensPerSecond
	if final.Error == "" && strings.TrimSpace(final.OutputText) != "" {
		receipt.Status = "complete"
	}
	return receipt
}

func LoadReceipts(dir string) ([]Receipt, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read receipt directory: %w", err)
	}
	var receipts []Receipt
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var receipt Receipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return nil, fmt.Errorf("parse receipt %s: %w", entry.Name(), err)
		}
		receipts = append(receipts, receipt)
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].ModelID < receipts[j].ModelID })
	return receipts, nil
}

func writeReceipt(dir string, receipt Receipt) error {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(filepath.Join(dir, modelFilename(receipt.ModelID)+".json"), data)
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".writing-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func modelFilename(id string) string {
	var out strings.Builder
	separator := false
	for _, r := range strings.ToLower(id) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if separator && out.Len() > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(r)
			separator = false
		} else {
			separator = true
		}
	}
	return out.String()
}

func contentHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
