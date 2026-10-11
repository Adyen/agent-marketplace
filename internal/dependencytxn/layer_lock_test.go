package dependencytxn

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func layerLockBytes(t *testing.T, scope lockfile.Scope) []byte {
	t.Helper()
	data, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReadLayerOverlayPrecedenceWithoutDiskRead(t *testing.T) {
	dir := t.TempDir()
	canonical := filepath.Join(dir, "lock.yaml")
	requested := filepath.Join(dir, "requested-lock.yaml")
	if err := os.WriteFile(canonical, []byte("not: [yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	canonicalData := layerLockBytes(t, lockfile.ScopeRepo)
	requestedData := layerLockBytes(t, lockfile.ScopeUser)

	ev, _, err := ReadLayerLock(canonical, requested, lockfile.ScopeRepo, map[string][]byte{
		canonical: canonicalData,
		requested: requestedData,
	})
	if err != nil {
		t.Fatalf("canonical overlay rejected: %v", err)
	}
	if ev.Digest == "" || !ev.Overlay || ev.Document.Scope != lockfile.ScopeRepo {
		t.Fatalf("canonical overlay evidence = %#v", ev)
	}
	if ev.Digest != fmt.Sprintf("%x", sha256.Sum256(canonicalData)) {
		t.Fatalf("digest = %q, want sha256 of canonical overlay", ev.Digest)
	}

	ev, _, err = ReadLayerLock(canonical, requested, lockfile.ScopeUser, map[string][]byte{
		requested: requestedData,
	})
	if err != nil {
		t.Fatalf("requested overlay rejected: %v", err)
	}
	if ev.Digest == "" || !ev.Overlay || ev.Document.Scope != lockfile.ScopeUser {
		t.Fatalf("requested overlay evidence = %#v", ev)
	}
}

func TestReadLayerAbsentReturnsNoEvidence(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "lock.yaml")
	ev, _, err := ReadLayerLock(missing, missing, lockfile.ScopeRepo, nil)
	if err != nil {
		t.Fatalf("absent lock returned error: %v", err)
	}
	if ev.Digest != "" || ev.Overlay {
		t.Fatalf("absent lock evidence = %#v, want empty", ev)
	}
}

func TestReadLayerDiskReadAndScopeValidation(t *testing.T) {
	dir := t.TempDir()
	canonical := filepath.Join(dir, "lock.yaml")
	data := layerLockBytes(t, lockfile.ScopeRepo)
	if err := os.WriteFile(canonical, data, 0o600); err != nil {
		t.Fatal(err)
	}

	ev, _, err := ReadLayerLock(canonical, canonical, lockfile.ScopeRepo, nil)
	if err != nil {
		t.Fatalf("disk read rejected: %v", err)
	}
	if ev.Digest == "" || ev.Overlay || ev.Document.Scope != lockfile.ScopeRepo {
		t.Fatalf("disk evidence = %#v", ev)
	}
	if ev.Digest != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("digest = %q", ev.Digest)
	}

	ev, decode, err := ReadLayerLock(canonical, canonical, lockfile.ScopeUser, nil)
	if err == nil || !decode {
		t.Fatalf("wrong-scope error = %v decode=%v, want decode failure", err, decode)
	}
	var coded *lockfile.Error
	if !errors.As(err, &coded) || coded.Code != lockfile.CodeScopeMismatch {
		t.Fatalf("wrong-scope inner = %v, want lock.scope-mismatch", err)
	}
	if ev.Digest == "" || ev.Overlay {
		t.Fatalf("decode-failure evidence = %#v, want digest retained", ev)
	}
}

func TestReadLayerUnsafeAndMalformedErrors(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "lock.yaml")
	target := filepath.Join(dir, "target.yaml")
	if err := os.WriteFile(target, layerLockBytes(t, lockfile.ScopeRepo), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	ev, decode, err := ReadLayerLock(link, link, lockfile.ScopeRepo, nil)
	if err == nil || decode {
		t.Fatalf("unsafe error = %v decode=%v, want read failure", err, decode)
	}
	if !errors.Is(err, lockfile.ErrUnsafeFile) {
		t.Fatalf("unsafe error = %v, want ErrUnsafeFile", err)
	}
	if ev.Digest != "" {
		t.Fatalf("unsafe evidence = %#v, want absent", ev)
	}

	malformed := filepath.Join(dir, "malformed.yaml.lock")
	if err := os.WriteFile(malformed, []byte("not: [yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, decode, err := ReadLayerLock(malformed, malformed, lockfile.ScopeRepo, nil); err == nil || !decode {
		t.Fatalf("malformed error = %v decode=%v, want decode failure", err, decode)
	}
}
