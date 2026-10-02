package ingest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"golang.org/x/sys/unix"
)

// spec: marketplace-api/mirrored-hook-support-safe-publication
func TestReadHookSupportFileSafety(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "dangling", "hardlink", "fifo", "oversized", "directory-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(t.TempDir(), "script")
			if err := os.WriteFile(outside, []byte("external bytes"), 0o755); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "script")
			name := "script"
			var err error
			switch kind {
			case "regular":
				err = os.WriteFile(file, []byte("safe bytes"), 0o755)
			case "symlink":
				err = os.Symlink(outside, file)
			case "dangling":
				err = os.Symlink(filepath.Join(root, "absent"), file)
			case "hardlink":
				err = os.Link(outside, file)
			case "fifo":
				err = unix.Mkfifo(file, 0o600)
			case "oversized":
				err = os.WriteFile(file, make([]byte, 2*1024*1024+1), 0o644)
			case "directory-symlink":
				err = os.Symlink(filepath.Dir(outside), filepath.Join(root, "sub"))
				name = "sub/script"
			}
			if err != nil {
				t.Fatal(err)
			}
			data, executable, err := readHookSupportFile(root, name)
			if kind == "regular" {
				if err != nil || !executable || string(data) != "safe bytes" {
					t.Fatalf("valid read: %q %v %v", data, executable, err)
				}
			} else if !errors.Is(err, ErrHookSupportUnsafe) {
				t.Fatalf("unsafe read error identity: %v", err)
			}
			if data, err := os.ReadFile(outside); err != nil || string(data) != "external bytes" {
				t.Fatalf("external bytes changed: %q %v", data, err)
			}
		})
	}
}

// spec: marketplace-api/mirrored-hook-support-acquisition-containment
func TestHookSupportReadAnchorsAtAcquisitionRoot(t *testing.T) {
	for _, alias := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "ancestor-symlink"}[alias], func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			path := filepath.Join(root, "plugin")
			if alias {
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			script := []byte("echo unchanged")
			if err := os.WriteFile(filepath.Join(path, "run.sh"), script, 0o700); err != nil {
				t.Fatal(err)
			}
			hook := ir.NewHook("guard", "PreToolUse", "echo safe", "command", nil, ir.SourceRef{})
			hook.SupportFiles = map[string]string{"run.sh": ""}
			_, _, err := fileSetForResource(root, catalog.Entry{Path: filepath.Join(path, "hooks.json"), Resource: hook})
			if alias && !errors.Is(err, ErrHookSupportUnsafe) || !alias && err != nil {
				t.Fatalf("acquisition boundary: %v", err)
			}
			if data, err := os.ReadFile(filepath.Join(path, "run.sh")); err != nil || string(data) != string(script) {
				t.Fatalf("support bytes changed: %q, %v", data, err)
			}
		})
	}
}

// spec: marketplace-api/mirrored-hook-support-publication-diagnostic
func TestHookPublicationWarnsUnavailableAssets(t *testing.T) {
	for _, command := range []string{"echo safe", "${CLAUDE_PLUGIN_ROOT}/hooks/missing.sh", "${CLAUDE_PLUGIN_ROOT}/scripts/run.sh", "${CLAUDE_PLUGIN_ROOT}/hooks/lib/run.sh"} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "hooks"), 0o700); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":%q}]}]}}`, command)
			if err := os.WriteFile(filepath.Join(root, "hooks", "hooks.json"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			registry := store.Registry{ID: "hook-source", Slug: "hook-source", Origins: []string{"file://" + root}, Acquisition: store.AcquisitionMirror, License: "MIT", Roots: []store.RegistryRoot{{Path: ".", SourceHarness: "claude"}}}
			rev, err := BuildRegistryRevision(t.Context(), testConfig(t, root), "test", registry, root, strings.Repeat("a", 40))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, diagnostic := range rev.Diagnostics {
				found = found || diagnostic.Code == "marketplace.ingest.hook-support-unavailable"
			}
			if found != (command != "echo safe") || len(rev.Resources) == 0 {
				t.Fatalf("publication diagnostic missing or vacuous: %v", rev.Diagnostics)
			}
		})
	}
}
