package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

func assertCommandFormatRefusal(t *testing.T, err error, code, path string) {
	t.Helper()
	var diagnostic *config.V4ValidationError
	if !errors.As(err, &diagnostic) || diagnostic.Code != code || diagnostic.Path != path || !strings.Contains(diagnostic.Msg, "apiVersion") {
		t.Fatalf("error = %v, want %s at %s apiVersion", err, code, path)
	}
}

func setupV4ConsumerCommand(t *testing.T) (string, string, string) {
	t.Helper()
	root := canonicalTempDir(t)
	home, repo := filepath.Join(root, "home"), filepath.Join(root, "repo")
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	config.SetTestOrganizationConfigPath(t, filepath.Join(root, "missing-org.yaml"))
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repo)
	return root, home, repo
}

func serveConsumerGitRegistry(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	origin, working := createBareRepoWithWorkingClone(t, "format-control.git")
	writeSkill(t, working, "review", "Format control", nil)
	commitAndPushAll(t, working, "Seed format control")
	runGit(t, origin, "update-server-info")
	var requests atomic.Int32
	files := http.FileServer(http.Dir(filepath.Dir(origin)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		files.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/format-control.git", &requests
}

// spec: consumer-config-format/configuration-reads-refuse-without-acquisition
// spec: consumer-config-format/consumers-do-not-offer-in-process-conversion
func TestConsumerReadsRefuseBeforeAdHocAcquisition(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, home, repo := setupV4ConsumerCommand(t)
	url, requests := serveConsumerGitRegistry(t)
	invalid := filepath.Join(home, ".agents", "config.yaml")
	before := "apiVersion: agent-marketplace/v3\nharnesses: [factory]\nregistries: []\n"
	writeFile(t, invalid, before)
	for _, args := range [][]string{{"list"}, {"status"}, {"list", "--from", url, "--ref", "main"}} {
		output, err := executeCommand(args...)
		assertCommandFormatRefusal(t, err, "v4.api-version", invalid)
		if strings.Contains(output, "[Y/n]") || strings.Contains(output, "compatibility reads") {
			t.Fatalf("consumer offered conversion: %s", output)
		}
	}
	intent := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{RepoRoot: repo})
	assertCommandFormatRefusal(t, errors.Join(intent.Errors...), "v4.api-version", invalid)
	if requests.Load() != 0 {
		t.Fatalf("refused reads acquired registry: %d requests", requests.Load())
	}
	if data, err := os.ReadFile(invalid); err != nil || string(data) != before {
		t.Fatalf("read changed unsupported root: %s, %v", data, err)
	}
	// This is a real successful ad-hoc browse over HTTP, proving acquisition
	// and catalog output were reachable rather than hidden by an invalid URL.
	writeFile(t, invalid, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	output, err := executeCommand("list", "--from", url, "--ref", "main")
	if err != nil || !strings.Contains(output, "review") || requests.Load() == 0 {
		t.Fatalf("v4 browse control: %v, requests=%d\n%s", err, requests.Load(), output)
	}
}

// spec: consumer-config-format/configuration-reads-refuse-without-acquisition
// spec: consumer-config-format/explicit-non-v4-fragments-refuse-the-include-tree
func TestConsumerFragmentRefusalPreventsAcquisition(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, _, repo := setupV4ConsumerCommand(t)
	url, requests := serveConsumerGitRegistry(t)
	path := config.ProjectConfigPath(repo)
	fragment := filepath.Join(repo, ".agents", "selection.yaml")
	writeFile(t, path, fmt.Sprintf("apiVersion: agent-marketplace/v4\ninclude: [selection.yaml]\nsources:\n  remote: {git: %s, ref: main}\n", url))
	writeFile(t, fragment, "apiVersion: agent-marketplace/v3\nresources: {remote: [skill/review]}\n")
	for _, args := range [][]string{{"list"}, {"list", "--from", url, "--ref", "main"}, {"sync", "--no-backup"}, {"lock"}} {
		_, err := executeCommand(args...)
		assertCommandFormatRefusal(t, err, "v4.api-version", fragment)
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid fragment triggered acquisition: %d", requests.Load())
	}
	writeFile(t, fragment, "resources: {remote: [skill/review]}\n")
	intent := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{RepoRoot: repo})
	if len(intent.Errors) != 0 || requests.Load() != 0 {
		t.Fatalf("configuration-only control acquired or refused: errors=%v requests=%d", intent.Errors, requests.Load())
	}
	if output, err := executeCommand("lock"); err != nil {
		t.Fatalf("fragment acquisition control: %v\n%s", err, output)
	}
	output, err := executeCommand("list")
	if err != nil || !strings.Contains(output, "review") || requests.Load() == 0 {
		t.Fatalf("fragment selection control: %v, requests=%d\n%s", err, requests.Load(), output)
	}
}

// spec: consumer-config-format/mutations-refuse-before-new-writes-or-prunes
func TestConsumerMutationsRefuseWithoutChangingOwnedOutput(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, home, repo := setupV4ConsumerCommand(t)
	url, requests := serveConsumerGitRegistry(t)
	path := config.ProjectConfigPath(repo)
	valid := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  remote: {git: %s, ref: main}\nresources:\n  remote: [skill/review]\n", url)
	writeFile(t, path, valid)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("seed real owned output: %v\n%s", err, output)
	}
	outputRoot := filepath.Join(repo, ".factory")
	if _, err := os.Stat(filepath.Join(outputRoot, "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("control did not materialize review: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); err != nil || requests.Load() == 0 {
		t.Fatalf("seed control did not acquire and publish lock: %v, requests=%d", err, requests.Load())
	}
	requests.Store(0)
	writeFile(t, path, "apiVersion: agent-marketplace/v2\nregistries: []\n")
	beforeRepo := dependencyTreeSnapshot(t, repo)
	beforeUser := dependencyTreeSnapshot(t, filepath.Join(home, ".agents"))
	for _, args := range [][]string{
		{"install", "review", "--from", url, "--ref", "main"}, {"remove", "review"}, {"init", "--harnesses", "factory"},
		{"lock"}, {"sync", "--no-backup"}, {"sync", "--no-backup", "--prune-stale"}, {"update"}, {"gc"},
	} {
		_, err := executeCommand(args...)
		assertCommandFormatRefusal(t, err, "v4.api-version", path)
		if after := dependencyTreeSnapshot(t, repo); !reflect.DeepEqual(after, beforeRepo) {
			t.Fatalf("%v changed config/lock/harness bytes or metadata", args)
		}
		if after := dependencyTreeSnapshot(t, filepath.Join(home, ".agents")); !reflect.DeepEqual(after, beforeUser) {
			t.Fatalf("%v changed user config or lock", args)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("refused mutation acquired Git registry: %d", requests.Load())
	}
	// Empty supported intent must actually prune the previously owned output.
	writeFile(t, path, "apiVersion: agent-marketplace/v4\n")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("prune control: %v\n%s", err, output)
	}
	if _, err := os.Lstat(filepath.Join(outputRoot, "skills", "review")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("control failed to prune owned review: %v", err)
	}
}

// spec: consumer-config-format/refused-layers-prevent-missing-root-creation
func TestConsumerRefusalPreventsMissingUserSkeleton(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, home, repo := setupV4ConsumerCommand(t)
	user := filepath.Join(home, ".agents", "config.yaml")
	if err := os.Remove(user); err != nil {
		t.Fatal(err)
	}
	path := config.ProjectConfigPath(repo)
	writeFile(t, path, "apiVersion: agent-marketplace/v3\nharnesses: [factory]\n")
	for _, args := range [][]string{{"sync", "--no-backup"}, {"init", "--harnesses", "factory"}, {"install", "review", "--scope", "user", "--from", "https://example.invalid/registry.git", "--ref", "main"}} {
		_, err := executeCommand(args...)
		assertCommandFormatRefusal(t, err, "v4.api-version", path)
		for _, target := range []string{user, filepath.Join(home, ".agents", "lock.yaml")} {
			if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%v created %s: %v", args, target, err)
			}
		}
	}
	writeFile(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("missing-root v4 control: %v\n%s", err, output)
	}
	if _, err := os.Stat(user); err != nil {
		t.Fatalf("valid sync did not create user skeleton: %v", err)
	}
}

