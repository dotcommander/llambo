package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	huggingFaceModelSearchAPI      = "https://huggingface.co/api/models"
	defaultWritingDiscoveryLimit   = 25
	maxWritingDiscoveryLimit       = 100
	writingDiscoveryReviewStatus   = "needs-review"
	writingDiscoveryCandidateNotes = "Fresh Hugging Face discovery only. Review identity, license, serving format, and writing suitability before running or redistributing."
)

// WritingOpenModelDiscovery records the bounded API query used to find new
// public model candidates. It deliberately does not imply benchmark coverage.
type WritingOpenModelDiscovery struct {
	SourceURL  string    `json:"source_url"`
	FetchedAt  time.Time `json:"fetched_at"`
	Status     string    `json:"status"`
	HTTPStatus int       `json:"http_status,omitempty"`
	Candidates int       `json:"candidates,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// WritingDiscoveredOpenModel is a fresh, unreviewed candidate from the public
// Hugging Face model listing. It is separate from WritingOpenModel so it cannot
// accidentally receive a reviewed coverage label or an inferred score.
type WritingDiscoveredOpenModel struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	HuggingFaceURL string   `json:"huggingface_url"`
	PipelineTag    string   `json:"pipeline_tag,omitempty"`
	License        string   `json:"license,omitempty"`
	CreatedAt      string   `json:"created_at,omitempty"`
	LastModified   string   `json:"last_modified,omitempty"`
	Downloads      int64    `json:"downloads,omitempty"`
	Likes          int64    `json:"likes,omitempty"`
	Gated          bool     `json:"gated,omitempty"`
	Private        bool     `json:"private,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	ReviewStatus   string   `json:"review_status"`
	Notes          string   `json:"notes"`
}

type huggingFaceModelListItem struct {
	ID           string   `json:"id"`
	PipelineTag  string   `json:"pipeline_tag"`
	CreatedAt    string   `json:"createdAt"`
	LastModified string   `json:"lastModified"`
	Downloads    int64    `json:"downloads"`
	Likes        int64    `json:"likes"`
	Gated        bool     `json:"gated"`
	Private      bool     `json:"private"`
	Tags         []string `json:"tags"`
}

func fetchWritingOpenModelDiscovery(ctx context.Context, client *http.Client, catalog WritingCatalog, opts WritingCatalogOptions, fetchedAt time.Time) (WritingOpenModelDiscovery, []WritingDiscoveredOpenModel) {
	limit := opts.DiscoverLimit
	if limit <= 0 {
		limit = defaultWritingDiscoveryLimit
	}
	if limit > maxWritingDiscoveryLimit {
		limit = maxWritingDiscoveryLimit
	}
	queryLimit := limit * 2
	if queryLimit > maxWritingDiscoveryLimit {
		queryLimit = maxWritingDiscoveryLimit
	}
	sourceURL := opts.DiscoveryURL
	if sourceURL == "" {
		params := url.Values{
			"pipeline_tag": []string{"text-generation"},
			"sort":         []string{"lastModified"},
			"direction":    []string{"-1"},
			"limit":        []string{strconv.Itoa(queryLimit)},
			"full":         []string{"false"},
		}
		sourceURL = huggingFaceModelSearchAPI + "?" + params.Encode()
	}
	discovery := WritingOpenModelDiscovery{
		SourceURL: sourceURL,
		FetchedAt: fetchedAt,
		Status:    "error",
	}
	artifact, err := fetchWritingArtifact(ctx, client, sourceURL, fetchedAt)
	discovery.HTTPStatus = artifact.HTTPStatus
	if err != nil {
		discovery.Error = err.Error()
		return discovery, nil
	}
	var items []huggingFaceModelListItem
	if err := json.Unmarshal(artifact.Body, &items); err != nil {
		discovery.Error = fmt.Sprintf("parse model discovery: %v", err)
		return discovery, nil
	}
	known := make(map[string]struct{}, len(catalog.OpenModels))
	for _, model := range catalog.OpenModels {
		known[model.ID] = struct{}{}
	}
	candidates := make([]WritingDiscoveredOpenModel, 0, minInt(limit, len(items)))
	for _, item := range items {
		if item.ID == "" || item.PipelineTag != "text-generation" || item.Private || item.Gated {
			continue
		}
		if _, exists := known[item.ID]; exists {
			continue
		}
		candidates = append(candidates, WritingDiscoveredOpenModel{
			ID:             item.ID,
			Name:           writingDiscoveredModelName(item.ID),
			HuggingFaceURL: "https://huggingface.co/" + item.ID,
			PipelineTag:    item.PipelineTag,
			License:        writingLicenseFromTags(item.Tags),
			CreatedAt:      item.CreatedAt,
			LastModified:   item.LastModified,
			Downloads:      item.Downloads,
			Likes:          item.Likes,
			Gated:          item.Gated,
			Private:        item.Private,
			Tags:           append([]string(nil), item.Tags...),
			ReviewStatus:   writingDiscoveryReviewStatus,
			Notes:          writingDiscoveryCandidateNotes,
		})
		if len(candidates) >= limit {
			break
		}
	}
	discovery.Status = "available"
	discovery.Candidates = len(candidates)
	return discovery, candidates
}

func writingDiscoveredModelName(id string) string {
	if slash := strings.LastIndexByte(id, '/'); slash >= 0 && slash+1 < len(id) {
		return id[slash+1:]
	}
	return id
}

func writingLicenseFromTags(tags []string) string {
	for _, tag := range tags {
		if strings.HasPrefix(tag, "license:") {
			license := strings.TrimPrefix(tag, "license:")
			if license != "" {
				return license
			}
		}
	}
	return "unknown"
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
