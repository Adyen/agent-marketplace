package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

type ownedCommandFixture struct {
	base, home, repo, origin, scope string
	configPath, lockPath, aggregate string
	store                           *ownership.Store
}

func newOwnedCommandFixture(t *testing.T, target, scope string) ownedCommandFixture {
	t.Helper()
	base := t.TempDir()
	home, repo := filepath.Join(base, "home"), filepath.Join(base, "repo")
	for _, path := range []string{home, repo, filepath.Join(base, "tmp")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	origin, working := createBareRepoWithWorkingClone(t, "owned-mcp.git")
	writeFile(t, filepath.Join(working, ".mcp.json"), "{\n  \"mcpServers\": {\n    \"remove\": {\"command\": \"remove-binary\"},\n    \"keep\": {\"command\": \"keep-binary\"}\n  }\n}\n")
	commitAndPushAll(t, working, "seed MCPs")
	t.Setenv("TMPDIR", filepath.Join(base, "tmp"))
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	global := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, global, "\nharnesses: ["+target+"]\n")
	writeConfig(t, configPath, "\nharnesses: ["+target+"]\n")
	if scope == "user" {
		configPath = global
	}
	writeConfig(t, configPath, "\nharnesses: ["+target+"]\nregistries:\n  - url: "+origin+"\n    ref: main\n    default: true\n")
	withWorkingDir(t, repo)
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, out)
	}
	root, rel := filepath.Join(repo, ".devin"), "mcp_config.json"
	if scope == "user" {
		root = filepath.Join(home, ".config", "devin")
	}
	if target == "codex" {
		root, rel = filepath.Join(repo, ".codex"), "config.toml"
		if scope == "user" {
			root = filepath.Join(home, ".codex")
		}
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	fixture := ownedCommandFixture{base: base, home: home, repo: repo, origin: origin, scope: scope, configPath: configPath, lockPath: filepath.Join(filepath.Dir(configPath), "lock.yaml"), aggregate: filepath.Join(root, rel), store: store}
	raw := ownedCommandRead(t, fixture.aggregate)
	if !bytes.Contains(raw, []byte("remove-binary")) || !bytes.Contains(raw, []byte("keep-binary")) {
		t.Fatalf("seed aggregate lacks both MCPs: %q", raw)
	}
	return fixture
}

func ownedCommandRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (f ownedCommandFixture) block(t *testing.T) {
	t.Helper()
	path := filepath.Join(f.home, ".agents", "config.yaml")
	raw := append(ownedCommandRead(t, path), []byte("\nallowed_registries: []\n")...)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f ownedCommandFixture) remove(inferred bool) (string, error) {
	args := []string{"remove", "remove", "--from", f.origin, "--scope", f.scope}
	if !inferred {
		args = append(args, "--kind", "mcp")
	}
	return executeCommand(args...)
}

