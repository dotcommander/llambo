package evals

import (
	"fmt"
	"strings"
)

func writingBenchScoreRow(headers map[string]int, row []string, rowIndex int) (WritingBenchScore, bool, error) {
	modelColumn, overallColumn := headers["model"], headers["overall"]
	organizationColumn, hasOrganization := headers["organization"]
	if modelColumn >= len(row) {
		return WritingBenchScore{}, false, nil
	}
	model := strings.TrimSpace(row[modelColumn])
	if model == "" {
		return WritingBenchScore{}, false, nil
	}
	if overallColumn >= len(row) {
		return WritingBenchScore{}, false, fmt.Errorf("WritingBench row %d has no Overall value", rowIndex+2)
	}
	overall, err := parseXLSXNumber(row[overallColumn])
	if err != nil {
		return WritingBenchScore{}, false, fmt.Errorf("WritingBench row %d overall: %w", rowIndex+2, err)
	}
	details := writingBenchRowDetails(headers, row)
	organization := ""
	if hasOrganization && organizationColumn < len(row) {
		organization = strings.TrimSpace(row[organizationColumn])
	}
	return WritingBenchScore{Model: model, Organization: organization, Result: BenchmarkResult{Score: &overall, Details: details}}, true, nil
}

func writingBenchRowDetails(headers map[string]int, row []string) map[string]float64 {
	details := make(map[string]float64)
	for header, column := range headers {
		if header == "model" || header == "organization" || header == "overall" || column >= len(row) || strings.TrimSpace(row[column]) == "" {
			continue
		}
		value, err := parseXLSXNumber(row[column])
		if err == nil {
			details[header] = value
		}
	}
	return details
}
