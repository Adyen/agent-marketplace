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
)

// spec: backup-recovery/include-fragment-outside-the-user-configuration-directory-is-not-archived
func TestCaptureUserConfigSkipsAndLogsOutsideInclude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".agents")
	outside := filepath.Join(home, "outside.yaml")
	writeFile(t, outside, "apiVersion: agent-marketplace/v4\n")
	configBytes := []byte("apiVersion: agent-marketplace/v4\ninclude: ../outside.yaml\n")
	writeFile(t, filepath.Join(userDir, "config.yaml"), string(configBytes))

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
	entry, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != "config.yaml" || string(data) != string(configBytes) {
		t.Fatalf("first archive entry = %q:%q, want config.yaml with root bytes", entry.Name, data)
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatalf("outside include was archived; next entry error = %v", err)
	}
	if !strings.Contains(logs.String(), outside) {
		t.Fatalf("debug log does not name outside include %s: %s", outside, logs.String())
	}
}
