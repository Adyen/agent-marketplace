package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

type installBindingFixture struct {
	base, home, repo, origin, working, commit string
}

func newInstallBindingFixture(t *testing.T) installBindingFixture {
	t.Helper()
	base := t.TempDir()
	home, repo := filepath.Join(base, "home"), filepath.Join(base, "repo")
	for _, dir := range []string{home, repo, filepath.Join(base, "tmp")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("TMPDIR", filepath.Join(base, "tmp"))
	origin, working := createBareRepoWithWorkingClone(t, "install-binding.git")
	writeSkill(t, working, "review", "Content A", nil)
	commitAndPushAll(t, working, "inventory A")
	commit, err := exec.Command("git", "-C", working, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, repo)
	return installBindingFixture{base, home, repo, origin, working, strings.TrimSpace(string(commit))}
}

func (f installBindingFixture) options() pipeline.InstallOptions {
	return pipeline.InstallOptions{RepoRoot: f.repo, Args: []string{"review"}, FromURL: f.origin, FromRef: "main", Scope: pipeline.ScopeRepo, DependencyReconcile: lifecycle.ReconcileCommandDependencies, PrepareDependencies: lifecycle.PrepareInstallDependencies}
}

func (f installBindingFixture) advance(t *testing.T) {
	t.Helper()
	writeSkill(t, f.working, "review", "Content B", nil)
	writeSkill(t, f.working, "new", "New B", nil)
	commitAndPushAll(t, f.working, "inventory B")
}

func (f installBindingFixture) seedLock(t *testing.T) {
	t.Helper()
	writeConfig(t, config.ProjectConfigPath(f.repo), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {git: file://"+f.origin+", ref: main}\n")
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}
}

func requireNoInstallStaging(t *testing.T) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), ".agent-marketplace-config-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("staging residue=%v error=%v", matches, err)
	}
}

