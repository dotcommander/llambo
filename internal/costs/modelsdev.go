package costs

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ModelsDevPrice is one model's per-1M-token pricing as stored in models-dev.json.
type ModelsDevPrice struct {
	InputPer1M  float64 `json:"input_per_1m"`
	OutputPer1M float64 `json:"output_per_1m"`
}

// ModelsDevFile is the on-disk normalized pricing map, keyed "<provider>:<model>".
type ModelsDevFile map[string]ModelsDevPrice

// ModelsDevPath returns the default path to the synced models.dev pricing file.
func ModelsDevPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "llambo", "models-dev.json")
}

// loadModelsDev reads the normalized models.dev pricing file. A missing file is
// not an error (returns nil, nil). Every present entry is treated as explicit.
func loadModelsDev(path string) (map[string]ModelCost, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, 16<<20))
	if err != nil {
		return nil, err
	}

	var raw map[string]struct {
		Input  *float64 `json:"input_per_1m"`
		Output *float64 `json:"output_per_1m"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	out := make(map[string]ModelCost, len(raw))
	for k, p := range raw {
		var mc ModelCost
		if p.Input != nil {
			mc.InputPer1M, mc.InputExplicit = parsePrice(strconv.FormatFloat(*p.Input, 'g', -1, 64))
		}
		if p.Output != nil {
			mc.OutputPer1M, mc.OutputExplicit = parsePrice(strconv.FormatFloat(*p.Output, 'g', -1, 64))
		}
		out[strings.ToLower(k)] = mc
	}
	return out, nil
}

// loadAll merges models.dev pricing (primary) with the CSV (override): a CSV key
// replaces the models.dev entry wholesale. Either source missing is non-fatal.
func loadAll(modelsDevPath, csvPath string) (map[string]ModelCost, error) {
	base, err := loadModelsDev(modelsDevPath)
	if err != nil {
		return nil, err
	}
	if base == nil {
		base = map[string]ModelCost{}
	}

	csv, err := Load(csvPath)
	if err != nil {
		return nil, err
	}
	for k, mc := range csv {
		base[k] = mc
	}
	if len(base) == 0 {
		return nil, nil
	}
	return base, nil
}

// LoadAll loads pricing from the default models.dev file (primary) overlaid with
// the default CSV (manual overrides).
func LoadAll() (map[string]ModelCost, error) {
	return loadAll(ModelsDevPath(), DefaultPath())
}
