package evals

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

//go:embed testdata/llambo-9-estimator-v1.json
var categoryEstimatorJSON []byte

const categoryEstimatorArtifactVersion = 1

type estimatorArtifact struct {
	Version     int                    `json:"version"`
	Formula     string                 `json:"formula"`
	Fingerprint string                 `json:"fingerprint"`
	Rows        []estimatorTrainingRow `json:"rows"`
	Targets     []estimatorTarget      `json:"targets"`
}

type estimatorTrainingRow struct {
	Key          string             `json:"key"`
	Organization string             `json:"organization"`
	Scores       map[string]float64 `json:"scores"`
}

type estimatorTarget struct {
	Category      string    `json:"category"`
	Method        string    `json:"method"`
	Alpha         float64   `json:"alpha,omitempty"`
	K             int       `json:"k,omitempty"`
	PriorShrink   float64   `json:"prior_shrink,omitempty"`
	Prior         float64   `json:"prior"`
	Support       int       `json:"support"`
	ValidationMAE float64   `json:"validation_mae"`
	ErrorP90      float64   `json:"error_p90"`
	Coefficients  []float64 `json:"coefficients,omitempty"`
}

type estimatorPayload struct {
	Version int                    `json:"version"`
	Formula string                 `json:"formula"`
	Rows    []estimatorTrainingRow `json:"rows"`
	Targets []estimatorTarget      `json:"targets"`
}

