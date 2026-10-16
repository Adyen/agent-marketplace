package pipeline

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// BenchmarkValidateConsumerIntentDiskIO includes bounded reads of four roots
// and one included fragment, but deliberately performs no dependency acquisition.
func BenchmarkValidateConsumerIntentDiskIO(b *testing.B) {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previousLogger) })
	root := b.TempDir()
	home, repo := filepath.Join(root, "home"), filepath.Join(root, "repo")
	b.Setenv("HOME", home)
	org := filepath.Join(root, "org", "config.yaml")
	config.SetTestOrganizationConfigPath(b, org)
	for _, path := range []string{org, filepath.Join(home, ".agents", "config.yaml"), config.ProjectConfigPath(repo), config.LocalConfigPath(repo)} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v4\n"), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	if err := os.WriteFile(config.ProjectConfigPath(repo), []byte("apiVersion: agent-marketplace/v4\ninclude: [selection.yaml]\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	fragment := "sources:\n  local: {directory: registry}\nresources:\n  local:\n    all: true\n"
	if err := os.WriteFile(filepath.Join(repo, ".agents", "selection.yaml"), []byte(fragment), 0o600); err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if err := ValidateConsumerIntent(ctx, repo, repo); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ValidateConsumerIntent(ctx, repo, repo); err != nil {
			b.Fatal(err)
		}
	}
}
