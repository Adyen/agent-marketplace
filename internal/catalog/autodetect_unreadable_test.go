package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
)

// The CLI's source filesystem opens a file to stat it, so an unreadable
// marker stays absent for layout detection and a readable lower-priority
// manifest still selects its harness.
func TestSourceAutodetectTreatsUnreadableMarkersAsAbsent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("file permissions do not restrict root")
	}
	dir := t.TempDir()
	for name, data := range map[string]string{
		".claude-plugin/plugin.json":  `{"name":"claude"}`,
		".factory-plugin/plugin.json": `{"name":"factory"}`,
		"skills/demo/SKILL.md":        "---\nname: demo\ndescription: d\n---\nbody\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	unreadable := filepath.Join(dir, ".claude-plugin", "plugin.json")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

	source, err := fsboundary.OpenSourceFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	if got, ok := ingest.Autodetect(source); !ok || got != ingest.HarnessFactory {
		t.Fatalf("Autodetect = %q, %t; want the readable Factory manifest", got, ok)
	}
}
