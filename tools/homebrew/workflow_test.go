package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func loadCI(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func mapOf(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%q is not a map", key)
	}
	return v
}

func jobSteps(t *testing.T, job map[string]any) []map[string]any {
	t.Helper()
	raw, ok := job["steps"].([]any)
	if !ok {
		t.Fatal("job has no steps")
	}
	var out []map[string]any
	for _, s := range raw {
		m, ok := s.(map[string]any)
		if !ok {
			t.Fatal("step is not a map")
		}
		out = append(out, m)
	}
	return out
}

func runScript(t *testing.T, steps []map[string]any, names ...string) string {
	t.Helper()
	for _, name := range names {
		for _, s := range steps {
			if s["name"] == name {
				run, ok := s["run"].(string)
				if !ok {
					t.Fatalf("step %q has no run", name)
				}
				return run
			}
		}
		t.Fatalf("step %q not found", name)
	}
	return ""
}

// spec: homebrew-packaging/successful-stable-release-publishes-after-binary-upload
func TestHomebrewJobPublishesAfterRelease(t *testing.T) {
	doc := loadCI(t)
	jobs := mapOf(t, doc, "jobs")
	job := mapOf(t, jobs, "homebrew")

	needs, _ := job["needs"].([]any)
	var needNames []string
	for _, n := range needs {
		needNames = append(needNames, n.(string))
	}
	for _, want := range []string{"release", "classify-release-tag"} {
		if !slices.Contains(needNames, want) {
			t.Errorf("homebrew needs = %v, missing %s", needNames, want)
		}
	}
	if got := job["if"]; got != "needs.classify-release-tag.outputs.stable == 'true'" {
		t.Errorf("homebrew if = %v, want stable classifier gate", got)
	}
	perms := mapOf(t, job, "permissions")
	if perms["contents"] != "write" {
		t.Errorf("homebrew permissions = %v, want contents: write", perms)
	}
	conc := mapOf(t, job, "concurrency")
	if conc["cancel-in-progress"] != false {
		t.Errorf("homebrew concurrency cancel-in-progress = %v, want false", conc)
	}
	if group, _ := conc["group"].(string); group == "" || strings.Contains(group, "${{") {
		t.Errorf("homebrew concurrency group = %v, want a fixed name", conc["group"])
	}
	if perms, ok := doc["permissions"].(map[string]any); !ok || len(perms) != 0 {
		t.Errorf("workflow-level permissions changed: %v", doc["permissions"])
	}
	release := mapOf(t, jobs, "release")
	if p := mapOf(t, release, "permissions"); p["contents"] != "write" {
		t.Errorf("release job permissions changed: %v", p)
	}
	foundUpload := false
	for _, s := range jobSteps(t, release) {
		if s["name"] == "Upload release binaries" {
			foundUpload = true
			if uses, _ := s["uses"].(string); !strings.Contains(uses, "action-gh-release@") {
				t.Errorf("release upload step changed: %v", s)
			}
		}
	}
	if !foundUpload {
		t.Error("release binary upload step missing")
	}
	steps := jobSteps(t, job)
	var order []string
	for _, s := range steps {
		order = append(order, s["name"].(string))
	}
	dl := slices.Index(order, "Download release binaries")
	gen := slices.Index(order, "Generate formula")
	pub := slices.Index(order, "Publish formula")
	if dl < 0 || gen < 0 || pub < 0 || dl >= gen || gen >= pub {
		t.Errorf("step order = %v, want download < generate < publish", order)
	}
	if checkout, ok := steps[0]["uses"].(string); !ok || !strings.HasPrefix(checkout, "actions/checkout@") {
		t.Errorf("first step = %v, want checkout", steps[0])
	} else if with := mapOf(t, steps[0], "with"); with["persist-credentials"] != false {
		t.Errorf("checkout must use persist-credentials: false, got %v", with)
	}
	if setup, ok := steps[1]["uses"].(string); !ok || !strings.HasPrefix(setup, "actions/setup-go@") {
		t.Errorf("second step = %v, want actions/setup-go", steps[1])
	} else if with := mapOf(t, steps[1], "with"); with["cache"] != false {
		t.Errorf("homebrew setup-go must disable the module cache, got %v", with)
	}
	for _, s := range steps {
		if run, ok := s["run"].(string); ok {
			if strings.Contains(run, "${{") {
				t.Errorf("step %q interpolates expressions into shell: %s", s["name"], run)
			}
			if !strings.Contains(run, "set -euo pipefail") {
				t.Errorf("step %q missing set -euo pipefail", s["name"])
			}
		}
	}
	publish := runScript(t, steps, "Publish formula")
	if !strings.Contains(publish, "tools/homebrew publish") {
		t.Errorf("publish step does not invoke the publisher: %s", publish)
	}
	for _, s := range steps {
		if run, ok := s["run"].(string); ok && (strings.Contains(run, "git push") ||
			strings.Contains(run, "brew tap") || strings.Contains(run, "brew install")) {
			t.Errorf("step %q writes a source branch or uses host brew: %s", s["name"], run)
		}
	}
}

