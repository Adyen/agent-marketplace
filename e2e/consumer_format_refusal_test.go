package e2e_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// These launch witnesses replace successful pre-v4 upgrade execution. They use
// the real CLI and retain evidence that rejection precedes acquisition/pruning.
var consumerRootPaths = []string{
	"/etc/agent-marketplace/config.yaml",
	"/root/.agents/config.yaml",
	"/workspace/.agents/config.yaml",
	"/workspace/.agents/config.local.yaml",
}

var unsupportedConsumerVersions = []struct {
	name, field, code string
}{
	{"v1", "apiVersion: agent-marketplace/v1\n", "v4.api-version"},
	{"v2", "apiVersion: agent-marketplace/v2\n", "v4.api-version"},
	{"v3", "apiVersion: agent-marketplace/v3\n", "v4.api-version"},
	{"unknown", "apiVersion: agent-marketplace/v99\n", "v4.api-version"},
	{"omitted", "", "v4.api-version-required"},
	{"empty", "apiVersion: ''\n", "v4.api-version"},
	{"null", "apiVersion: null\n", "v4.api-version"},
	{"integer", "apiVersion: 4\n", "v4.api-version"},
	{"boolean", "apiVersion: true\n", "v4.api-version"},
	{"sequence", "apiVersion: [agent-marketplace/v4]\n", "v4.api-version"},
	{"mapping", "apiVersion: {version: agent-marketplace/v4}\n", "v4.api-version"},
	{"whitespace", "apiVersion: ' agent-marketplace/v4 '\n", "v4.api-version"},
	{"case", "apiVersion: Agent-Marketplace/v4\n", "v4.api-version"},
}

func consumerAcquisitionSentinel(t *testing.T) (string, *atomic.Int64) {
	t.Helper()
	count := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		http.Error(w, "acquisition sentinel must not be reached", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/registry.git", count
}

func seedV4ConsumerInstallation(t *testing.T) (*Env, map[string]string) {
	t.Helper()
	env := newEnv(t)
	env.InitGitRepo()
	url := env.CreateBareRepo("format-controls", SkillsOnly(Skill{Name: "owned", Description: "Owned format-control resource"}, Skill{Name: "extra"}))
	configs := map[string]string{}
	for i, path := range consumerRootPaths {
		body := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {team: {git: %q, ref: main}}\n", url)
		if i < 3 {
			body += "harnesses: [factory]\n"
		}
		if i == 1 || i == 2 {
			body += "materialization: {mode: file}\n"
		}
		if i == 2 {
			body += "resources: {team: {include: [skill/owned]}}\n"
		}
		env.WriteFile(path, body)
		configs[path] = body
	}
	env.MustRunAM("sync")
	env.AssertFileContains("/workspace/.factory/skills/owned/SKILL.md", "Owned format-control resource")
	env.MustRunAM("sync", "--check")
	return env, configs
}

func snapshotConsumerEvidence(t *testing.T, env *Env) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, path := range []string{
		"/etc/agent-marketplace", "/root/.agents", "/workspace/.agents", "/workspace/.factory",
	} {
		actual := env.mustResolvePath(path)
		if _, err := os.Lstat(actual); os.IsNotExist(err) {
			result[path] = "absent"
			continue
		}
		fingerprint, err := pathFingerprint(actual)
		if err != nil {
			t.Fatal(err)
		}
		result[path] = fingerprint
	}
	return result
}

func assertConsumerEvidence(t *testing.T, env *Env, before map[string]string) {
	t.Helper()
	after := snapshotConsumerEvidence(t, env)
	for path, want := range before {
		if after[path] != want {
			t.Fatalf("refused operation changed config/lock/owned output bytes, mode, mtime, or staging state at %s", path)
		}
	}
}

func assertConsumerFormatRefusal(t *testing.T, env *Env, result ExecResult, path, code string) {
	t.Helper()
	if result.ExitCode == 0 || !strings.Contains(result.Combined, code) || !strings.Contains(result.Combined, env.mustResolvePath(path)) || !strings.Contains(result.Combined, "apiVersion") {
		t.Fatalf("want nonzero %s at %s apiVersion, got exit %d:\n%s", code, path, result.ExitCode, result.Combined)
	}
}

// spec: regression-gates/unsupported-roots-are-witnessed-by-the-launch-gate
func TestUnsupportedConsumerRootsRefuseBeforeWork(t *testing.T) {
	for layer, path := range consumerRootPaths {
		for _, version := range unsupportedConsumerVersions {
			t.Run(fmt.Sprintf("layer-%d/%s", layer, version.name), func(t *testing.T) {
				t.Parallel()
				env, _ := seedV4ConsumerInstallation(t)
				url, count := consumerAcquisitionSentinel(t)
				body := version.field + fmt.Sprintf("sources: {remote: {git: %q, ref: main}}\n", url)
				// Omitted roots cannot inherit their version from a valid fragment.
				fragment := filepath.Join(filepath.Dir(path), "version-control.yaml")
				env.WriteFile(fragment, "apiVersion: agent-marketplace/v4\n")
				body += "include: [version-control.yaml]\n"
				env.WriteFile(path, body)
				before := snapshotConsumerEvidence(t, env)
				for _, args := range [][]string{{"list"}, {"sync"}, {"update"}, {"install", "extra", "--from", url, "--scope", "repo"}} {
					assertConsumerFormatRefusal(t, env, env.RunAM(args...), path, version.code)
					assertConsumerEvidence(t, env, before)
				}
				if count.Load() != 0 {
					t.Fatalf("unsupported root made %d acquisition requests", count.Load())
				}
				// Non-vacuity: exact v4 reaches the same acquisition sentinel.
				env.WriteFile(path, "apiVersion: agent-marketplace/v4\n"+strings.TrimPrefix(body, version.field))
				result := env.RunAM("install", "extra", "--from", url, "--scope", "repo")
				if count.Load() == 0 || strings.Contains(result.Combined, "v4.api-version") {
					t.Fatalf("exact-v4 control did not reach acquisition: count=%d\n%s", count.Load(), result.Combined)
				}
			})
		}
	}
}

