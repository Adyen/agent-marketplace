package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func loadWorkflow(t *testing.T, name string) map[string]any {
	t.Helper()
	p := filepath.Join("..", "..", ".github", "workflows", name)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", p, err)
	}
	return doc
}

func loadCIWorkflow(t *testing.T) map[string]any {
	return loadWorkflow(t, "ci.yml")
}

func getMap(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%q is not a map", key)
	}
	return v
}

func getSlice(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	v, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%q is not a slice", key)
	}
	return v
}

func toStringSlice(t *testing.T, v []any) []string {
	t.Helper()
	out := make([]string, len(v))
	for i, e := range v {
		s, ok := e.(string)
		if !ok {
			t.Fatalf("element %d is not a string", i)
		}
		out[i] = s
	}
	return out
}

func stepMap(t *testing.T, step any) map[string]any {
	t.Helper()
	m, ok := step.(map[string]any)
	if !ok {
		t.Fatalf("step is not a map: %T", step)
	}
	return m
}

func TestTUIQADriverJob(t *testing.T) {
	doc := loadCIWorkflow(t)

	jobs := getMap(t, doc, "jobs")
	driver := getMap(t, jobs, "tui-qa-required")

	if got := driver["name"]; got != "TUI QA Driver" {
		t.Errorf("name = %v, want %q", got, "TUI QA Driver")
	}

	needs := toStringSlice(t, getSlice(t, driver, "needs"))
	if !slices.Equal(needs, []string{"changes", "tui-qa"}) {
		t.Errorf("needs = %v, want [changes tui-qa]", needs)
	}

	if got := driver["if"]; got != "always()" {
		t.Errorf("if = %v, want %q", got, "always()")
	}

	if got := driver["runs-on"]; got != "ubuntu-latest" {
		t.Errorf("runs-on = %v, want %q", got, "ubuntu-latest")
	}

	if got := driver["timeout-minutes"]; got != 1 {
		t.Errorf("timeout-minutes = %v, want %d", got, 1)
	}

	if perms, ok := driver["permissions"].(map[string]any); !ok || len(perms) != 0 {
		t.Errorf("permissions must be empty; got %v", driver["permissions"])
	}

	steps := getSlice(t, driver, "steps")
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	step := stepMap(t, steps[0])
	if step["name"] != "Require all TUI QA platforms" {
		t.Errorf("step name = %v, want %q", step["name"], "Require all TUI QA platforms")
	}

	env := getMap(t, step, "env")
	wantBindings := map[string]string{
		"CHANGES_RESULT": "${{ needs.changes.result }}",
		"RUN_TUI_QA":     "${{ needs.changes.outputs.tui_qa }}",
		"TUI_QA_RESULT":  "${{ needs.tui-qa.result }}",
	}
	for k, want := range wantBindings {
		if got := env[k]; got != want {
			t.Errorf("env[%s] = %v, want %q", k, got, want)
		}
	}

	if _, ok := step["run"].(string); !ok {
		t.Fatalf("step run is not a string: %v", step["run"])
	}
}

func TestTUIQADriverShell(t *testing.T) {
	doc := loadCIWorkflow(t)

	jobs := getMap(t, doc, "jobs")
	driver := getMap(t, jobs, "tui-qa-required")
	step := stepMap(t, getSlice(t, driver, "steps")[0])
	run := step["run"].(string)

	cases := []struct {
		name          string
		changesResult string
		runTUIQA      string
		tuiQAResult   string
		wantPass      bool
	}{
		{"relevant+success", "success", "true", "success", true},
		{"unrelated+skipped", "success", "false", "skipped", true},
		{"planner+failure", "failure", "false", "skipped", false},
		{"empty+invalid", "success", "", "success", false},
		{"relevant+skipped", "success", "true", "skipped", false},
		{"unrelated+success", "success", "false", "success", false},
		{"upstream+failure", "success", "true", "failure", false},
		{"cancellation", "cancelled", "false", "cancelled", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-c", run)
			cmd.Env = []string{
				"CHANGES_RESULT=" + tc.changesResult,
				"RUN_TUI_QA=" + tc.runTUIQA,
				"TUI_QA_RESULT=" + tc.tuiQAResult,
			}
			err := cmd.Run()
			if tc.wantPass && err != nil {
				t.Fatalf("expected pass, got: %v", err)
			}
			if !tc.wantPass && err == nil {
				t.Fatalf("expected failure, got pass")
			}
		})
	}
}

