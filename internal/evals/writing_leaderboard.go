package evals

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var markdownLinkRE = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)

// ParseWritingLeaderboard parses source-native writing leaderboard rows.
func ParseWritingLeaderboard(markdown string) ([]WritingLeaderboardRow, error) {
	lines := strings.Split(markdown, "\n")
	headerIndex := -1
	var headers []string
	for index, line := range lines {
		columns := splitMarkdownTableRow(line)
		if len(columns) == 0 {
			continue
		}
		joined := strings.ToLower(strings.Join(columns, " "))
		if strings.Contains(joined, "rank") && strings.Contains(joined, "comparison score") && strings.Contains(joined, "uncertainty") {
			headerIndex = index
			headers = columns
			break
		}
	}
	if headerIndex < 0 {
		return nil, errors.New("writing leaderboard table header not found")
	}

	rankColumn := findWritingColumn(headers, "rank")
	modelColumn := findWritingColumn(headers, "model")
	scoreColumn := findWritingColumn(headers, "comparison score")
	winChanceColumn := findWritingColumn(headers, "estimated win chance")
	uncertaintyColumn := findWritingColumn(headers, "uncertainty")
	if rankColumn < 0 || modelColumn < 0 || scoreColumn < 0 || winChanceColumn < 0 || uncertaintyColumn < 0 {
		return nil, errors.New("writing leaderboard table is missing required columns")
	}

	rows := make([]WritingLeaderboardRow, 0, 32)
	for _, line := range lines[headerIndex+1:] {
		row, admitted := parseWritingLeaderboardRow(line, writingLeaderboardColumns{rank: rankColumn, model: modelColumn, score: scoreColumn, winChance: winChanceColumn, uncertainty: uncertaintyColumn})
		if admitted {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return nil, errors.New("writing leaderboard table contained no data rows")
	}
	return rows, nil
}

type writingLeaderboardColumns struct {
	rank, model, score, winChance, uncertainty int
}

func parseWritingLeaderboardRow(line string, selected writingLeaderboardColumns) (WritingLeaderboardRow, bool) {
	rankColumn, modelColumn, scoreColumn := selected.rank, selected.model, selected.score
	winChanceColumn, uncertaintyColumn := selected.winChance, selected.uncertainty
	columns := splitMarkdownTableRow(line)
	if len(columns) <= maxWritingColumn(rankColumn, modelColumn, scoreColumn, winChanceColumn, uncertaintyColumn) {
		return WritingLeaderboardRow{}, false
	}
	rank, err := strconv.Atoi(strings.TrimSpace(columns[rankColumn]))
	if err != nil {
		return WritingLeaderboardRow{}, false
	}
	score, err := parseWritingFloat(columns[scoreColumn])
	if err != nil {
		return WritingLeaderboardRow{}, false
	}
	winChance, err := parseWritingPercent(columns[winChanceColumn])
	if err != nil {
		return WritingLeaderboardRow{}, false
	}
	lower, upper, err := parseWritingRange(columns[uncertaintyColumn])
	if err != nil {
		return WritingLeaderboardRow{}, false
	}
	model := cleanWritingModelName(columns[modelColumn])
	if model == "" {
		return WritingLeaderboardRow{}, false
	}
	return WritingLeaderboardRow{
		Rank:      rank,
		Model:     model,
		Score:     score,
		WinChance: winChance,
		Lower:     lower,
		Upper:     upper,
	}, true
}

func splitMarkdownTableRow(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.Contains(line, "|") {
		return nil
	}
	columns := strings.Split(line, "|")
	if len(columns) > 0 && strings.TrimSpace(columns[0]) == "" {
		columns = columns[1:]
	}
	if len(columns) > 0 && strings.TrimSpace(columns[len(columns)-1]) == "" {
		columns = columns[:len(columns)-1]
	}
	for index := range columns {
		columns[index] = strings.TrimSpace(columns[index])
	}
	return columns
}

func findWritingColumn(columns []string, want string) int {
	for index, column := range columns {
		normalized := strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(column, "_", " "))), " ")
		if strings.Contains(normalized, want) {
			return index
		}
	}
	return -1
}

func maxWritingColumn(columns ...int) int {
	maximum := -1
	for _, column := range columns {
		if column > maximum {
			maximum = column
		}
	}
	return maximum
}

func parseWritingFloat(value string) (float64, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "−", "-"))
	value = strings.Trim(value, "`*$")
	return strconv.ParseFloat(value, 64)
}

func parseWritingPercent(value string) (float64, error) {
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	return parseWritingFloat(value)
}

func parseWritingRange(value string) (minValue, maxValue float64, err error) {
	value = strings.ReplaceAll(strings.ToLower(value), "−", "-")
	value = strings.ReplaceAll(value, "to", " ")
	parts := strings.Fields(value)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("invalid uncertainty range %q", value)
	}
	lower, err := parseWritingFloat(parts[0])
	if err != nil {
		return 0, 0, err
	}
	upper, err := parseWritingFloat(parts[len(parts)-1])
	if err != nil {
		return 0, 0, err
	}
	return lower, upper, nil
}

func cleanWritingModelName(value string) string {
	value = markdownLinkRE.ReplaceAllString(value, "$1")
	value = strings.TrimSpace(strings.Trim(value, "`"))
	return strings.TrimRight(value, " †‡§*")
}
