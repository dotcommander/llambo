package evals

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var receiptSHA256 = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

type ValidationDiagnostic struct {
	Source     string               `json:"source"`
	Categories []ValidationCategory `json:"categories"`
}

type ValidationCategory struct {
	Category   string   `json:"category"`
	SampleSize int      `json:"sample_size"`
	Spearman   *float64 `json:"spearman_rank_correlation"`
	Status     string   `json:"status"`
}

type validationReceiptDocument struct {
	Version int                       `json:"version"`
	Records []validationReceiptRecord `json:"records"`
}

type validationReceiptManifest struct {
	Version int                        `json:"version"`
	Records []validationReceiptPointer `json:"records"`
}

type validationReceiptPointer struct {
	ReceiptPath   string `json:"receipt_path"`
	ReceiptSHA256 string `json:"receipt_sha256"`
}

type validationReceiptRecord struct {
	ModelKey      string  `json:"model_key"`
	Category      string  `json:"category"`
	Score         float64 `json:"score"`
	IdentityMatch string  `json:"identity_match"`
}

// ApplyValidationReceipts annotates a report with a read-only comparison to
// sealed local receipts. It never changes scores, row order, or filtering.
func ApplyValidationReceipts(report *Report, path string) error {
	if report == nil {
		return fmt.Errorf("validation report is nil")
	}
	document, err := readValidationReceipts(path)
	if err != nil {
		return err
	}
	applyValidationReceiptDocument(report, document, path)
	return nil
}

func applyValidationReceiptDocument(report *Report, document validationReceiptDocument, source string) {
	byModel := make(map[string]ReportModel, len(report.Models))
	for _, model := range report.Models {
		if model.Projection == nil {
			byModel[model.Key] = model
		}
	}
	byCategory := make(map[string][]validationPair)
	for _, record := range document.Records {
		model, ok := byModel[record.ModelKey]
		if !ok {
			continue
		}
		score := model.LlamboScores[record.Category]
		if score == nil {
			continue
		}
		byCategory[record.Category] = append(byCategory[record.Category], validationPair{external: score.Score, local: record.Score})
	}
	categories := make([]ValidationCategory, 0, len(categorySpecs))
	for _, spec := range categorySpecs {
		pairs := byCategory[spec.name]
		category := ValidationCategory{Category: spec.name, SampleSize: len(pairs), Status: "insufficient_validation"}
		if len(pairs) >= 8 {
			category.Status = "sufficient"
			correlation := spearmanCorrelation(pairs)
			category.Spearman = correlation
		}
		categories = append(categories, category)
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i].Category < categories[j].Category })
	report.Validation = &ValidationDiagnostic{Source: source, Categories: categories}
}

func readValidationReceipts(path string) (validationReceiptDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return validationReceiptDocument{}, fmt.Errorf("read validation receipts: %w", err)
	}
	manifest, err := parseValidationReceiptManifest(data)
	if err != nil {
		return validationReceiptDocument{}, err
	}
	document := validationReceiptDocument{Version: 1, Records: make([]validationReceiptRecord, 0, len(manifest.Records))}
	for index, pointer := range manifest.Records {
		receiptPath := pointer.ReceiptPath
		if !filepath.IsAbs(receiptPath) {
			receiptPath = filepath.Join(filepath.Dir(path), receiptPath)
		}
		receipt, err := os.ReadFile(receiptPath)
		if err != nil {
			return validationReceiptDocument{}, fmt.Errorf("read validation receipt %d: %w", index+1, err)
		}
		sum := fmt.Sprintf("%x", sha256.Sum256(receipt))
		if !strings.EqualFold(sum, pointer.ReceiptSHA256) {
			return validationReceiptDocument{}, fmt.Errorf("validation receipt %d SHA-256 mismatch", index+1)
		}
		record, err := parseValidationReceipt(receipt)
		if err != nil {
			return validationReceiptDocument{}, fmt.Errorf("validation receipt %d: %w", index+1, err)
		}
		document.Records = append(document.Records, record)
	}
	if err := validateValidationRecords(document.Records); err != nil {
		return validationReceiptDocument{}, err
	}
	return document, nil
}

