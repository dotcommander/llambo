package evals

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	maxWritingGenerationAttempts = 1
	maxWritingJudgmentAttempts   = 3
)

var ErrWritingRunIncomplete = errors.New("writing evaluation completed with failures")

func LoadWritingPromptFile(path string) ([]WritingPromptRecord, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("read writing prompt input: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxWritingRunInputSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("read writing prompt input: %w", err)
	}
	if len(data) > maxWritingRunInputSize {
		return nil, "", fmt.Errorf("writing prompt input exceeds %d bytes", maxWritingRunInputSize)
	}
	hash := sha256.Sum256(data)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 64<<10), maxWritingRunRecordSize)
	records := make([]WritingPromptRecord, 0)
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var record WritingPromptRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, "", fmt.Errorf("parse writing prompt input line %d: %w", line, err)
		}
		record.SourceRecord = cloneRawMessage(record.SourceRecord)
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, "", fmt.Errorf("scan writing prompt input: %w", err)
	}
	if len(records) == 0 {
		return nil, "", fmt.Errorf("writing prompt input is empty")
	}
	return records, hex.EncodeToString(hash[:]), nil
}

func NewWritingRunManifest(inputPath, inputHash string, records []WritingPromptRecord, adapter WritingBenchmarkAdapter, models []WritingModelSpec, judge WritingModelSpec, iterations, concurrency int, timeout time.Duration, maxRunCost float64, now time.Time) WritingRunManifest {
	promptIDs := make([]string, len(records))
	for i := range records {
		promptIDs[i] = records[i].ID
	}
	manifest := WritingRunManifest{
		SchemaVersion: WritingRunSchemaVersion,
		CreatedAt:     now.UTC(),
		InputPath:     inputPath,
		Identity: WritingRunIdentity{
			InputSHA256:             inputHash,
			BenchmarkID:             adapter.ID(),
			AdapterVersion:          adapter.Version(),
			JudgePromptVersion:      WritingJudgePromptVersion,
			GenerationPromptVersion: WritingGenerationPromptVersion,
			PromptIDs:               promptIDs,
			Models:                  append([]WritingModelSpec(nil), models...),
			Judge:                   judge,
			Iterations:              iterations,
			Concurrency:             concurrency,
			TimeoutSeconds:          int(timeout / time.Second),
			MaxRunCostUSD:           maxRunCost,
			Generation:              adapter.GenerationSettings(),
		},
	}
	identityJSON, _ := json.Marshal(manifest.Identity)
	runHash := sha256.Sum256(identityJSON)
	manifest.RunID = hex.EncodeToString(runHash[:8])
	return manifest
}

func PlanWritingRun(manifest WritingRunManifest, records []WritingPromptRecord, adapter WritingBenchmarkAdapter) (WritingRunPlan, error) {
	if err := ValidateWritingPromptRecords(records, adapter); err != nil {
		return WritingRunPlan{}, err
	}
	if len(manifest.Identity.Models) == 0 {
		return WritingRunPlan{}, fmt.Errorf("at least one generation model is required")
	}
	if manifest.Identity.Judge.Provider == "" || manifest.Identity.Judge.Model == "" {
		return WritingRunPlan{}, fmt.Errorf("an exact judge model is required")
	}
	if manifest.Identity.Iterations < 1 || manifest.Identity.Concurrency < 1 || manifest.Identity.TimeoutSeconds < 1 {
		return WritingRunPlan{}, fmt.Errorf("iterations, concurrency, and timeout must be positive")
	}
	for _, model := range append(append([]WritingModelSpec(nil), manifest.Identity.Models...), manifest.Identity.Judge) {
		if model.Provider == "" || model.Model == "" || model.MaxOutputTokens < 1 {
			return WritingRunPlan{}, fmt.Errorf("writing model identity and max output tokens are required")
		}
		if model.InputPer1M < 0 || model.OutputPer1M < 0 {
			return WritingRunPlan{}, fmt.Errorf("writing model prices cannot be negative")
		}
	}
	plan := WritingRunPlan{
		BenchmarkID:         adapter.ID(),
		ScoreIdentity:       adapter.ScoreIdentity(),
		PromptCount:         len(records),
		MaxJudgmentAttempts: maxWritingJudgmentAttempts,
	}
	for _, record := range records {
		criteria, err := adapter.Criteria(record)
		if err != nil {
			return WritingRunPlan{}, fmt.Errorf("prompt %q criteria: %w", record.ID, err)
		}
		for _, model := range manifest.Identity.Models {
			for iteration := 1; iteration <= manifest.Identity.Iterations; iteration++ {
				plan.GenerationCalls++
				plan.WorstCaseCostUSD += estimateWritingCallCost(record.Prompt, model.MaxOutputTokens, model)
				for _, criterion := range criteria {
					system, user := BuildWritingJudgePrompt(record, strings.Repeat("x", model.MaxOutputTokens*4), criterion)
					plan.JudgmentCalls++
					plan.WorstCaseCostUSD += float64(maxWritingJudgmentAttempts) * estimateWritingCallCost(system+"\n"+user, manifest.Identity.Judge.MaxOutputTokens, manifest.Identity.Judge)
				}
			}
		}
	}
	plan.WorstCaseProviderCalls = plan.GenerationCalls + plan.JudgmentCalls*maxWritingJudgmentAttempts
	return plan, nil
}

func writingGenerationKey(benchmark, promptID, model string, iteration int) string {
	return "g-" + writingHash(fmt.Sprintf("%s\x00%s\x00%s\x00%d", benchmark, promptID, model, iteration))[:24]
}

func writingJudgmentKey(generationKey, responseHash, criterionID, judgeID, promptVersion string) string {
	return "j-" + writingHash(strings.Join([]string{generationKey, responseHash, criterionID, judgeID, promptVersion}, "\x00"))[:24]
}

func writingHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func estimateWritingTokens(text string) int {
	runes := len([]rune(text))
	if runes == 0 {
		return 0
	}
	if runes < 4 {
		return 1
	}
	return runes / 4
}

func estimateWritingCallCost(prompt string, maxOutputTokens int, model WritingModelSpec) float64 {
	return float64(estimateWritingTokens(prompt))*model.InputPer1M/1_000_000 + float64(maxOutputTokens)*model.OutputPer1M/1_000_000
}
