package evals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// OMLXSettingsGuard owns the only permitted persistent mutation in paired MTP
// evaluation. It restores the original bytes, hash, reload state, and health
// after both success and failure. Any third-party change while enabled fails
// closed without overwriting that actor's state.
type OMLXSettingsGuard struct {
	Path        string
	ParentModel string
	Reload      func(context.Context) error
	Health      func(context.Context) error
}

// WithMTPEnabled owns the guarded transition and restoration of exact settings.
func (g OMLXSettingsGuard) WithMTPEnabled(ctx context.Context, enabled bool, run func(context.Context) error) error {
	if strings.TrimSpace(g.Path) == "" || strings.TrimSpace(g.ParentModel) == "" || g.Reload == nil || g.Health == nil || run == nil {
		return errors.New("settings path, parent model, reload, health, and run are required")
	}
	state, err := g.snapshot(enabled)
	if err != nil {
		return err
	}
	if state.changedHash == state.originalHash {
		return g.runUnchangedMTP(ctx, state, run)
	}
	return g.runChangedMTP(ctx, state, run)
}

type mtpSettingsSnapshot struct {
	original, changed         []byte
	originalHash, changedHash string
	mode                      os.FileMode
}

func (g OMLXSettingsGuard) snapshot(enabled bool) (mtpSettingsSnapshot, error) {
	original, err := os.ReadFile(g.Path)
	if err != nil {
		return mtpSettingsSnapshot{}, fmt.Errorf("read model settings: %w", err)
	}
	info, err := os.Stat(g.Path)
	if err != nil {
		return mtpSettingsSnapshot{}, fmt.Errorf("stat model settings: %w", err)
	}
	changed, err := setMTPEnabled(original, g.ParentModel, enabled)
	if err != nil {
		return mtpSettingsSnapshot{}, err
	}
	return mtpSettingsSnapshot{original: original, changed: changed, originalHash: bytesSHA256(original), changedHash: bytesSHA256(changed), mode: info.Mode().Perm()}, nil
}

func (g OMLXSettingsGuard) runUnchangedMTP(ctx context.Context, state mtpSettingsSnapshot, run func(context.Context) error) error {
	if err := g.Reload(ctx); err != nil {
		return fmt.Errorf("reload unchanged model settings: %w", err)
	}
	if err := g.Health(ctx); err != nil {
		return fmt.Errorf("verify OMLX health with unchanged MTP settings: %w", err)
	}
	if err := run(ctx); err != nil {
		return err
	}
	current, err := os.ReadFile(g.Path)
	if err != nil {
		return fmt.Errorf("read unchanged model settings after run: %w", err)
	}
	if bytesSHA256(current) != state.originalHash {
		return errors.New("model settings changed concurrently during guarded run")
	}
	return nil
}

func (g OMLXSettingsGuard) runChangedMTP(ctx context.Context, state mtpSettingsSnapshot, run func(context.Context) error) (err error) {
	current, err := os.ReadFile(g.Path)
	if err != nil {
		return fmt.Errorf("re-read model settings before mutation: %w", err)
	}
	if bytesSHA256(current) != state.originalHash {
		return errors.New("model settings changed concurrently before mutation")
	}
	if err = os.WriteFile(g.Path, state.changed, state.mode); err != nil {
		return fmt.Errorf("write guarded model settings: %w", err)
	}
	defer func() {
		if restoreErr := g.restoreMTP(context.WithoutCancel(ctx), state); restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
	}()
	if err := g.Reload(ctx); err != nil {
		return fmt.Errorf("reload guarded model settings: %w", err)
	}
	if err := g.Health(ctx); err != nil {
		return fmt.Errorf("verify OMLX health after MTP change: %w", err)
	}
	return run(ctx)
}

func (g OMLXSettingsGuard) restoreMTP(ctx context.Context, state mtpSettingsSnapshot) error {
	current, err := os.ReadFile(g.Path)
	if err != nil {
		return fmt.Errorf("read model settings for restore: %w", err)
	}
	if bytesSHA256(current) != state.changedHash {
		return errors.New("model settings changed concurrently during guarded run; refusing overwrite")
	}
	if writeErr := os.WriteFile(g.Path, state.original, state.mode); writeErr != nil { //nolint:gosec // user-supplied local CLI path by design
		return fmt.Errorf("restore model settings: %w", writeErr)
	}
	restored, err := os.ReadFile(g.Path)
	if err != nil {
		return fmt.Errorf("verify restored model settings: %w", err)
	}
	if bytesSHA256(restored) != state.originalHash {
		return errors.New("restored model settings hash mismatch")
	}
	var restoreErr error
	if err := g.Reload(ctx); err != nil {
		restoreErr = errors.Join(restoreErr, fmt.Errorf("reload restored model settings: %w", err))
	}
	if err := g.Health(ctx); err != nil {
		restoreErr = errors.Join(restoreErr, fmt.Errorf("verify OMLX health after restore: %w", err))
	}
	return restoreErr
}

func bytesSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
