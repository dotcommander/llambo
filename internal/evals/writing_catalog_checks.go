package evals

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const huggingFaceModelAPI = "https://huggingface.co/api/models/"

type WritingSourceStatus struct {
	BenchmarkID string    `json:"benchmark_id"`
	SourceURL   string    `json:"source_url"`
	FetchedAt   time.Time `json:"fetched_at"`
	Status      string    `json:"status"`
	HTTPStatus  int       `json:"http_status,omitempty"`
	ContentType string    `json:"content_type,omitempty"`
	Bytes       int       `json:"bytes,omitempty"`
	Records     int       `json:"records,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type WritingModelStatus struct {
	ModelID         string    `json:"model_id"`
	ModelName       string    `json:"model_name"`
	MetadataURL     string    `json:"metadata_url"`
	FetchedAt       time.Time `json:"fetched_at"`
	Status          string    `json:"status"`
	HTTPStatus      int       `json:"http_status,omitempty"`
	RegistryLicense string    `json:"registry_license,omitempty"`
	RemoteLicense   string    `json:"remote_license,omitempty"`
	LicenseMatch    bool      `json:"license_match"`
	PipelineTag     string    `json:"pipeline_tag,omitempty"`
	CreatedAt       string    `json:"created_at,omitempty"`
	LastModified    string    `json:"last_modified,omitempty"`
	Downloads       int64     `json:"downloads,omitempty"`
	Gated           bool      `json:"gated,omitempty"`
	Private         bool      `json:"private,omitempty"`
	Error           string    `json:"error,omitempty"`
}

type writingHTTPArtifact struct {
	Body        []byte
	FetchedAt   time.Time
	HTTPStatus  int
	ContentType string
	Bytes       int
}

func fetchWritingArtifact(ctx context.Context, client *http.Client, sourceURL string, fetchedAt time.Time) (writingHTTPArtifact, error) {
	artifact := writingHTTPArtifact{FetchedAt: fetchedAt}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return artifact, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json, text/markdown, text/plain;q=0.9, text/html;q=0.8")
	request.Header.Set("User-Agent", "llambo-writing-benchmarks/1")
	response, err := client.Do(request)
	if err != nil {
		return artifact, err
	}
	defer response.Body.Close()
	artifact.HTTPStatus = response.StatusCode
	artifact.ContentType = response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return artifact, fmt.Errorf("HTTP %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxWritingSourceBody+1))
	if err != nil {
		return artifact, fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxWritingSourceBody {
		return artifact, fmt.Errorf("response exceeds %d-byte limit", maxWritingSourceBody)
	}
	artifact.Body = body
	artifact.Bytes = len(body)
	return artifact, nil
}

func fetchWritingSourceChecks(ctx context.Context, client *http.Client, catalog WritingCatalog, opts WritingCatalogOptions, fetchedAt time.Time) ([]WritingSourceStatus, []WritingPromptRecord) {
	checks := make([]WritingSourceStatus, 0, len(catalog.Benchmarks)-1)
	promptRecords := make([]WritingPromptRecord, 0)
	for _, benchmark := range catalog.Benchmarks {
		if benchmark.ID == WritingPrimaryID {
			continue
		}
		sourceURL := benchmark.DataURL
		if sourceURL == "" {
			sourceURL = benchmark.URL
		}
		if override := opts.SourceURLs[benchmark.ID]; override != "" {
			sourceURL = override
		}
		artifact, err := fetchWritingArtifact(ctx, client, sourceURL, fetchedAt)
		check := WritingSourceStatus{
			BenchmarkID: benchmark.ID,
			SourceURL:   sourceURL,
			FetchedAt:   artifact.FetchedAt,
			Status:      "error",
			HTTPStatus:  artifact.HTTPStatus,
			ContentType: artifact.ContentType,
			Bytes:       artifact.Bytes,
		}
		if err != nil {
			check.Error = err.Error()
			checks = append(checks, check)
			continue
		}
		var records []WritingPromptRecord
		if writingPromptSourceSupported(benchmark.ID) {
			records, err = extractWritingPromptRecords(benchmark.ID, sourceURL, artifact.Body)
			check.Records = len(records)
		} else {
			check.Records, err = inspectWritingArtifact(benchmark.ID, artifact.Body)
		}
		if err != nil {
			check.Status = "invalid"
			check.Error = err.Error()
		} else {
			check.Status = "available"
			if opts.IncludePromptRecords {
				promptRecords = append(promptRecords, records...)
			}
		}
		checks = append(checks, check)
	}
	return checks, promptRecords
}

func inspectWritingArtifact(benchmarkID string, body []byte) (int, error) {
	if writingPromptSourceSupported(benchmarkID) {
		records, err := extractWritingPromptRecords(benchmarkID, "", body)
		return len(records), err
	}
	return 0, nil
}

func applyWritingPromptCounts(catalog *WritingCatalog) {
	for _, check := range catalog.SourceChecks {
		if check.Records == 0 {
			continue
		}
		for index := range catalog.Benchmarks {
			if catalog.Benchmarks[index].ID != check.BenchmarkID {
				continue
			}
			switch check.BenchmarkID {
			case "writingbench", "eqbench-creative-v3", "ifeval":
				catalog.Benchmarks[index].PromptCount = check.Records
			}
		}
	}
}

func countWritingJSONL(body []byte) (int, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record json.RawMessage
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return count, fmt.Errorf("parse JSONL record %d: %w", count+1, err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("read JSONL: %w", err)
	}
	if count == 0 {
		return 0, fmt.Errorf("JSONL contained no records")
	}
	return count, nil
}

func fetchWritingModelChecks(ctx context.Context, client *http.Client, models []WritingOpenModel, opts WritingCatalogOptions, fetchedAt time.Time) []WritingModelStatus {
	checks := make([]WritingModelStatus, 0, len(models))
	for _, model := range models {
		metadataURL := huggingFaceModelAPI + model.ID
		if override := opts.ModelURLs[model.ID]; override != "" {
			metadataURL = override
		}
		artifact, err := fetchWritingArtifact(ctx, client, metadataURL, fetchedAt)
		check := WritingModelStatus{
			ModelID:         model.ID,
			ModelName:       model.Name,
			MetadataURL:     metadataURL,
			FetchedAt:       artifact.FetchedAt,
			Status:          "error",
			HTTPStatus:      artifact.HTTPStatus,
			RegistryLicense: model.License,
		}
		if err != nil {
			check.Error = err.Error()
			checks = append(checks, check)
			continue
		}
		var metadata huggingFaceModelMetadata
		if err := json.Unmarshal(artifact.Body, &metadata); err != nil {
			check.Status = "invalid"
			check.Error = fmt.Sprintf("parse model metadata: %v", err)
			checks = append(checks, check)
			continue
		}
		if metadata.ID != "" && metadata.ID != model.ID {
			check.Status = "invalid"
			check.Error = fmt.Sprintf("metadata id %q does not match %q", metadata.ID, model.ID)
			checks = append(checks, check)
			continue
		}
		check.Status = "available"
		check.RemoteLicense = metadata.license()
		check.LicenseMatch = normalizeWritingLicense(check.RegistryLicense) != "" && normalizeWritingLicense(check.RegistryLicense) == normalizeWritingLicense(check.RemoteLicense)
		check.PipelineTag = metadata.PipelineTag
		check.CreatedAt = metadata.CreatedAt
		check.LastModified = metadata.LastModified
		check.Downloads = metadata.Downloads
		check.Gated = metadata.Gated
		check.Private = metadata.Private
		checks = append(checks, check)
	}
	return checks
}

type huggingFaceModelMetadata struct {
	ID           string   `json:"id"`
	Private      bool     `json:"private"`
	PipelineTag  string   `json:"pipeline_tag"`
	Downloads    int64    `json:"downloads"`
	Gated        bool     `json:"gated"`
	CreatedAt    string   `json:"createdAt"`
	LastModified string   `json:"lastModified"`
	Tags         []string `json:"tags"`
	CardData     struct {
		License string `json:"license"`
	} `json:"cardData"`
}

func (metadata huggingFaceModelMetadata) license() string {
	if metadata.CardData.License != "" {
		return metadata.CardData.License
	}
	for _, tag := range metadata.Tags {
		if strings.HasPrefix(tag, "license:") {
			return strings.TrimPrefix(tag, "license:")
		}
	}
	return ""
}

func normalizeWritingLicense(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	return strings.ReplaceAll(value, " ", "")
}