// spec: regression-gates/nightly-budget-accommodates-all-repetitions
func TestTUIQAStressBudget(t *testing.T) {
	doc := loadWorkflow(t, "tui-qa-stress.yml")
	on := getMap(t, doc, "on")
	if len(getSlice(t, on, "schedule")) == 0 {
		t.Fatal("nightly stress must retain its schedule")
	}
	if _, ok := on["workflow_dispatch"]; !ok {
		t.Fatal("nightly stress must allow manual verification")
	}
	stress := getMap(t, getMap(t, doc, "jobs"), "stress")
	// The measured suite takes about 38 minutes for 20 iterations. Include
	// setup and runner headroom without weakening any journey deadline.
	if got := stress["timeout-minutes"]; got != 60 {
		t.Errorf("nightly deadline = %v minutes, want 60 for the measured 20-iteration workload", got)
	}
	run := findStepRun(t, doc, "stress", "Run complete journey suite 20 times")
	for _, required := range []string{"set -euo pipefail", "seq 1 20", "pnpm qa:compiled"} {
		if !strings.Contains(run, required) {
			t.Errorf("nightly stress must retain %q:\n%s", required, run)
		}
	}
	for _, tc := range []struct {
		name, failAt, wantCount string
		wantPass                bool
	}{
		{"all iterations execute", "0", "20", true},
		{"journey failure stops the job", "7", "7", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			countFile := filepath.Join(dir, "count")
			if err := os.WriteFile(countFile, []byte("0"), 0o600); err != nil {
				t.Fatal(err)
			}
			const pnpm = "#!/bin/sh\n[ \"$1\" = qa:compiled ] || exit 41\nn=$(cat \"$COUNT_FILE\")\nn=$((n + 1))\nprintf '%s' \"$n\" > \"$COUNT_FILE\"\n[ \"$n\" -ne \"$FAIL_AT\" ]\n"
			if err := os.WriteFile(filepath.Join(dir, "pnpm"), []byte(pnpm), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", run)
			cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "COUNT_FILE=" + countFile, "FAIL_AT=" + tc.failAt}
			if out, err := cmd.CombinedOutput(); (err == nil) != tc.wantPass {
				t.Fatalf("pass = %v, want %v; err %v, output:\n%s", err == nil, tc.wantPass, err, out)
			}
			got, err := os.ReadFile(countFile)
			if err != nil || string(got) != tc.wantCount {
				t.Fatalf("iterations = %q, want %s: %v", got, tc.wantCount, err)
			}
		})
	}
}

