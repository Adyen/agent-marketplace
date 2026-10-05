package tui

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/charmbracelet/x/ansi"
)

// spec: registry-symlink-containment/detail-source-previews-refuse-multiply-linked-payloads
func TestDetailSourcePreviewRefusesHardLink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.md")
	payload := []byte("inert source preview")
	if err := os.WriteFile(path, payload, 0o640); err != nil {
		t.Fatal(err)
	}
	content, truncated, err := readDetailSourceLimit(path, 8)
	if err != nil || !truncated || content != "inert so" {
		t.Fatalf("bounded source preview positive control failed: %q %v %v", content, truncated, err)
	}
	if snapshot := captureDetailSource(path); snapshot.err != nil || snapshot.content != string(payload) {
		t.Fatalf("native snapshot positive control failed: %+v", snapshot)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(t.TempDir(), "retained.md")); err != nil {
		t.Fatal(err)
	}
	content, truncated, err = readDetailSourceLimit(path, 8)
	if !errors.Is(err, fsboundary.ErrUnsafeFile) || content != "" || truncated {
		t.Fatalf("hard link supplied preview data: %q %v %v", content, truncated, err)
	}
	if snapshot := captureDetailSource(path); !errors.Is(snapshot.err, fsboundary.ErrUnsafeFile) || snapshot.content != "" || snapshot.truncationMarker != "" {
		t.Fatalf("native snapshot imported hardlink payload: %+v", snapshot)
	}
	if after, err := os.Stat(path); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("preview read changed source metadata: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(payload) {
		t.Fatalf("preview read changed source bytes: %q %v", data, err)
	}
}

func TestDetailSourcePreviewConsumersDiscardGenerationConflict(t *testing.T) {
	for _, test := range []struct {
		name     string
		snapshot bool
	}{
		{name: "captured-source", snapshot: true},
		{name: "selected-bundle"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sandbox := t.TempDir()
			home := filepath.Join(sandbox, "home")
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			const probe = "generationprobe484"
			payload := probe + "\n\n" + strings.Repeat("x", maxDetailSourceBytes)
			files := map[string]string{
				"workspace/registry/SKILL.md":                 payload,
				"workspace/.factory/skills/retained/SKILL.md": "native output sentinel",
				"home/cache/sentinel":                         "cache sentinel",
				"home/state/sentinel":                         "state sentinel",
				"outside/source.md":                           "external sentinel",
			}
			for relative, body := range files {
				path := filepath.Join(sandbox, filepath.FromSlash(relative))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
					t.Fatal(err)
				}
			}
			root := filepath.Join(sandbox, "workspace", "registry")
			sourcePath := filepath.Join(root, "SKILL.md")
			if err := os.Chmod(sourcePath, 0o640); err != nil {
				t.Fatal(err)
			}
			before := make(map[string]os.FileInfo)
			if err := filepath.WalkDir(sandbox, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				info, err := entry.Info()
				before[path] = info
				return err
			}); err != nil {
				t.Fatal(err)
			}
			var healthy string
			if test.snapshot {
				control := captureDetailSource(root)
				if control.err != nil || control.truncationMarker != detailTruncationMarker {
					t.Fatalf("healthy snapshot did not exercise truncation: %+v", control)
				}
				healthy = control.content
			} else {
				healthy = ansi.Strip(renderBundleFile(root, "SKILL.md", 80))
				if !strings.Contains(healthy, detailTruncationMarker) {
					t.Fatal("healthy selected preview did not exercise truncation")
				}
			}
			if !strings.Contains(healthy, probe) {
				t.Fatalf("healthy consumer did not expose the unique payload: %q", healthy)
			}
			var observed bytes.Buffer
			var refusal error
			calls := 0
			readTarget := func(source *fsboundary.Destination, read func(io.Reader) error) error {
				refusal = source.ReadTargetWith(func(reader io.Reader) error {
					if err := read(io.TeeReader(reader, &observed)); err != nil {
						return err
					}
					calls++
					return os.Chmod(sourcePath, 0o600)
				})
				return refusal
			}
			var output, prefix string
			if test.snapshot {
				snapshot := captureDetailSource(root, readTarget)
				if !errors.Is(snapshot.err, fsboundary.ErrGenerationConflict) || snapshot.content != "" || snapshot.truncationMarker != "" || snapshot.omitted {
					t.Fatalf("refused snapshot retained content or markers: err=%v bytes=%d marker=%q omitted=%v", snapshot.err, len(snapshot.content), snapshot.truncationMarker, snapshot.omitted)
				}
				output = renderDetailFullContent(row{DetailSource: snapshot}, 80)
				prefix = "Error reading resource: "
			} else {
				output = renderBundleFile(root, "SKILL.md", 80, readTarget)
				prefix = "Error reading file: "
			}
			if calls != 1 || !bytes.Equal(observed.Bytes(), []byte(payload[:maxDetailSourceBytes+1])) {
				t.Fatalf("post-read injection did not exercise actual bounded payload capture: calls=%d bytes=%d", calls, observed.Len())
			}
			if !errors.Is(refusal, fsboundary.ErrGenerationConflict) {
				t.Fatalf("actual destination read lost generation refusal: %v", refusal)
			}
			want := prefix + sanitizeTerminalData(refusal.Error(), terminalSingleLine)
			if output != want || strings.Contains(output, probe) || strings.Contains(output, "[truncated:") || strings.Contains(output, detailCaptureOmittedMarker) {
				t.Fatalf("consumer rendered refused payload or markers: bytes=%d want=%q payload=%v truncated=%v", len(output), want, strings.Contains(output, probe), strings.Contains(output, "[truncated:"))
			}
			seen := 0
			if err := filepath.WalkDir(sandbox, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				info, err := entry.Info()
				prior, ok := before[path]
				if err != nil || !ok {
					t.Fatalf("consumer changed scope inventory at %s: %v", path, err)
				}
				mode := prior.Mode()
				if path == sourcePath {
					mode = mode&^os.ModePerm | 0o600
				}
				if !os.SameFile(prior, info) || mode != info.Mode() || !prior.ModTime().Equal(info.ModTime()) || prior.Size() != info.Size() {
					t.Fatalf("consumer changed scope/external/cache/native generation at %s", path)
				}
				seen++
				return nil
			}); err != nil || seen != len(before) {
				t.Fatalf("consumer changed scope inventory: seen=%d want=%d err=%v", seen, len(before), err)
			}
			for relative, body := range files {
				if data, err := os.ReadFile(filepath.Join(sandbox, filepath.FromSlash(relative))); err != nil || string(data) != body {
					t.Fatalf("consumer changed %s bytes: %v", relative, err)
				}
			}
		})
	}
}

func TestDetailSourcePreviewBundleRefusesHardLink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("native preview control"), 0o640); err != nil {
		t.Fatal(err)
	}
	if content := renderBundleFile(root, "source.txt", 80); strings.Contains(content, "Error reading file") || !strings.Contains(content, "native preview") {
		t.Fatalf("selected native bundle preview positive control failed: %q", content)
	}
	if err := os.Link(path, filepath.Join(t.TempDir(), "retained.txt")); err != nil {
		t.Fatal(err)
	}
	if content := renderBundleFile(root, "source.txt", 80); !strings.Contains(content, "Error reading file") || strings.Contains(content, "native preview") {
		t.Fatalf("selected bundle hardlink supplied preview bytes: %q", content)
	}
}
