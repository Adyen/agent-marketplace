package harnesscompile

import (
	"io"
	"log/slog"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkRestrictionCompile(b *testing.B) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previous) })
	for _, tc := range []struct {
		name     string
		target   Target
		resource ir.Resource
	}{
		{"factory-known", TargetFactory, ir.NewSubagent("known", "Known", "Body", "", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})},
		{"factory-unknown", TargetFactory, ir.NewSubagent("unknown", "Unknown", "Body", "", []string{"MysteryTool"}, "", nil, "", nil, ir.SourceRef{})},
		{"codex-restricted-command", TargetCodex, ir.NewCommand("restricted", "Restricted", "Body", map[string]any{"allowed-tools": []string{"Read"}}, ir.SourceRef{})},
		{"factory-unsupported-event", TargetFactory, ir.NewHook("permission", "PermissionRequest", "check", "command", nil, ir.SourceRef{})},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Compile(tc.resource, tc.target); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