func parseValidationReceiptManifest(data []byte) (validationReceiptManifest, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var manifest validationReceiptManifest
	if err := decoder.Decode(&manifest); err != nil {
		return validationReceiptManifest{}, fmt.Errorf("decode validation receipt manifest: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return validationReceiptManifest{}, fmt.Errorf("decode validation receipt manifest: %w", err)
	}
	if manifest.Version != 1 {
		return validationReceiptManifest{}, fmt.Errorf("unsupported validation receipt manifest version %d", manifest.Version)
	}
	for index, record := range manifest.Records {
		record.ReceiptPath, record.ReceiptSHA256 = strings.TrimSpace(record.ReceiptPath), strings.TrimSpace(record.ReceiptSHA256)
		if record.ReceiptPath == "" || !receiptSHA256.MatchString(record.ReceiptSHA256) {
			return validationReceiptManifest{}, fmt.Errorf("invalid validation receipt pointer %d", index+1)
		}
		manifest.Records[index] = record
	}
	return manifest, nil
}

func parseValidationReceipt(data []byte) (validationReceiptRecord, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var record validationReceiptRecord
	if err := decoder.Decode(&record); err != nil {
		return validationReceiptRecord{}, fmt.Errorf("decode sealed receipt: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return validationReceiptRecord{}, fmt.Errorf("decode sealed receipt: %w", err)
	}
	return record, nil
}

func validateValidationRecords(records []validationReceiptRecord) error {
	seen := make(map[string]bool, len(records))
	for index, record := range records {
		record.ModelKey, record.Category, record.IdentityMatch = strings.TrimSpace(record.ModelKey), strings.TrimSpace(record.Category), strings.TrimSpace(record.IdentityMatch)
		if record.ModelKey == "" || !isCapabilityCategory(record.Category) || record.IdentityMatch != string(IdentityMatchExact) || math.IsNaN(record.Score) || math.IsInf(record.Score, 0) {
			return fmt.Errorf("invalid sealed validation receipt %d", index+1)
		}
		key := record.ModelKey + "\x00" + record.Category
		if seen[key] {
			return fmt.Errorf("duplicate validation receipt model/category %q/%q", record.ModelKey, record.Category)
		}
		seen[key] = true
	}
	return nil
}

type validationPair struct{ external, local float64 }

func spearmanCorrelation(pairs []validationPair) *float64 {
	if len(pairs) < 2 {
		return nil
	}
	left, right := make([]float64, len(pairs)), make([]float64, len(pairs))
	for i, pair := range pairs {
		left[i], right[i] = pair.external, pair.local
	}
	left, right = averageRanks(left), averageRanks(right)
	meanLeft, meanRight := mean(left), mean(right)
	covariance, leftVariance, rightVariance := 0.0, 0.0, 0.0
	for i := range left {
		dx, dy := left[i]-meanLeft, right[i]-meanRight
		covariance += dx * dy
		leftVariance += dx * dx
		rightVariance += dy * dy
	}
	if leftVariance == 0 || rightVariance == 0 {
		return nil
	}
	value := covariance / math.Sqrt(leftVariance*rightVariance)
	return &value
}

func averageRanks(values []float64) []float64 {
	indices := make([]int, len(values))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool { return values[indices[i]] < values[indices[j]] })
	ranks := make([]float64, len(values))
	for start := 0; start < len(indices); {
		end := start + 1
		for end < len(indices) && values[indices[end]] == values[indices[start]] {
			end++
		}
		rank := (float64(start+1) + float64(end)) / 2
		for _, index := range indices[start:end] {
			ranks[index] = rank
		}
		start = end
	}
	return ranks
}
