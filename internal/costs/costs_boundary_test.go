package costs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAbsentFileAndEmptyPath(t *testing.T) {
	t.Parallel()

	got, err := Load(filepath.Join(t.TempDir(), "absent.csv"))
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = Load("")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestLoadHeaderOnlyYieldsEmptyMap(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "model-costs.csv")
	require.NoError(t, os.WriteFile(path, []byte("provider,model,input_per_1m_usd,output_per_1m_usd,notes\n"), 0o644))

	got, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestLoadSkipsUnnamedRowsAndParsesKeptRows(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "model-costs.csv")
	data := "provider,model,input,output,notes\n" +
		"# comment-only line\n" +
		",,1,2,\n" +
		"openai,,1,2,\n" +
		"openai,kept,0.5,1.5,kept row\n"
	require.NoError(t, os.WriteFile(path, []byte(data), 0o644))

	got, err := Load(path)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, ModelCost{InputPer1M: 0.5, OutputPer1M: 1.5, InputExplicit: true, OutputExplicit: true}, got["openai:kept"])
}

func TestLoadWrapsRowErrorsWithPath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "model-costs.csv")
	require.NoError(t, os.WriteFile(path, []byte("provider,model\nopenai,kept\nbroken-row\n"), 0o644))

	_, err := Load(path)
	require.ErrorContains(t, err, "read CSV row")
	require.ErrorContains(t, err, path)
}

func TestParseCostRowRejectsRowsWithoutIdentity(t *testing.T) {
	t.Parallel()

	for _, rec := range [][]string{nil, {}, {"openai"}, {"", "model"}} {
		mc, ok := parseCostRow(rec)
		require.False(t, ok, "rec %v", rec)
		require.Equal(t, ModelCost{}, mc)
	}

	mc, ok := parseCostRow([]string{"openai", "m", "1", "2"})
	require.True(t, ok)
	require.Equal(t, ModelCost{InputPer1M: 1, OutputPer1M: 2, InputExplicit: true, OutputExplicit: true}, mc)

	// Blank cells stay unknown rather than explicit zero.
	mc, ok = parseCostRow([]string{"openai", "m", "", ""})
	require.True(t, ok)
	require.Equal(t, ModelCost{}, mc)
}
