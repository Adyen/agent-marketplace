package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec: hook-sync-diagnostics/mutation-commands-list-completed-hook-changes
func TestMutationCommandsPrintHookReview(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	registry := t.TempDir()
	hookReviewRegistry(t, registry, "echo mutation-hook-v1")
	repo, nativeFile := hookReviewRepo(t, registry)
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, configPath, strings.Replace(string(configBytes), "[factory]", "[factory, codex]", 1))

	steps := []struct {
		args   []string
		action string
		before string
		after  string
	}{
		{[]string{"install", "PreToolUse-command", "--kind", "hook", "--scope", "repo"}, "add", "", "echo mutation-hook-v1"},
		{[]string{"install", "PreToolUse-command", "--kind", "hook", "--scope", "repo"}, "", "", ""},
		{[]string{"update"}, "replace", "echo mutation-hook-v1", "echo mutation-hook-v2"},
		{[]string{"update"}, "", "", ""},
		{[]string{"remove", "PreToolUse-command", "--kind", "hook", "--scope", "repo"}, "remove", "echo mutation-hook-v2", ""},
	}
	for i, step := range steps {
		if i == 2 {
			hookReviewRegistry(t, registry, "echo mutation-hook-v2")
		}
		output, err := executeCommand(step.args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", step.args, err, output)
		}
		if step.action == "" {
			if strings.Contains(output, "Hook changes") || strings.Contains(output, "materialize.hook.review") || strings.Contains(output, "materialize.codex.trust-review") {
				t.Fatalf("unchanged %v unexpectedly reports a review:\n%s", step.args, output)
			}
			continue
		}
		for _, want := range []string{"Hook changes:", "factory (repository) " + step.action, "materialize.hook.review", "codex (repository) " + step.action, "materialize.codex.trust-review"} {
			if !strings.Contains(output, want) {
				t.Errorf("%v missing %q:\n%s", step.args, want, output)
			}
		}
		for _, side := range []struct{ label, value string }{{"-", step.before}, {"+", step.after}} {
			if side.value != "" && !strings.Contains(output, side.label+` command: "`+side.value+`"`) {
				t.Errorf("%v missing %s command %q:\n%s", step.args, side.label, side.value, output)
			}
		}
		if step.after != "" && hookReviewFileCommand(t, nativeFile) != step.after {
			t.Fatalf("%v did not publish the reviewed command", step.args)
		}
	}
	if data, err := os.ReadFile(nativeFile); err == nil && strings.Contains(string(data), "mutation-hook") {
		t.Fatalf("removed hook remains installed: %s", data)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// spec: hook-sync-diagnostics/mutation-commands-retain-materialization-warnings
func TestMutationCommandsPrintMaterializationWarnings(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	registry, repo := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(registry, ".factory", "skills", "lossy", "SKILL.md"), "---\nname: lossy\ndescription: Tool restriction fixture\nallowed-tools: [Read]\n---\nReview.\n")
	writeFile(t, filepath.Join(registry, ".factory", "skills", "removable", "SKILL.md"), "---\nname: removable\ndescription: Plain fixture\n---\nReview.\n")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  fixture:\n    directory: "+registry+"\n    source_harness: factory\nresources:\n  fixture: {include: [skill/lossy, skill/removable]}\n")
	withWorkingDir(t, repo)
	for _, args := range [][]string{{"install", "lossy", "--scope", "repo"}, {"update"}, {"remove", "removable", "--scope", "repo"}} {
		output, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
		if strings.Count(output, "skill.allowed-tools-omitted") != 1 {
			t.Fatalf("%v did not show the materialization warning once:\n%s", args, output)
		}
		body, err := os.ReadFile(filepath.Join(repo, ".claude", "skills", "lossy", "SKILL.md"))
		if err != nil || strings.Contains(string(body), "allowed-tools") || !strings.Contains(string(body), "Review.") {
			t.Fatalf("warning-only translation did not install its intended content: %v\n%s", err, body)
		}
	}
}

// spec: hook-sync-diagnostics/marketplace-and-collection-installs-retain-hook-review
func TestMarketplaceInstallsPrintPendingHookReview(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, test := range []struct {
		name    string
		server  func(*testing.T) *httptest.Server
		locator string
	}{
		{"resource", func(t *testing.T) *httptest.Server { return newSelfCanonicalV4MarketplaceServer(t, "mp", "review") }, "/r/tools/skill/review"},
		{"collection", func(t *testing.T) *httptest.Server {
			return newV4MarketplaceCollectionServer(t, "mp", "bundle", "review")
		}, "/r/tools/collection/bundle"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			registry := t.TempDir()
			hookReviewRegistry(t, registry, "echo marketplace-install-review")
			_, native := hookReviewRepo(t, registry)
			server := test.server(t)
			defer server.Close()
			output, err := executeCommand("install", server.URL+test.locator, "--scope", "repo", "--yes")
			if err != nil {
				t.Fatalf("Marketplace %s install: %v\n%s", test.name, err, output)
			}
			for _, want := range []string{"Hook changes:", "echo marketplace-install-review", "materialize.hook.review"} {
				if !strings.Contains(output, want) {
					t.Fatalf("Marketplace %s install lost %q:\n%s", test.name, want, output)
				}
			}
			if hookReviewFileCommand(t, native) != "echo marketplace-install-review" {
				t.Fatal("Marketplace install did not publish the reviewed hook")
			}
		})
	}
}

// spec: hook-sync-diagnostics/partial-mutation-failures-retain-completed-review
func TestMutationFailuresPrintPublishedHookReview(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, command := range []string{"install", "update"} {
		t.Run(command, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			registry := t.TempDir()
			hookReviewRegistry(t, registry, "echo partial-review")
			writeFile(t, filepath.Join(registry, ".factory", "droids", "restricted.md"), "---\nname: restricted\ndescription: Read-only fixture\ntools: read-only\n---\nReview.\n")
			repo := t.TempDir()
			configPath := filepath.Join(repo, ".agents", "config.yaml")
			writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory, gemini]\nsources:\n  fixture:\n    directory: "+registry+"\n    source_harness: factory\nresources:\n  fixture: {include: [hook/PreToolUse-command, subagent/restricted]}\n")
			withWorkingDir(t, repo)
			args := []string{command}
			if command == "install" {
				args = append(args, "--all", "--scope", "repo")
			}
			output, err := executeCommand(args...)
			if err == nil || !strings.Contains(err.Error(), "subagent.security-restriction-unsupported") {
				t.Fatalf("%s did not retain the independent materialization failure: %v\n%s", command, err, output)
			}
			for _, want := range []string{"Hook changes:", "echo partial-review", "materialize.hook.review"} {
				if !strings.Contains(output, want) {
					t.Fatalf("failed %s lost completed review %q:\n%s", command, want, output)
				}
			}
			native := filepath.Join(repo, ".factory", "hooks.json")
			if hookReviewFileCommand(t, native) != "echo partial-review" {
				t.Fatal("review did not describe a published hook")
			}
			before, readErr := os.ReadFile(native)
			if readErr != nil {
				t.Fatal(readErr)
			}
			writeConfig(t, configPath, "apiVersion: agent-marketplace/v3\nharnesses: [factory]\n")
			output, err = executeCommand(args...)
			if err == nil || strings.Contains(output, "Hook changes:") || strings.Contains(output, "echo partial-review") {
				t.Fatalf("preflight refusal invented a completed review: %v\n%s", err, output)
			}
			after, readErr := os.ReadFile(native)
			if readErr != nil || string(after) != string(before) {
				t.Fatalf("preflight refusal changed installed bytes: %v", readErr)
			}
		})
	}
}