// spec: homebrew-packaging/successful-stable-release-publishes-after-binary-upload
func TestHomebrewStagingUsesRunnerEnvironment(t *testing.T) {
	doc := loadCI(t)
	jobs := mapOf(t, doc, "jobs")
	job := mapOf(t, jobs, "homebrew")
	for k, v := range mapOf(t, job, "env") {
		if s, _ := v.(string); strings.Contains(s, "runner.") {
			t.Errorf("job env %s references the runner context, unavailable in jobs.<id>.env: %q", k, s)
		}
	}
	steps := jobSteps(t, job)
	var order []string
	for _, s := range steps {
		order = append(order, s["name"].(string))
	}
	prep := slices.Index(order, "Prepare staging directory")
	if prep < 0 {
		t.Fatalf("no staging preparation step: %v", order)
	}
	for _, later := range []string{"Fetch release metadata", "Download release binaries", "Generate formula", "Publish formula"} {
		if idx := slices.Index(order, later); idx < prep {
			t.Errorf("step %q must run after staging preparation: %v", later, order)
		}
	}
	run := runScript(t, steps, "Prepare staging directory")
	runnerTemp := filepath.Join(t.TempDir(), "runner-temp")
	if err := os.MkdirAll(runnerTemp, 0o755); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(t.TempDir(), "github_env")
	cmd := exec.Command("bash", "-c", run)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "RUNNER_TEMP=" + runnerTemp, "GITHUB_ENV=" + envFile}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("staging preparation failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if want := "STAGING=" + runnerTemp + "/homebrew\n"; string(data) != want {
		t.Errorf("GITHUB_ENV = %q, want %q", data, want)
	}
}

// spec: homebrew-packaging/non-stable-or-failed-release-does-not-publish
func TestHomebrewJobSkipsNonStableOrFailed(t *testing.T) {
	doc := loadCI(t)
	jobs := mapOf(t, doc, "jobs")
	job := mapOf(t, jobs, "homebrew")
	if got := job["if"]; got != "needs.classify-release-tag.outputs.stable == 'true'" {
		t.Fatalf("non-stable gating missing: %v", got)
	}
	if got, _ := job["if"].(string); strings.Contains(got, "always()") {
		t.Error("homebrew job must not use always()")
	}
	release := mapOf(t, jobs, "release")
	if got := release["if"]; got != "needs.classify-release-tag.outputs.stable == 'true'" {
		t.Errorf("release gate changed: %v", got)
	}
	steps := jobSteps(t, job)
	dl := runScript(t, steps, "Download release binaries")
	gen := runScript(t, steps, "Generate formula")
	fake := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	failGH := "#!/bin/sh\necho 'gh: boom (HTTP 500)' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(fake, "gh"), []byte(failGH), 0o755); err != nil {
		t.Fatal(err)
	}
	staging := t.TempDir()
	cmd := exec.Command("bash", "-c", dl)
	cmd.Env = []string{"PATH=" + fake + ":/usr/bin:/bin", "STAGING=" + staging,
		"REPOSITORY=o/r", "TAG=v1.2.3", "GH_TOKEN=x"}
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("download script passed despite gh failure:\n%s", out)
	}
	if entries, _ := os.ReadDir(staging); len(entries) != 0 {
		t.Errorf("failed download left staged artifacts: %v", entries)
	}
	if err := os.WriteFile(filepath.Join(staging, "visibility"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	genCmd := exec.Command("bash", "-c", gen)
	genLog := filepath.Join(t.TempDir(), "gen.log")
	fakeGo := "#!/bin/sh\necho \"$@\" >> \"$GEN_LOG\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(fake, "go"), []byte(fakeGo), 0o755); err != nil {
		t.Fatal(err)
	}
	genCmd.Env = []string{"PATH=" + fake + ":/usr/bin:/bin", "STAGING=" + staging,
		"REPOSITORY=o/r", "TAG=v1.2.3", "GH_TOKEN=x", "GEN_LOG=" + genLog}
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate script failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(genLog)
	if err != nil {
		t.Fatalf("generate step did not invoke go run: %v", err)
	}
	got := string(data)
	for _, want := range []string{"tools/homebrew", "generate", "--repository", "o/r", "--tag", "v1.2.3", "--visibility", "private"} {
		if !strings.Contains(got, want) {
			t.Errorf("generate invocation missing %q: %s", want, got)
		}
	}
}
