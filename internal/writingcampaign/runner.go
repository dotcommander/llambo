package writingcampaign

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RunOptions controls one campaign invocation.
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

// Plan is the provider-free campaign plan.
type Plan struct {
	Models               int  `json:"models"`
	InitialMaxTokens     int  `json:"initial_max_completion_tokens"`
	LengthRetryMaxTokens int  `json:"length_retry_max_completion_tokens"`
	Execute              bool `json:"execute"`
}

// BuildPlan validates the roster and computes the campaign plan.
func BuildPlan(roster Roster, _, _ string, options RunOptions) (Plan, error) {
	if options.InitialTokens < 1 || options.RetryTokens < options.InitialTokens {
		return Plan{}, errors.New("token limits must be positive and retry limit must not be lower than initial limit")
	}
	if options.Concurrency < 1 || options.Concurrency > 8 {
		return Plan{}, errors.New("concurrency must be between 1 and 8")
	}
	return Plan{Models: len(roster.Models), InitialMaxTokens: options.InitialTokens, LengthRetryMaxTokens: options.RetryTokens, Execute: options.Execute}, nil
}

// RunInput keeps one campaign's immutable content and execution limits together.
type RunInput struct {
	Source       string
	SystemPrompt string
	Options      RunOptions
}

type campaignContent struct {
	source, systemPrompt, sourceHash, systemHash string
}

type modelRun struct {
	model   Model
	content campaignContent
	options RunOptions
}

type campaignExecution struct {
	content   campaignContent
	options   RunOptions
	completed map[string]Receipt
}

// Run executes or previews one campaign with its sealed content and bounds.
func Run(ctx context.Context, client *Client, roster Roster, input RunInput) ([]Receipt, Plan, error) {
	source, systemPrompt, options := input.Source, input.SystemPrompt, input.Options
	plan, err := BuildPlan(roster, source, systemPrompt, options)
	if err != nil {
		return nil, Plan{}, err
	}
	if !options.Execute {
		var receipts []Receipt
		receipts, err = LoadReceipts(options.OutputDir)
		return receipts, plan, err
	}
	if strings.TrimSpace(client.APIKey) == "" {
		return nil, plan, errors.New("OpenRouter API key is required")
	}
	if err = os.MkdirAll(options.OutputDir, 0o750); err != nil { //nolint:gosec // reviewed directory mode
		return nil, plan, fmt.Errorf("create output directory: %w", err)
	}
	sourceHash := contentHash(source)
	systemHash := contentHash(systemPrompt)
	existing, err := LoadReceipts(options.OutputDir)
	if err != nil {
		return nil, plan, err
	}
	content := campaignContent{source: source, systemPrompt: systemPrompt, sourceHash: sourceHash, systemHash: systemHash}
	completed := completedCampaignReceipts(existing, content)
	receipts := runCampaignWorkers(ctx, client, roster.Models, campaignExecution{content: content, options: options, completed: completed})
	if err := ctx.Err(); err != nil {
		return receipts, plan, err
	}
	return receipts, plan, nil
}

// LoadReceipts loads all model receipts from a campaign output directory.
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
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // reviewed directory mode
		return err
	}
	file, err := os.CreateTemp(dir, ".writing-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
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
