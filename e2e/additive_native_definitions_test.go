package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2EAdditiveNativeDefinitions(t *testing.T) {
	e := newEnv(t)
	e.InitGitRepo()

	first := e.CreateBareRepo("first", map[string]string{
		".devin/hooks.v1.json":    `{"PreToolUse":[{"matcher":"^exec$","hooks":[{"type":"command","command":"echo first"}]}]}`,
		".devin/scripts/guard.sh": "echo source-first",
	})
	second := e.CreateBareRepo("second", map[string]string{
		".devin/hooks.v1.json":    `{"PreToolUse":[{"matcher":"^exec$","hooks":[{"type":"command","command":"echo second"}]}]}`,
		".devin/scripts/guard.sh": "echo source-second",
	})

	e.WriteGlobalConfig("")
	e.WriteRepoConfig("harnesses: [windsurf]\nregistries:\n  - url: " + first + "\n    ref: main\n    source_harness: devin\n  - url: " + second + "\n    ref: main\n    source_harness: devin\n")

	native := "/workspace/.devin/hooks.json"
	supportRoot := e.mustResolvePath("/workspace/hook-support")

	supportContents := func() map[string]string {
		contents := make(map[string]string)
		if err := filepath.WalkDir(supportRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			contents[path] = string(data)
			return nil
		}); err != nil {
			t.Fatalf("scan hook support tree: %v", err)
		}
		return contents
	}
	assertSupport := func(wantFirst, wantSecond int) {
		t.Helper()
		contents := supportContents()
		var firstGuard, secondGuard int
		for path, data := range contents {
			if filepath.Base(path) == "hooks.v1.json" {
				t.Errorf("native hook definition published into support tree: %s", path)
			}
			if !strings.HasSuffix(filepath.ToSlash(path), "/scripts/guard.sh") {
				continue
			}
			if data == "echo source-first\n" {
				firstGuard++
			}
			if data == "echo source-second\n" {
				secondGuard++
			}
		}
		if firstGuard != wantFirst || secondGuard != wantSecond {
			t.Errorf("support tree guard copies: first=%d second=%d, want %d/%d; tree=%v", firstGuard, secondGuard, wantFirst, wantSecond, contents)
		}
	}
	assertHookFile := func() {
		t.Helper()
		text := e.ReadFile(native)
		for _, command := range []string{"echo first", "echo second"} {
			if strings.Count(text, command) != 1 {
				t.Errorf("%s holds %d %q commands, want 1\n%s", native, strings.Count(text, command), command, text)
			}
		}
		if strings.Contains(text, "_managed_by") {
			t.Errorf("%s contains an in-file ownership marker\n%s", native, text)
		}
	}
	fingerprint := func(path string) string {
		t.Helper()
		sum, err := pathFingerprint(e.mustResolvePath(path))
		if err != nil {
			t.Fatalf("fingerprint %s: %v", path, err)
		}
		return sum
	}
	assertUnchanged := func(before map[string]string) {
		t.Helper()
		for path, want := range before {
			if got := fingerprint(path); got != want {
				t.Errorf("path %q changed", path)
			}
		}
	}

	r := e.RunAM("install", "--all", "--kind", "hook")
	if r.ExitCode != 0 {
		t.Fatalf("install --all --kind hook failed (exit %d):\n%s", r.ExitCode, r.Combined)
	}
	e.AssertOutputContains(r, "PreToolUse-command")
	if !e.CheckRegularFile(native) {
		t.Fatalf("%s is not a regular file", native)
	}
	if !e.CheckFileContains(native, "echo first") {
		t.Fatalf("%s missing first provider command\n%s", native, e.ReadFile(native))
	}
	assertSupport(1, 1)

	r = e.RunAM("list", "--kind", "hook")
	if r.ExitCode != 0 {
		t.Fatalf("list --kind hook failed (exit %d):\n%s", r.ExitCode, r.Combined)
	}
	e.AssertOutputContains(r, "PreToolUse-command")
	if !e.CheckFileContains(native, "echo second") {
		t.Fatalf("%s missing second provider command\n%s", native, e.ReadFile(native))
	}
	assertHookFile()

	before := map[string]string{native: fingerprint(native), "/workspace/.agents/lock.yaml": fingerprint("/workspace/.agents/lock.yaml")}
	if r := e.RunAM("sync", "--check"); r.ExitCode != 0 {
		t.Fatalf("sync --check failed (exit %d):\n%s", r.ExitCode, r.Combined)
	}
	if r := e.RunAM("sync"); r.ExitCode != 0 {
		t.Fatalf("sync failed (exit %d):\n%s", r.ExitCode, r.Combined)
	}
	assertUnchanged(before)

	r = e.RunAM("remove", "PreToolUse-command", "--kind", "hook", "--from", first)
	if r.ExitCode != 0 {
		t.Fatalf("remove failed (exit %d):\n%s", r.ExitCode, r.Combined)
	}
	if e.CheckFileContains(native, "echo first") {
		t.Fatalf("%s still holds the removed provider command\n%s", native, e.ReadFile(native))
	}
	if !e.CheckFileContains(native, "echo second") {
		t.Fatalf("%s lost the retained provider command\n%s", native, e.ReadFile(native))
	}

	before = map[string]string{native: fingerprint(native), "/workspace/.agents/lock.yaml": fingerprint("/workspace/.agents/lock.yaml")}
	if r := e.RunAM("sync", "--check"); r.ExitCode != 0 {
		t.Fatalf("post-remove sync --check failed (exit %d):\n%s", r.ExitCode, r.Combined)
	}
	assertUnchanged(before)
	assertSupport(0, 1)
}
