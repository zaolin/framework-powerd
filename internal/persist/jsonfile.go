// Package persist provides small, dependency-free helpers for durable JSON
// state files. Writes are atomic: data is written to a temporary file in the
// destination directory, fsynced, renamed into place, and the directory is
// fsynced so a crash or power loss cannot leave a truncated file behind.
package persist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SaveJSON atomically writes v as indented JSON to path.
func SaveJSON(path string, v any) error {
	if path == "" {
		return fmt.Errorf("persist: empty path")
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("persist: marshal: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("persist: create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".metrics-*.tmp")
	if err != nil {
		return fmt.Errorf("persist: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if err := tmp.Chmod(0o644); err != nil {
		cleanup()
		return fmt.Errorf("persist: chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("persist: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("persist: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("persist: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("persist: rename temp file: %w", err)
	}

	// Best-effort directory fsync so the rename itself is durable.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	return nil
}

// LoadJSON reads and decodes a JSON file into v. A missing file is reported as
// os.ErrNotExist so callers can distinguish "fresh start" from corruption.
func LoadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("persist: decode %s: %w", path, err)
	}
	return nil
}
