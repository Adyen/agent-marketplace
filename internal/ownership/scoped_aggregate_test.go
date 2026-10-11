package ownership

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreReadAggregateAbsenceAndRootAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	store, err := Open(alias, WithStateRoot(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"missing.json", "nested/missing.json"} {
		if data, err := store.ReadAggregate(rel); data != nil || !errors.Is(err, os.ErrNotExist) || !os.IsNotExist(err) {
			t.Fatalf("genuine absence contract lost for %s: %q %v", rel, data, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "native.json"), []byte("{\"native\":true}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if data, err := store.ReadAggregate("native.json"); err != nil || string(data) != "{\"native\":true}\n" {
		t.Fatalf("admitted canonical root alias failed: %q %v", data, err)
	}
	for _, rel := range []string{"../outside.json", "/outside.json", "."} {
		if data, err := store.ReadAggregate(rel); data != nil || !errors.Is(err, ErrUnsafeAggregateFile) {
			t.Fatalf("escaped aggregate path accepted: %s %q %v", rel, data, err)
		}
	}
}

func TestStoreReadAggregateRefusesReplacedRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	payload := []byte("{\"native\":true}\n")
	for _, admitted := range []string{"existing", "created-later"} {
		t.Run(admitted, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			if admitted == "existing" {
				if err := os.Mkdir(root, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			store, err := Open(root, WithStateRoot(t.TempDir()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "native.json"), payload, 0o640); err != nil {
				t.Fatal(err)
			}
			if data, err := store.ReadAggregate("native.json"); err != nil || !bytes.Equal(data, payload) {
				t.Fatalf("admitted root read control failed: %q %v", data, err)
			}
			retained := filepath.Join(t.TempDir(), "retained-root")
			if err := os.Rename(root, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "native.json"), payload, 0o640); err != nil {
				t.Fatal(err)
			}
			if data, err := store.ReadAggregate("native.json"); data != nil || !errors.Is(err, ErrGenerationConflict) || os.IsNotExist(err) {
				t.Fatalf("replaced root accepted for aggregate read: %q %v", data, err)
			}
			if data, err := os.ReadFile(filepath.Join(retained, "native.json")); err != nil || !bytes.Equal(data, payload) {
				t.Fatalf("retained admitted root changed: %q %v", data, err)
			}
		})
	}
}

// spec: owned-entry-reconciliation/scoped-aggregate-reader-refuses-intermediate-alias
func TestStoreReadAggregateRefusesIntermediateAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root, foreign := t.TempDir(), t.TempDir()
	store, err := Open(root, WithStateRoot(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "native.json"), []byte("{\"native\":true}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	data, err := store.ReadAggregate("native.json")
	if err != nil || string(data) != "{\"native\":true}\n" {
		t.Fatalf("regular native read control failed: %q %v", data, err)
	}
	payload := []byte("{\"foreign\":\"inert bytes\"}\n")
	path := filepath.Join(foreign, "aggregate.json")
	if err := os.WriteFile(path, payload, 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, filepath.Join(root, "nested")); err != nil {
		t.Fatal(err)
	}
	data, err = store.ReadAggregate("nested/aggregate.json")
	if data != nil || !errors.Is(err, ErrUnsafeAggregateFile) {
		t.Fatalf("scoped reader imported aliased foreign payload: %q %v", data, err)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, payload) {
		t.Fatalf("foreign bytes changed: %q %v", after, err)
	}
	if after, err := os.Stat(path); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign metadata changed: %v", err)
	}
}
