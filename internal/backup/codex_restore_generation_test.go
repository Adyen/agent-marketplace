package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type codexRestoreObserver struct {
	slog.Handler
	onPreflight        func()
	onPublication      func(string)
	onSymbolic         func(string)
	onDirectory        func(string)
	onDirectoryOpened  func(string)
	onDirectoryDescend func(string)
	onListingTarget    func(string)
	onRestoreSelected  func(string)
}

func (h codexRestoreObserver) Handle(ctx context.Context, record slog.Record) error {
	phase, path := "", ""
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "phase" {
			phase = attr.Value.String()
		}
		if attr.Key == "path" {
			path = attr.Value.String()
		}
		return true
	})
	if phase == "preflight-complete" && h.onPreflight != nil {
		h.onPreflight()
	}
	if phase == "restore-file-publication" && h.onPublication != nil {
		h.onPublication(path)
	}
	if phase == "restore-symbolic-replace" && h.onSymbolic != nil {
		h.onSymbolic(path)
	}
	if phase == "backup-source-directory" && h.onDirectory != nil {
		h.onDirectory(path)
	}
	if phase == "backup-source-directory-opened" && h.onDirectoryOpened != nil {
		h.onDirectoryOpened(path)
	}
	if phase == "backup-source-directory-descend" && h.onDirectoryDescend != nil {
		h.onDirectoryDescend(path)
	}
	if phase == "backup-listing-target" && h.onListingTarget != nil {
		h.onListingTarget(path)
	}
	if phase == "ordinary-restore-selected" && h.onRestoreSelected != nil {
		h.onRestoreSelected(path)
	}
	return h.Handler.Handle(ctx, record)
}

// spec: backup-recovery/codex-restore-refuses-replaced-preflight-generation
func TestRestoreCodexSetRefusesArchiveReplacementAfterPreflight(t *testing.T) {
	for _, member := range []string{"archive", "manifest"} {
		t.Run(member, func(t *testing.T) { testCodexRestorePreflightGeneration(t, member) })
	}
}

func testCodexRestorePreflightGeneration(t *testing.T, member string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".agents")
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(root, "skills", "entry")
	if err := os.WriteFile(native, []byte("inert archived payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	partitions := []CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"skills"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
	operation, err := CreateCodexSet(partitions)
	if err != nil {
		t.Fatal(err)
	}
	archives, err := filepath.Glob(filepath.Join(home, ".agent-marketplace", "backups", "codex", operation+"-machine-agents-*.tar.gz"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("actual agents archive absent: %v %v", archives, err)
	}
	archive := archives[0]
	if member == "manifest" {
		archive = filepath.Join(home, ".agent-marketplace", "backups", "codex", operation+".set.json")
	}
	original, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte("inert current payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(native)
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "original-archive")
	changed := false
	// The replacement runs inside a slog handler during RestoreCodexSet, so it
	// records failures instead of calling t.Fatal mid-restore; they are
	// reported after the restore returns.
	var replaceErr error
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), onPreflight: func() {
		if changed {
			return
		}
		changed = true
		if err := os.Rename(archive, retained); err != nil {
			replaceErr = err
			return
		}
		replaceErr = os.WriteFile(archive, original, originalInfo.Mode().Perm())
	}}))
	_, err = RestoreCodexSet(partitions)
	if replaceErr != nil {
		t.Fatalf("replace preflight member: %v (restore err %v)", replaceErr, err)
	}
	if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("replaced preflight generation authorized extraction: changed=%t err=%v", changed, err)
	}
	if data, err := os.ReadFile(native); err != nil || string(data) != "inert current payload" {
		t.Fatalf("refusal changed current native payload: %q %v", data, err)
	}
	if after, err := os.Lstat(native); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("refusal changed native generation: %v", err)
	}
	if raw, err := os.ReadFile(archive); err != nil || !bytes.Equal(raw, original) {
		t.Fatalf("refusal changed replacement archive: %v", err)
	}
	if raw, err := os.ReadFile(retained); err != nil || !bytes.Equal(raw, original) {
		t.Fatalf("refusal changed original retained member: %v", err)
	}
	if residue, err := filepath.Glob(filepath.Join(filepath.Dir(native), ".agent-marketplace-restore-*")); err != nil || len(residue) != 0 {
		t.Fatalf("refusal left staging residue: %v %v", residue, err)
	}
	if err := os.Remove(archive); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, archive); err != nil {
		t.Fatal(err)
	}
	if restored, err := RestoreCodexSet(partitions); err != nil || restored != operation {
		t.Fatalf("restored archive authority did not permit real restore: %q %v", restored, err)
	}
	if data, err := os.ReadFile(native); err != nil || string(data) != "inert archived payload" {
		t.Fatalf("valid control did not restore actual payload: %q %v", data, err)
	}
}
