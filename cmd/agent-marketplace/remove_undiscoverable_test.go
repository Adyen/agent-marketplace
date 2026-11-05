package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec: resource-removal/remove-a-selection-after-its-resource-disappears
// spec: resource-removal/remove-a-malformed-explicit-selection
// spec: resource-removal/undiscoverable-removal-respects-scope-and-required-policy
func TestRemoveUndiscoverableExplicitSelection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, scope := range []string{"repo", "user", "local"} {
		for _, malformed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/malformed=%t", scope, malformed), func(t *testing.T) {
				home, repo, source := t.TempDir(), t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				withWorkingDir(t, repo)
				userPath := filepath.Join(home, ".agents", "config.yaml")
				repoPath := filepath.Join(repo, ".agents", "config.yaml")
				localPath := filepath.Join(repo, ".agents", "config.local.yaml")
				writeFile(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
				writeFile(t, repoPath, "apiVersion: agent-marketplace/v4\n")
				writeFile(t, localPath, "apiVersion: agent-marketplace/v4\n")
				target := map[string]string{"repo": repoPath, "user": userPath, "local": localPath}[scope]
				writeFile(t, target, fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  team: {directory: %q, source_harness: factory}\nresources:\n  team: [skill/gone, skill/keep]\n", source))
				if scope == "user" {
					data, err := os.ReadFile(target)
					if err != nil {
						t.Fatal(err)
					}
					writeFile(t, target, string(data)+"harnesses: [factory]\n")
				}
				writeSkill(t, source, "gone", "Will disappear", nil)
				writeSkill(t, source, "keep", "Keep selected", nil)
				if out, err := executeCommand("sync"); err != nil {
					t.Fatalf("initial sync: %v\n%s", err, out)
				}
				nativeRoot := repo
				if scope == "user" {
					nativeRoot = home
				}
				if _, err := os.Stat(filepath.Join(nativeRoot, ".factory", "skills", "gone", "SKILL.md")); err != nil {
					t.Fatalf("initial resource was not materialized: %v", err)
				}
				if malformed {
					writeFile(t, filepath.Join(source, "skills", "gone", "SKILL.md"), "---\nname: [invalid\n---\n")
				} else if err := os.Rename(filepath.Join(source, "skills", "gone"), filepath.Join(t.TempDir(), "gone")); err != nil {
					t.Fatal(err)
				}
				before, err := executeCommand("sync")
				if err != nil || !strings.Contains(before, `"gone"`) {
					t.Fatalf("expected selected-resource warning: %v\n%s", err, before)
				}
				untouched := map[string][]byte{}
				for _, path := range []string{userPath, repoPath, localPath} {
					if path != target {
						untouched[path], err = os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				args := []string{"remove", "gone"}
				if malformed {
					args = append(args, "--scope", scope, "--kind", "skill")
				}
				if out, err := executeCommand(args...); err != nil {
					t.Fatalf("remove: %v\n%s", err, out)
				}
				for path, want := range untouched {
					got, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("unrelated layer changed: %s: %v\n%s", path, err, got)
					}
				}
				out, err := executeCommand("sync")
				if err != nil || strings.Contains(out, `"gone"`) {
					t.Fatalf("stale selection still reported: %v\n%s", err, out)
				}
				if _, err := os.Stat(filepath.Join(nativeRoot, ".factory", "skills", "keep", "SKILL.md")); err != nil {
					t.Fatalf("unrelated resource removed: %v", err)
				}
			})
		}
	}
}
