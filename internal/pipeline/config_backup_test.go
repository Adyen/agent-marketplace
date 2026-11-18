package pipeline

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

type archivedConfigFile struct {
	name string
	data string
}

func readConfigArchive(t *testing.T, path string) []archivedConfigFile {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := gz.Close(); err != nil {
			t.Error(err)
		}
	}()
	reader := tar.NewReader(gz)
	var files []archivedConfigFile
	for {
		entry, err := reader.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, archivedConfigFile{name: entry.Name, data: string(data)})
	}
}

// spec: backup-recovery/include-fragment-outside-the-user-configuration-directory-is-not-archived
func TestCaptureUserConfigSkipsAndLogsOutsideInclude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".agents")
	outside := filepath.Join(home, "outside.yaml")
	writeFile(t, outside, "apiVersion: agent-marketplace/v4\n")
	configBytes := "apiVersion: agent-marketplace/v4\ninclude: ../outside.yaml\n"
	writeFile(t, filepath.Join(userDir, "config.yaml"), configBytes)

	var logs strings.Builder
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	path, err := CaptureUserConfig().WriteArchive()
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("configuration archive path is empty")
	}
	files := readConfigArchive(t, path)
	if len(files) != 1 || files[0] != (archivedConfigFile{"config.yaml", configBytes}) {
		t.Fatalf("archive = %q, want only config.yaml with root bytes", files)
	}
	if !strings.Contains(logs.String(), outside) {
		t.Fatalf("debug log does not name outside include %s: %s", outside, logs.String())
	}
}

func TestCaptureUserConfigNamesSymlinkedIncludeByAuthoredPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".agents")
	writeFile(t, filepath.Join(userDir, "config.yaml"), "apiVersion: agent-marketplace/v4\ninclude: fragments/current.yaml\n")
	writeFile(t, filepath.Join(userDir, "versions", "v2.yaml"), "include: nested.yaml\n")
	writeFile(t, filepath.Join(userDir, "versions", "nested.yaml"), "apiVersion: agent-marketplace/v4\n")
	if err := os.MkdirAll(filepath.Join(userDir, "fragments"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "versions", "v2.yaml"), filepath.Join(userDir, "fragments", "current.yaml")); err != nil {
		t.Fatal(err)
	}

	path, err := CaptureUserConfig().WriteArchive()
	if err != nil {
		t.Fatal(err)
	}
	restored := t.TempDir()
	got := map[string]string{}
	for _, file := range readConfigArchive(t, path) {
		got[file.name] = file.data
		writeFile(t, filepath.Join(restored, file.name), file.data)
	}
	if got["fragments/current.yaml"] != "include: nested.yaml\n" || got["fragments/nested.yaml"] != "apiVersion: agent-marketplace/v4\n" {
		t.Fatalf("archive = %q, want link target bytes under the authored include paths", got)
	}
	if _, err := config.LoadConfigFromPath(filepath.Join(restored, "config.yaml")); err != nil {
		t.Fatalf("restored configuration does not load: %v", err)
	}
}
