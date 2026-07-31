package cmd

// Catalog subcommands for `llambo models`:
//   models catalog          — list models from catalog
//   models catalog pin      — pin a model
//   models catalog unpin    — unpin a model
//   models catalog avoid    — mark a model to avoid
//   models catalog unavoid  — clear avoid flag

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
)

var (
	catalogFilterPinned bool
	catalogFilterAvoid  bool
	catalogFilterNew    bool
	catalogFilterFree   bool
	catalogFilterTag    string
	catalogShowMetadata bool
	catalogOutputJSON   bool
	avoidReason         string
)

func runModelsCatalog(cmd *commandIO, args []string) error {
	out := cmd.OutOrStdout()
	catPath, err := catalog.CatalogPath()
	if err != nil {
		return err
	}
	cat, err := catalog.Load(catPath)
	if err != nil {
		return err
	}

	// Optional provider filter.
	filterProvider := ""
	if len(args) > 0 {
		filterProvider = args[0]
	}

	type row struct {
		Provider        string
		Model           string
		FirstSeen       string
		UpstreamCreated string
		Status          string
		Tags            string
		Health          string
		Cost            string
		Context         string
		Params          string
		Reasoning       string
		Quality         string
	}
	var rows []row
	costMap, err := costs.LoadAll()
	if err != nil {
		return err
	}

	for _, pname := range sortedCatalogProviders(cat) {
		if filterProvider != "" && pname != filterProvider {
			continue
		}
		pc := cat.Providers[pname]

		for _, mid := range sortedModelIDs(pc) {
			m := pc.Models[mid]

			if catalogFilterPinned && !m.Pinned {
				continue
			}
			if catalogFilterAvoid && !m.Avoid {
				continue
			}
			if catalogFilterNew && !isNewModel(m, pc.LastRefresh) {
				continue
			}
			costStatus, inputCost, outputCost := catalog.CostForEntry(costMap, pname, mid, m)
			if catalogFilterFree && costStatus != catalog.CostFree {
				continue
			}
			if catalogFilterTag != "" && !catalog.HasTag(m, catalogFilterTag) {
				continue
			}

			status := ""
			if m.Pinned {
				status = "pinned"
			}
			if m.Avoid {
				s := "avoid"
				if m.AvoidReason != "" {
					s += ": " + m.AvoidReason
				}
				status = s
			}
			if m.QuarantineUntil.After(time.Now()) {
				if status != "" {
					status += ", "
				}
				status += "quarantine until " + m.QuarantineUntil.Format(time.RFC3339)
			}

			uc := "—"
			if !m.UpstreamCreated.IsZero() {
				uc = m.UpstreamCreated.Format("2006-01-02")
			}

			rows = append(rows, row{
				Provider:        pname,
				Model:           mid,
				FirstSeen:       humanAge(m.FirstSeen),
				UpstreamCreated: uc,
				Status:          status,
				Tags:            strings.Join(m.Tags, ","),
				Health:          healthLabel(m),
				Cost:            catalog.PriceLabel(costStatus, inputCost, outputCost),
				Context:         contextLabel(m),
				Params:          paramsLabel(m),
				Reasoning:       reasoningLabel(m),
				Quality:         qualityLabel(m),
			})
		}
	}

	if len(rows) == 0 {
		if catalogOutputJSON {
			fmt.Fprintln(out, "[]")
			return nil
		}
		fmt.Fprintln(out, "No catalog entries found. Run: llambo providers refresh")
		return nil
	}

	if catalogOutputJSON {
		data, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	if catalogShowMetadata {
		fmt.Fprintf(out, "%-14s %-36s %-10s %-8s %-12s %-10s %-18s %s\n",
			"PROVIDER", "MODEL", "CTX", "PARAMS", "REASONING", "QUALITY", "TAGS", "STATUS")
		fmt.Fprintln(out, strings.Repeat("-", 132))
		for _, r := range rows {
			fmt.Fprintf(out, "%-14s %-36s %-10s %-8s %-12s %-10s %-18s %s\n",
				r.Provider, r.Model, r.Context, r.Params, r.Reasoning, r.Quality, r.Tags, r.Status)
		}
		return nil
	}

	fmt.Fprintf(out, "%-14s %-36s %-12s %-17s %-18s %-12s %-18s %s\n",
		"PROVIDER", "MODEL", "FIRST_SEEN", "UPSTREAM_CREATED", "TAGS", "HEALTH", "COST", "STATUS")
	fmt.Fprintln(out, strings.Repeat("-", 136))
	for _, r := range rows {
		fmt.Fprintf(out, "%-14s %-36s %-12s %-17s %-18s %-12s %-18s %s\n",
			r.Provider, r.Model, r.FirstSeen, r.UpstreamCreated, r.Tags, r.Health, r.Cost, r.Status)
	}
	return nil
}

func runModelsCatalogPin(cmd *commandIO, args []string) error {
	return mutateCatalogEntry(cmd, args[0], args[1], func(m *catalog.ModelEntry) {
		m.Pinned = true
	})
}

func runModelsCatalogUnpin(cmd *commandIO, args []string) error {
	return mutateCatalogEntry(cmd, args[0], args[1], func(m *catalog.ModelEntry) {
		m.Pinned = false
	})
}

func runModelsCatalogAvoid(cmd *commandIO, args []string) error {
	return mutateCatalogEntry(cmd, args[0], args[1], func(m *catalog.ModelEntry) {
		m.Avoid = true
		m.AvoidReason = avoidReason
		m.AvoidSince = time.Now().UTC()
	})
}

func runModelsCatalogUnavoid(cmd *commandIO, args []string) error {
	return mutateCatalogEntry(cmd, args[0], args[1], func(m *catalog.ModelEntry) {
		m.Avoid = false
		m.AvoidReason = ""
		m.AvoidSince = time.Time{}
	})
}

func runModelsCatalogTag(cmd *commandIO, args []string) error {
	return mutateCatalogEntry(cmd, args[0], args[1], func(m *catalog.ModelEntry) {
		catalog.AddTag(m, args[2])
	})
}

func runModelsCatalogUntag(cmd *commandIO, args []string) error {
	return mutateCatalogEntry(cmd, args[0], args[1], func(m *catalog.ModelEntry) {
		catalog.RemoveTag(m, args[2])
	})
}

func runModelsCatalogImportQuality(cmd *commandIO, args []string) error {
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read quality import: %w", err)
	}
	var records []catalog.QualityImportRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("parse quality import: %w", err)
	}
	if len(records) == 0 {
		return fmt.Errorf("quality import contains no records")
	}

	catPath, err := catalog.CatalogPath()
	if err != nil {
		return err
	}
	cat, err := catalog.Load(catPath)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for i, record := range records {
		if err := catalog.RecordQualityEvidence(cat, record, now); err != nil {
			return fmt.Errorf("quality record %d: %w", i+1, err)
		}
	}
	if err := catalog.Save(catPath, cat); err != nil {
		return fmt.Errorf("save catalog: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Imported %d quality record(s)\n", len(records))
	return nil
}

// mutateCatalogEntry loads, mutates one entry, and saves atomically.
func mutateCatalogEntry(cmd *commandIO, providerName, modelID string, mutate func(*catalog.ModelEntry)) error {
	catPath, err := catalog.CatalogPath()
	if err != nil {
		return err
	}
	cat, err := catalog.Load(catPath)
	if err != nil {
		return err
	}

	pc, ok := cat.Providers[providerName]
	if !ok {
		return fmt.Errorf("provider %q not found in catalog (run: llambo providers refresh)", providerName)
	}
	entry, ok := pc.Models[modelID]
	if !ok {
		return fmt.Errorf("model %q not found for provider %q in catalog", modelID, providerName)
	}

	mutate(entry)

	if err := catalog.Save(catPath, cat); err != nil {
		return fmt.Errorf("save catalog: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Updated %s/%s\n", providerName, modelID)
	return nil
}
