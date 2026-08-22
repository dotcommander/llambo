package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const omlxResponseLimit = 2 << 20

type OMLXDiscovery struct {
	Endpoint string
	Models   []string
	Excluded []string
	Attempts []string
}

type OMLXDiagnostics struct {
	Status                 string   `json:"status"`
	Endpoint               string   `json:"endpoint,omitempty"`
	Discovered             int      `json:"discovered"`
	MatchedModels          []string `json:"matched_models,omitempty"`
	UnmatchedModels        []string `json:"unmatched_models,omitempty"`
	InactiveReviewedModels []string `json:"inactive_reviewed_models,omitempty"`
	ExcludedModels         []string `json:"excluded_models,omitempty"`
	Attempts               []string `json:"attempts,omitempty"`
	Error                  string   `json:"error,omitempty"`
}

// DiscoverOMLXModels reads the richer admin inventory when available and falls
// back to the OpenAI-compatible model list. Only text-generating model IDs are
// returned; excluded IDs remain visible in diagnostics.
func DiscoverOMLXModels(ctx context.Context, baseURL, apiKey string, client *http.Client) (OMLXDiscovery, error) {
	if err := ValidateOMLXLoopbackBaseURL(baseURL); err != nil {
		return OMLXDiscovery{}, err
	}
	root, err := normalizeOMLXBaseURL(baseURL)
	if err != nil {
		return OMLXDiscovery{}, err
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	client = omlxNoRedirectClient(client)
	adminURL := root + "/admin/api/models"
	fallbackURL := root + "/v1/models"
	discovery := OMLXDiscovery{Attempts: []string{adminURL}}

	body, adminErr := fetchOMLXJSON(ctx, client, adminURL, apiKey)
	if adminErr == nil {
		models, excluded, parseErr := parseOMLXAdmin(body)
		if parseErr == nil {
			discovery.Endpoint, discovery.Models, discovery.Excluded = adminURL, models, excluded
			return discovery, nil
		}
		adminErr = parseErr
	}

	discovery.Attempts = append(discovery.Attempts, fallbackURL)
	body, fallbackErr := fetchOMLXJSON(ctx, client, fallbackURL, apiKey)
	if fallbackErr == nil {
		models, excluded, parseErr := parseOMLXFallback(body)
		if parseErr == nil {
			discovery.Endpoint, discovery.Models, discovery.Excluded = fallbackURL, models, excluded
			return discovery, nil
		}
		fallbackErr = parseErr
	}
	return discovery, fmt.Errorf("OMLX discovery failed: admin: %v; fallback: %v", adminErr, fallbackErr)
}

// VerifyOMLXExactModel proves an exact ID is present in the unfiltered live
// admin inventory, including audio, embedding, and helper model types.
func VerifyOMLXExactModel(ctx context.Context, baseURL, apiKey, exactID string, client *http.Client) (string, error) {
	if err := ValidateOMLXLoopbackBaseURL(baseURL); err != nil {
		return "", err
	}
	root, err := normalizeOMLXBaseURL(baseURL)
	if err != nil {
		return "", err
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	body, err := fetchOMLXJSON(ctx, omlxNoRedirectClient(client), root+"/admin/api/models", apiKey)
	if err != nil {
		return "", err
	}
	var payload struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("parse OMLX admin response: %w", err)
	}
	for _, model := range payload.Models {
		if strings.TrimSpace(model.ID) == exactID {
			return "omlx/" + exactID, nil
		}
	}
	return "", fmt.Errorf("exact OMLX model %q is absent from live admin inventory", exactID)
}

func omlxNoRedirectClient(client *http.Client) *http.Client {
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

// ValidateOMLXLoopbackBaseURL prevents local inventory credentials from being
// sent to non-loopback hosts.
func ValidateOMLXLoopbackBaseURL(baseURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return fmt.Errorf("invalid OMLX base URL %q", baseURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("OMLX base URL must use http or https")
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("OMLX base URL must use a loopback host")
		}
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path != "" && path != "/v1" {
		return fmt.Errorf("OMLX base URL path must be empty or /v1")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("OMLX base URL must not include credentials, query, or fragment")
	}
	return nil
}

func normalizeOMLXBaseURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(strings.TrimRight(baseURL, "/"))
	if strings.HasSuffix(baseURL, "/v1") {
		baseURL = strings.TrimSuffix(baseURL, "/v1")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid OMLX base URL %q", baseURL)
	}
	return strings.TrimRight(baseURL, "/"), nil
}

func fetchOMLXJSON(ctx context.Context, client *http.Client, endpoint, apiKey string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build OMLX request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request OMLX endpoint: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, omlxResponseLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read OMLX response: %w", err)
	}
	if len(body) > omlxResponseLimit {
		return nil, fmt.Errorf("response exceeds %d bytes", omlxResponseLimit)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return body, nil
}

func parseOMLXAdmin(body []byte) ([]string, []string, error) {
	var payload struct {
		Models []struct {
			ID        string `json:"id"`
			ModelType string `json:"model_type"`
			IsHidden  bool   `json:"is_hidden"`
			IsHelper  bool   `json:"is_helper"`
			Virtual   bool   `json:"virtual"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, fmt.Errorf("parse OMLX admin response: %w", err)
	}
	if payload.Models == nil {
		return nil, nil, fmt.Errorf("admin response is missing models")
	}
	models, excluded := make([]string, 0, len(payload.Models)), make([]string, 0)
	for _, model := range payload.Models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		modelType := strings.ToLower(strings.TrimSpace(model.ModelType))
		if (modelType != "llm" && modelType != "vlm") || model.IsHidden || model.IsHelper || model.Virtual || isUnresolvedLocalModel(id) {
			excluded = append(excluded, id)
			continue
		}
		models = append(models, id)
	}
	return sortedUnique(models), sortedUnique(excluded), nil
}

func parseOMLXFallback(body []byte) ([]string, []string, error) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, fmt.Errorf("parse OMLX models response: %w", err)
	}
	if payload.Data == nil {
		return nil, nil, fmt.Errorf("fallback response is missing data")
	}
	models, excluded := make([]string, 0, len(payload.Data)), make([]string, 0)
	for _, model := range payload.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if !conservativeTextModelID(id) {
			excluded = append(excluded, id)
			continue
		}
		models = append(models, id)
	}
	return sortedUnique(models), sortedUnique(excluded), nil
}

func conservativeTextModelID(id string) bool {
	if isUnresolvedLocalModel(id) {
		return false
	}
	lower := strings.ToLower(id)
	for _, marker := range []string{"embedding", "embed-", "-embed", "rerank", "tts", "asr", "whisper", "audio", "markitdown", "helper", "draft"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func isUnresolvedLocalModel(id string) bool {
	return strings.EqualFold(strings.TrimSpace(id), "local")
}

func sortedUnique(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// FilterProjectedRowsToLiveOMLX narrows projected rows only after successful
// discovery. Canonical rows and formula diagnostics are never modified. A
// discovery error omits projections whose live availability cannot be proven.
func FilterProjectedRowsToLiveOMLX(report *Report, discovery OMLXDiscovery, discoveryErr error) {
	if report == nil {
		return
	}
	diagnostics := &OMLXDiagnostics{Attempts: append([]string(nil), discovery.Attempts...), ExcludedModels: append([]string(nil), discovery.Excluded...)}
	if discoveryErr != nil {
		diagnostics.Status = "unavailable"
		diagnostics.Error = discoveryErr.Error()
		filtered := report.Models[:0]
		for _, row := range report.Models {
			if row.Projection == nil {
				filtered = append(filtered, row)
			} else {
				diagnostics.InactiveReviewedModels = append(diagnostics.InactiveReviewedModels, row.Key)
			}
		}
		diagnostics.InactiveReviewedModels = sortedUnique(diagnostics.InactiveReviewedModels)
		report.Models = filtered
		report.OMLX = diagnostics
		refreshCoverageCampaign(report)
		return
	}
	diagnostics.Status = "filtered"
	diagnostics.Endpoint = discovery.Endpoint
	diagnostics.Discovered = len(discovery.Models)
	live := make(map[string]struct{}, len(discovery.Models))
	for _, id := range discovery.Models {
		live[id] = struct{}{}
	}
	projected := make(map[string]struct{})
	for _, row := range report.Models {
		if row.Projection != nil {
			projected[row.Key] = struct{}{}
		}
	}
	for _, id := range discovery.Models {
		if _, ok := projected[id]; !ok {
			diagnostics.UnmatchedModels = append(diagnostics.UnmatchedModels, id)
		}
	}
	filtered := make([]ReportModel, 0, len(report.Models))
	for _, row := range report.Models {
		if row.Projection == nil {
			filtered = append(filtered, row)
			continue
		}
		if _, ok := live[row.Key]; ok {
			diagnostics.MatchedModels = append(diagnostics.MatchedModels, row.Key)
			filtered = append(filtered, row)
		} else {
			diagnostics.InactiveReviewedModels = append(diagnostics.InactiveReviewedModels, row.Key)
		}
	}
	diagnostics.MatchedModels = sortedUnique(diagnostics.MatchedModels)
	diagnostics.UnmatchedModels = sortedUnique(diagnostics.UnmatchedModels)
	diagnostics.InactiveReviewedModels = sortedUnique(diagnostics.InactiveReviewedModels)
	diagnostics.ExcludedModels = sortedUnique(diagnostics.ExcludedModels)
	report.Models = filtered
	report.OMLX = diagnostics
	refreshCoverageCampaign(report)
}

func refreshCoverageCampaign(report *Report) {
	campaign, err := buildCoverageCampaign(report.Models)
	if err == nil {
		report.CoverageCampaign = campaign
	}
}
