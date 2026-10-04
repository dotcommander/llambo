package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpdatePreservesBytesOnCallbackAndLoadFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		seed          []byte
		callbackError bool
	}{
		{
			name:          "callback failure",
			seed:          catalogBytes(t, &Catalog{Version: 1, Providers: map[string]*ProviderCatalog{}}),
			callbackError: true,
		},
		{
			name: "corrupt catalog",
			seed: []byte("not json"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "catalog.json")
			require.NoError(t, os.WriteFile(path, test.seed, 0o644))
			called := false
			require.Error(t, Update(context.Background(), path, func(cat *Catalog) error {
				called = true
				if test.callbackError {
					ensureProviderCatalog(cat, "unpublished")
					return errors.New("stop")
				}
				return nil
			}))
			require.Equal(t, test.callbackError, called)
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, test.seed, got)
			require.Empty(t, catalogTemps(t, path))
		})
	}
}

func TestUpdateRejectsNilCallbackAndCanceledContext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "catalog.json")
	require.Error(t, Update(context.Background(), path, nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	require.ErrorIs(t, Update(ctx, path, func(*Catalog) error { called = true; return nil }), context.Canceled)
	require.False(t, called)
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestUpdateCancellationWhileLockIsHeldDoesNotPublish(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("subprocess integration")
	}

	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, Save(path, &Catalog{Version: 1, Providers: map[string]*ProviderCatalog{}}))
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	_, _, holderOut := startCatalogUpdateProcess(t, path, "holder", true)
	require.Equal(t, "attempting", <-holderOut)
	require.Equal(t, "locked", <-holderOut)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	waiting := &lockWaitContext{Context: ctx, entered: make(chan struct{})}
	result := make(chan error, 1)
	called := make(chan struct{}, 1)
	go func() {
		result <- Update(waiting, path, func(*Catalog) error {
			called <- struct{}{}
			return nil
		})
	}()
	select {
	case <-waiting.entered:
	case <-ctx.Done():
		t.Fatal("update did not reach lock contention")
	}
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	select {
	case <-called:
		t.Fatal("callback ran while catalog lock was held")
	default:
	}
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestUpdateCanonicalizesParentAndPreservesTargetSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	aliasDir := filepath.Join(root, "alias")
	require.NoError(t, os.Mkdir(realDir, 0o755))
	require.NoError(t, os.Symlink(realDir, aliasDir))
	aliasPath := filepath.Join(aliasDir, "catalog.json")
	realPath := filepath.Join(realDir, "catalog.json")
	canonicalDir, err := filepath.EvalSymlinks(realDir)
	require.NoError(t, err)
	canonical, err := canonicalCatalogPath(aliasPath)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(canonicalDir, "catalog.json"), canonical)

	oldTarget := filepath.Join(realDir, "old.json")
	require.NoError(t, os.WriteFile(oldTarget, []byte("old"), 0o644))
	require.NoError(t, os.Symlink(oldTarget, aliasPath))
	require.NoError(t, Save(aliasPath, &Catalog{Version: 1, Providers: map[string]*ProviderCatalog{}}))
	info, err := os.Lstat(realPath)
	require.NoError(t, err)
	require.True(t, info.Mode()&os.ModeSymlink != 0)
	old, err := os.ReadFile(oldTarget)
	require.NoError(t, err)
	require.NotEqual(t, []byte("old"), old)
}

func TestPublishCatalogCleansUpFailedReplacement(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "catalog.json")
	require.NoError(t, os.Mkdir(target, 0o755))
	require.Error(t, publishCatalog(target, &Catalog{Version: 1}, nil))
	require.Empty(t, catalogTemps(t, target))
}

func TestUpdateUsesRetainedReusableLock(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "catalog.json")
	for _, id := range []string{"first", "second"} {
		require.NoError(t, Update(context.Background(), path, func(cat *Catalog) error {
			if cat.Providers == nil {
				cat.Providers = make(map[string]*ProviderCatalog)
			}
			pc := ensureProviderCatalog(cat, "provider")
			ensureModelEntry(pc, id, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
			return nil
		}))
	}
	_, err := os.Stat(path + ".lock")
	require.NoError(t, err)
	cat, err := Load(path)
	require.NoError(t, err)
	require.Contains(t, cat.Providers["provider"].Models, "first")
	require.Contains(t, cat.Providers["provider"].Models, "second")
}

type lockWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *lockWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestUpdateCancellationAfterMutationPreservesCatalog(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, Save(path, &Catalog{Version: 1, Providers: map[string]*ProviderCatalog{}}))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	err = Update(ctx, path, func(cat *Catalog) error {
		ensureProviderCatalog(cat, "unpublished")
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Empty(t, catalogTemps(t, path))
}

func catalogBytes(t *testing.T, cat *Catalog) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, Save(path, cat))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func catalogTemps(t *testing.T, path string) []string {
	t.Helper()
	temps, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	require.NoError(t, err)
	return temps
}
