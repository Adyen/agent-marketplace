package ownership

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestLockMetadataPreservesLinkAddedAfterAcquisition(t *testing.T) {
	f := newFixture(t)
	foreign := filepath.Join(t.TempDir(), "inert-linked-metadata")
	store := f.store()
	if err := os.MkdirAll(filepath.Dir(store.LockPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.LockPath(), []byte("inert prior diagnostic"), 0o640); err != nil {
		t.Fatal(err)
	}
	hit := false
	store = f.store(WithClock(func() time.Time {
		hit = true
		if err := os.Link(store.LockPath(), foreign); err != nil {
			t.Fatal(err)
		}
		return time.Now()
	}))
	release, err := store.Lock()
	if err != nil || release == nil {
		t.Fatalf("kernel lock failed: %v", err)
	}
	release()
	if !hit {
		t.Fatal("actual metadata producer did not run")
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "inert prior diagnostic" {
		t.Fatalf("metadata write changed now-linked bytes: %q %v", data, err)
	}
	if store.LockDepth() != 0 {
		t.Fatal("diagnostic refusal leaked kernel session")
	}
}

// spec: owned-entry-reconciliation/lock-admission-refuses-hardlinked-metadata
func TestLockContextRefusesHardlinkedMetadata(t *testing.T) {
	store := newFixture(t).store()
	if err := os.MkdirAll(filepath.Dir(store.LockPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "inert-foreign-metadata")
	if err := os.WriteFile(foreign, []byte("inert foreign bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(foreign, store.LockPath()); err != nil {
		t.Fatal(err)
	}
	release, err := store.LockContext(context.Background())
	if release != nil {
		release()
	}
	if !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("multiply linked metadata admitted to kernel lock/write: %v", err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "inert foreign bytes" {
		t.Fatalf("lock acquisition changed foreign bytes: %q %v", data, err)
	}
	if store.LockDepth() != 0 {
		t.Fatal("refused metadata left a held session")
	}
	if err := os.Remove(store.LockPath()); err != nil {
		t.Fatal(err)
	}
	release, err = store.LockContext(context.Background())
	if err != nil || release == nil {
		t.Fatalf("real regular lock acquisition failed: %v", err)
	}
	release()
	data, err := os.ReadFile(store.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "pid="+strconv.Itoa(os.Getpid())+" ") {
		t.Fatalf("healthy lock did not write actual diagnostic metadata: %q", data)
	}
}
