package evals

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/parquet-go/parquet-go"
	_ "modernc.org/sqlite"
)

func DecodeObservationsParquet(data []byte, maxBytes int64) ([]Observation, error) {
	if maxBytes <= 0 {
		maxBytes = maxSourceBody
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("parquet input exceeds %d bytes", maxBytes)
	}
	rows, err := parquet.Read[Observation](bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("decode observations parquet: %w", err)
	}
	for i := range rows {
		if err := rows[i].Validate(); err != nil {
			return nil, fmt.Errorf("parquet row %d: %w", i+1, err)
		}
	}
	return rows, nil
}

func RebuildObservationIndex(ctx context.Context, path string, observations []Observation) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); err == nil {
		return fmt.Errorf("temporary index already exists: %s", tmp)
	} else if !os.IsNotExist(err) {
		return err
	}
	db, err := sql.Open("sqlite", tmp)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		_ = db.Close()
		if !complete {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := db.ExecContext(ctx, `CREATE TABLE observations (
observation_id INTEGER PRIMARY KEY,
source_id TEXT NOT NULL, source_revision TEXT NOT NULL, source_sha256 TEXT NOT NULL,
benchmark TEXT NOT NULL, benchmark_version TEXT NOT NULL, cohort TEXT NOT NULL,
model_id TEXT NOT NULL, organization_id TEXT NOT NULL, raw_score REAL NOT NULL,
unit TEXT NOT NULL, direction TEXT NOT NULL, methodology TEXT NOT NULL, judge TEXT NOT NULL,
sample_size INTEGER NOT NULL, evidence_grade TEXT NOT NULL, fetched_at TEXT NOT NULL,
provenance_locator TEXT NOT NULL, source_class TEXT NOT NULL) STRICT`); err != nil {
		return fmt.Errorf("create observation index: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO observations VALUES(NULL,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for i, observation := range observations {
		if err := observation.Validate(); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return fmt.Errorf("observation %d: %w", i+1, err)
		}
		if _, err := stmt.ExecContext(ctx, observation.SourceID, observation.SourceRevision, observation.SourceSHA256, observation.Benchmark, observation.BenchmarkVersion, observation.Cohort, observation.ModelID, observation.OrganizationID, observation.RawScore, observation.Unit, observation.Direction, observation.Methodology, observation.Judge, observation.SampleSize, observation.EvidenceGrade, observation.FetchedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), observation.Locator, string(observation.SourceClass)); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return fmt.Errorf("insert observation %d: %w", i+1, err)
		}
	}
	if err := stmt.Close(); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return fmt.Errorf("check observation index integrity: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("observation index integrity: %s", integrity)
	}
	if err := db.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	complete = true
	return nil
}

func EncodeObservationsJSONL(observations []Observation) ([]byte, error) {
	rows := append([]Observation(nil), observations...)
	sort.Slice(rows, func(i, j int) bool {
		return observationLess(rows[i], rows[j])
	})
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	for _, row := range rows {
		if err := row.Validate(); err != nil {
			return nil, err
		}
		if err := encoder.Encode(row); err != nil {
			return nil, err
		}
	}
	return output.Bytes(), nil
}
