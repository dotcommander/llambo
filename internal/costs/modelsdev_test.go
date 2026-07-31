package costs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAllMergesModelsDevWithCSVOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		modelsDev ModelsDevFile
		csv       string
		want      map[string]ModelCost
	}{
		{
			name: "CSV overrides models.dev for shared key",
			modelsDev: ModelsDevFile{
				"openai:gpt-4o": {InputPer1M: 1.25, OutputPer1M: 5.00},
			},
			csv: csvFixture("openai,gpt-4o,2.50,10.00,manual override\n"),
			want: map[string]ModelCost{
				"openai:gpt-4o": {InputPer1M: 2.50, OutputPer1M: 10.00, InputExplicit: true, OutputExplicit: true},
			},
		},
		{
			name: "explicit-free CSV row overrides non-zero models.dev entry",
			modelsDev: ModelsDevFile{
				"openrouter:free-model": {InputPer1M: 0.10, OutputPer1M: 0.20},
			},
			csv: csvFixture("openrouter,free-model,0,0,free override\n"),
			want: map[string]ModelCost{
				"openrouter:free-model": {InputPer1M: 0, OutputPer1M: 0, InputExplicit: true, OutputExplicit: true},
			},
		},
		{
			name: "models.dev-only key survives when CSV lacks it",
			modelsDev: ModelsDevFile{
				"gemini:flash": {InputPer1M: 0.075, OutputPer1M: 0.30},
			},
			csv: csvFixture("openai,gpt-4o,2.50,10.00,manual override\n"),
			want: map[string]ModelCost{
				"gemini:flash":  {InputPer1M: 0.075, OutputPer1M: 0.30, InputExplicit: true, OutputExplicit: true},
				"openai:gpt-4o": {InputPer1M: 2.50, OutputPer1M: 10.00, InputExplicit: true, OutputExplicit: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			modelsDevPath := filepath.Join(dir, "models-dev.json")
			csvPath := filepath.Join(dir, "model-costs.csv")
			writeModelsDevFixture(t, modelsDevPath, tt.modelsDev)
			require.NoError(t, os.WriteFile(csvPath, []byte(tt.csv), 0o644))

			got, err := loadAll(modelsDevPath, csvPath)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadAllWithMissingFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		writeCSV      bool
		want          map[string]ModelCost
		wantNilResult bool
	}{
		{
			name:     "missing models.dev plus present CSV returns CSV-only result",
			writeCSV: true,
			want: map[string]ModelCost{
				"openai:gpt-4o": {InputPer1M: 2.50, OutputPer1M: 10.00, InputExplicit: true, OutputExplicit: true},
			},
		},
		{
			name:          "missing both files returns nil map and nil error",
			wantNilResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			modelsDevPath := filepath.Join(dir, "missing-models-dev.json")
			csvPath := filepath.Join(dir, "missing-model-costs.csv")
			if tt.writeCSV {
				require.NoError(t, os.WriteFile(csvPath, []byte(csvFixture("openai,gpt-4o,2.50,10.00,manual override\n")), 0o644))
			}

			got, err := loadAll(modelsDevPath, csvPath)
			require.NoError(t, err)
			if tt.wantNilResult {
				require.Nil(t, got)
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadModelsDev(t *testing.T) {
	t.Parallel()

	t.Run("missing file returns nil map and nil error", func(t *testing.T) {
		t.Parallel()

		got, err := loadModelsDev(filepath.Join(t.TempDir(), "missing-models-dev.json"))
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("present entries are explicit", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "models-dev.json")
		writeModelsDevFixture(t, path, ModelsDevFile{
			"openai:gpt-4o": {InputPer1M: 1.25, OutputPer1M: 5.00},
		})

		got, err := loadModelsDev(path)
		require.NoError(t, err)
		require.Equal(t, map[string]ModelCost{
			"openai:gpt-4o": {InputPer1M: 1.25, OutputPer1M: 5.00, InputExplicit: true, OutputExplicit: true},
		}, got)
	})
}

func csvFixture(rows string) string {
	return "provider,model,input_per_1m_usd,output_per_1m_usd,notes\n" + rows
}

func writeModelsDevFixture(t *testing.T, path string, fixture ModelsDevFile) {
	t.Helper()

	data, err := json.Marshal(fixture)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}