func TestConfiguredInstallFreezesDiscoveryBeforePublication(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, promote := range []bool{false, true} {
		t.Run(map[bool]string{false: "repo bootstrap", true: "user to repo"}[promote], func(t *testing.T) {
			f := newInstallBindingFixture(t)
			path := config.ProjectConfigPath(f.repo)
			if promote {
				path = filepath.Join(f.home, ".agents", "config.yaml")
			}
			writeConfig(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {git: file://"+f.origin+", ref: main}\n")
			opts := f.options()
			opts.FromURL, opts.FromRef = "", ""
			calls := 0
			wrap := func(original pipeline.DependencyReconciler) pipeline.DependencyReconciler {
				return func(ctx context.Context, root, local string, blocked bool, edits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
					calls++
					f.advance(t)
					return original(ctx, root, local, blocked, edits)
				}
			}
			opts.DependencyReconcile = wrap(lifecycle.ReconcileCommandDependencies)
			opts.PrepareDependencies = func(ctx context.Context, root, local string, edits []pipeline.StagedConfigEdit) (pipeline.PreparedInstallDependencies, error) {
				p, err := lifecycle.PrepareInstallDependencies(ctx, root, local, edits)
				if err == nil {
					p.Reconcile = wrap(p.Reconcile)
				}
				return p, err
			}
			result, err := pipeline.Install(context.Background(), opts)
			if err != nil || calls != 1 || result == nil || !slices.Equal(result.Installed, []string{"review"}) {
				t.Fatalf("calls=%d result=%#v err=%v", calls, result, err)
			}
			doc := readExactManifestLock(t, filepath.Join(f.repo, ".agents", "lock.yaml"))
			if len(doc.Registries) != 1 || doc.Registries[0].Commit.Hex() != f.commit {
				t.Fatalf("install changed revision after discovery: got=%#v want=%s", doc.Registries, f.commit)
			}
			raw := ownedCommandRead(t, filepath.Join(f.repo, ".factory", "skills", "review", "SKILL.md"))
			if !bytes.Contains(raw, []byte("Content A")) || bytes.Contains(raw, []byte("Content B")) {
				t.Fatalf("installed wrong content: %s", raw)
			}
			requireNoInstallStaging(t)
		})
	}
}

func TestConfiguredInstallSelectorsAndScopesKeepPreparedPins(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, mode := range []string{"all", "tag", "all user", "all local", "tag user", "tag local", "collection", "repo to user", "user to local", "organization"} {
		t.Run(mode, func(t *testing.T) {
			f := newInstallBindingFixture(t)
			opts := f.options()
			opts.FromURL, opts.FromRef = "", ""
			sourcePath := config.ProjectConfigPath(f.repo)
			outputRoot, lockName := f.repo, "lock.yaml"
			switch mode {
			case "all", "all user", "all local":
				opts.Args, opts.InstallAll = nil, true
			case "tag", "tag user", "tag local":
				writeSkill(t, f.working, "review", "Content A", []string{"chosen"})
				commitAndPushAll(t, f.working, "tagged A")
				opts.Args, opts.Tag = nil, "chosen"
			case "collection":
				writeCollection(t, f.working, "bundle", "name: bundle\nskills:\n  - name: review\n")
				commitAndPushAll(t, f.working, "collection A")
				opts.Args = []string{"bundle"}
			case "repo to user":
				opts.Scope, outputRoot = pipeline.ScopeUser, f.home
			case "user to local":
				opts.Scope, lockName = pipeline.ScopeLocal, "lock.local.yaml"
				sourcePath = filepath.Join(f.home, ".agents", "config.yaml")
			case "organization":
				sourcePath = filepath.Join(f.base, "org", "config.yaml")
				config.SetTestOrganizationConfigPath(t, sourcePath)
			}
			switch mode {
			case "all user", "tag user":
				opts.Scope, outputRoot = pipeline.ScopeUser, f.home
			case "all local", "tag local":
				opts.Scope, lockName = pipeline.ScopeLocal, "lock.local.yaml"
			}
			commit, err := exec.Command("git", "-C", f.working, "rev-parse", "HEAD").Output()
			if err != nil {
				t.Fatal(err)
			}
			f.commit = strings.TrimSpace(string(commit))
			writeConfig(t, sourcePath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {git: file://"+f.origin+", ref: main}\n")
			beforeSource := ownedCommandRead(t, sourcePath)
			calls := 0
			opts.PrepareDependencies = func(ctx context.Context, root, local string, edits []pipeline.StagedConfigEdit) (pipeline.PreparedInstallDependencies, error) {
				p, err := lifecycle.PrepareInstallDependencies(ctx, root, local, edits)
				if err != nil {
					return p, err
				}
				original := p.Reconcile
				p.Reconcile = func(ctx context.Context, root, local string, blocked bool, edits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
					calls++
					f.advance(t)
					return original(ctx, root, local, blocked, edits)
				}
				return p, nil
			}
			result, err := pipeline.Install(context.Background(), opts)
			if err != nil || calls != 1 || result == nil {
				t.Fatalf("calls=%d result=%#v error=%v", calls, result, err)
			}
			if mode == "collection" {
				if !result.IsCollection {
					t.Fatal("collection path was not exercised")
				}
			} else if !slices.Equal(result.Installed, []string{"review"}) {
				t.Fatalf("installed=%v", result.Installed)
			}
			doc := readExactManifestLock(t, filepath.Join(outputRoot, ".agents", lockName))
			if len(doc.Registries) != 1 || doc.Registries[0].Commit.Hex() != f.commit {
				t.Fatalf("pins=%#v want=%s", doc.Registries, f.commit)
			}
			raw := ownedCommandRead(t, filepath.Join(outputRoot, ".factory", "skills", "review", "SKILL.md"))
			if !bytes.Contains(raw, []byte("Content A")) || bytes.Contains(raw, []byte("Content B")) {
				t.Fatalf("installed content=%s", raw)
			}
			if mode == "organization" {
				if !bytes.Equal(beforeSource, ownedCommandRead(t, sourcePath)) {
					t.Fatal("organization configuration changed")
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(sourcePath), "lock.yaml")); !os.IsNotExist(err) {
					t.Fatalf("install created an organization lock: %v", err)
				}
			}
			requireNoInstallStaging(t)
		})
	}
}

func TestConfiguredInstallAbsentNonstandardOrganizationPath(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newInstallBindingFixture(t)
	orgPath := filepath.Join(f.base, "organization", "absent-policy.yaml")
	config.SetTestOrganizationConfigPath(t, orgPath)
	f.seedLock(t)
	if out, err := executeCommand("install", "review"); err != nil {
		t.Fatalf("install with absent organization layer: %v\n%s", err, out)
	}
	if _, err := os.Lstat(filepath.Dir(orgPath)); !os.IsNotExist(err) {
		t.Fatalf("install created organization state: %v", err)
	}
}

func TestConfiguredInstallRejectsIncompatibleScopePins(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newInstallBindingFixture(t)
	userPath := filepath.Join(f.home, ".agents", "config.yaml")
	body := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {git: file://" + f.origin + ", ref: main}\n"
	writeConfig(t, userPath, body)
	if out, err := executeCommand("lock", "--scope", "user"); err != nil {
		t.Fatalf("lock user A: %v\n%s", err, out)
	}
	if err := os.RemoveAll(filepath.Join(f.working, "skills", "review")); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, f.working, "new", "Only in B", nil)
	commitAndPushAll(t, f.working, "replace inventory with B")
	writeConfig(t, config.ProjectConfigPath(f.repo), body)
	if out, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("lock repo B: %v\n%s", err, out)
	}
	before := ownedCommandSnapshot(t, f.base)
	out, err := executeCommand("install", "review")
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("incompatible scope pins were not refused: %v\n%s", err, out)
	}
	if after := ownedCommandSnapshot(t, f.base); !reflect.DeepEqual(before, after) {
		t.Fatal("incompatible pin refusal changed disk state")
	}
	if out, err := executeCommand("install", "new"); err != nil {
		t.Fatalf("compatible target-scope control: %v\n%s", err, out)
	}
	raw := ownedCommandRead(t, filepath.Join(f.repo, ".factory", "skills", "new", "SKILL.md"))
	if !bytes.Contains(raw, []byte("Only in B")) {
		t.Fatalf("compatible control content=%s", raw)
	}
}

func TestConfiguredInstallPinsDiscoveredSubBundle(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newInstallBindingFixture(t)
	child := filepath.Join(f.working, "entries", "team")
	writeSkill(t, child, "nested", "Nested A", nil)
	commitAndPushAll(t, f.working, "nested source A")
	commit, err := exec.Command("git", "-C", f.working, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(f.home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {git: file://"+f.origin+", ref: main, path: entries}\n")
	opts := f.options()
	opts.FromURL, opts.FromRef, opts.Args = "", "", []string{"nested"}
	calls := 0
	opts.PrepareDependencies = func(ctx context.Context, root, local string, edits []pipeline.StagedConfigEdit) (pipeline.PreparedInstallDependencies, error) {
		p, err := lifecycle.PrepareInstallDependencies(ctx, root, local, edits)
		if err != nil {
			return p, err
		}
		original := p.Reconcile
		p.Reconcile = func(ctx context.Context, root, local string, blocked bool, edits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
			calls++
			writeSkill(t, child, "nested", "Nested B", nil)
			commitAndPushAll(t, f.working, "nested source B")
			return original(ctx, root, local, blocked, edits)
		}
		return p, nil
	}
	if _, err := pipeline.Install(context.Background(), opts); err != nil || calls != 1 {
		t.Fatalf("nested install: calls=%d error=%v", calls, err)
	}
	doc := readExactManifestLock(t, filepath.Join(f.repo, ".agents", "lock.yaml"))
	if len(doc.Registries) != 1 || doc.Registries[0].Commit.Hex() != strings.TrimSpace(string(commit)) || len(doc.Registries[0].Roots) != 1 || doc.Registries[0].Roots[0].Path != "entries" {
		t.Fatalf("nested source binding=%#v", doc.Registries)
	}
	raw := ownedCommandRead(t, filepath.Join(f.repo, ".factory", "skills", "nested", "SKILL.md"))
	if !bytes.Contains(raw, []byte("Nested A")) || bytes.Contains(raw, []byte("Nested B")) {
		t.Fatalf("nested output=%s", raw)
	}
}

func TestConfiguredInstallUnlockedOrganizationOutputSurvivesGC(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newInstallBindingFixture(t)
	writeSkill(t, f.working, "review", "Content A", []string{"chosen"})
	commitAndPushAll(t, f.working, "tagged organization resource")
	orgPath := filepath.Join(f.base, "org", "config.yaml")
	writeConfig(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {git: file://"+f.origin+", ref: main}\nresources:\n  hosted: {all: true, required: true}\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	if out, err := executeCommand("install", "--tag", "chosen"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	output := filepath.Join(f.home, ".factory", "skills", "review", "SKILL.md")
	before := ownedCommandRead(t, output)
	if !bytes.Contains(before, []byte("Content A")) {
		t.Fatalf("missing installed content: %s", before)
	}
	if out, err := executeCommand("gc"); err != nil {
		t.Fatalf("gc: %v\n%s", err, out)
	}
	if after := ownedCommandRead(t, output); !bytes.Equal(before, after) {
		t.Fatal("GC invalidated unlocked organization output")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(orgPath), "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("organization lock was created: %v", err)
	}
}

func TestAdHocInstallPreparationPinsOriginalMutableRef(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, mode := range []string{"named", "bundle", "inferred", "user"} {
		t.Run(mode, func(t *testing.T) {
			f := newInstallBindingFixture(t)
			opts := f.options()
			if mode == "bundle" || mode == "inferred" {
				opts.Args = nil
			}
			if mode == "inferred" {
				opts.FromPath = "skills/review"
			}
			if mode == "user" {
				opts.Scope = pipeline.ScopeUser
			}
			calls := 0
			opts.PrepareDependencies = func(ctx context.Context, root, local string, edits []pipeline.StagedConfigEdit) (pipeline.PreparedInstallDependencies, error) {
				p, err := lifecycle.PrepareInstallDependencies(ctx, root, local, edits)
				if err != nil {
					return p, err
				}
				original := p.Reconcile
				p.Reconcile = func(ctx context.Context, root, local string, blocked bool, edits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
					calls++
					if mode == "named" {
						gcCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
						gcErr := runGC(gcCtx, io.Discard, true)
						cancel()
						if !errors.Is(gcErr, context.DeadlineExceeded) {
							t.Fatalf("GC crossed prepared snapshot lease: %v", gcErr)
						}
					}
					f.advance(t)
					return original(ctx, root, local, blocked, edits)
				}
				return p, nil
			}
			result, err := pipeline.Install(context.Background(), opts)
			if err != nil || calls != 1 || result == nil || !slices.Equal(result.Installed, []string{"review"}) {
				t.Fatalf("calls=%d result=%#v err=%v", calls, result, err)
			}
			root := f.repo
			if mode == "user" {
				root = f.home
			}
			doc := readExactManifestLock(t, filepath.Join(root, ".agents", "lock.yaml"))
			if len(doc.Registries) != 1 || doc.Registries[0].Commit.Hex() != f.commit {
				t.Fatalf("pins=%#v want=%s", doc.Registries, f.commit)
			}
			raw := ownedCommandRead(t, filepath.Join(root, ".factory", "skills", "review", "SKILL.md"))
			if !bytes.Contains(raw, []byte("Content A")) || bytes.Contains(raw, []byte("Content B")) {
				t.Fatalf("installed content=%s", raw)
			}
			if _, err := os.Lstat(filepath.Join(root, ".factory", "skills", "new")); !os.IsNotExist(err) {
				t.Fatalf("B-only output exists: %v", err)
			}
			requireNoInstallStaging(t)
		})
	}
}

func TestAdHocInstallWarmContentAndLocalControl(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "warm exact", true: "local"}[local], func(t *testing.T) {
			f := newInstallBindingFixture(t)
			opts := f.options()
			var before []byte
			lockPath := filepath.Join(f.repo, ".agents", "lock.yaml")
			if local {
				opts.FromURL = f.working
				opts.FromRef = ""
			} else {
				f.seedLock(t)
				before = ownedCommandRead(t, lockPath)
				f.advance(t)
			}
			if _, err := pipeline.Install(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			raw := ownedCommandRead(t, filepath.Join(f.repo, ".factory", "skills", "review", "SKILL.md"))
			if !bytes.Contains(raw, []byte("Content A")) || bytes.Contains(raw, []byte("Content B")) {
				t.Fatalf("content=%s", raw)
			}
			if local {
				if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
					t.Fatalf("local registry wrote Git lock: %v", err)
				}
			} else if !bytes.Equal(before, ownedCommandRead(t, lockPath)) {
				t.Fatal("warm lock changed")
			}
			requireNoInstallStaging(t)
		})
	}
}

func TestAdHocInstallPreparedReadsRejectConcurrentChanges(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testInstallPreparedReadsRejectConcurrentChanges(t, false)
}

func TestConfiguredInstallPreparedReadsRejectConcurrentChanges(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testInstallPreparedReadsRejectConcurrentChanges(t, true)
}

func testInstallPreparedReadsRejectConcurrentChanges(t *testing.T, configured bool) {
	for _, scenario := range []string{"include", "late include", "local", "lock", "exact lock", "requirements"} {
		t.Run(scenario, func(t *testing.T) {
			f := newInstallBindingFixture(t)
			f.seedLock(t)
			fragment := filepath.Join(f.repo, ".agents", "fragment.yaml")
			if scenario == "include" || scenario == "late include" {
				writeFile(t, fragment, "resources:\n  hosted: {tags: [before]}\n")
				path := config.ProjectConfigPath(f.repo)
				writeFile(t, path, string(ownedCommandRead(t, path))+"\ninclude: [fragment.yaml]\n")
			}
			opts := f.options()
			if configured {
				opts.FromURL, opts.FromRef = "", ""
			}
			var expected map[string]string
			capture := func() {
				expected = ownedCommandSnapshot(t, f.base)
				for path := range expected {
					if strings.HasPrefix(path, "tmp/.agent-marketplace-config-") {
						delete(expected, path)
					}
				}
			}
			originalCommit := lifecycle.CommitDependencyTransaction
			t.Cleanup(func() { lifecycle.CommitDependencyTransaction = originalCommit })
			calls := 0
			opts.PrepareDependencies = func(ctx context.Context, root, local string, edits []pipeline.StagedConfigEdit) (pipeline.PreparedInstallDependencies, error) {
				p, err := lifecycle.PrepareInstallDependencies(ctx, root, local, edits)
				if err != nil {
					return p, err
				}
				original := p.Reconcile
				p.Reconcile = func(ctx context.Context, root, local string, blocked bool, edits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
					calls++
					switch scenario {
					case "include":
						writeFile(t, fragment, "resources:\n  hosted: {tags: [after]}\n")
					case "late include":
						lifecycle.CommitDependencyTransaction = func(ctx context.Context, state string, desired []dependencytxn.DesiredFile, options ...dependencytxn.Option) (dependencytxn.Result, error) {
							writeFile(t, fragment, "resources:\n  hosted: {tags: [after]}\n")
							capture()
							return originalCommit(ctx, state, desired, options...)
						}
					case "local":
						writeFile(t, config.LocalConfigPath(f.repo), "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {git: file://"+f.origin+", ref: main}\nresources:\n  hosted: {exclude: [skill/review]}\n")
					case "lock":
						path := filepath.Join(f.repo, ".agents", "lock.yaml")
						writeFile(t, path, string(ownedCommandRead(t, path))+"\n")
					case "exact lock":
						f.advance(t)
						if out, err := executeCommand("lock", "--upgrade", "--scope", "repo"); err != nil {
							t.Fatalf("foreign lock update: %v\n%s", err, out)
						}
					case "requirements":
						changed := false
						for i := range edits {
							next := bytes.Replace(edits[i].Bytes, []byte("ref: main"), []byte("ref: other"), 1)
							changed = changed || !bytes.Equal(next, edits[i].Bytes)
							edits[i].Bytes = next
						}
						if !changed {
							t.Fatal("fixture did not change a prepared requirement")
						}
					}
					if scenario != "late include" {
						capture()
					}
					return original(ctx, root, local, blocked, edits)
				}
				return p, nil
			}
			_, err := pipeline.Install(context.Background(), opts)
			if calls != 1 || expected == nil || !errors.Is(err, dependencytxn.ErrGenerationConflict) {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
			if after := ownedCommandSnapshot(t, f.base); !reflect.DeepEqual(expected, after) {
				t.Fatal("refused install changed disk state beyond the injected foreign edit")
			}
			requireNoInstallStaging(t)
		})
	}
}

func TestAdHocInstallMissingSelectionDoesNotPublishOrLeakLease(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testInstallMissingSelectionDoesNotPublishOrLeakLease(t, false)
}

func TestConfiguredInstallMissingSelectionDoesNotPublishOrLeakLease(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testInstallMissingSelectionDoesNotPublishOrLeakLease(t, true)
}

func testInstallMissingSelectionDoesNotPublishOrLeakLease(t *testing.T, configured bool) {
	f := newInstallBindingFixture(t)
	opts := f.options()
	opts.Args = []string{"absent"}
	if configured {
		writeConfig(t, config.ProjectConfigPath(f.repo), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {git: file://"+f.origin+", ref: main}\n")
		opts.FromURL, opts.FromRef = "", ""
	}
	before := ownedCommandSnapshot(t, f.repo)
	var retained pipeline.PreparedInstallDependencies
	prepares := 0
	opts.PrepareDependencies = func(ctx context.Context, root, local string, edits []pipeline.StagedConfigEdit) (pipeline.PreparedInstallDependencies, error) {
		p, err := lifecycle.PrepareInstallDependencies(ctx, root, local, edits)
		if err == nil {
			prepares++
			retained = p
		}
		return p, err
	}
	_, err := pipeline.Install(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "not found") || prepares != 1 {
		t.Fatalf("prepares=%d error=%v", prepares, err)
	}
	if after := ownedCommandSnapshot(t, f.repo); !reflect.DeepEqual(before, after) {
		t.Fatal("missing selection published state")
	}
	if _, err := retained.Reconcile(context.Background(), f.repo, "", false, nil); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("released plan reused: %v", err)
	}
	if err := retained.BindRegistry(config.RepoCommitted, config.RegistryEntry{}); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("released binding reused: %v", err)
	}
	requireNoInstallStaging(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	visited := false
	_, err = cache.GarbageCollect(ctx, true, func(context.Context) (cache.GCReachability, error) {
		visited = true
		return cache.GCReachability{}, nil
	})
	if err != nil || !visited {
		t.Fatalf("GC after failed install: visited=%t error=%v", visited, err)
	}
}

func reviewBindingRegistry(t testing.TB) lockfile.GitRegistry {
	t.Helper()
	commit, err := lockfile.ParseGitOID("sha1:" + strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	digest := lockfile.Digest("sha256:" + strings.Repeat("b", 64))
	return lockfile.GitRegistry{URL: "https://example.invalid/registry", Ref: "main", Commit: commit, Roots: []lockfile.GitRoot{{Path: ".", SourceHarness: "claude", RevisionSchema: "v3", RevisionDigest: digest, Resources: []lockfile.GitResource{{Kind: "skill", Name: "review", ContentDigest: digest, ArchiveDigest: digest}}}}}
}

func TestInstallBindingSourceLookup(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	userPath := filepath.Join(t.TempDir(), "user-lock.yaml")
	repoPath := filepath.Join(t.TempDir(), "repo-lock.yaml")
	registry := reviewBindingRegistry(t)
	plan := &lifecycle.PreparedInstallLocks{Documents: map[string]lockfile.Document{userPath: {APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeUser, Registries: []lockfile.GitRegistry{registry}}}, LockPaths: map[config.Source]string{config.UserPersonal: userPath, config.RepoCommitted: repoPath}, Bindings: map[string][]lockfile.GitRegistry{}}
	entry := config.RegistryEntry{URL: registry.URL, Ref: registry.Ref, Source: config.Organization}
	if err := plan.BindRegistry(config.RepoCommitted, entry); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("source mismatch error = %v", err)
	}
	if len(plan.Bindings) != 0 {
		t.Fatalf("bindings after mismatch = %#v", plan.Bindings)
	}
	entry.Source = config.UserPersonal
	if err := plan.BindRegistry(config.RepoCommitted, entry); err != nil {
		t.Fatal(err)
	}
	want := []lockfile.GitRegistry{registry}
	if !reflect.DeepEqual(plan.Bindings[repoPath], want) {
		t.Fatalf("bindings = %#v, want %#v", plan.Bindings[repoPath], want)
	}
}

func TestMergeInstallBindingsPreservesInputs(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	registry := reviewBindingRegistry(t)
	root := registry.Roots[0]
	backing := make([]lockfile.GitRoot, 2)
	backing[0] = root
	base := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo, Registries: []lockfile.GitRegistry{{URL: registry.URL, Ref: registry.Ref, Commit: registry.Commit, Roots: backing[:1]}}}
	inputRoots := append([]lockfile.GitRoot(nil), base.Registries[0].Roots...)
	bindingRoot := root
	bindingRoot.Path = "extra"
	binding := []lockfile.GitRegistry{{URL: base.Registries[0].URL, Ref: base.Registries[0].Ref, Commit: base.Registries[0].Commit, Roots: []lockfile.GitRoot{bindingRoot}}}
	merged, err := lifecycle.MergeInstallBindings(base, lockfile.ScopeRepo, binding)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Registries) != 1 || len(merged.Registries[0].Roots) != 2 || merged.Registries[0].Roots[0].Path != "." || merged.Registries[0].Roots[1].Path != "extra" {
		t.Fatalf("merged = %#v", merged)
	}
	if !reflect.DeepEqual(backing, []lockfile.GitRoot{root, lockfile.GitRoot{}}) || !reflect.DeepEqual(base.Registries[0].Roots, inputRoots) || !reflect.DeepEqual(binding[0].Roots, []lockfile.GitRoot{bindingRoot}) {
		t.Fatalf("inputs mutated: backing=%#v roots=%#v binding=%#v", backing, base.Registries[0].Roots, binding[0].Roots)
	}
	conflicting := binding[0]
	conflicting.Commit, err = lockfile.ParseGitOID("sha1:" + strings.Repeat("c", 40))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.MergeInstallBindings(base, lockfile.ScopeRepo, []lockfile.GitRegistry{conflicting}); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("commit conflict error = %v", err)
	}
	if !reflect.DeepEqual(backing, []lockfile.GitRoot{root, lockfile.GitRoot{}}) || !reflect.DeepEqual(base.Registries[0].Roots, inputRoots) || !reflect.DeepEqual(binding[0].Roots, []lockfile.GitRoot{bindingRoot}) {
		t.Fatal("commit conflict mutated inputs")
	}
	digestConflict := binding[0]
	digestConflict.Roots = []lockfile.GitRoot{{Path: ".", SourceHarness: "claude", RevisionSchema: "v3", RevisionDigest: lockfile.Digest("sha256:" + strings.Repeat("c", 64)), Resources: root.Resources}}
	if _, err := lifecycle.MergeInstallBindings(base, lockfile.ScopeRepo, []lockfile.GitRegistry{digestConflict}); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("digest conflict error = %v", err)
	}
	if !reflect.DeepEqual(backing, []lockfile.GitRoot{root, lockfile.GitRoot{}}) || !reflect.DeepEqual(base.Registries[0].Roots, inputRoots) || !reflect.DeepEqual(binding[0].Roots, []lockfile.GitRoot{bindingRoot}) {
		t.Fatal("digest conflict mutated inputs")
	}
}
