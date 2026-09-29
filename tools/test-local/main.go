package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

var strippedGitEnv = map[string]bool{
	"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true, "GIT_PREFIX": true,
	"GIT_OBJECT_DIRECTORY": true, "GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_COMMON_DIR": true, "GIT_CONFIG_PARAMETERS": true,
}

func gitEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if strippedGitEnv[e[:strings.IndexByte(e, '=')]] {
			continue
		}
		env = append(env, e)
	}
	return append(env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=maintenance.auto", "GIT_CONFIG_VALUE_0=0")
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func nulSplit(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '\x00' })
}

func changedFiles(dir, base string) ([]string, error) {
	out, err := git(dir, "diff", "--name-only", "--no-renames", "-z", base)
	if err != nil {
		return nil, err
	}
	untracked, err := git(dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	return append(nulSplit(out), nulSplit(untracked)...), nil
}

func packageFor(root, rel string) (pkg string, e2e bool) {
	d := path.Dir(filepath.ToSlash(rel))
	if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(d))); err != nil || !st.IsDir() {
		return "", false
	}
	for d != "." && d != "" {
		if d == "e2e" || strings.HasPrefix(d, "e2e/") {
			return "", true
		}
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(d)))
		if err == nil && !strings.Contains("/"+d+"/", "/testdata/") {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
					return "./" + d, false
				}
			}
		}
		d = path.Dir(d)
	}
	return "", false
}

func selectPackages(root, ref string) (pkgs []string, notes []string) {
	fallback := func(note string) ([]string, []string) {
		return []string{"./..."}, append(notes, note+"; falling back to ./...")
	}
	mb, err := git(root, "merge-base", ref, "HEAD")
	if err != nil {
		return fallback("test-local: no merge base with " + ref)
	}
	base := strings.TrimSpace(mb)
	files, err := changedFiles(root, base)
	if err != nil {
		return fallback("test-local: cannot list changes")
	}
	if len(files) == 0 {
		return nil, []string{"test-local: no changes vs " + base + "; nothing to run"}
	}
	set := map[string]bool{}
	for _, f := range files {
		pkg, e2e := packageFor(root, f)
		if e2e {
			notes = append(notes, "test-local: "+f+" is in the e2e module; not selectable from the root module")
			continue
		}
		if pkg == "" && path.Ext(f) == ".md" && (path.Dir(f) == "." || strings.HasPrefix(f, "docs/")) {
			continue
		}
		if pkg == "" || strings.ContainsAny(pkg, " \t\n") {
			return fallback("test-local: " + f + " does not map to a root-module Go package")
		}
		set[pkg] = true
	}
	for p := range set {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	if len(pkgs) == 0 && len(notes) == 0 {
		notes = append(notes, "test-local: only documentation changed; nothing to run")
	}
	return pkgs, notes
}

func main() {
	ref := os.Getenv("TEST_LOCAL_BASE")
	if ref == "" {
		ref = "origin/main"
	}
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "test-local:", err)
		os.Exit(1)
	}
	pkgs, notes := selectPackages(dir, ref)
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, n)
	}
	fmt.Println(strings.Join(pkgs, " "))
}
