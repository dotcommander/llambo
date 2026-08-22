package evals

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidationReceiptsDoNotChangeScoresOrOrder(t *testing.T) {
	report, document := validationFixture(8)
	beforeKeys := validationModelKeys(report.Models)
	beforeScore := report.Models[0].LlamboScores["coding"].Score
	applyValidationReceiptDocument(&report, document, "sealed.json")
	if strings.Join(beforeKeys, ",") != strings.Join(validationModelKeys(report.Models), ",") || report.Models[0].LlamboScores["coding"].Score != beforeScore {
		t.Fatalf("validation changed scored report: %#v", report)
	}
	coding := validationCategory(report.Validation, "coding")
	if coding.SampleSize != 8 || coding.Status != "sufficient" || coding.Spearman == nil || math.Abs(*coding.Spearman-1) > 1e-9 {
		t.Fatalf("unexpected coding diagnostic: %#v", coding)
	}
	if !strings.Contains(RenderMarkdown(report, 0), "Local validation diagnostic") || !strings.Contains(RenderHTML(report, 0), "Local validation diagnostic") {
		t.Fatal("validation diagnostic was not rendered")
	}
}

func TestValidationSpearmanUsesAverageRanksForTies(t *testing.T) {
	value := spearmanCorrelation([]validationPair{{1, 1}, {2, 2}, {2, 2}, {3, 3}})
	if value == nil || math.Abs(*value-1) > 1e-9 {
		t.Fatalf("tie correlation = %#v", value)
	}
}

func TestValidationReceiptsRejectMalformedRecords(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, body := range []string{
		`{"version":2,"records":[]}`,
		fmt.Sprintf(`{"version":1,"records":[{"receipt_path":"","receipt_sha256":"%s"}]}`, hash),
		`{"version":1,"records":[{"receipt_path":"receipt.json","receipt_sha256":"bad"}]}`,
	} {
		if _, err := parseValidationReceiptManifest([]byte(body)); err == nil {
			t.Fatalf("accepted malformed receipts: %s", body)
		}
	}
}

func TestValidationReceiptsVerifySealedContent(t *testing.T) {
	dir := t.TempDir()
	receipt := []byte(`{"model_key":"m","category":"coding","score":1,"identity_match":"exact"}`)
	receiptPath := filepath.Join(dir, "receipt.json")
	if err := os.WriteFile(receiptPath, receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(receipt))
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest := []byte(fmt.Sprintf(`{"version":1,"records":[{"receipt_path":"receipt.json","receipt_sha256":"%s"}]}`, sum))
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if document, err := readValidationReceipts(manifestPath); err != nil || len(document.Records) != 1 {
		t.Fatalf("verified receipt rejected: records=%#v err=%v", document.Records, err)
	}
	manifest = []byte(fmt.Sprintf(`{"version":1,"records":[{"receipt_path":"receipt.json","receipt_sha256":"%s"}]}`, strings.Repeat("0", 64)))
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readValidationReceipts(manifestPath); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("tampered receipt accepted: %v", err)
	}
}

func TestValidationBelowEightIsInsufficient(t *testing.T) {
	report, document := validationFixture(7)
	applyValidationReceiptDocument(&report, document, "sealed.json")
	coding := validationCategory(report.Validation, "coding")
	if coding.SampleSize != 7 || coding.Status != "insufficient_validation" || coding.Spearman != nil {
		t.Fatalf("unexpected below-eight result: %#v", coding)
	}
}

func validationFixture(count int) (Report, validationReceiptDocument) {
	models := make([]ReportModel, count)
	records := make([]validationReceiptRecord, count)
	for i := range models {
		value := float64(i + 1)
		key := fmt.Sprintf("m-%d", i)
		models[i] = ReportModel{Key: key, Name: key, LlamboScores: map[string]*LlamboScore{"coding": {Score: value}}, Scores: map[string]*ExternalScore{}}
		records[i] = validationReceiptRecord{ModelKey: key, Category: "coding", Score: value, IdentityMatch: "exact"}
	}
	return Report{Models: models}, validationReceiptDocument{Version: 1, Records: records}
}

func validationCategory(diagnostic *ValidationDiagnostic, name string) ValidationCategory {
	for _, category := range diagnostic.Categories {
		if category.Category == name {
			return category
		}
	}
	return ValidationCategory{}
}
func validationModelKeys(models []ReportModel) []string {
	keys := make([]string, len(models))
	for i, model := range models {
		keys[i] = model.Key
	}
	return keys
}