func ownedCommandSnapshot(t *testing.T, root string, excluded ...string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, skip := range excluded {
			if path == skip {
				return nil
			}
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == "home/.local/state/agent-marketplace/locks" || rel == "home/.local/state/agent-marketplace/dependencies/locks" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var data string
		if info.Mode()&os.ModeSymlink != 0 {
			data, err = os.Readlink(path)
		} else if info.Mode().IsRegular() {
			var raw []byte
			raw, err = os.ReadFile(path)
			data = string(raw)
		} else {
			return fmt.Errorf("unexpected fixture file type at %s", path)
		}
		if err != nil {
			return err
		}
		state[rel] = fmt.Sprintf("%s %d\n%s", info.Mode(), info.ModTime().UnixNano(), data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func requireOwnedCommandSnapshot(t *testing.T, f ownedCommandFixture, before map[string]string) {
	t.Helper()
	if after := ownedCommandSnapshot(t, f.base); !reflect.DeepEqual(before, after) {
		for path, want := range before {
			if after[path] != want {
				t.Errorf("refused operation changed or removed %s", path)
			}
		}
		for path := range after {
			if _, exists := before[path]; !exists {
				t.Errorf("refused operation created %s", path)
			}
		}
		t.FailNow()
	}
}

func TestRemoveBlockedOwnedMCPPreservesSiblings(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		target, scope string
		inferred      bool
	}{
		{target: "devin", scope: "repo"},
		{target: "devin", scope: "repo", inferred: true},
		{target: "codex", scope: "user"},
		{target: "codex", scope: "user", inferred: true},
		{target: "codex", scope: "repo"},
	} {
		t.Run(fmt.Sprintf("%s/%s/inferred=%t", tc.target, tc.scope, tc.inferred), func(t *testing.T) {
			f := newOwnedCommandFixture(t, tc.target, tc.scope)
			raw := ownedCommandRead(t, f.aggregate)
			edited := bytes.Replace(raw, []byte("keep-binary"), []byte("keep-edited"), 1)
			if bytes.Equal(raw, edited) {
				t.Fatal("fixture failed to drift keep")
			}
			var spans []string
			if tc.target == "devin" {
				edited = []byte("{\n  // retained setting\n  \"custom\": 42,\n  \"mcpServers\": {\n    \"remove\": {\"command\":\"remove-binary\"},\n    \"keep\":  { \"command\": \"keep-edited\" },\n    \"handwritten\": {\"command\":\"manual\"}\n  }\n}\n")
				spans = []string{"// retained setting\n  \"custom\": 42,", "\"keep\":  { \"command\": \"keep-edited\" }", "\"handwritten\": {\"command\":\"manual\"}"}
			} else {
				edited = append([]byte("# retained setting\ncustom = 42\n"), edited...)
				edited = append(edited, []byte("\n[mcp_servers.handwritten]\ncommand = \"manual\"\n")...)
				spans = []string{"# retained setting\ncustom = 42\n", "command = 'keep-edited'", "[mcp_servers.handwritten]\ncommand = \"manual\"\n"}
			}
			for _, span := range spans {
				if !bytes.Contains(edited, []byte(span)) {
					t.Fatalf("fixture lacks expected span %q", span)
				}
			}
			if err := os.WriteFile(f.aggregate, edited, 0o600); err != nil {
				t.Fatal(err)
			}
			record, err := f.store.Load()
			if err != nil {
				t.Fatal(err)
			}
			var keeper ownership.Entry
			for _, entry := range record.Files[filepath.Base(f.aggregate)].Entries {
				if entry.Key == "keep" {
					keeper = entry
				}
			}
			if keeper.Key != "keep" {
				t.Fatal("seed ownership lacks keep")
			}
			f.block(t)
			var acquisitions atomic.Int32
			previous := lockSnapshotAcquiredHook
			lockSnapshotAcquiredHook = func() { acquisitions.Add(1) }
			defer func() { lockSnapshotAcquiredHook = previous }()
			out, err := f.remove(tc.inferred)
			if err != nil {
				t.Fatalf("remove: %v\n%s", err, out)
			}
			wantTrustReview := tc.target == "codex" && tc.scope == "repo"
			if strings.Contains(out, "materialize.codex.trust-review") != wantTrustReview {
				t.Fatalf("trust-review diagnostic mismatch: %s", out)
			}
			if acquisitions.Load() != 0 {
				t.Fatalf("new acquisitions = %d", acquisitions.Load())
			}
			after := ownedCommandRead(t, f.aggregate)
			if bytes.Contains(after, []byte("remove-binary")) {
				t.Fatal("target remains")
			}
			last := -1
			for _, span := range spans {
				index := bytes.Index(after, []byte(span))
				if index <= last {
					t.Fatalf("retained span changed or reordered: %q\n%s", span, after)
				}
				last = index
			}
			next, err := f.store.Load()
			if err != nil {
				t.Fatal(err)
			}
			entries := next.Files[filepath.Base(f.aggregate)].Entries
			if len(entries) != 1 || !reflect.DeepEqual(entries[0], keeper) {
				t.Fatalf("sibling ownership changed: %#v", entries)
			}
			proof, err := f.store.Proof(filepath.Base(f.aggregate))
			if err != nil || proof.Classify(keeper.ID(), ownership.Digest([]byte(`{"command":"keep-edited"}`))) != ownership.StatusOwnedDrifted {
				t.Fatalf("sibling drift laundered: %v", err)
			}
			cfg, err := config.LoadConfigFromPath(f.configPath)
			if err != nil || cfg == nil || len(cfg.Registries) != 1 || !containsStr(cfg.Registries[0].MCP.Exclude, "remove") {
				t.Fatalf("config=%#v error=%v", cfg, err)
			}
			if _, err := lockfile.Read(f.lockPath); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRemoveBlockedOwnedMCPRefusesUnsafeState(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, scenario := range []string{"missing ownership", "corrupt ownership", "drifted target", "pending journal", "unsupported target", "missing snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			f := newOwnedCommandFixture(t, "devin", "repo")
			want := materialize.ErrRepairCandidateStale
			switch scenario {
			case "missing ownership":
				if err := os.Remove(f.store.StateFile()); err != nil {
					t.Fatal(err)
				}
			case "corrupt ownership":
				want = materialize.ErrOwnedCleanupUnsupported
				if err := os.WriteFile(f.store.StateFile(), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "drifted target":
				raw := ownedCommandRead(t, f.aggregate)
				changed := bytes.Replace(raw, []byte("remove-binary"), []byte("user-edited-target"), 1)
				if bytes.Equal(raw, changed) {
					t.Fatal("target was not edited")
				}
				if err := os.WriteFile(f.aggregate, changed, 0o600); err != nil {
					t.Fatal(err)
				}
			case "pending journal":
				want = materialize.ErrRepairGenerationInFlight
				crashed, err := ownership.Open(filepath.Dir(f.aggregate), ownership.WithCrasher(ownership.CrashAt(ownership.CrashAfterJournalPrepared)))
				if err != nil {
					t.Fatal(err)
				}
				record, err := crashed.Load()
				if err != nil {
					t.Fatal(err)
				}
				raw := ownedCommandRead(t, f.aggregate)
				_, err = crashed.Apply(ownership.Mutation{Rel: filepath.Base(f.aggregate), AbsFile: f.aggregate, NextBytes: append(append([]byte(nil), raw...), '\n'), Entries: record.Files[filepath.Base(f.aggregate)].Entries, Reason: ownership.ReasonSync, ExpectPriorGeneration: record.Generation, ExpectPriorDigest: ownership.Digest(raw)})
				if !errors.Is(err, ownership.ErrInjectedCrash) {
					t.Fatalf("pending fixture error = %v", err)
				}
			case "unsupported target":
				want = materialize.ErrOwnedCleanupUnsupported
				raw := ownedCommandRead(t, f.configPath)
				changed := bytes.Replace(raw, []byte("[devin]"), []byte("[devin, factory]"), 1)
				if bytes.Equal(raw, changed) {
					t.Fatal("target mix unchanged")
				}
				if err := os.WriteFile(f.configPath, changed, 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing snapshot":
				doc, err := lockfile.Read(f.lockPath)
				if err != nil || len(doc.Registries) != 1 {
					t.Fatalf("lock=%#v err=%v", doc, err)
				}
				oid, err := cache.ParseGitOID(doc.Registries[0].Commit.String())
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := cache.OpenExactGit(doc.Registries[0].URL, oid)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(snapshot); err != nil {
					t.Fatal(err)
				}
			}
			f.block(t)
			before := ownedCommandSnapshot(t, f.base)
			out, err := f.remove(false)
			if scenario == "missing snapshot" {
				var lockErr *pipeline.LockViewError
				if !errors.As(err, &lockErr) || lockErr.Code != "lock.snapshot-unavailable" {
					t.Fatalf("error=%v, want snapshot-unavailable\n%s", err, out)
				}
			} else if !errors.Is(err, want) {
				t.Fatalf("error=%v, want %v\n%s", err, want, out)
			}
			requireOwnedCommandSnapshot(t, f, before)
		})
	}
}

func TestRemoveBlockedOwnedMCPRejectsChangedAuthorization(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, scenario := range []string{"included config", "new local config", "lock generation", "restored after publish"} {
		t.Run(scenario, func(t *testing.T) {
			f := newOwnedCommandFixture(t, "devin", "repo")
			fragment := filepath.Join(f.repo, ".agents", "fragment.yaml")
			if scenario == "included config" {
				writeFile(t, fragment, "tags: [original]\n")
				raw := append(ownedCommandRead(t, f.configPath), []byte("\ninclude: [fragment.yaml]\n")...)
				if err := os.WriteFile(f.configPath, raw, 0o600); err != nil {
					t.Fatal(err)
				}
				if out, err := executeCommand("sync"); err != nil {
					t.Fatalf("seed include: %v\n%s", err, out)
				}
			}
			f.block(t)
			originalConfig := ownedCommandRead(t, f.configPath)
			originalAggregate := ownedCommandRead(t, f.aggregate)
			originalCommit := commitDependencyTransaction
			defer func() { commitDependencyTransaction = originalCommit }()
			var expected map[string]string
			calls := 0
			commitDependencyTransaction = func(ctx context.Context, state string, desired []dependencytxn.DesiredFile, options ...dependencytxn.Option) (dependencytxn.Result, error) {
				calls++
				if scenario == "restored after publish" {
					options = append(options, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
						if point.Stage == dependencytxn.FaultAfterFinalize {
							if err := os.WriteFile(f.configPath, originalConfig, 0o600); err != nil {
								return err
							}
							expected = ownedCommandSnapshot(t, f.base)
						}
						return nil
					}))
				} else {
					var path string
					var data []byte
					switch scenario {
					case "included config":
						path, data = fragment, []byte("tags: [changed]\n")
					case "new local config":
						path, data = filepath.Join(f.repo, ".agents", "config.local.yaml"), []byte("mcp: {include: [remove]}\n")
					case "lock generation":
						path, data = f.lockPath, append(ownedCommandRead(t, f.lockPath), '\n')
					}
					if err := os.WriteFile(path, data, 0o600); err != nil {
						return dependencytxn.Result{}, err
					}
					expected = ownedCommandSnapshot(t, f.base)
				}
				return originalCommit(ctx, state, desired, options...)
			}
			out, err := f.remove(false)
			if calls != 1 || expected == nil || !errors.Is(err, dependencytxn.ErrGenerationConflict) {
				t.Fatalf("calls=%d captured=%t error=%v\n%s", calls, expected != nil, err, out)
			}
			var committed *dependencytxn.CommittedScopeError
			if errors.As(err, &committed) != (scenario == "restored after publish") {
				t.Fatalf("incorrect committed classification: %v", err)
			}
			if !bytes.Equal(originalAggregate, ownedCommandRead(t, f.aggregate)) {
				t.Fatal("stale authorization deleted aggregate content")
			}
			for path := range expected {
				if strings.HasPrefix(path, "tmp/.agent-marketplace-config-") {
					delete(expected, path)
				}
			}
			requireOwnedCommandSnapshot(t, f, expected)
		})
	}
}

func TestRemoveBlockedOwnedMCPScopeMismatch(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newOwnedCommandFixture(t, "devin", "user")
	writeConfig(t, filepath.Join(f.repo, ".agents", "config.yaml"), "\nharnesses: [devin]\nregistries:\n  - url: "+f.origin+"\n    ref: main\n    default: false\n")
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed scoped registry: %v\n%s", err, out)
	}
	f.block(t)
	before := ownedCommandSnapshot(t, f.base)
	out, err := executeCommand("remove", "remove", "--kind", "mcp", "--from", f.origin, "--scope", "repo")
	if !errors.Is(err, materialize.ErrOwnedCleanupUnsupported) {
		t.Fatalf("error=%v, want scope refusal\n%s", err, out)
	}
	requireOwnedCommandSnapshot(t, f, before)
}

func TestRemoveBlockedOwnedMCPKeepsOtherScope(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newOwnedCommandFixture(t, "devin", "user")
	repoConfig := filepath.Join(f.repo, ".agents", "config.yaml")
	writeConfig(t, repoConfig, "\nharnesses: [devin]\nregistries:\n  - url: "+f.origin+"\n    ref: main\n    default: true\n")
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed scoped copies: %v\n%s", err, out)
	}
	repoAggregate := filepath.Join(f.repo, ".devin", "mcp_config.json")
	before := ownedCommandRead(t, repoAggregate)
	if !bytes.Contains(before, []byte("remove-binary")) || !bytes.Contains(ownedCommandRead(t, f.aggregate), []byte("remove-binary")) {
		t.Fatalf("both scoped copies must exist: repo=%s user=%s", before, ownedCommandRead(t, f.aggregate))
	}
	rootStore, err := ownership.Open(filepath.Dir(repoAggregate))
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{repoAggregate, repoConfig, filepath.Join(f.repo, ".agents", "lock.yaml"), rootStore.StateFile()}
	retained := map[string][]byte{}
	modTimes := map[string]int64{}
	for _, path := range paths {
		retained[path] = ownedCommandRead(t, path)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		modTimes[path] = info.ModTime().UnixNano()
	}
	f.block(t)
	if out, err := f.remove(false); err != nil {
		t.Fatalf("remove user occurrence: %v\n%s", err, out)
	}
	if bytes.Contains(ownedCommandRead(t, f.aggregate), []byte("remove-binary")) {
		t.Fatal("user target remains")
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !bytes.Equal(retained[path], ownedCommandRead(t, path)) || info.ModTime().UnixNano() != modTimes[path] {
			t.Fatalf("unrelated repo state changed at %s: %v", path, err)
		}
	}
}

func TestRemoveBlockedOwnedMCPLateFailureRetainsTrustWarning(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newOwnedCommandFixture(t, "codex", "repo")
	f.block(t)
	before := ownedCommandSnapshot(t, f.base)
	sentinel := errors.New("late cleanup failure")
	original := commitDependencyTransaction
	defer func() { commitDependencyTransaction = original }()
	calls := 0
	commitDependencyTransaction = func(ctx context.Context, state string, desired []dependencytxn.DesiredFile, options ...dependencytxn.Option) (dependencytxn.Result, error) {
		calls++
		result, err := original(ctx, state, desired, options...)
		if err != nil {
			return result, err
		}
		return result, sentinel
	}
	out, err := f.remove(false)
	if calls != 1 || !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "materialize.codex.trust-review") {
		t.Fatalf("calls=%d error=%v output=%s", calls, err, out)
	}
	if raw := ownedCommandRead(t, f.aggregate); bytes.Contains(raw, []byte("remove-binary")) || !bytes.Contains(raw, []byte("keep-binary")) {
		t.Fatalf("cleanup did not complete: %s", raw)
	}
	if raw := ownedCommandRead(t, f.aggregate); bytes.Contains(raw, []byte("projects")) || bytes.Contains(raw, []byte("trust_level")) {
		t.Fatalf("cleanup introduced trust fields: %s", raw)
	}
	after := ownedCommandSnapshot(t, f.base)
	for path, state := range before {
		if strings.HasSuffix(path, ".gitignore") && after[path] != state {
			t.Errorf("trust-neutral cleanup changed %s", path)
		}
	}
	for path := range after {
		if strings.HasSuffix(path, ".gitignore") {
			if _, ok := before[path]; !ok {
				t.Errorf("trust-neutral cleanup created %s", path)
			}
		}
	}
}
