package evals

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const EQBenchCreativeCommit = "bf21868fd5dc4c48480e01ae354079eb1cec13fb"

type EQBenchCreativeScore struct {
	Model        string          `json:"model"`
	Organization string          `json:"organization,omitempty"`
	Result       BenchmarkResult `json:"result"`
}

// ParseEQBenchCreativeJS extracts the source CSV template literal from the
// pinned EQ-Bench Creative v3 JavaScript artifact. elo_score is the published
// corroborating score; all other numeric columns remain source-native details.
func ParseEQBenchCreativeJS(data []byte) ([]EQBenchCreativeScore, error) {
	csvData, err := eqBenchCreativeCSV(data)
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(bytes.NewReader(csvData))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse EQ-Bench Creative CSV: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("EQ-Bench Creative CSV has no scores")
	}
	headers := make(map[string]int, len(records[0]))
	for index, header := range records[0] {
		header = strings.ToLower(strings.TrimSpace(header))
		if header != "" {
			headers[header] = index
		}
	}
	modelColumn, hasModel := headers["model_name"]
	eloColumn, hasELO := headers["elo_score"]
	if !hasModel || !hasELO {
		return nil, fmt.Errorf("EQ-Bench Creative CSV requires model_name and elo_score")
	}
	organizationColumn, hasOrganization := headers["organization"]
	result := make([]EQBenchCreativeScore, 0, len(records)-1)
	for line, record := range records[1:] {
		if modelColumn >= len(record) || strings.TrimSpace(record[modelColumn]) == "" {
			continue
		}
		if eloColumn >= len(record) {
			return nil, fmt.Errorf("EQ-Bench Creative row %d has no elo_score", line+2)
		}
		elo, err := eqBenchNumber(record[eloColumn])
		if err != nil {
			return nil, fmt.Errorf("EQ-Bench Creative row %d elo_score: %w", line+2, err)
		}
		details := make(map[string]float64)
		for header, column := range headers {
			if header == "model_name" || header == "organization" || header == "elo_score" || column >= len(record) || strings.TrimSpace(record[column]) == "" {
				continue
			}
			if number, err := eqBenchNumber(record[column]); err == nil {
				details[header] = number
			}
		}
		organization := ""
		if hasOrganization && organizationColumn < len(record) {
			organization = strings.TrimSpace(record[organizationColumn])
		}
		result = append(result, EQBenchCreativeScore{Model: strings.TrimSpace(record[modelColumn]), Organization: organization, Result: BenchmarkResult{Score: &elo, Details: details}})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("EQ-Bench Creative CSV has no scored models")
	}
	return result, nil
}

func eqBenchCreativeCSV(data []byte) ([]byte, error) {
	const header = "model_name,elo_score"
	start := bytes.Index(data, []byte(header))
	if start < 0 {
		return nil, fmt.Errorf("EQ-Bench Creative JS has no score CSV header")
	}
	open := bytes.LastIndexByte(data[:start], '`')
	if open < 0 {
		return nil, fmt.Errorf("EQ-Bench Creative CSV is not a template literal")
	}
	close := bytes.IndexByte(data[start:], '`')
	if close < 0 {
		return nil, fmt.Errorf("EQ-Bench Creative CSV template literal is unterminated")
	}
	return data[open+1 : start+close], nil
}

func eqBenchNumber(value string) (float64, error) {
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("expected number %q", value)
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("expected finite number %q", value)
	}
	return number, nil
}
