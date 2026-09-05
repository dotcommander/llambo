package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const catalogLockRetry = 10 * time.Millisecond

// Save atomically replaces a complete catalog snapshot for exclusive callers.
// Call Update for read-modify-write changes shared with other catalog writers.
func Save(path string, cat *Catalog) error {
	path, err := canonicalCatalogPath(path)
	if err != nil {
		return err
	}
	return publishCatalog(path, cat, nil)
}

// Update applies one catalog mutation while holding the catalog's retained advisory lock.
// The callback runs once against freshly loaded state and must not perform network
// work or recursively update this path. All cooperating writers must use Update;
// older binaries and direct Save calls are not covered by the advisory lock.
// Cancellation before publication leaves the catalog unchanged. An error stating
// "catalog published" means replacement succeeded; do not replay the callback.
// The .lock file is retained so waiting writers always share one lock inode.
func Update(ctx context.Context, path string, mutate func(*Catalog) error) error {
	if mutate == nil {
		return errors.New("catalog update callback is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := canonicalCatalogPath(path)
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open catalog lock: %w", err)
	}
	defer lock.Close()
	if err := lockCatalog(ctx, lock); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()

	if err := ctx.Err(); err != nil {
		return err
	}
	cat, err := Load(path)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := mutate(cat); err != nil {
		return err
	}
	return publishCatalog(path, cat, ctx.Err)
}

func canonicalCatalogPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("absolute catalog path: %w", err)
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create catalog dir: %w", err)
	}
	canonicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("canonicalize catalog dir: %w", err)
	}
	return filepath.Join(canonicalDir, filepath.Base(abs)), nil
}

func lockCatalog(ctx context.Context, lock *os.File) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return fmt.Errorf("lock catalog: %w", err)
		}
		timer := time.NewTimer(catalogLockRetry)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func publishCatalog(path string, cat *Catalog, beforePublish func() error) error {
	data, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal catalog: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create catalog tmp: %w", err)
	}
	tmpPath := tmp.Name()
	published := false
	defer func() {
		if !published {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod catalog tmp: %w", err)
	}
	if n, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write catalog tmp: %w", err)
	} else if n != len(data) {
		_ = tmp.Close()
		return fmt.Errorf("write catalog tmp: short write %d of %d bytes", n, len(data))
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync catalog tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close catalog tmp: %w", err)
	}
	if beforePublish != nil {
		if err := beforePublish(); err != nil {
			return err
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish catalog: %w", err)
	}
	published = true
	if err := syncCatalogDir(dir); err != nil {
		return fmt.Errorf("catalog published but sync directory: %w", err)
	}
	return nil
}

func syncCatalogDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}