func TestV4SourceFlagsRemainUsable(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, _, repo := setupV4ConsumerCommand(t)
	origin, working := createBareRepoWithWorkingClone(t, "source-flags.git")
	writeSkill(t, working, "root-only", "Unrelated root resource", nil)
	writeSkill(t, filepath.Join(working, "component"), "review", "Selected component resource", nil)
	writeSkill(t, filepath.Join(working, "component"), "keep", "Preserved component resource", nil)
	commitAndPushAll(t, working, "Seed source flag control")
	url := "file://" + origin
	output, err := executeCommand("list", "--from", url, "--ref", "main", "--path", "component")
	if err != nil || !strings.Contains(output, "review") || strings.Contains(output, "root-only") {
		t.Fatalf("list source flag control: %v\n%s", err, output)
	}
	for _, name := range []string{"review", "keep"} {
		if output, err := executeCommand("install", "--scope", "repo", name, "--from", url, "--ref", "main", "--path", "component"); err != nil {
			t.Fatalf("install %s source flags: %v\n%s", name, err, output)
		}
	}
	if output, err := executeCommand("remove", "review", "--from", url, "--ref", "main", "--path", "component"); err != nil {
		t.Fatalf("remove source flag control: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "keep", "SKILL.md")); err != nil {
		t.Fatalf("source-specific removal lost keep: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".factory", "skills", "review")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source-specific removal retained review: %v", err)
	}
}

// spec: consumer-config-format/recovery-finishes-admitted-work-before-format-refusal
// spec: consumer-config-format/blocked-recovery-preserves-evidence
func TestConsumerRecoveryPrecedesFormatRefusal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprintf("conflict-%t", conflict), func(t *testing.T) {
			_, _, repo := setupV4ConsumerCommand(t)
			path := config.ProjectConfigPath(repo)
			writeFile(t, path, "apiVersion: agent-marketplace/v4\n")
			state, err := dependencyStateDir()
			if err != nil {
				t.Fatal(err)
			}
			generation, err := dependencytxn.ReadGeneration(path)
			if err != nil {
				t.Fatal(err)
			}
			admitted := []byte("apiVersion: agent-marketplace/v2\n# captured admitted bytes, not a new decoder\n")
			interrupted := errors.New("interrupted admitted transaction")
			_, err = dependencytxn.Commit(context.Background(), state, []dependencytxn.DesiredFile{{Path: path, Expected: generation, Bytes: admitted, Mode: 0o600}}, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
				if point.Stage == dependencytxn.FaultAfterJournal {
					return interrupted
				}
				return nil
			}))
			if !errors.Is(err, interrupted) {
				t.Fatalf("seed journal: %v", err)
			}
			if conflict {
				writeFile(t, path, "apiVersion: agent-marketplace/v3\n# foreign writer\n")
				beforeRepo := dependencyTreeSnapshot(t, repo)
				beforeState := dependencyTreeSnapshot(t, state)
				err = runSyncWithLocks(context.Background(), io.Discard, true, false, false, false, false)
				if !errors.Is(err, dependencytxn.ErrRecoveryBlocked) {
					t.Fatalf("recovery must preserve foreign generation: %v", err)
				}
				if !reflect.DeepEqual(dependencyTreeSnapshot(t, repo), beforeRepo) || !reflect.DeepEqual(dependencyTreeSnapshot(t, state), beforeState) {
					t.Fatal("blocked recovery changed foreign state or journal evidence")
				}
				return
			}
			err = runSyncWithLocks(context.Background(), io.Discard, true, false, false, false, false)
			assertCommandFormatRefusal(t, err, "v4.api-version", path)
			if data, err := os.ReadFile(path); err != nil || string(data) != string(admitted) {
				t.Fatalf("admitted recovery did not finish: %s, %v", data, err)
			}
			if err := dependencytxn.CheckPending(state); err != nil {
				t.Fatalf("recovered journal remains pending: %v", err)
			}
			if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("fresh lock work began after format refusal: %v", err)
			}
		})
	}
}
