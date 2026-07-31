package evals

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

//go:embed projections.json
var builtInProjectionRegistry []byte

type projectionRegistry struct {
	Version     int              `json:"version"`
	Projections []projectionSpec `json:"projections"`
}

type projectionSpec struct {
	ArtifactKey string `json:"artifact_key"`
	SourceKey   string `json:"source_key"`
	Confidence  string `json:"confidence"`
	Basis       string `json:"basis"`
	ReviewedAt  string `json:"reviewed_at"`
}

// ProjectionInfo records that this row inherits external evidence from an
// upstream model rather than representing a source-native model identity.
type ProjectionInfo struct {
	SourceKey  string `json:"source_key"`
	Confidence string `json:"confidence"`
	Basis      string `json:"basis"`
	ReviewedAt string `json:"reviewed_at"`
}

type ProjectionDiagnostics struct {
	RegistrySource string              `json:"registry_source"`
	Version        int                 `json:"version"`
	Configured     int                 `json:"configured"`
	Applied        int                 `json:"applied"`
	Missing        []MissingProjection `json:"missing,omitempty"`
}

type MissingProjection struct {
	ArtifactKey string `json:"artifact_key"`
	SourceKey   string `json:"source_key"`
}

func loadProjectionRegistry(path string) (projectionRegistry, error) {
	data := builtInProjectionRegistry
	if path != "" {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return projectionRegistry{}, fmt.Errorf("read projection registry: %w", err)
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var registry projectionRegistry
	if err := decoder.Decode(&registry); err != nil {
		return projectionRegistry{}, fmt.Errorf("decode projection registry: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return projectionRegistry{}, fmt.Errorf("decode projection registry: %w", err)
	}
	if registry.Version != 1 {
		return projectionRegistry{}, fmt.Errorf("unsupported projection registry version %d", registry.Version)
	}

	seen := make(map[string]struct{}, len(registry.Projections))
	for i := range registry.Projections {
		projection := &registry.Projections[i]
		projection.ArtifactKey = strings.TrimSpace(projection.ArtifactKey)
		projection.SourceKey = strings.TrimSpace(projection.SourceKey)
		projection.Basis = strings.TrimSpace(projection.Basis)
		projection.ReviewedAt = strings.TrimSpace(projection.ReviewedAt)
		if projection.ArtifactKey == "" || projection.SourceKey == "" {
			return projectionRegistry{}, fmt.Errorf("projection artifact_key and source_key must not be empty")
		}
		if projection.Basis == "" {
			return projectionRegistry{}, fmt.Errorf("projection %q must declare a review basis", projection.ArtifactKey)
		}
		if _, err := time.Parse(time.RFC3339, projection.ReviewedAt); err != nil {
			return projectionRegistry{}, fmt.Errorf("projection %q has invalid reviewed_at %q: %w", projection.ArtifactKey, projection.ReviewedAt, err)
		}
		if projection.Confidence != "high" && projection.Confidence != "medium" && projection.Confidence != "low" {
			return projectionRegistry{}, fmt.Errorf("projection %q has invalid confidence %q", projection.ArtifactKey, projection.Confidence)
		}
		if _, ok := seen[projection.ArtifactKey]; ok {
			return projectionRegistry{}, fmt.Errorf("duplicate projection artifact_key %q", projection.ArtifactKey)
		}
		seen[projection.ArtifactKey] = struct{}{}
	}
	sort.Slice(registry.Projections, func(i, j int) bool {
		return registry.Projections[i].ArtifactKey < registry.Projections[j].ArtifactKey
	})
	return registry, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func appendProjectedRows(rows []ReportModel, projections []projectionSpec) ([]ReportModel, []MissingProjection, error) {
	byKey := make(map[string]ReportModel, len(rows))
	for _, row := range rows {
		byKey[row.Key] = row
	}
	result := make([]ReportModel, 0, len(rows)+len(projections))
	missing := make([]MissingProjection, 0)
	result = append(result, rows...)
	for _, projection := range projections {
		if _, exists := byKey[projection.ArtifactKey]; exists {
			return nil, nil, fmt.Errorf("projection artifact_key %q collides with a canonical model key", projection.ArtifactKey)
		}
		source, ok := byKey[projection.SourceKey]
		if !ok {
			missing = append(missing, MissingProjection{ArtifactKey: projection.ArtifactKey, SourceKey: projection.SourceKey})
			continue
		}
		projected := cloneReportModel(source)
		projected.Key = projection.ArtifactKey
		projected.Name = projection.ArtifactKey
		projected.IdentityMatch = IdentityMatchProjected
		projected.Open = nil
		projected.Projection = &ProjectionInfo{SourceKey: projection.SourceKey, Confidence: projection.Confidence, Basis: projection.Basis, ReviewedAt: projection.ReviewedAt}
		for _, score := range projected.Scores {
			if score != nil {
				score.Confidence = lowerConfidence(score.Confidence, projection.Confidence)
			}
		}
		removeProjectedOperationalEvidence(&projected)
		result = append(result, projected)
	}
	return result, missing, nil
}

func removeProjectedOperationalEvidence(projected *ReportModel) {
	projected.Scores["price"] = nil
	projected.Scores["speed"] = nil
	projected.Scores["value"] = nil
	for _, metric := range externalMetrics {
		if metric.dimension == "price" || metric.dimension == "speed" {
			delete(projected.MetricPercentiles, metric.name)
		}
	}
	if projected.LLMStats != nil {
		projected.LLMStats.InputPrice = nil
		projected.LLMStats.OutputPrice = nil
		projected.LLMStats.Throughput = nil
		projected.LLMStats.Latency = nil
	}
	if projected.AA != nil {
		projected.AA.InputPrice = nil
		projected.AA.OutputPrice = nil
		projected.AA.OutputTokensPS = nil
		projected.AA.TTFTSeconds = nil
		projected.AA.E2ESeconds = nil
	}
}

func cloneReportModel(source ReportModel) ReportModel {
	clone := source
	clone.Open = cloneBool(source.Open)
	clone.Scores = make(map[string]*ExternalScore, len(source.Scores))
	for name, score := range source.Scores {
		if score == nil {
			clone.Scores[name] = nil
			continue
		}
		clonedScore := *score
		clonedScore.Sources = append([]string(nil), score.Sources...)
		clonedScore.Disagreement = cloneFloat64(score.Disagreement)
		clone.Scores[name] = &clonedScore
	}
	clone.MetricPercentiles = make(map[string]float64, len(source.MetricPercentiles))
	for name, value := range source.MetricPercentiles {
		clone.MetricPercentiles[name] = value
	}
	clone.LLMStats = cloneLLMStats(source.LLMStats)
	clone.AA = cloneArtificialMetrics(source.AA)
	if source.Projection != nil {
		projection := *source.Projection
		clone.Projection = &projection
	}
	return clone
}

func cloneLLMStats(source *LLMStatsMetrics) *LLMStatsMetrics {
	if source == nil {
		return nil
	}
	clone := *source
	clone.InputPrice = cloneFloat64(source.InputPrice)
	clone.OutputPrice = cloneFloat64(source.OutputPrice)
	clone.Throughput = cloneFloat64(source.Throughput)
	clone.Latency = cloneFloat64(source.Latency)
	clone.GPQA = cloneFloat64(source.GPQA)
	clone.SWEVerified = cloneFloat64(source.SWEVerified)
	clone.SWEPro = cloneFloat64(source.SWEPro)
	clone.SciCode = cloneFloat64(source.SciCode)
	clone.MCPAtlas = cloneFloat64(source.MCPAtlas)
	clone.Indexes = make(map[string]Index, len(source.Indexes))
	for name, index := range source.Indexes {
		clone.Indexes[name] = index
	}
	return &clone
}

func cloneArtificialMetrics(source *ArtificialMetrics) *ArtificialMetrics {
	if source == nil {
		return nil
	}
	clone := *source
	clone.Intelligence = cloneFloat64(source.Intelligence)
	clone.Coding = cloneFloat64(source.Coding)
	clone.Agentic = cloneFloat64(source.Agentic)
	clone.InputPrice = cloneFloat64(source.InputPrice)
	clone.OutputPrice = cloneFloat64(source.OutputPrice)
	clone.OutputTokensPS = cloneFloat64(source.OutputTokensPS)
	clone.TTFTSeconds = cloneFloat64(source.TTFTSeconds)
	clone.E2ESeconds = cloneFloat64(source.E2ESeconds)
	return &clone
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func lowerConfidence(left, right string) string {
	rank := map[string]int{"low": 0, "medium": 1, "high": 2}
	if rank[left] <= rank[right] {
		return left
	}
	return right
}
