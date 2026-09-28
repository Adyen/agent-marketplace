package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(gitEnv(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	testGit(t, root, "init", "-b", "main")
	writeFile(t, root, "go.mod", "module example.com/t\n")
	writeFile(t, root, "internal/a/a.go", "package a\n")
	writeFile(t, root, "internal/b/b.go", "package b\n")
	writeFile(t, root, "internal/c/c.go", "package c\n")
	testGit(t, root, "add", "-A")
	testGit(t, root, "commit", "-m", "base")
	testGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	testGit(t, root, "checkout", "-b", "feature")
	return root
}

func TestSelectPackages(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, root string)
		want   string
	}{
		{"committed staged unstaged untracked", func(t *testing.T, root string) {
			writeFile(t, root, "internal/a/a.go", "package a\n// committed\n")
			testGit(t, root, "commit", "-am", "change a")
			writeFile(t, root, "internal/b/b.go", "package b\n// unstaged\n")
			writeFile(t, root, "internal/c/c.go", "package c\n// staged\n")
			testGit(t, root, "add", "internal/c/c.go")
			writeFile(t, root, "internal/d/d.go", "package d\n")
		}, "./internal/a ./internal/b ./internal/c ./internal/d"},
		{"rename selects both locations", func(t *testing.T, root string) {
			writeFile(t, root, "internal/a/extra.go", "package a\n")
			testGit(t, root, "add", "internal/a/extra.go")
			testGit(t, root, "mv", "internal/a/a.go", "internal/b/moved.go")
		}, "./internal/a ./internal/b"},
		{"deleted package falls back despite surviving ancestor package", func(t *testing.T, root string) {
			writeFile(t, root, "internal/root.go", "package internal\n")
			if err := os.RemoveAll(filepath.Join(root, "internal", "a")); err != nil {
				t.Fatal(err)
			}
		}, "./..."},
		{"shared and unmappable paths fall back", func(t *testing.T, root string) {
			writeFile(t, root, "go.mod", "module example.com/t\n\n")
			writeFile(t, root, "docs/guide.md", "changed\n")
		}, "./..."},
		{"fixture maps to owning package", func(t *testing.T, root string) {
			writeFile(t, root, "internal/a/testdata/golden.txt", "x\n")
			writeFile(t, root, "internal/a/testdata/fixture/example.go", "package fixture\n")
		}, "./internal/a"},
		{"whitespace and unicode names", func(t *testing.T, root string) {
			writeFile(t, root, "internal/a/my file.go", "package a\n")
			writeFile(t, root, "internal/b/tab\tname.go", "package b\n")
			writeFile(t, root, "internal/c/üñí.go", "package c\n")
		}, "./internal/a ./internal/b ./internal/c"},
		{"whitespace package path falls back", func(t *testing.T, root string) {
			writeFile(t, root, "internal/sp ace/x.go", "package x\n")
		}, "./..."},
		{"e2e module never selected", func(t *testing.T, root string) {
			writeFile(t, root, "e2e/e2e_test.go", "package e2e\n")
		}, ""},
		{"no changes selects nothing", func(t *testing.T, root string) {}, ""},
		{"missing ref falls back", func(t *testing.T, root string) {
			testGit(t, root, "update-ref", "-d", "refs/remotes/origin/main")
			writeFile(t, root, "internal/a/a.go", "package a\n// changed\n")
		}, "./..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newRepo(t)
			tc.mutate(t, root)
			pkgs, notes := selectPackages(root, "origin/main")
			got := strings.Join(pkgs, " ")
			if got != tc.want {
				t.Fatalf("pkgs=%q, want %q", got, tc.want)
			}
			if got == "" && len(notes) == 0 {
				t.Fatal("empty selection must be reported in notes")
			}
		})
	}
}

func runMake(t *testing.T, env []string, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "go.log")
	fake := filepath.Join(dir, "fakego")
	script := "#!/bin/sh\necho \"$@\" >> \"$FAKE_GO_LOG\"\nif [ \"$1\" = run ]; then\n" +
		"  [ -z \"$FAKE_FAIL\" ] || exit 1\n" +
		"  if [ -n \"$FAKE_EMPTY\" ]; then echo; else echo ./internal/pkgfake; fi\nfi\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("make", append(args, "GO="+fake)...)
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = []string{"FAKE_GO_LOG=" + log}
	for _, e := range append(os.Environ(), env...) {
		k := e[:strings.IndexByte(e, '=')]
		if k == "MAKEFLAGS" || k == "MFLAGS" || k == "MAKELEVEL" || strings.HasPrefix(k, "TEST_") {
			continue
		}
		cmd.Env = append(cmd.Env, e)
	}
	out, err := cmd.CombinedOutput()
	data, readErr := os.ReadFile(log)
	if readErr != nil && (args[0] != "-n" || !os.IsNotExist(readErr)) {
		t.Fatal(readErr)
	}
	return string(data) + string(out), err
}

func TestMakeTestRouting(t *testing.T) {
	out, err := runMake(t, nil, "test")
	if err != nil {
		t.Fatalf("make test failed: %v\n%s", err, out)
	}
	if strings.Count(out, "run ./tools/test-local") != 1 ||
		strings.Count(out, "test -short ./internal/pkgfake") != 1 {
		t.Fatalf("want selector + one short selected run: %q", out)
	}
	out, err = runMake(t, nil, "test", "TEST_PACKAGES=./internal/explicit", "TEST_FLAGS=-count=1")
	if err != nil {
		t.Fatalf("make test failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "run ./tools/test-local") ||
		strings.Count(out, "test -short -count=1 ./internal/explicit") != 1 {
		t.Fatalf("explicit packages/flags must bypass the selector unchanged: %q", out)
	}
	out, err = runMake(t, nil, "test-full")
	if err != nil {
		t.Fatalf("make test-full failed: %v\n%s", err, out)
	}
	if strings.Count(out, "test ./...") != 1 || strings.Contains(out, "-short") {
		t.Fatalf("test-full must run the full suite unshortened: %q", out)
	}
}

func TestMakeTestRunsNoTestsOnSelectorFailureOrEmpty(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     []string
		wantErr bool
	}{
		{"selector failure propagates", []string{"FAKE_FAIL=1"}, true},
		{"empty selection runs nothing", []string{"FAKE_EMPTY=1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runMake(t, tc.env, "test")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v: %s", err, tc.wantErr, out)
			}
			if strings.Contains(out, "test -short") {
				t.Fatalf("no go test may run: %q", out)
			}
		})
	}
}

func TestMakeWitnessedAndValidateFastStayFull(t *testing.T) {
	out, err := runMake(t, nil, "-n", "test-witnessed", "TEST_PACKAGES=./scoped")
	if err != nil {
		t.Fatalf("make -n test-witnessed failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "test-full TEST_PACKAGES=./...") ||
		strings.Contains(out, "-short") || strings.Contains(out, "./scoped") {
		t.Fatalf("test-witnessed must force the unshortened full package set: %s", out)
	}
	out, err = runMake(t, nil, "-n", "validate-fast")
	if err != nil {
		t.Fatalf("make -n validate-fast failed: %v\n%s", err, out)
	}
	if strings.Count(out, "-short") != 1 {
		t.Fatalf("validate-fast must not duplicate the short flag: %s", out)
	}
}
