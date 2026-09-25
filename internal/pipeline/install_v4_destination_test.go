package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestEditV4DestinationRejectsUnhandledEdit(t *testing.T) {
	dir := t.TempDir()
	entry := config.RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	for name, content := range map[string]string{"missing": "", "legacy": "apiVersion: agent-marketplace/v2\n"} {
		path := filepath.Join(dir, name+".yaml")
		if content != "" {
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		err := editV4Destination(path, path, entry, ir.KindSkill, "review", config.V4SelectionEnable)
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("%s destination error = %v, want refusal naming %s", name, err, path)
		}
	}
}
