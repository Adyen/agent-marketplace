package materialize

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func BenchmarkNativeHookMetadataNoopDisk(b *testing.B) {
	base := b.TempDir()
	repo := filepath.Join(base, "repo")
	home := filepath.Join(base, "home")
	state := filepath.Join(base, "state")
	b.Setenv("HOME", home)
	b.Setenv("XDG_STATE_HOME", state)
	b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	if err := os.MkdirAll(home, 0o755); err != nil {
		b.Fatal(err)
	}
	root := filepath.Join(repo, ".factory")
	if err := os.MkdirAll(root, 0o755); err != nil {
		b.Fatal(err)
	}
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	entries, err := ownedJSONHookEntries("hooks.json", map[string][]json.RawMessage{
		"PreToolUse": {json.RawMessage(`{"hooks":[{"command":"echo metadata-benchmark","type":"command"}],"matcher":"Execute"}`)},
	}, "alpha")
	if err != nil {
		b.Fatal(err)
	}
	entries[0].sourceDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	work := &ownedJSONAggregateWork{}
	work.add(entries[0])
	store, err := ownership.Open(root)
	if err != nil {
		b.Fatal(err)
	}
	_, err = store.Apply(ownership.Mutation{
		Rel: "hooks.json", AbsFile: filepath.Join(root, "hooks.json"),
		NextBytes: []byte(`{"PreToolUse":[{"hooks":[{"command":"echo metadata-benchmark","type":"command"}],"matcher":"Execute"}]}` + "\n"),
		Entries:   []ownership.Entry{entries[0].ownershipEntry()}, Reason: ownership.ReasonSync,
	})
	if err != nil {
		b.Fatal(err)
	}
	key := outputKey{target: hcc.TargetFactory}
	options := ownedJSONReconcileOptions{
		sessionName: "native-hook-metadata-benchmark", writeCode: "materialize.compile.error",
		malformedCode: "materialize.compile.error", shapeCode: "materialize.compile.error",
		satisfiedCode: "materialize.hook.satisfied-by-unmanaged", prunePostponedCode: "ownership.prune-postponed",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		report := reconcileNativeHookJSON(context.Background(), &Report{PerTarget: map[hcc.Target]TargetReport{}}, key, repo, work, options, false)
		for _, diagnostic := range report.Diags {
			if diagnostic.Severity == diag.SeverityError {
				b.Fatal(diagnostic.Code)
			}
		}
	}
}
