package evals

import (
	"context"
	"database/sql"
	"fmt"
)

func insertObservationIndexRows(ctx context.Context, db *sql.DB, observations []Observation) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO observations VALUES(NULL,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer func() { _ = stmt.Close() }()
	for i, observation := range observations {
		if err := observation.Validate(); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("observation %d: %w", i+1, err)
		}
		if _, err := stmt.ExecContext(ctx, observation.SourceID, observation.SourceRevision, observation.SourceSHA256, observation.Benchmark, observation.BenchmarkVersion, observation.Cohort, observation.ModelID, observation.OrganizationID, observation.RawScore, observation.Unit, observation.Direction, observation.Methodology, observation.Judge, observation.SampleSize, observation.EvidenceGrade, observation.FetchedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), observation.Locator, string(observation.SourceClass)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert observation %d: %w", i+1, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
