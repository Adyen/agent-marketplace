package pipeline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestPreviewSourceRejectsBlockedURLBeforeFetching(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v4\nallowed_registries: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, err := NewWorkspace(repo, repo)
	if err != nil {
		t.Fatal(err)
	}

	_, err = PreviewSource(context.Background(), ws, "https://example.invalid/missing.git", "main", "")
	if err == nil || !strings.Contains(err.Error(), "blocked by allowed_registries") {
		t.Fatalf("PreviewSource error = %v, want allowlist refusal before an unreachable fetch", err)
	}
	var rejected *SourceRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("PreviewSource error type = %T, want *SourceRejectedError", err)
	}
	wantBlocked := config.ValidateRegistryURLs([]config.RegistryEntry{{URL: "https://example.invalid/missing.git", Ref: "main"}}, []string{})
	if err.Error() != wantBlocked.Error() {
		t.Fatalf("wrapped blocked-source error = %q, want unchanged message %q", err, wantBlocked)
	}

	if err := os.WriteFile(filepath.Join(repo, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v4\nallowed_registries: ['.*']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = PreviewSource(context.Background(), ws, "https://example.invalid/missing.git", "main", "../escape")
	if err == nil {
		t.Fatal("PreviewSource accepted an invalid registry path")
	}
	if !errors.As(err, &rejected) {
		t.Fatalf("invalid-path error type = %T, want *SourceRejectedError", err)
	}
	wantPath := config.ValidateRegistryPath("../escape")
	if err.Error() != wantPath.Error() {
		t.Fatalf("wrapped invalid-path error = %q, want unchanged message %q", err, wantPath)
	}
}

// spec: resource-installation/expected-commit-for-a-directory-source-is-refused
func TestInstallRefusesExpectedCommitForDirectoryWithoutWriting(t *testing.T) {
	repo := t.TempDir()
	source := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(filepath.Join(source, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "alpha", "SKILL.md"), []byte("---\nname: alpha\ndescription: Alpha\n---\n\nAlpha.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := config.ProjectConfigPath(repo)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	before := []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	if err := os.WriteFile(configPath, before, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Install(context.Background(), InstallOptions{
		RepoRoot: repo, Args: []string{"alpha"}, FromURL: source, ExpectedCommit: "0123456789abcdef0123456789abcdef01234567", Scope: ScopeRepo,
	})
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("Install error = %v, want directory expected-commit refusal", err)
	}
	after, readErr := os.ReadFile(configPath)
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("config after refusal = %q, %v; want original bytes %q", after, readErr, before)
	}
}

// spec: resource-installation/moved-source-refuses-an-expected-commit-install
func TestInstallRefusesMovedSourceWithoutPublishing(t *testing.T) {
	repo := t.TempDir()
	configPath := config.ProjectConfigPath(repo)
	writeSourceFile(t, repo, ".agents/config.yaml", "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	lockPath := config.RepoCommitted.LockPath(configPath)
	writeSourceFile(t, repo, filepath.Base(filepath.Dir(lockPath))+"/"+filepath.Base(lockPath), "apiVersion: agent-marketplace-lock/v2\nscope: repo\n")
	harnessPath := filepath.Join(repo, ".factory", "skills", "preserve-me", "SKILL.md")
	writeSourceFile(t, repo, ".factory/skills/preserve-me/SKILL.md", "unchanged harness\n")
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeHarness, err := os.ReadFile(harnessPath)
	if err != nil {
		t.Fatal(err)
	}
	const expected = "0123456789abcdef0123456789abcdef01234567"
	const actual = "89abcdef0123456789abcdef0123456789abcdef"
	var dependencyView = DependencyLockView{CatalogOption: catalog.WithCloneOrPull(func(string, string) (string, error) {
		return "", errors.New("catalog must not be loaded for a moved source")
	})}
	reconcile := func(context.Context, string, string, bool, []StagedConfigEdit) (DependencyLockView, error) {
		return dependencyView, nil
	}
	prepare := func(ctx context.Context, _, _ string, _ []StagedConfigEdit) (PreparedInstallDependencies, error) {
		return PreparedInstallDependencies{
			Context:         ctx,
			DiscoveryConfig: &config.Config{},
			View:            dependencyView,
			Reconcile:       reconcile,
			BindRegistry:    func(config.Source, config.RegistryEntry) error { return nil },
			Release:         func() {},
			ResolvedCommit:  func(config.RegistryEntry) (string, error) { return actual, nil },
		}, nil
	}

	_, err = Install(context.Background(), InstallOptions{
		RepoRoot: repo, Args: []string{"alpha"}, FromURL: "https://example.invalid/source.git",
		FromRef: "main", ExpectedCommit: expected, Scope: ScopeRepo,
		DependencyReconcile: reconcile, PrepareDependencies: prepare,
	})
	var moved *SourceMovedError
	if !errors.As(err, &moved) || moved.Expected != expected || moved.Actual != actual {
		t.Fatalf("Install error = %v, want SourceMovedError expected=%s actual=%s", err, expected, actual)
	}
	for _, check := range []struct {
		name string
		path string
		want []byte
	}{
		{"config", configPath, beforeConfig},
		{"lock", lockPath, beforeLock},
		{"harness", harnessPath, beforeHarness},
	} {
		got, readErr := os.ReadFile(check.path)
		if readErr != nil || string(got) != string(check.want) {
			t.Fatalf("%s after moved-ref refusal = %q, %v; want %q", check.name, got, readErr, check.want)
		}
	}
}

func TestInstallExpectedCommitFailsClosedWithoutPreparedCommit(t *testing.T) {
	repo := t.TempDir()
	configPath := config.ProjectConfigPath(repo)
	writeSourceFile(t, repo, ".agents/config.yaml", "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	reconcile := func(context.Context, string, string, bool, []StagedConfigEdit) (DependencyLockView, error) {
		return DependencyLockView{}, nil
	}

	_, err = Install(context.Background(), InstallOptions{
		RepoRoot: repo, Args: []string{"alpha"}, FromURL: "https://example.invalid/source.git",
		FromRef: "main", ExpectedCommit: "0123456789abcdef0123456789abcdef01234567",
		Scope: ScopeRepo, DependencyReconcile: reconcile,
	})
	if err == nil || !strings.Contains(err.Error(), "exact prepared source commit") {
		t.Fatalf("Install error = %v, want fail-closed missing prepared commit", err)
	}
	after, readErr := os.ReadFile(configPath)
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("config after missing-prepared-commit refusal = %q, %v; want unchanged bytes", after, readErr)
	}
}

