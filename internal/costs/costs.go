package costs

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ModelCost holds per-1M-token pricing for a single model.
// Zero values mean unknown/unset.
type ModelCost struct {
	InputPer1M     float64
	OutputPer1M    float64
	InputExplicit  bool
	OutputExplicit bool
}

// Key builds the canonical case-insensitive pricing-map key for a provider/model.
func Key(provider, model string) string {
	return strings.ToLower(provider) + ":" + strings.ToLower(model)
}

// DefaultPath returns ~/.config/llambo/model-costs.csv.
// Returns empty string on error.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "llambo", "model-costs.csv")
}

// Load reads the CSV at path. Returns nil map (no error) if file is absent.
// Empty cost cells are treated as unknown. Key format: "<provider>:<model>".
//
// Expected CSV format (with header row):
//
//	provider,model,input_per_1m_usd,output_per_1m_usd,notes
func Load(path string) (map[string]ModelCost, error) {
	if path == "" {
		return nil, nil
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open model-costs CSV %q: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comment = '#'
	r.TrimLeadingSpace = true

	// Skip header row.
	if _, err := r.Read(); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("read CSV header %q: %w", path, err)
	}

	result := make(map[string]ModelCost)

	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read CSV row %q: %w", path, err)
		}

		// Require at least provider and model columns.
		if len(rec) < 2 {
			continue
		}
		provider := rec[0]
		model := rec[1]
		if provider == "" || model == "" {
			continue
		}

		var mc ModelCost
		if len(rec) > 2 && rec[2] != "" {
			mc.InputPer1M, _ = strconv.ParseFloat(rec[2], 64)
			mc.InputExplicit = true
		}
		if len(rec) > 3 && rec[3] != "" {
			mc.OutputPer1M, _ = strconv.ParseFloat(rec[3], 64)
			mc.OutputExplicit = true
		}

		result[Key(provider, model)] = mc
	}

	return result, nil
}
