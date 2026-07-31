package costs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadTracksExplicitCostCells(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "model-costs.csv")
	data := "provider,model,input_per_1m_usd,output_per_1m_usd,notes\n" +
		"openai,free,0,0,\n" +
		"openai,unknown,,,\n"
	require.NoError(t, os.WriteFile(path, []byte(data), 0o644))

	got, err := Load(path)
	require.NoError(t, err)

	require.True(t, got["openai:free"].InputExplicit)
	require.True(t, got["openai:free"].OutputExplicit)
	require.False(t, got["openai:unknown"].InputExplicit)
	require.False(t, got["openai:unknown"].OutputExplicit)
}
