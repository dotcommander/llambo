package evals

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
)

// WritingBenchScore is one source-native row from WritingBench's official
// versioned score.xlsx. Result.Details keeps every additional numeric column
// without assigning it scoring weight.
type WritingBenchScore struct {
	Model        string          `json:"model"`
	Organization string          `json:"organization,omitempty"`
	Result       BenchmarkResult `json:"result"`
}

// ParseWritingBenchXLSX parses the first worksheet in an official
// WritingBench score workbook using only the Go standard library. The caller
// supplies snapshot provenance after fetching and hashing the artifact.
func ParseWritingBenchXLSX(data []byte) ([]WritingBenchScore, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open WritingBench XLSX: %w", err)
	}
	shared, err := readXLSXSharedStrings(reader)
	if err != nil {
		return nil, err
	}
	worksheet, err := firstXLSXWorksheet(reader)
	if err != nil {
		return nil, err
	}
	rows, err := readXLSXRows(worksheet, shared)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("WritingBench XLSX has no rows")
	}
	headers := normalizeXLSXHeaders(rows[0])
	modelColumn, hasModel := headers["model"]
	overallColumn, hasOverall := headers["overall"]
	if !hasModel || !hasOverall {
		return nil, fmt.Errorf("WritingBench XLSX requires Model and Overall headers")
	}
	organizationColumn, hasOrganization := headers["organization"]
	results := make([]WritingBenchScore, 0, len(rows)-1)
	for rowIndex, row := range rows[1:] {
		if modelColumn >= len(row) {
			continue
		}
		model := strings.TrimSpace(row[modelColumn])
		if model == "" {
			continue
		}
		if overallColumn >= len(row) {
			return nil, fmt.Errorf("WritingBench row %d has no Overall value", rowIndex+2)
		}
		overall, err := parseXLSXNumber(row[overallColumn])
		if err != nil {
			return nil, fmt.Errorf("WritingBench row %d overall: %w", rowIndex+2, err)
		}
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
		organization := ""
		if hasOrganization && organizationColumn < len(row) {
			organization = strings.TrimSpace(row[organizationColumn])
		}
		results = append(results, WritingBenchScore{Model: model, Organization: organization, Result: BenchmarkResult{Score: &overall, Details: details}})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("WritingBench XLSX has no scored models")
	}
	return results, nil
}

type xlsxWorksheet struct {
	SheetData struct {
		Rows []xlsxRow `xml:"row"`
	} `xml:"sheetData"`
}
type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}
type xlsxCell struct {
	Reference string `xml:"r,attr"`
	Type      string `xml:"t,attr"`
	Value     string `xml:"v"`
	Inline    struct {
		Text string `xml:"t"`
	} `xml:"is"`
}
type xlsxSharedStrings struct {
	Items []struct {
		Text string `xml:"t"`
		Runs []struct {
			Text string `xml:"t"`
		} `xml:"r"`
	} `xml:"si"`
}

func readXLSXSharedStrings(reader *zip.Reader) ([]string, error) {
	file := findXLSXFile(reader, "xl/sharedStrings.xml")
	if file == nil {
		return nil, nil
	}
	stream, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open XLSX shared strings: %w", err)
	}
	defer stream.Close()
	var document xlsxSharedStrings
	if err := xml.NewDecoder(io.LimitReader(stream, 16<<20)).Decode(&document); err != nil {
		return nil, fmt.Errorf("decode XLSX shared strings: %w", err)
	}
	values := make([]string, len(document.Items))
	for i, item := range document.Items {
		values[i] = item.Text
		for _, run := range item.Runs {
			values[i] += run.Text
		}
	}
	return values, nil
}

func firstXLSXWorksheet(reader *zip.Reader) (*zip.File, error) {
	files := make([]*zip.File, 0)
	for _, file := range reader.File {
		if strings.HasPrefix(file.Name, "xl/worksheets/") && strings.HasSuffix(file.Name, ".xml") {
			files = append(files, file)
		}
	}
	sort.Slice(files, func(i, j int) bool { return path.Base(files[i].Name) < path.Base(files[j].Name) })
	if len(files) == 0 {
		return nil, fmt.Errorf("WritingBench XLSX has no worksheet")
	}
	return files[0], nil
}

func readXLSXRows(file *zip.File, shared []string) ([][]string, error) {
	stream, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open XLSX worksheet: %w", err)
	}
	defer stream.Close()
	var sheet xlsxWorksheet
	if err := xml.NewDecoder(io.LimitReader(stream, 32<<20)).Decode(&sheet); err != nil {
		return nil, fmt.Errorf("decode XLSX worksheet: %w", err)
	}
	rows := make([][]string, 0, len(sheet.SheetData.Rows))
	for _, source := range sheet.SheetData.Rows {
		row := make([]string, 0, len(source.Cells))
		for _, cell := range source.Cells {
			column, err := xlsxColumn(cell.Reference)
			if err != nil {
				return nil, err
			}
			for len(row) <= column {
				row = append(row, "")
			}
			value := strings.TrimSpace(cell.Value)
			switch cell.Type {
			case "s":
				index, err := strconv.Atoi(value)
				if err != nil || index < 0 || index >= len(shared) {
					return nil, fmt.Errorf("invalid XLSX shared string index %q", value)
				}
				value = shared[index]
			case "inlineStr":
				value = cell.Inline.Text
			}
			row[column] = strings.TrimSpace(value)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func normalizeXLSXHeaders(row []string) map[string]int {
	result := make(map[string]int, len(row))
	for column, header := range row {
		header = strings.ToLower(strings.TrimSpace(header))
		if header != "" {
			result[header] = column
		}
	}
	return result
}
func parseXLSXNumber(value string) (float64, error) {
	value = strings.TrimSpace(value)
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("expected number %q", value)
	}
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("expected finite number %q", value)
	}
	return parsed, nil
}
func findXLSXFile(reader *zip.Reader, name string) *zip.File {
	for _, file := range reader.File {
		if file.Name == name {
			return file
		}
	}
	return nil
}
func xlsxColumn(reference string) (int, error) {
	reference = strings.TrimSpace(reference)
	column := 0
	found := false
	for _, rune := range reference {
		if rune >= 'A' && rune <= 'Z' {
			column = column*26 + int(rune-'A'+1)
			found = true
			continue
		}
		if rune >= 'a' && rune <= 'z' {
			column = column*26 + int(rune-'a'+1)
			found = true
			continue
		}
		break
	}
	if !found {
		return 0, fmt.Errorf("invalid XLSX cell reference %q", reference)
	}
	return column - 1, nil
}