// spec: resource-installation/matching-expected-commit-installs-at-the-requested-ref
func TestInstallMatchingExpectedCommitKeepsRequestedRefInStagedConfig(t *testing.T) {
	repo := t.TempDir()
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(t.TempDir(), "source.git")
	work := filepath.Join(t.TempDir(), "work")
	runGit(t, "", "init", "--bare", "--initial-branch=main", bare)
	runGit(t, "", "clone", bare, work)
	writeSourceFile(t, work, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: Alpha\n---\n\nAlpha.\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "add catalog")
	runGit(t, work, "push", "origin", "main")
	commitOutput, err := exec.Command("git", "-C", work, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.TrimSpace(string(commitOutput))
	sourceURL := "file://" + filepath.ToSlash(bare)
	writeSourceFile(t, repo, ".agents/config.yaml", "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	catalogOption := catalog.WithCloneOrPull(func(string, string) (string, error) { return work, nil })
	reconcile := func(context.Context, string, string, bool, []StagedConfigEdit) (DependencyLockView, error) {
		return DependencyLockView{CatalogOption: catalogOption}, nil
	}
	var preparedConfig []byte
	prepare := func(ctx context.Context, _, _ string, edits []StagedConfigEdit) (PreparedInstallDependencies, error) {
		if len(edits) > 0 {
			preparedConfig = append([]byte(nil), edits[0].Bytes...)
		}
		return PreparedInstallDependencies{
			Context: ctx,
			DiscoveryConfig: &config.Config{
				APIVersion: config.APIVersionV4,
				Registries: []config.RegistryEntry{{URL: sourceURL, Ref: "main"}},
			},
			View:           DependencyLockView{CatalogOption: catalogOption},
			Reconcile:      reconcile,
			BindRegistry:   func(config.Source, config.RegistryEntry) error { return nil },
			Release:        func() {},
			ResolvedCommit: func(config.RegistryEntry) (string, error) { return commit, nil },
		}, nil
	}

	result, err := Install(context.Background(), InstallOptions{
		RepoRoot: repo, Args: []string{"alpha"}, FromURL: sourceURL, FromRef: "main",
		ExpectedCommit: commit, Scope: ScopeRepo,
		DependencyReconcile: reconcile, PrepareDependencies: prepare,
	})
	if err != nil || result == nil {
		t.Fatalf("Install matching expected commit = %#v, %v", result, err)
	}
	if !strings.Contains(string(preparedConfig), "ref: main") {
		t.Fatalf("prepared install did not retain the requested ref: %s", preparedConfig)
	}
}

// spec: tui-interactions/source-with-several-bundles-asks-for-a-bundle-path
func TestPreviewSourceReportsSubBundlePaths(t *testing.T) {
	repo := t.TempDir()
	source := filepath.Join(t.TempDir(), "registry")
	for _, name := range []string{"alpha", "beta"} {
		writeSourceFile(t, source, filepath.Join(name, "commands", name+".md"), "---\nname: "+name+"\ndescription: "+name+"\n---\n\n"+name+"\n")
	}
	ws, err := NewWorkspace(repo, repo)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewSource(context.Background(), ws, source, "", "")
	if err != nil {
		t.Fatalf("PreviewSource: %v", err)
	}
	if strings.Join(preview.BundlePaths, ",") != "alpha,beta" {
		t.Fatalf("bundle paths = %#v, want [alpha beta]", preview.BundlePaths)
	}
}

// spec: tui-interactions/malformed-and-empty-sources-are-not-installable
func TestPreviewSourceReturnsMalformedResourcesAndGoodEntries(t *testing.T) {
	repo := t.TempDir()
	source := filepath.Join(t.TempDir(), "registry")
	writeSourceFile(t, source, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: Alpha\n---\n\nAlpha.\n")
	writeSourceFile(t, source, "skills/broken/SKILL.md", "---\nname: [\n---\nBroken.\n")
	ws, err := NewWorkspace(repo, repo)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewSource(context.Background(), ws, source, "", "")
	if err != nil {
		t.Fatalf("PreviewSource: %v", err)
	}
	if len(preview.Catalog.Entries) != 1 || preview.Catalog.Entries[0].Name != "alpha" {
		t.Fatalf("catalog entries = %#v, want only alpha", preview.Catalog.Entries)
	}
	if len(preview.Catalog.Malformed) != 1 {
		t.Fatalf("malformed resources = %#v, want broken resource", preview.Catalog.Malformed)
	}
}

// spec: tui-interactions/previewing-a-source-writes-nothing
func TestPreviewSourceResolvesGitDefaultBranchAndCommit(t *testing.T) {
	repo := t.TempDir()
	bare := filepath.Join(t.TempDir(), "source.git")
	work := filepath.Join(t.TempDir(), "work")
	runGit(t, "", "init", "--bare", "--initial-branch=stable", bare)
	runGit(t, "", "clone", bare, work)
	writeSourceFile(t, work, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: Alpha\n---\n\nAlpha.\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "add catalog")
	runGit(t, work, "push", "origin", "stable")
	runGit(t, bare, "symbolic-ref", "HEAD", "refs/heads/stable")

	ws, err := NewWorkspace(repo, repo)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewSource(context.Background(), ws, "file://"+filepath.ToSlash(bare), "", "")
	if err != nil {
		t.Fatalf("PreviewSource: %v", err)
	}
	if preview.Ref != "stable" || len(preview.Commit) != 40 {
		t.Fatalf("resolved ref/commit = %q/%q, want stable and a full commit", preview.Ref, preview.Commit)
	}
	if len(preview.Catalog.Entries) != 1 || preview.Catalog.Entries[0].Name != "alpha" {
		t.Fatalf("catalog entries = %#v, want alpha", preview.Catalog.Entries)
	}
}

func writeSourceFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// spec: resource-installation/kind-qualified-picks-select-resources-that-share-a-name
func TestInstallFromAdHocLocatorsSelectKindsAndCollection(t *testing.T) {
	repo := t.TempDir()
	source := filepath.Join(t.TempDir(), "registry")
	writeSourceFile(t, source, "skills/shared/SKILL.md", "---\nname: shared\ndescription: Shared skill\n---\n\nShared skill.\n")
	writeSourceFile(t, source, "commands/shared.md", "---\nname: shared\ndescription: Shared command\n---\n\nShared command.\n")
	writeSourceFile(t, source, "collections/docs-pack/COLLECTION.yaml", "name: docs-pack\ndescription: Documentation pack\nskills:\n  - name: shared\n")
	writeSourceFile(t, repo, ".agents/config.yaml", "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")

	result, err := Install(context.Background(), InstallOptions{
		RepoRoot: repo, FromURL: source, Scope: ScopeRepo,
		Locators: []config.ResourceLocator{
			{Kind: ir.KindSkill, Name: "shared"},
			{Kind: ir.KindCommand, Name: "shared"},
			{Kind: ir.KindCollection, Name: "docs-pack"},
		},
	})
	if err != nil {
		t.Fatalf("Install with kind-qualified locators: %v", err)
	}
	if result == nil {
		t.Fatal("Install returned no result")
	}
	v4, err := config.LoadV4ConfigFromPath(config.ProjectConfigPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, request := range v4.Requests {
		kinds[request.Kind+"/"+request.Name] = true
	}
	for _, want := range []string{"skill/shared", "command/shared", "collection/docs-pack"} {
		if !kinds[want] {
			t.Errorf("v4 requests do not include %q: %#v", want, v4.Requests)
		}
	}
	for _, path := range []string{
		filepath.Join(repo, ".factory", "skills", "shared", "SKILL.md"),
		filepath.Join(repo, ".factory", "commands", "shared.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("selected resource was not materialized at %s: %v", path, err)
		}
	}
}