// E2E Gates is the single required context for the Marketplace, pre-v4
// upgrade, and released-client gates, which share one planner output.
func TestE2EGatesDriver(t *testing.T) {
	jobs := getMap(t, loadCIWorkflow(t), "jobs")
	driver := getMap(t, jobs, "e2e-gates-required")
	gates := []string{"marketplace-gate", "upgrade-gate", "released-client-gate"}
	if driver["name"] != "E2E Gates" || driver["if"] != "always()" || !slices.Equal(toStringSlice(t, getSlice(t, driver, "needs")), append([]string{"changes"}, gates...)) {
		t.Fatalf("E2E Gates driver = name %v, if %v, needs %v", driver["name"], driver["if"], driver["needs"])
	}
	if perms, ok := driver["permissions"].(map[string]any); !ok || len(perms) != 0 {
		t.Errorf("permissions must be empty; got %v", driver["permissions"])
	}
	for _, gate := range gates {
		if cond := getMap(t, jobs, gate)["if"]; cond != "needs.changes.outputs.marketplace_e2e == 'true'" {
			t.Errorf("%s runs on %v, not the E2E planner output the driver checks", gate, cond)
		}
	}
	steps := getSlice(t, driver, "steps")
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	step := stepMap(t, steps[0])
	env := getMap(t, step, "env")
	for key, want := range map[string]string{
		"CHANGES_RESULT":         "${{ needs.changes.result }}",
		"RUN_E2E":                "${{ needs.changes.outputs.marketplace_e2e }}",
		"MARKETPLACE_RESULT":     "${{ needs.marketplace-gate.result }}",
		"UPGRADE_RESULT":         "${{ needs.upgrade-gate.result }}",
		"RELEASED_CLIENT_RESULT": "${{ needs.released-client-gate.result }}",
	} {
		if env[key] != want {
			t.Errorf("env[%s] = %v, want %q", key, env[key], want)
		}
	}
	run, ok := step["run"].(string)
	if !ok {
		t.Fatalf("step run is not a string: %v", step["run"])
	}
	for _, tc := range []struct {
		name                                     string
		changes, planned, market, upgrade, older string
		wantPass                                 bool
	}{
		{"relevant+success", "success", "true", "success", "success", "success", true},
		{"unrelated+skipped", "success", "false", "skipped", "skipped", "skipped", true},
		{"one gate failed", "success", "true", "success", "failure", "success", false},
		{"one gate skipped when planned", "success", "true", "success", "success", "skipped", false},
		{"gate ran when unplanned", "success", "false", "skipped", "success", "skipped", false},
		{"planner failed", "failure", "false", "skipped", "skipped", "skipped", false},
		{"empty plan", "success", "", "success", "success", "success", false},
		{"cancelled", "success", "true", "cancelled", "success", "success", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-c", run)
			cmd.Env = []string{"CHANGES_RESULT=" + tc.changes, "RUN_E2E=" + tc.planned, "MARKETPLACE_RESULT=" + tc.market, "UPGRADE_RESULT=" + tc.upgrade, "RELEASED_CLIENT_RESULT=" + tc.older}
			if out, err := cmd.CombinedOutput(); (err == nil) != tc.wantPass {
				t.Fatalf("pass = %v, want %v; err %v, output:\n%s", err == nil, tc.wantPass, err, out)
			}
		})
	}
}

func TestTestJobWarmsModulesBeforeWitnessedTests(t *testing.T) {
	job := getMap(t, getMap(t, loadCIWorkflow(t), "jobs"), "test")
	steps := getSlice(t, job, "steps")
	setupIndex, cacheIndex, testIndex := -1, -1, -1
	var witnessedTests map[string]any
	for i, raw := range steps {
		step := stepMap(t, raw)
		name, _ := step["name"].(string)
		switch name {
		case "Set up Go":
			setupIndex = i
		case "Cache dependencies for offline Unix cross-builds":
			cacheIndex = i
			if got := step["run"]; got != "go mod download" {
				t.Errorf("module cache step run = %v, want %q", got, "go mod download")
			}
			if _, ok := step["env"]; ok {
				t.Errorf("module cache step must not override the environment: %v", step["env"])
			}
		case "Run tests and verify CLI behavior witnesses":
			testIndex = i
			witnessedTests = step
		}
	}
	if setupIndex < 0 || cacheIndex < 0 || testIndex < 0 {
		t.Fatalf("Test job steps omit Go setup, cache warm, or witnessed tests: %+v", steps)
	}
	if cacheIndex != setupIndex+1 || cacheIndex >= testIndex {
		t.Fatalf("module cache warm step index = %d, Set up Go = %d, witnessed tests = %d", cacheIndex, setupIndex, testIndex)
	}
	if got := witnessedTests["run"]; got != "make test-witnessed" {
		t.Errorf("witnessed test step run = %v, want %q", got, "make test-witnessed")
	}
}

