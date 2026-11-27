package acquire

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClonePlaceInputsAreExact(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"skills/root.md":                          "legacy root",
		"profiles/exact.yaml":                     "profile bytes",
		"packages/target/mod/name/payload/source": "package bytes",
		"legacy-target.txt":                       "legacy symlink target",
	})
	addSymlink(t, repo, "legacy-target.txt", "legacy-link.txt")
	commitAll(t, repo, "add exact place inputs")

	exactPaths := []string{"profiles/exact.yaml", "packages/target/mod/name"}
	res, err := Clone(context.Background(), repo, "main", Credentials{}, Limits{
		RepositoryBytes: 1 << 20, FileCount: 100, FileBytes: 1 << 16,
		ExactPaths: exactPaths,
	}, time.Minute, "skills")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	defer func() { _ = res.Release() }()

	profile, err := os.ReadFile(filepath.Join(res.Dir, "profiles", "exact.yaml"))
	if err != nil {
		t.Fatalf("read exact profile input: %v", err)
	}
	if string(profile) != "profile bytes" {
		t.Fatalf("profile bytes = %q", profile)
	}
	pkg, err := os.ReadFile(filepath.Join(res.Dir, "packages", "target", "mod", "name", "payload", "source"))
	if err != nil {
		t.Fatalf("read exact package input: %v", err)
	}
	if string(pkg) != "package bytes" {
		t.Fatalf("package bytes = %q", pkg)
	}
	legacyLink := filepath.Join(res.Dir, "legacy-link.txt")
	info, err := os.Lstat(legacyLink)
	if err != nil {
		t.Fatalf("lstat unrelated legacy symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("unrelated legacy symlink remained a symlink")
	}
	legacy, err := os.ReadFile(legacyLink)
	if err != nil || string(legacy) != "legacy symlink target" {
		t.Fatalf("expanded legacy symlink = %q, err=%v", legacy, err)
	}
	if len(exactPaths) != 2 || exactPaths[0] != "profiles/exact.yaml" || exactPaths[1] != "packages/target/mod/name" {
		t.Fatalf("Clone mutated ExactPaths input: %q", exactPaths)
	}

	t.Run("case variant package symlink", func(t *testing.T) {
		repo := createGitRepo(t, map[string]string{
			"skills/root.md":      "legacy root",
			"profiles/exact.yaml": "profile bytes",
			"outside/packages/target/mod/name/payload/source": "package bytes",
		})
		addSymlink(t, repo, "outside/packages", "Packages")
		commitAll(t, repo, "add case variant package symlink")
		assertCloneRefusesExactInputSymlink(t, repo, []string{"profiles/exact.yaml", "packages/target/mod/name"})
	})
	t.Run("canonical Unicode profile alias symlink", func(t *testing.T) {
		repo := createGitRepo(t, map[string]string{
			"skills/root.md":  "legacy root",
			"outside/profile": "profile bytes",
		})
		if err := os.MkdirAll(filepath.Join(repo, "profiles"), 0o755); err != nil {
			t.Fatal(err)
		}
		addSymlink(t, repo, "../outside/profile", "profiles/cafe\u0301.yaml")
		commitAll(t, repo, "add Unicode alias profile symlink")
		assertCloneRefusesExactInputSymlink(t, repo, []string{"profiles/café.yaml"})
	})
}

func TestCloneRefusesSymlinksForExactPlaceInputs(t *testing.T) {
	for _, tc := range []struct {
		name, linkPath, linkTarget string
		files                      map[string]string
	}{
		{
			name: "exact package directory", linkPath: "packages/target/mod/name",
			linkTarget: "../../../outside/package",
			files:      map[string]string{"outside/package/payload/source": "package bytes"},
		},
		{
			name: "ancestor of package", linkPath: "packages", linkTarget: "outside/packages",
			files: map[string]string{"outside/packages/target/mod/name/payload/source": "package bytes"},
		},
		{
			name: "descendant of package", linkPath: "packages/target/mod/name/payload/source",
			linkTarget: "../../../../../outside/source",
			files:      map[string]string{"outside/source": "package bytes"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"skills/root.md": "legacy root", "profiles/exact.yaml": "profile bytes"}
			for path, data := range tc.files {
				files[path] = data
			}
			repo := createGitRepo(t, files)
			if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(tc.linkPath)), 0o755); err != nil {
				t.Fatal(err)
			}
			addSymlink(t, repo, tc.linkTarget, tc.linkPath)
			commitAll(t, repo, "add package symlink")
			assertCloneRefusesExactInputSymlink(t, repo, []string{"profiles/exact.yaml", "packages/target/mod/name"})
		})
	}
}

func TestCloneRejectsInvalidExactPaths(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"skills/root.md": "legacy root"})
	for _, exact := range []string{"", " ", ".", "/absolute", "../escape", "packages/../profile", `packages\name`, "packages//name", ".git", ".GIT/config", "nested/.Git/config"} {
		t.Run(exact, func(t *testing.T) {
			_, err := Clone(context.Background(), repo, "main", Credentials{}, Limits{ExactPaths: []string{exact}}, time.Minute, "skills")
			if err == nil || !strings.Contains(err.Error(), "validate exact path") {
				t.Fatalf("Clone error = %v, want exact path validation refusal", err)
			}
		})
	}
}

func assertCloneRefusesExactInputSymlink(t *testing.T, repo string, exactPaths []string) {
	t.Helper()
	_, err := Clone(context.Background(), repo, "main", Credentials{}, Limits{
		RepositoryBytes: 1 << 20, FileCount: 100, FileBytes: 1 << 16,
		ExactPaths: exactPaths,
	}, time.Minute, "skills")
	if err == nil || !strings.Contains(err.Error(), "marketplace.package.checkout-symlink") {
		t.Fatalf("Clone error = %v, want exact input symlink refusal", err)
	}
}
