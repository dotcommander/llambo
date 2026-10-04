// Package filetxn provides cooperating local-file transactions and publication.
package filetxn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// CanonicalPath follows existing file symlinks, preserving aliases on publication.
// Dangling or cyclic file symlinks are rejected rather than replaced.
func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err == nil {
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", fmt.Errorf("resolve file path: %w", err)
		}
		return resolved, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(abs)), nil
}

// WithLock executes once under a retained canonical-path advisory lock. All
// cooperating read-modify-write callers must use this transaction; external
// editors and older binaries are outside the guarantee. Never recurse on path.
func WithLock(ctx context.Context, path string, apply func(string) error) error {
	if apply == nil {
		return errors.New("file transaction callback is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := CanonicalPath(path)
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err := ctx.Err(); err != nil {
		return err
	}
	return apply(path)
}

// WriteAtomic publishes a complete snapshot using a unique adjacent temporary
// file. Call inside WithLock when publication belongs to a read-modify-write.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	path, err := CanonicalPath(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("file published but open directory: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("file published but sync directory: %w", err)
	}
	return nil
}
