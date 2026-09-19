package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
)

func TestDeinitManifestFailurePreservesLaterPhases(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, scope := range []string{"user", "repo", "local"} {
		t.Run(scope, func(t *testing.T) {
			fixtureScope := scope
			if scope == "local" {
				fixtureScope = "repo"
			}
			f := newManagedSkillLinkFixture(t, "factory", fixtureScope)
			if scope == "local" {
				writeConfig(t, filepath.Join(f.repo, ".agents", "config.local.yaml"), "skills: []\n")
				writeFile(t, filepath.Join(f.repo, ".agents", "lock.local.yaml"), "apiVersion: agent-marketplace-lock/v1\nscope: local\nregistries: []\n")
			}
			manifest := filepath.Join(f.repo, ".agents", "materialization-manifest.yaml")
			if scope == "user" {
				manifest = filepath.Join(f.home, ".agent-marketplace", "materialization-manifest.yaml")
			}
			if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, []byte("version: 99\nentries: []\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			orphan := filepath.Join(cache.CacheDir(), cache.HashURL("ssh://git@example.invalid/deinit-orphan.git"), "main", "sentinel")
			if err := os.MkdirAll(filepath.Dir(orphan), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(orphan, []byte("orphan cache sentinel\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := ownedCommandSnapshot(t, f.base)
			args := []string{"deinit", "--scope", scope, "--purge-config"}
			out, err := executeCommand(args...)
			var coded interface{ DiagnosticCode() string }
			if !errors.As(err, &coded) || coded.DiagnosticCode() != "materialize.manifest.error" {
				t.Errorf("error=%v output=%s", err, out)
			}
			after := ownedCommandSnapshot(t, f.base)
			if !reflect.DeepEqual(before, after) {
				for path, value := range before {
					if after[path] != value {
						t.Errorf("refusal changed or removed %s", path)
					}
				}
				for path := range after {
					if _, exists := before[path]; !exists {
						t.Errorf("refusal created %s", path)
					}
				}
			}
			if strings.Contains(out, "Removed config") || strings.Contains(out, "Would remove config") || strings.Contains(out, "Removed lock") || strings.Contains(out, "Would remove lock") || strings.Contains(out, "Removed cache") || strings.Contains(out, "Would remove cache") || strings.Contains(out, "No managed artifacts to remove") {
				t.Fatalf("destructive output=%s", out)
			}
		})
	}
}
