package evals

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func writeSyncedJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeDurableFile(path, data, 0, 0o600, ".llambo-writing-*")
}

func writeDurableFile(path string, data []byte, dirMode, fileMode os.FileMode, pattern string) error {
	dir := filepath.Dir(path)
	if dirMode != 0 {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
