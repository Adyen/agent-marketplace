package ownership

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// spec: owned-entry-reconciliation/existing-admitted-root-replaced-with-an-ordinary-directory
func TestStoreReadAggregateRefusesOrdinaryRootReplacement(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	rel := "settings.json"
	payload := []byte("{\"mcpServers\":{\"first\":{\"command\":\"echo\"}}}\n")
	if err := os.WriteFile(filepath.Join(root, rel), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root, WithStateRoot(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.ReadAggregate(rel)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("control read failed: %q %v", data, err)
	}
	// Replace the root with an ordinary foreign directory containing a copied
	// aggregate with identical bytes.
	foreign := t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, rel), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(filepath.Dir(root), "retained-root")
	if err := os.Rename(root, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(foreign, root); err != nil {
		t.Fatal(err)
	}
	data, err = store.ReadAggregate(rel)
	if data != nil || !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("ordinary root replacement accepted: %q %v", data, err)
	}
	// Both directory generations are preserved.
	for _, path := range []string{
		filepath.Join(root, rel),
		filepath.Join(retained, rel),
	} {
		if after, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(after, payload) {
			t.Fatalf("payload changed at %s: %q %v", path, after, readErr)
		}
	}
}

// spec: owned-entry-reconciliation/missing-admitted-root-may-be-created-legitimately
func TestStoreReadAggregateAbsentRootAllowsLaterCreation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	parent := t.TempDir()
	root := filepath.Join(parent, ".gemini")
	rel := "settings.json"
	store, err := Open(root, WithStateRoot(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	// Initial read: root genuinely absent.
	data, err := store.ReadAggregate(rel)
	if data != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent root did not report absence: %q %v", data, err)
	}
	// Later authorized creation.
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("{\"mcpServers\":{\"first\":{\"command\":\"echo\"}}}\n")
	if err := os.WriteFile(filepath.Join(root, rel), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err = store.ReadAggregate(rel)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("later creation read failed: %q %v", data, err)
	}
}

func TestStoreReadAggregateSameRootUpdateRemainsValid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	rel := "settings.json"
	if err := os.WriteFile(filepath.Join(root, rel), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root, WithStateRoot(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	// A legitimate same-root content update (different bytes, same directory
	// object) must remain valid after admission.
	payload := []byte("{\"mcpServers\":{\"first\":{\"command\":\"echo\"}}}\n")
	if err := os.WriteFile(filepath.Join(root, rel), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := store.ReadAggregate(rel)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("same-root update rejected: %q %v", data, err)
	}
}

func TestStoreReadAggregateAdmittedRootDisappearedRefuses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	rel := "settings.json"
	if err := os.WriteFile(filepath.Join(root, rel), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root, WithStateRoot(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	data, err := store.ReadAggregate(rel)
	if data != nil || !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("disappeared admitted root accepted: %q %v", data, err)
	}
}
