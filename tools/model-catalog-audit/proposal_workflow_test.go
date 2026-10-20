package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// spec: model-catalog-audit/nightly-proposals-require-catalog-changes
func TestNightlyProposalRequiresCatalogChanges(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/harness-images.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct{ Run string } `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	var script string
	for _, step := range workflow.Jobs["model-catalog-drift-pr"].Steps {
		if strings.Contains(step.Run, "./tools/model-catalog-audit propose") {
			script = step.Run
		}
	}
	if script == "" {
		t.Fatal("proposal workflow script missing")
	}
	bin := t.TempDir()
	tool := filepath.Join(bin, "audit")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", tool, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build audit tool: %v\n%s", err, output)
	}
	for name, content := range map[string]string{
		"go": `#!/bin/sh
set -eu
case "$1" in
  run) "$AUDIT_TOOL" propose
       if [ "$INJECT_BASELINE_EDIT" = 1 ]; then
         printf '\n' >> tools/model-catalog-audit/baselines.json
       fi ;;
  test) exit 0 ;;
  *) exit 1 ;;
esac
`,
		"gh": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$GH_CALLS\"\nif [ \"$1 $2\" = 'pr list' ]; then printf '%s' \"$EXISTING_PR\"; fi\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const claudeCatalog = "schema: 1\nharness: claude\nsurfaces:\n  subagent:\n    model_field: model\nmodels:\n  anthropic/claude-opus-5:\n    model: claude-opus-5\n"
	for _, tt := range []struct {
		name         string
		domain       string
		model        string
		baselineEdit string
		existingPR   string
		wantPR       bool
	}{
		{name: "model documentation"},
		{name: "hook documentation", domain: "hookpolicy"},
		{name: "tool documentation", domain: "toolpolicy"},
		{name: "manual candidate and evidence drift", model: "gemma-4-31b-it"},
		{name: "baseline-only defense", baselineEdit: "1"},
		{name: "existing hash-only PR is left untouched", existingPR: "797"},
		{name: "actual catalog addition", model: "claude-sonnet-5-5", wantPR: true},
		{name: "existing PR gets catalog addition", model: "claude-sonnet-5-5", existingPR: "797", wantPR: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := writeProposeTree(t, map[string]string{"claude": claudeCatalog})
			before := readProposeFile(t, root, baselinesPath)
			temp := t.TempDir()
			calls := filepath.Join(temp, "gh-calls")
			summary := filepath.Join(temp, "summary")
			findings := []finding{{Code: "source.evidence-changed"}}
			if tt.model != "" {
				findings = append(findings, finding{Code: "catalog.model-missing", Model: tt.model})
			}
			r := proposeReport(sourceReport{Name: "claude-models", Harness: "claude", CatalogDomain: tt.domain, URL: proposeSourceURL, Status: "drift", ObservedSHA256: digest([]byte("changed evidence")), Findings: findings})
			if err := os.WriteFile(filepath.Join(temp, "report.json"), []byte(mustJSON(t, r)), 0o644); err != nil {
				t.Fatal(err)
			}
			env := []string{}
			for _, value := range os.Environ() {
				if !strings.HasPrefix(value, "GIT_") {
					env = append(env, value)
				}
			}
			env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
				"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=core.hooksPath", "GIT_CONFIG_VALUE_0=/dev/null",
				"GIT_CONFIG_KEY_1=commit.gpgsign", "GIT_CONFIG_VALUE_1=false",
				"GIT_AUTHOR_NAME=github-actions[bot]", "GIT_COMMITTER_NAME=github-actions[bot]",
				"GIT_AUTHOR_EMAIL=41898282+github-actions[bot]@users.noreply.github.com",
				"GIT_COMMITTER_EMAIL=41898282+github-actions[bot]@users.noreply.github.com",
				"AUDIT_TOOL="+tool, "INJECT_BASELINE_EDIT="+tt.baselineEdit, "GH_CALLS="+calls, "EXISTING_PR="+tt.existingPR,
				"RUNNER_TEMP="+temp, "REPORT_DIR="+temp, "GITHUB_STEP_SUMMARY="+summary,
				"DEFAULT_BRANCH=main", "RUN_ID=123")
			run := func(command string, args ...string) string {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				cmd := exec.CommandContext(ctx, command, args...)
				cmd.Dir, cmd.Env = root, env
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s %v: %v\n%s", command, args, err, output)
				}
				return string(output)
			}
			remote := filepath.Join(temp, "remote.git")
			run("git", "init", "--bare", remote)
			run("git", "init", "-b", "main")
			run("git", "add", ".")
			run("git", "commit", "-m", "fixture")
			run("git", "remote", "add", "origin", remote)
			if tt.existingPR != "" {
				run("git", "push", "origin", "HEAD:refs/heads/automation/model-catalog-drift")
			}
			remoteBefore := run("git", "ls-remote", "--heads", "origin", "automation/model-catalog-drift")
			run("bash", "-c", script)
			ghCalls, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(ghCalls), "pr create "); got != (tt.wantPR && tt.existingPR == "") {
				t.Fatalf("created PR = %t, want %t:\n%s", got, tt.wantPR, ghCalls)
			}
			if got := strings.Contains(string(ghCalls), "pr edit "); got != (tt.wantPR && tt.existingPR != "") {
				t.Fatalf("edited PR = %t, want %t:\n%s", got, tt.wantPR, ghCalls)
			}
			if strings.Contains(string(ghCalls), "pr close ") {
				t.Fatal("proposal must not automatically close an existing PR")
			}
			published := run("git", "ls-remote", "--heads", "origin", "automation/model-catalog-drift") != remoteBefore
			if published != tt.wantPR {
				t.Fatalf("published branch = %t, want %t", published, tt.wantPR)
			}
			if tt.baselineEdit == "" && readProposeFile(t, root, baselinesPath) != before {
				t.Fatal("workflow changed reviewed baselines")
			}
			if tt.wantPR {
				if got := run("git", "show", "--format=", "--name-only", "HEAD"); strings.TrimSpace(got) != catalogPath("claude") {
					t.Fatalf("proposal committed unexpected files:\n%s", got)
				}
			} else {
				alert, err := os.ReadFile(summary)
				if err != nil || !strings.Contains(string(alert), "Evidence drift requiring review") || !strings.Contains(string(alert), "No catalog changes proposed") {
					t.Fatalf("missing evidence-only alert: %v\n%s", err, alert)
				}
				if readProposeFile(t, root, catalogPath("claude")) != claudeCatalog {
					t.Fatal("non-actionable proposal edited the catalog")
				}
			}
		})
	}
}
