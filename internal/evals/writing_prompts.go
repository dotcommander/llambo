package evals

import (
	"bufio"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// WritingPromptRecord is a normalized, runnable view of a public benchmark
// prompt. SourceRecord preserves the upstream fields needed by benchmark
// evaluators while Prompt gives callers one stable field to send to a model.
type WritingPromptRecord struct {
	BenchmarkID  string          `json:"benchmark_id"`
	ID           string          `json:"id"`
	Prompt       string          `json:"prompt"`
	SourceURL    string          `json:"source_url"`
	SourceRecord json.RawMessage `json:"source_record,omitempty"`
}

func writingPromptSourceSupported(benchmarkID string) bool {
	switch benchmarkID {
	case "writingbench", "eqbench-creative-v3", "ifeval":
		return true
	default:
		return false
	}
}

func extractWritingPromptRecords(benchmarkID, sourceURL string, body []byte) ([]WritingPromptRecord, error) {
	switch benchmarkID {
	case "writingbench", "ifeval":
		return extractWritingJSONLPrompts(benchmarkID, sourceURL, body)
	case "eqbench-creative-v3":
		return extractEQCreativePrompts(benchmarkID, sourceURL, body)
	default:
		return nil, nil
	}
}

func extractWritingJSONLPrompts(benchmarkID, sourceURL string, body []byte) ([]WritingPromptRecord, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	records := make([]WritingPromptRecord, 0)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			return records, fmt.Errorf("parse JSONL record %d: %w", lineNumber, err)
		}
		prompt := writingJSONFieldString(fields, "prompt", "query", "question", "instruction", "input")
		if prompt == "" {
			return records, fmt.Errorf("JSONL record %d has no prompt field", lineNumber)
		}
		id := writingJSONFieldString(fields, "key", "id", "index")
		if id == "" {
			id = strconv.Itoa(lineNumber)
		}
		records = append(records, WritingPromptRecord{
			BenchmarkID:  benchmarkID,
			ID:           id,
			Prompt:       prompt,
			SourceURL:    sourceURL,
			SourceRecord: json.RawMessage(append([]byte(nil), []byte(line)...)),
		})
	}
	if err := scanner.Err(); err != nil {
		return records, fmt.Errorf("read JSONL: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("JSONL contained no prompt records")
	}
	return records, nil
}

func extractEQCreativePrompts(benchmarkID, sourceURL string, body []byte) ([]WritingPromptRecord, error) {
	var prompts map[string]json.RawMessage
	if err := json.Unmarshal(body, &prompts); err != nil {
		return nil, fmt.Errorf("parse prompt JSON: %w", err)
	}
	if len(prompts) == 0 {
		return nil, fmt.Errorf("prompt JSON contained no records")
	}
	keys := make([]string, 0, len(prompts))
	for key := range prompts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	records := make([]WritingPromptRecord, 0, len(keys))
	for _, key := range keys {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(prompts[key], &fields); err != nil {
			return records, fmt.Errorf("parse prompt record %q: %w", key, err)
		}
		prompt := writingJSONFieldString(fields, "writing_prompt", "prompt", "query")
		if prompt == "" {
			return records, fmt.Errorf("prompt record %q has no writing_prompt field", key)
		}
		records = append(records, WritingPromptRecord{
			BenchmarkID:  benchmarkID,
			ID:           key,
			Prompt:       prompt,
			SourceURL:    sourceURL,
			SourceRecord: json.RawMessage(append([]byte(nil), prompts[key]...)),
		})
	}
	return records, nil
}

func writingJSONFieldString(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		var number json.Number
		if err := json.Unmarshal(raw, &number); err == nil && number.String() != "" {
			return number.String()
		}
	}
	return ""
}
