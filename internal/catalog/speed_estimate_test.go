package catalog

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseOMLXModelSize(t *testing.T) {
	tests := []struct {
		modelID string
		want    ModelSize
		ok      bool
	}{
		{modelID: "model-8B-Q8", want: ModelSize{TotalParams: 8_000_000_000, BitsPerWeight: 8}, ok: true},
		{modelID: "model-8B-oQ4-fp16", want: ModelSize{TotalParams: 8_000_000_000, BitsPerWeight: 4}, ok: true},
		{modelID: "model-999999999999999999B", ok: false},
		{modelID: "LFM2.5-2.6B-bf16", want: ModelSize{TotalParams: 2_600_000_000, BitsPerWeight: 16}, ok: true},
		{modelID: "LFM2.5-8B-A1B-MLX-4bit", want: ModelSize{TotalParams: 8_000_000_000, ActiveParams: 1_000_000_000, BitsPerWeight: 4}, ok: true},
		{modelID: "Ornith-1.5-35B-A3B-oQ4e-fp16-mtp", want: ModelSize{TotalParams: 35_000_000_000, ActiveParams: 3_000_000_000, BitsPerWeight: 4}, ok: true},
		{modelID: "Qwen3.8-27B-AWQ-5.0bpw", want: ModelSize{TotalParams: 27_000_000_000, BitsPerWeight: 5}, ok: true},
		{modelID: "gpt-oss-20b-MXFP4-Q8", want: ModelSize{TotalParams: 20_000_000_000, BitsPerWeight: 4}, ok: true},
		{modelID: "granite-4.1-8b-nvfp4", want: ModelSize{TotalParams: 8_000_000_000, BitsPerWeight: 4}, ok: true},
		{modelID: "MarkItDown", ok: false},
		{modelID: "local", ok: false},
	}
	for _, test := range tests {
		got, ok := ParseOMLXModelSize(test.modelID)
		require.Equal(t, test.ok, ok, test.modelID)
		require.Equal(t, test.want, got, test.modelID)
	}
}

func TestEstimateModelSpeedTokensPerSecond(t *testing.T) {
	hardware := HardwareProfile{Name: "test", MemoryBytes: 96 << 30, MemoryBandwidthBytesPerSecond: 400_000_000_000}

	dense, ok := EstimateModelSpeedTokensPerSecond(ModelSize{
		TotalParams:   27_000_000_000,
		BitsPerWeight: 4,
	}, hardware)
	require.True(t, ok)
	require.InDelta(t, 20.259, dense, 0.001)

	sparse, ok := EstimateModelSpeedTokensPerSecond(ModelSize{
		TotalParams:   35_000_000_000,
		ActiveParams:  3_000_000_000,
		BitsPerWeight: 4,
	}, hardware)
	require.True(t, ok)
	require.InDelta(t, 86.133, sparse, 0.001)

	oversized, ok := EstimateModelSpeedTokensPerSecond(ModelSize{
		TotalParams:   300_000_000_000,
		BitsPerWeight: 4,
	}, hardware)
	require.False(t, ok)
	require.Zero(t, oversized)

	invalid, ok := EstimateModelSpeedTokensPerSecond(ModelSize{TotalParams: 1}, hardware)
	require.False(t, ok)
	require.True(t, math.IsNaN(invalid) || invalid == 0)
	hardware.MemoryBandwidthBytesPerSecond = math.Inf(1)
	_, ok = EstimateModelSpeedTokensPerSecond(ModelSize{TotalParams: 1, BitsPerWeight: 4}, hardware)
	require.False(t, ok)
}

func TestEstimateOMLXSpeedTokensPerSecondMatchesBenchmarks(t *testing.T) {
	lfm, ok := EstimateOMLXSpeedTokensPerSecond("LFM2.5-2.6B-mxfp4")
	require.True(t, ok)
	require.InDelta(t, 162.338, lfm, 0.001)

	qwen, ok := EstimateOMLXSpeedTokensPerSecond("Qwen3.8-27B-oQ4e-fp16-mtp")
	require.True(t, ok)
	require.InDelta(t, 20.259, qwen, 0.001)

	ornith, ok := EstimateOMLXSpeedTokensPerSecond("Ornith-1.5-35B-A3B-oQ4e-fp16-mtp")
	require.True(t, ok)
	require.InDelta(t, 86.133, ornith, 0.001)

	lfm8, ok := EstimateOMLXSpeedTokensPerSecond("LFM2.5-8B-A1B-MLX-4bit")
	require.True(t, ok)
	require.InDelta(t, 171.821, lfm8, 0.001)
}

func TestEstimateOMLXSpeedTokensPerSecondExcludesNonChatModels(t *testing.T) {
	for _, modelID := range []string{
		"Qwen3-ASR-1.7B-4bit",
		"Qwen3-TTS-12Hz-1.7B-Base-4bit",
		"Qwen3-Embedding-4B-mxfp8",
	} {
		speed, ok := EstimateOMLXSpeedTokensPerSecond(modelID)
		require.False(t, ok, modelID)
		require.Zero(t, speed, modelID)
	}
}
