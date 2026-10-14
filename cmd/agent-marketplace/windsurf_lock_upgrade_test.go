package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// spec: hook-resources/legacy-windsurf-hook-lock-is-retained-until-upgrade
func TestWindsurfLegacyHookLockRetainedUntilUpgrade(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "windsurf-hook-lock.git")
	writeFile(t, filepath.Join(working, ".windsurf", "hooks.json"), `{"hooks":{"pre_run_command":[{"command":"echo first"},{"command":"echo second"}]}}`)
	commitAndPushAll(t, working, "add source hooks")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [windsurf]\nsources:\n  origin: {git: file://"+origin+", ref: main, source_harness: windsurf}\nresources:\n  origin: {all: true}\n")
	withWorkingDir(t, repo)
	if out, err := executeCommand("lock"); err != nil {
		t.Fatalf("initial lock: %v\n%s", err, out)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	doc := readExactManifestLock(t, lockPath)
	if len(doc.Registries) != 1 || len(doc.Registries[0].Roots) != 1 {
		t.Fatalf("unexpected lock: %#v", doc)
	}
	root := &doc.Registries[0].Roots[0]
	if root.RevisionSchema != "v4" || len(root.Resources) != 2 {
		t.Fatalf("new inventory: %#v", root)
	}
	root.RevisionSchema = "v2"
	root.RevisionDigest = lockfile.Digest("sha256:aa3878efc2d734de9234b40cc5d1d9023a0aa83dfe45feb6183b1e7e30728b6e")
	root.Resources = []lockfile.GitResource{{Kind: "hook", Name: "hooks", ContentDigest: lockfile.Digest("sha256:6021b0d682e22a4c5b087420b9b4eded1680619cbe6989e0bc89bdc77a6cc9f4"), ArchiveDigest: lockfile.Digest("sha256:6021b0d682e22a4c5b087420b9b4eded1680619cbe6989e0bc89bdc77a6cc9f4")}}
	writeExactManifestLock(t, lockPath, doc)
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"sync", "--locked"}, {"list", "--kind", "hook"}, {"status"}, {"sync", "--check"}, {"lock"}} {
		if out, err := executeCommand(args...); err != nil {
			t.Fatalf("legacy %v: %v\n%s", args, err, out)
		}
		after, err := os.ReadFile(lockPath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%v changed legacy lock: %v", args, err)
		}
	}
	assertWindsurfLockCommands(t, repo, []string{"echo second"})
	if out, err := executeCommand("lock", "--upgrade"); err != nil {
		t.Fatalf("upgrade: %v\n%s", err, out)
	}
	updated := readExactManifestLock(t, lockPath)
	if updated.Registries[0].Commit != doc.Registries[0].Commit || updated.Registries[0].Roots[0].RevisionSchema != "v4" || len(updated.Registries[0].Roots[0].Resources) != 2 {
		t.Fatalf("upgraded lock: %#v", updated)
	}
	if out, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("upgraded sync: %v\n%s", err, out)
	}
	assertWindsurfLockCommands(t, repo, []string{"echo first", "echo second"})
}

func assertWindsurfLockCommands(t *testing.T, repo string, want []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, ".devin", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Hooks map[string][]struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var commands []string
	for _, rule := range document.Hooks["pre_run_command"] {
		commands = append(commands, rule.Command)
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("hook commands=%v, want %v", commands, want)
	}
}
