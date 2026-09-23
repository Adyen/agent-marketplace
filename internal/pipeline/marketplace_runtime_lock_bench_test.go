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

func BenchmarkOnlineConfigPreflightLocalDisk(b *testing.B) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previous) })
	home := b.TempDir()
	b.Setenv("HOME", home)
	config.SetTestOrganizationConfigPath(b, filepath.Join(home, "organization.yaml"))
	path := filepath.Join(home, "user.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	opts := config.LoadEffectiveOptions{GlobalPath: path, RepoRoot: b.TempDir()}
	ctx := context.Background()
	for _, online := range []bool{false, true} {
		name := "strict"
		load := config.LoadEffective
		if online {
			name = "online"
			load = func(ctx context.Context, opts config.LoadEffectiveOptions) config.EffectiveConfig {
				return LoadEffectiveOnline(ctx, opts, nil)
			}
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if ec := load(ctx, opts); len(ec.Errors) != 0 {
					b.Fatal(ec.Errors)
				}
			}
		})
	}
}