func TestPullRequestTrigger(t *testing.T) {
	doc := loadCIWorkflow(t)

	on := getMap(t, doc, "on")
	pr := getMap(t, on, "pull_request")
	gotTypes := toStringSlice(t, getSlice(t, pr, "types"))
	wantTypes := []string{"opened", "synchronize", "reopened", "labeled"}
	sort.Strings(gotTypes)
	sort.Strings(wantTypes)
	if !slices.Equal(gotTypes, wantTypes) {
		t.Errorf("pull_request.types = %v, want %v", gotTypes, wantTypes)
	}
}

func TestBenchmarkCondition(t *testing.T) {
	doc := loadCIWorkflow(t)

	jobs := getMap(t, doc, "jobs")
	benchmark := getMap(t, jobs, "benchmark")
	cond := benchmark["if"].(string)

	if !strings.Contains(cond, "github.event_name == 'pull_request'") {
		t.Errorf("benchmark condition missing pull_request event guard: %s", cond)
	}
	if !strings.Contains(cond, "needs.changes.outputs.benchmark == 'true'") {
		t.Errorf("benchmark condition missing path output: %s", cond)
	}
	if !strings.Contains(cond, "contains(github.event.pull_request.labels.*.name, 'run-benchmarks')") {
		t.Errorf("benchmark condition missing run-benchmarks label: %s", cond)
	}
	if !strings.Contains(cond, " || ") {
		t.Errorf("benchmark condition missing OR between path output and label: %s", cond)
	}
}

func findStepRun(t *testing.T, doc map[string]any, jobName, stepName string) string {
	t.Helper()
	jobs := getMap(t, doc, "jobs")
	job := getMap(t, jobs, jobName)
	steps := getSlice(t, job, "steps")
	for _, step := range steps {
		m := stepMap(t, step)
		if n, ok := m["name"].(string); ok && n == stepName {
			run, ok := m["run"].(string)
			if !ok {
				t.Fatalf("step %q has no run string", stepName)
			}
			return run
		}
	}
	t.Fatalf("step %q not found in job %q", stepName, jobName)
	return ""
}

func TestPlannerInvocation(t *testing.T) {
	want := `git diff --no-renames --name-only -z "$BASE_SHA" "$HEAD_SHA" | go run ./tools/ci-plan`
	cases := []struct {
		file     string
		jobName  string
		stepName string
	}{
		{"ci.yml", "changes", "Detect relevant changes"},
		{"harness-compatibility.yml", "plan", "Detect relevant changes"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			doc := loadWorkflow(t, tc.file)
			run := findStepRun(t, doc, tc.jobName, tc.stepName)
			if !strings.Contains(run, want) {
				t.Errorf("planner step in %s does not contain exact invocation %q:\n%s", tc.file, want, run)
			}
		})
	}
}

func TestBenchmarkPackagesParity(t *testing.T) {
	doc := loadWorkflow(t, "ci.yml")

	jobs := getMap(t, doc, "jobs")
	benchmark := getMap(t, jobs, "benchmark")
	env := getMap(t, benchmark, "env")
	raw, ok := env["BENCHMARK_PACKAGES"].(string)
	if !ok {
		t.Fatalf("BENCHMARK_PACKAGES is not a string: %v", env["BENCHMARK_PACKAGES"])
	}

	got := strings.Fields(raw)
	want := make([]string, 0, len(expectedBenchmarkRoots))
	for _, root := range expectedBenchmarkRoots {
		want = append(want, "./"+root)
	}

	if !slices.Equal(got, want) {
		t.Errorf("BENCHMARK_PACKAGES does not match expectedBenchmarkRoots prefixed with ./\n got (%d): %v\nwant (%d): %v", len(got), got, len(want), want)
	}
}
