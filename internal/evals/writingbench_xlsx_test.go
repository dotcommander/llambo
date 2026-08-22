package evals

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestParseWritingBenchXLSX(t *testing.T) {
	data := syntheticWritingBenchXLSX(t)
	rows, err := ParseWritingBenchXLSX(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Model != "Llambo-1" || row.Organization != "Example Labs" || row.Result.Score == nil || *row.Result.Score != 8.7 {
		t.Fatalf("unexpected row: %#v", row)
	}
	if row.Result.Details["domain: fiction"] != 9.1 || row.Result.Details["format"] != 8.4 {
		t.Fatalf("details = %#v", row.Result.Details)
	}
}

func TestParseWritingBenchXLSXRejectsMalformedInput(t *testing.T) {
	if _, err := ParseWritingBenchXLSX([]byte("not a zip")); err == nil {
		t.Fatal("malformed workbook accepted")
	}
	data := writeSyntheticXLSX(t, map[string]string{"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>Model</t></is></c></row></sheetData></worksheet>`})
	if _, err := ParseWritingBenchXLSX(data); err == nil {
		t.Fatal("missing Overall header accepted")
	}
	for _, value := range []string{"", "NaN", "+Inf"} {
		data = writeSyntheticXLSX(t, map[string]string{
			"xl/sharedStrings.xml": `<sst><si><t>Model</t></si><si><t>Overall</t></si><si><t>model</t></si></sst>`,
			"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row><row r="2"><c r="A2" t="s"><v>2</v></c>` + func() string {
				if value == "" {
					return ""
				}
				return `<c r="B2"><v>` + value + `</v></c>`
			}() + `</row></sheetData></worksheet>`,
		})
		if _, err := ParseWritingBenchXLSX(data); err == nil {
			t.Fatalf("invalid Overall %q accepted", value)
		}
	}
}

func syntheticWritingBenchXLSX(t *testing.T) []byte {
	t.Helper()
	return writeSyntheticXLSX(t, map[string]string{
		"xl/sharedStrings.xml":     `<sst><si><t>Model</t></si><si><t>Overall</t></si><si><t>Organization</t></si><si><t>Domain: Fiction</t></si><si><t>Format</t></si><si><t>Llambo-1</t></si><si><t>Example Labs</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c><c r="E1" t="s"><v>4</v></c></row><row r="2"><c r="A2" t="s"><v>5</v></c><c r="B2"><v>8.7</v></c><c r="C2" t="s"><v>6</v></c><c r="D2"><v>9.1</v></c><c r="E2"><v>8.4</v></c></row></sheetData></worksheet>`,
	})
}

func writeSyntheticXLSX(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, body := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
