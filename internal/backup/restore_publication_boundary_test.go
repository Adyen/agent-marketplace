package backup

import (
	"archive/tar"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

type restorePublicationReader struct {
	io.Reader
	before func()
}

func (r *restorePublicationReader) Read(data []byte) (int, error) {
	if r.before != nil {
		r.before()
		r.before = nil
	}
	return r.Reader.Read(data)
}

func TestMappedRestoreRefusesReplacedGeneration(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "native.json")
	if err := os.WriteFile(destination, []byte("original inert user bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained.json")
	reader := &restorePublicationReader{Reader: strings.NewReader("inert archive bytes"), before: func() {
		if err := os.Rename(destination, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, []byte("replacement inert user bytes"), 0o640); err != nil {
			t.Fatal(err)
		}
	}}
	const name = ".agent-marketplace-native/machine-mcp/.claude.json"
	header := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o640, Size: int64(len("inert archive bytes"))}
	if err := restoreMappedFile(reader, header, []map[string]string{{name: destination}}); !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("mapped replaced generation promoted: %v", err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "replacement inert user bytes" {
		t.Fatalf("mapped replacement changed: %q %v", data, err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 {
		t.Fatalf("mapped refusal residue: %v %v", entries, err)
	}
}

// spec: backup-recovery/restore-file-hardlink-refuses-without-foreign-write
func TestRestoreRegularFileRefusesHardlinkedDestination(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	file := filepath.Join(root, "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("inert archive generation"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"skills"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("inert current user bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign.md")
	if err := os.Link(file, foreign); err != nil {
		t.Fatal(err)
	}
	if restored, err := Restore(hcc.TargetFactory, root); restored != "" || !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("restore accepted hardlinked destination: %q %v", restored, err)
	}
	for _, path := range []string{file, foreign} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "inert current user bytes" {
			t.Fatalf("restore changed user bytes at %s: %q %v", path, data, err)
		}
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if restored, err := Restore(hcc.TargetFactory, root); err != nil || restored == "" {
		t.Fatalf("regular destination control refused: %q %v", restored, err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "inert archive generation" {
		t.Fatalf("positive restore failed to update: %q %v", data, err)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("archive mode not preserved: %v", err)
	}
}