func loadCategoryEstimator() (estimatorArtifact, error) {
	decoder := json.NewDecoder(bytes.NewReader(categoryEstimatorJSON))
	decoder.DisallowUnknownFields()
	var artifact estimatorArtifact
	if err := decoder.Decode(&artifact); err != nil {
		return estimatorArtifact{}, fmt.Errorf("decode LLAMBO-9 estimator: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return estimatorArtifact{}, fmt.Errorf("decode LLAMBO-9 estimator: %w", err)
	}
	if artifact.Version != categoryEstimatorArtifactVersion || artifact.Formula != CategoryFormulaVersion {
		return estimatorArtifact{}, fmt.Errorf("unsupported LLAMBO-9 estimator version %d formula %q", artifact.Version, artifact.Formula)
	}
	if got := estimatorFingerprint(artifact); got != artifact.Fingerprint {
		return estimatorArtifact{}, fmt.Errorf("LLAMBO-9 estimator fingerprint mismatch: got %s want %s", got, artifact.Fingerprint)
	}
	if len(artifact.Targets) != len(categorySpecs) {
		return estimatorArtifact{}, fmt.Errorf("LLAMBO-9 estimator has %d targets, want %d", len(artifact.Targets), len(categorySpecs))
	}
	for i, target := range artifact.Targets {
		if target.Category != categorySpecs[i].name || !finite(target.Prior) || target.Prior < 0 || target.Prior > 100 || target.Support < 0 || !validEstimatorMethod(target.Method) {
			return estimatorArtifact{}, fmt.Errorf("invalid LLAMBO-9 estimator target %q", target.Category)
		}
	}
	return artifact, nil
}

func estimatorFingerprint(artifact estimatorArtifact) string {
	payload := estimatorPayload{Version: artifact.Version, Formula: artifact.Formula, Rows: artifact.Rows, Targets: artifact.Targets}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func validEstimatorMethod(method string) bool {
	switch method {
	case estimatorMethodPrior, estimatorMethodRidge, estimatorMethodKNN:
		return true
	default:
		return false
	}
}

func estimateOMLXRows(rows []ReportModel, artifact estimatorArtifact) {
	targets := make(map[string]estimatorTarget, len(artifact.Targets))
	for _, target := range artifact.Targets {
		targets[target.Category] = target
	}
	for i := range rows {
		row := &rows[i]
		if row.Projection == nil {
			continue
		}
		direct := cloneCategoryScores(row.LlamboScores)
		for _, category := range categorySpecs {
			if direct[category.name] != nil {
				continue
			}
			target := targets[category.name]
			value, sources, method := predictCategoryEstimate(*row, direct, target, artifact.Rows)
			support := target.Support
			validationMAE, errorP90 := estimatorValidation(target, method)
			row.LlamboScores[category.name] = &LlamboScore{
				Score: value, Coverage: 0, TrustedCoverage: 0, Checks: nil, Agreement: nil,
				Confidence: scoreConfidenceLow, Estimated: true, EstimateMethod: method,
				EstimateSources: sources, EstimateSupport: &support,
				EstimateValidationMAE: validationMAE, EstimateErrorP90: errorP90,
				EstimateCalibrationFingerprint: artifact.Fingerprint,
			}
			delete(row.UnresolvedReasons, category.name)
		}
	}
}

func estimatorValidation(target estimatorTarget, method string) (*float64, *float64) {
	if target.Support == 0 || target.Method == estimatorMethodPrior || method == "prior-only" {
		return nil, nil
	}
	return &target.ValidationMAE, &target.ErrorP90
}

func predictCategoryEstimate(row ReportModel, direct map[string]*LlamboScore, target estimatorTarget, training []estimatorTrainingRow) (float64, []string, string) {
	sources := directEstimateSources(direct, target.Category)
	if len(sources) == 0 || row.Projection == nil || strings.TrimSpace(row.Projection.SourceKey) == "" {
		return clampScore(target.Prior), nil, "prior-only"
	}
	switch target.Method {
	case estimatorMethodRidge:
		features := estimatorFeatures(direct, target.Category)
		if len(target.Coefficients) == len(features)+1 {
			prediction := target.Coefficients[0]
			for i, feature := range features {
				prediction += target.Coefficients[i+1] * feature
			}
			return clampScore(prediction), sources, target.Method
		}
	case estimatorMethodKNN:
		if prediction, ok := predictKNN(knnPredictionInput{
			scores: directScores(direct), target: target.Category, training: training,
			k: target.K, shrink: target.PriorShrink, prior: target.Prior,
		}); ok {
			return clampScore(prediction), sources, target.Method
		}
	}
	return clampScore(target.Prior), sources, target.Method
}

func directEstimateSources(scores map[string]*LlamboScore, target string) []string {
	sources := make([]string, 0, len(categorySpecs)-1)
	for _, category := range categorySpecs {
		score := scores[category.name]
		if category.name != target && score != nil && !score.Estimated && finite(score.Score) {
			sources = append(sources, category.name)
		}
	}
	return sources
}

func directScores(scores map[string]*LlamboScore) map[string]float64 {
	result := make(map[string]float64, len(scores))
	for category, score := range scores {
		if score != nil && !score.Estimated && finite(score.Score) {
			result[category] = score.Score
		}
	}
	return result
}

func estimatorFeatures(scores map[string]*LlamboScore, target string) []float64 {
	direct := directScores(scores)
	return estimatorMapFeatures(direct, target)
}

func estimatorMapFeatures(scores map[string]float64, target string) []float64 {
	values := make([]float64, 0, 2*(len(categorySpecs)-1))
	presence := make([]float64, 0, len(categorySpecs)-1)
	for _, category := range categorySpecs {
		if category.name == target {
			continue
		}
		value, ok := scores[category.name]
		if ok {
			values = append(values, value/100)
			presence = append(presence, 1)
		} else {
			values = append(values, 0)
			presence = append(presence, 0)
		}
	}
	return append(values, presence...)
}

type knnPredictionInput struct {
	scores   map[string]float64
	target   string
	training []estimatorTrainingRow
	k        int
	shrink   float64
	prior    float64
}

func predictKNN(input knnPredictionInput) (float64, bool) {
	type neighbor struct{ distance, score float64 }
	neighbors := make([]neighbor, 0, len(input.training))
	for _, row := range input.training {
		targetScore, ok := row.Scores[input.target]
		if !ok {
			continue
		}
		sum, shared := 0.0, 0
		for category, value := range input.scores {
			if category == input.target {
				continue
			}
			if other, exists := row.Scores[category]; exists {
				delta := (value - other) / 100
				sum += delta * delta
				shared++
			}
		}
		if shared > 0 {
			neighbors = append(neighbors, neighbor{distance: math.Sqrt(sum / float64(shared)), score: targetScore})
		}
	}
	if len(neighbors) == 0 {
		return 0, false
	}
	sort.Slice(neighbors, func(i, j int) bool { return neighbors[i].distance < neighbors[j].distance })
	if input.k <= 0 || input.k > len(neighbors) {
		input.k = len(neighbors)
	}
	weighted, weight := input.prior*input.shrink, input.shrink
	for _, candidate := range neighbors[:input.k] {
		w := 1 / (candidate.distance + .05)
		weighted += w * candidate.score
		weight += w
	}
	return weighted / weight, true
}

func clampScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
