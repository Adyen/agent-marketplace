package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
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
