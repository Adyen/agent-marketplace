package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func BenchmarkWarmConfiguredLegacyInstallDiskIO(b *testing.B) {
	root := b.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "workspace")
	source := filepath.Join(root, "registry")
	for _, dir := range []string{home, repo, filepath.Join(source, "skills", "review"), filepath.Join(home, ".agents")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			b.Fatal(err)
		}
	}
	b.Setenv("HOME", home)
	b.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	config.SetTestOrganizationConfigPath(b, filepath.Join(root, "missing-organization-config.yaml"))
	if err := os.WriteFile(filepath.Join(source, "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: Review\n---\n\n# Review\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	data := fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - path: %q\n    skills: {include: [review]}\n", source)
	if err := os.WriteFile(filepath.Join(home, ".agents", "config.yaml"), []byte(data), 0o600); err != nil {
		b.Fatal(err)
	}
	opts := InstallOptions{RepoRoot: repo, Scope: ScopeUser, Args: []string{"review"}}
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previousLogger) })
	if _, err := Install(context.Background(), opts); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Install(context.Background(), opts); err != nil {
			b.Fatal(err)
		}
	}
}
