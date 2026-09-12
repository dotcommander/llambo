package catalog

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	// M2 Max with 96 GiB unified memory has 400 GB/s theoretical memory
	// bandwidth. Dense efficiency and overhead are fitted to warmed oMLX
	// pp1024/tg128 TPOT values for LFM2.5-2.6B-mxfp4 (6.16ms) and
	// Qwen3.8-27B-oQ4e-fp16-mtp (49.36ms). Sparse efficiency and overhead are
	// fitted to Ornith-1.5-35B-A3B-oQ4e-fp16-mtp (11.61ms) and
	// LFM2.5-8B-A1B-MLX-4bit (5.82ms).
	m2Max96GBMemoryBandwidthBytesPerSecond = 400_000_000_000
	m2Max96GBMemoryBytes                   = 96 << 30
	denseMemoryBandwidthEfficiency         = 0.706023407982456
	sparseMemoryBandwidthEfficiency        = 0.43177892918825556

	// Per-token fixed overhead captures kernel dispatch and runtime bookkeeping.
	// The dense and sparse values are fitted to the benchmarks above.
	denseTokenOverheadSeconds  = 0.00155672131147541
	sparseTokenOverheadSeconds = 0.002925
	defaultLocalBitsPerWeight  = 4
	localWeightsMemoryReserve  = 1.10
)

// HardwareProfile describes the memory capacity and bandwidth used by a local
// speed estimate.
type HardwareProfile struct {
	Name                          string
	MemoryBytes                   uint64
	MemoryBandwidthBytesPerSecond float64
}

// ModelSize is a best-effort size description parsed from a local model ID.
// ActiveParams is nonzero only for IDs that explicitly identify MoE active
// parameters (for example 35B-A3B).
type ModelSize struct {
	TotalParams   uint64
	ActiveParams  uint64
	BitsPerWeight float64
}

var (
	totalParamsPattern    = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])([0-9]+(?:\.[0-9]+)?)b(?:$|[^a-z0-9])`)
	activeParamsPattern   = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])a([0-9]+(?:\.[0-9]+)?)b(?:$|[^a-z0-9])`)
	bitsPerWeightPattern  = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:o?q)?([0-9]+(?:\.[0-9]+)?)(?:bit|bpw|e(?:_[a-z0-9]+)?|_[a-z0-9]+)(?:$|[^a-z0-9])`)
	bareQuantPattern      = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])o?q([0-9]+(?:\.[0-9]+)?)(?:$|[^a-z0-9])`)
	nonChatModelIDPattern = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:asr|tts|embedding|embed)(?:$|[^a-z0-9])`)
)

// M2Max96GB returns the hardware profile for the local 96 GiB M2 Max MacBook Pro.
func M2Max96GB() HardwareProfile {
	return HardwareProfile{
		Name:                          "M2 Max 96GB",
		MemoryBytes:                   m2Max96GBMemoryBytes,
		MemoryBandwidthBytesPerSecond: m2Max96GBMemoryBandwidthBytesPerSecond,
	}
}

// ParseOMLXModelSize extracts total/active parameter counts and quantization
// from an OMLX model ID. It returns false when no parameter size is present.
// A size-bearing ID without an explicit quantization uses the common local
// four-bit default.
func ParseOMLXModelSize(modelID string) (ModelSize, bool) {
	totalMatch := totalParamsPattern.FindStringSubmatch(modelID)
	if totalMatch == nil {
		return ModelSize{}, false
	}
	total, ok := parseParameterCount(totalMatch[1])
	if !ok || total == 0 {
		return ModelSize{}, false
	}

	size := ModelSize{TotalParams: total, BitsPerWeight: defaultLocalBitsPerWeight}
	if activeMatch := activeParamsPattern.FindStringSubmatch(modelID); activeMatch != nil {
		if active, ok := parseParameterCount(activeMatch[1]); ok && active > 0 && active < total {
			size.ActiveParams = active
		}
	}
	if bits, ok := bitsPerWeightFromModelID(modelID); ok {
		size.BitsPerWeight = bits
	}
	return size, true
}

// EstimateModelSpeedTokensPerSecond returns a deterministic decode estimate.
// It is an instant roofline approximation, not benchmark evidence.
func EstimateModelSpeedTokensPerSecond(size ModelSize, hardware HardwareProfile) (float64, bool) {
	if size.TotalParams == 0 || size.BitsPerWeight <= 0 {
		return 0, false
	}
	if hardware.MemoryBytes == 0 || hardware.MemoryBandwidthBytesPerSecond <= 0 || math.IsInf(hardware.MemoryBandwidthBytesPerSecond, 0) {
		return 0, false
	}

	totalWeightBytes := float64(size.TotalParams) * size.BitsPerWeight / 8
	if totalWeightBytes*localWeightsMemoryReserve > float64(hardware.MemoryBytes) {
		return 0, false
	}

	activeParams := size.TotalParams
	overhead := denseTokenOverheadSeconds
	efficiency := denseMemoryBandwidthEfficiency
	if size.ActiveParams > 0 && size.ActiveParams < size.TotalParams {
		activeParams = size.ActiveParams
		overhead = sparseTokenOverheadSeconds
		efficiency = sparseMemoryBandwidthEfficiency
	}
	activeWeightBytes := float64(activeParams) * size.BitsPerWeight / 8
	effectiveBandwidth := hardware.MemoryBandwidthBytesPerSecond * efficiency
	secondsPerToken := activeWeightBytes/effectiveBandwidth + overhead
	if secondsPerToken <= 0 || math.IsInf(secondsPerToken, 0) || math.IsNaN(secondsPerToken) {
		return 0, false
	}
	return 1 / secondsPerToken, true
}

// EstimateOMLXSpeedTokensPerSecond estimates local decode speed for an OMLX
// model ID on the M2 Max 96 GiB profile.
func EstimateOMLXSpeedTokensPerSecond(modelID string) (float64, bool) {
	if nonChatModelIDPattern.MatchString(modelID) {
		return 0, false
	}
	size, ok := ParseOMLXModelSize(modelID)
	if !ok {
		return 0, false
	}
	return EstimateModelSpeedTokensPerSecond(size, M2Max96GB())
}

func parseParameterCount(raw string) (uint64, bool) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 || math.IsInf(value, 0) || math.IsNaN(value) {
		return 0, false
	}
	params := math.Round(value * 1_000_000_000)
	if params >= math.Exp2(64) {
		return 0, false
	}
	return uint64(params), true
}

func bitsPerWeightFromModelID(modelID string) (float64, bool) {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "mxfp4"), strings.Contains(lower, "nvfp4"):
		return 4, true
	}
	if match := bitsPerWeightPattern.FindStringSubmatch(modelID); match != nil {
		bits, err := strconv.ParseFloat(match[1], 64)
		if err == nil && bits > 0 && bits <= 16 {
			return bits, true
		}
	}
	if match := bareQuantPattern.FindStringSubmatch(modelID); match != nil {
		bits, err := strconv.ParseFloat(match[1], 64)
		if err == nil && bits > 0 && bits <= 16 {
			return bits, true
		}
	}
	switch {
	case strings.Contains(lower, "bf16"), strings.Contains(lower, "fp16"):
		return 16, true
	case strings.Contains(lower, "fp8"):
		return 8, true
	}
	return 0, false
}