// spec: regression-gates/unsupported-roots-are-witnessed-by-the-launch-gate
func TestUnsupportedConsumerIncludesRefuseBeforeWork(t *testing.T) {
	for layer, path := range consumerRootPaths {
		for _, nested := range []bool{false, true} {
			for _, version := range unsupportedConsumerVersions {
				if version.name == "omitted" { // Omitted fragment versions are supported.
					continue
				}
				t.Run(fmt.Sprintf("layer-%d/nested-%t/%s", layer, nested, version.name), func(t *testing.T) {
					t.Parallel()
					env, configs := seedV4ConsumerInstallation(t)
					url, count := consumerAcquisitionSentinel(t)
					fragment := filepath.Join(filepath.Dir(path), "bad-fragment.yaml")
					if nested {
						fragment = filepath.Join(filepath.Dir(path), "nested", "bad-fragment.yaml")
						env.WriteFile(filepath.Join(filepath.Dir(path), "parent.yaml"), "include: [nested/bad-fragment.yaml]\n")
						env.WriteFile(path, configs[path]+"include: [parent.yaml]\n")
					} else {
						env.WriteFile(path, configs[path]+"include: [bad-fragment.yaml]\n")
					}
					env.WriteFile(fragment, version.field+fmt.Sprintf("sources: {remote: {git: %q, ref: main}}\nresources: {remote: {include: [skill/extra]}}\n", url))
					before := snapshotConsumerEvidence(t, env)
					for _, args := range [][]string{{"list"}, {"sync"}, {"install", "extra", "--from", url, "--scope", "repo"}} {
						assertConsumerFormatRefusal(t, env, env.RunAM(args...), fragment, "v4.api-version")
						assertConsumerEvidence(t, env, before)
					}
					if count.Load() != 0 {
						t.Fatalf("unsupported include made %d acquisition requests", count.Load())
					}
				})
			}
		}
		// Positive controls declare the selection only in the fragment.
		for _, field := range []string{"", "apiVersion: agent-marketplace/v4\n"} {
			t.Run(fmt.Sprintf("layer-%d/positive-%t", layer, field != ""), func(t *testing.T) {
				env, configs := seedV4ConsumerInstallation(t)
				env.WriteFile(path, strings.Split(configs[path], "resources:")[0]+"include: [positive.yaml]\n")
				env.WriteFile(filepath.Join(filepath.Dir(path), "positive.yaml"), field+"resources: {team: {include: [skill/extra]}}\n")
				env.MustRunAM("sync")
				output := "/workspace/.factory/skills/extra/SKILL.md"
				if layer < 2 {
					output = "/root/.factory/skills/extra/SKILL.md"
				}
				env.AssertFileContains(output, "name: extra")
			})
		}
	}
}

func TestUnsupportedConsumerConfigPreventsMissingDestinations(t *testing.T) {
	for layer, badPath := range consumerRootPaths {
		for _, target := range []struct{ scope, root, lock string }{
			{"user", consumerRootPaths[1], "/root/.agents/lock.yaml"},
			{"repo", consumerRootPaths[2], "/workspace/.agents/lock.yaml"},
			{"local", consumerRootPaths[3], "/workspace/.agents/lock.local.yaml"},
		} {
			if badPath == target.root {
				continue
			}
			t.Run(fmt.Sprintf("layer-%d/to-%s", layer, target.scope), func(t *testing.T) {
				env := newEnv(t)
				env.InitGitRepo()
				for _, path := range consumerRootPaths {
					if path != target.root {
						env.WriteFile(path, "apiVersion: agent-marketplace/v4\n")
					}
				}
				env.WriteFile(badPath, "apiVersion: agent-marketplace/v3\n")
				url, count := consumerAcquisitionSentinel(t)
				before := snapshotConsumerEvidence(t, env)
				result := env.RunAM("install", "extra", "--from", url, "--ref", "main", "--scope", target.scope)
				assertConsumerFormatRefusal(t, env, result, badPath, "v4.api-version")
				assertConsumerEvidence(t, env, before)
				env.AssertFileAbsent(target.root)
				env.AssertFileAbsent(target.lock)
				env.AssertFileAbsent("/workspace/.factory")
				if count.Load() != 0 {
					t.Fatalf("missing destination refusal fetched %d requests", count.Load())
				}
			})
		}
	}
}
