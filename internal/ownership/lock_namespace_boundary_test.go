package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestLockNamespaceRefusesLeafReplacedWhileWaiting(t *testing.T) {
	f := newFixture(t)
	holder := f.store()
	releaseHolder, err := holder.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer releaseHolder()
	retained := holder.LockPath() + "-retained"
	hit := false
	waiter := f.store(WithLockTimeout(time.Second), WithLockObserver(func(event LockEvent) {
		if event.Kind != LockContended || hit {
			return
		}
		hit = true
		if err := os.Rename(holder.LockPath(), retained); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(holder.LockPath(), []byte("inert replacement diagnostic"), 0o640); err != nil {
			t.Fatal(err)
		}
		releaseHolder()
	}))
	release, err := waiter.Lock()
	if release != nil {
		release()
	}
	if !hit || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("replaced waiting lock authorized metadata/session: %v", err)
	}
	if data, err := os.ReadFile(holder.LockPath()); err != nil || string(data) != "inert replacement diagnostic" {
		t.Fatalf("replacement metadata changed: %q %v", data, err)
	}
	if waiter.LockDepth() != 0 {
		t.Fatal("refused waiter left held session")
	}
}

// spec: owned-entry-reconciliation/lock-directory-retarget-preserves-foreign-metadata
func TestLockRefusesDirectoryRetargetAfterStoreAdmission(t *testing.T) {
	f := newFixture(t)
	lockDir := filepath.Join(f.stateRoot, lockDirName)
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := f.store()
	foreign := t.TempDir()
	foreignLeaf := filepath.Join(foreign, filepath.Base(store.LockPath()))
	if err := os.WriteFile(foreignLeaf, []byte("inert foreign lock diagnostic"), 0o640); err != nil {
		t.Fatal(err)
	}
	retained := lockDir + "-retained"
	if err := os.Rename(lockDir, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, lockDir); err != nil {
		t.Fatal(err)
	}
	release, err := store.Lock()
	if release != nil {
		release()
	}
	if !errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("retargeted lock directory admitted foreign writer: %v", err)
	}
	if data, err := os.ReadFile(foreignLeaf); err != nil || string(data) != "inert foreign lock diagnostic" {
		t.Fatalf("foreign diagnostic changed: %q %v", data, err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
		t.Fatalf("refusal wrote to retained original namespace: %v %v", entries, err)
	}
	if err := os.Remove(lockDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, lockDir); err != nil {
		t.Fatal(err)
	}
	release, err = store.Lock()
	if err != nil || release == nil {
		t.Fatalf("restored original namespace could not acquire: %v", err)
	}
	release()
	if data, err := os.ReadFile(store.LockPath()); err != nil || len(data) == 0 {
		t.Fatalf("healthy lock did not write actual diagnostics: %q %v", data, err)
	}
}
