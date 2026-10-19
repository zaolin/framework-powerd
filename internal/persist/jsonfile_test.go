package persist

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type sample struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestSaveLoadJSON_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	want := sample{Name: "energy", Count: 42}

	if err := SaveJSON(path, want); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	var got sample
	if err := LoadJSON(path, &got); err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("file mode = %o, want 644", perm)
	}
}

func TestSaveJSON_NoTempFilesLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := SaveJSON(path, sample{Name: "x"}); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Errorf("unexpected leftover file %q", e.Name())
		}
	}
}

func TestLoadJSON_Missing(t *testing.T) {
	var got sample
	err := LoadJSON(filepath.Join(t.TempDir(), "missing.json"), &got)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestLoadJSON_Corrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{oops"), 0644); err != nil {
		t.Fatal(err)
	}
	var got sample
	if err := LoadJSON(path, &got); err == nil {
		t.Error("expected an error for corrupt JSON")
	}
}

func TestSaveJSON_EmptyPath(t *testing.T) {
	if err := SaveJSON("", sample{}); err == nil {
		t.Error("expected an error for an empty path")
	}
}

func TestSaveJSON_OverwritesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := SaveJSON(path, sample{Name: "old", Count: 1}); err != nil {
		t.Fatal(err)
	}
	if err := SaveJSON(path, sample{Name: "new", Count: 2}); err != nil {
		t.Fatal(err)
	}

	var got sample
	if err := LoadJSON(path, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "new" || got.Count != 2 {
		t.Errorf("got %+v, want the second write", got)
	}
}
