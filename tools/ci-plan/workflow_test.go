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

func TestBrowserGatesPinSupportedRunner(t *testing.T) {
	for _, tc := range []struct{ file, job string }{
		{"web.yml", "check"},
		{"ci.yml", "web"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			job := getMap(t, getMap(t, loadWorkflow(t, tc.file), "jobs"), tc.job)
			if got := job["runs-on"]; got != "ubuntu-24.04" {
				t.Errorf("browser runner = %v, want ubuntu-24.04 supported by Playwright", got)
			}
		})
	}
}

func TestTUIQAMatrixBudget(t *testing.T) {
	job := getMap(t, getMap(t, loadCIWorkflow(t), "jobs"), "tui-qa")
	if got := job["timeout-minutes"]; got != 20 {
		t.Errorf("TUI QA job budget = %v, want 20 minutes for cold build, all journeys and cache cleanup", got)
	}
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

// E2E Gates requires both supported launch gates under the same planner output.
// spec: regression-gates/launch-aggregate-does-not-hide-missing-gates
func TestE2EGatesDriver(t *testing.T) {
	jobs := getMap(t, loadCIWorkflow(t), "jobs")
	driver := getMap(t, jobs, "e2e-gates-required")
	gates := []string{"marketplace-gate", "released-client-gate"}
	if _, exists := jobs["upgrade-gate"]; exists {
		t.Fatal("retired pre-v4 upgrade job is still published")
	}
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
		name                            string
		changes, planned, market, older string
		wantPass                        bool
	}{
		{"relevant+success", "success", "true", "success", "success", true},
		{"unrelated+skipped", "success", "false", "skipped", "skipped", true},
		{"market failed", "success", "true", "failure", "success", false},
		{"release failed", "success", "true", "success", "failure", false},
		{"market unexpectedly skipped", "success", "true", "skipped", "success", false},
		{"release unexpectedly skipped", "success", "true", "success", "skipped", false},
		{"market ran when unplanned", "success", "false", "success", "skipped", false},
		{"release ran when unplanned", "success", "false", "skipped", "success", false},
		{"planner failed", "failure", "false", "skipped", "skipped", false},
		{"planner cancelled", "cancelled", "true", "success", "success", false},
		{"planner unexecuted", "", "false", "skipped", "skipped", false},
		{"empty plan", "success", "", "success", "success", false},
		{"market cancelled", "success", "true", "cancelled", "success", false},
		{"release cancelled", "success", "true", "success", "cancelled", false},
		{"market unexecuted", "success", "true", "", "success", false},
		{"release unexecuted", "success", "true", "success", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-c", run)
			cmd.Env = []string{"CHANGES_RESULT=" + tc.changes, "RUN_E2E=" + tc.planned, "MARKETPLACE_RESULT=" + tc.market, "RELEASED_CLIENT_RESULT=" + tc.older}
			if out, err := cmd.CombinedOutput(); (err == nil) != tc.wantPass {
				t.Fatalf("pass = %v, want %v; err %v, output:\n%s", err == nil, tc.wantPass, err, out)
			}
		})
	}
}

// spec: regression-gates/unsupported-roots-are-witnessed-by-the-launch-gate
func TestLaunchRefusalWitnessesUseNormalE2EPath(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(data)
	for _, name := range []string{"TestUnsupportedConsumerRootsRefuseBeforeWork", "TestUnsupportedConsumerIncludesRefuseBeforeWork", "TestUnsupportedConsumerConfigPreventsMissingDestinations"} {
		if !strings.Contains(makefile, name) {
			t.Fatalf("normal launch gate is missing required refusal witness %s", name)
		}
	}
	if strings.Contains(makefile, "UPGRADE_GATE_TESTS") || strings.Contains(makefile, "upgrade-gate:") || strings.Contains(makefile, "AM_E2E_PRE_V4_BINARY") {
		t.Fatal("retired pre-v4 executable gate is still required")
	}
	if strings.Count(makefile, "./tools/witness-check -require $(E2E_REQUIRED_TESTS)") != 2 || !strings.Contains(makefile, `./tools/witness-check -require "$$E2E_REQUIRED_TESTS"`) {
		t.Fatal("host, combined, and Docker E2E paths must enforce the same refusal witnesses")
	}
	for _, target := range []string{"marketplace-gate", "validate-e2e"} {
		cmd := exec.Command("make", "--no-print-directory", "-n", target, "E2E_SKIP_DOCKER=1")
		cmd.Dir = filepath.Join("..", "..")
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), "test -json") || !strings.Contains(string(output), "witness-check -require TestUnsupportedConsumerRootsRefuseBeforeWork,TestUnsupportedConsumerIncludesRefuseBeforeWork,TestUnsupportedConsumerConfigPreventsMissingDestinations") {
			t.Fatalf("%s does not witness real E2E executions: %v\n%s", target, err, output)
		}
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

func TestInstallationQueryJob(t *testing.T) {
	jobs := getMap(t, loadCIWorkflow(t), "jobs")
	changes := getMap(t, jobs, "changes")
	if got := getMap(t, changes, "outputs")["installation_query"]; got != "${{ steps.changes.outputs.installation_query }}" {
		t.Fatalf("installation_query output = %v", got)
	}
	job := getMap(t, jobs, "installation-query")
	if job["name"] != "Installation query" || job["if"] != "needs.changes.outputs.installation_query == 'true'" {
		t.Fatalf("installation query job = name %v, if %v", job["name"], job["if"])
	}
	for _, step := range getSlice(t, job, "steps") {
		if stepMap(t, step)["run"] == "make installation-query-check" {
			return
		}
	}
	t.Fatal("installation query job does not run make installation-query-check")
}

func TestHarnessImageAuditUsesRunnerStepDirectory(t *testing.T) {
	doc := loadWorkflow(t, "harness-images.yml")
	on := getMap(t, doc, "on")
	if len(on) != 2 {
		t.Fatalf("harness image workflow triggers = %v, want only schedule and workflow_dispatch", on)
	}
	if _, ok := on["schedule"]; !ok {
		t.Fatal("harness image workflow lost its schedule")
	}
	if _, ok := on["workflow_dispatch"]; !ok {
		t.Fatal("harness image workflow lost manual dispatch")
	}

	job := getMap(t, getMap(t, doc, "jobs"), "model-catalog-audit")
	if got := job["if"]; got != "github.ref == format('refs/heads/{0}', github.event.repository.default_branch)" {
		t.Fatalf("model audit condition = %v", got)
	}
	permissions := getMap(t, job, "permissions")
	if len(permissions) != 2 || permissions["contents"] != "read" || permissions["packages"] != "read" {
		t.Fatalf("model audit permissions = %v, want only contents: read and packages: read", permissions)
	}
	if _, ok := job["env"]; ok {
		t.Fatal("model audit job-level env cannot use the runner context")
	}

	steps := getSlice(t, job, "steps")
	if len(steps) != 9 {
		t.Fatalf("model audit steps = %d, want checkout, setup, and seven named steps", len(steps))
	}
	wantNames := []string{
		"Prepare model audit diagnostics",
		"Verify offline audit contracts",
		"Log in to GitHub Container Registry",
		"Capture Cursor CLI model inventory",
		"Audit public model evidence without modifying catalogs",
		"Publish model drift summary",
		"Upload model audit evidence",
	}
	setupGoIndex, prepareIndex, verifyIndex := -1, -1, -1
	stepsByName := make(map[string]map[string]any, len(wantNames))
	gotNames := make([]string, 0, len(wantNames))
	for i, raw := range steps {
		step := stepMap(t, raw)
		if uses, ok := step["uses"].(string); ok && strings.HasPrefix(uses, "actions/setup-go@") {
			setupGoIndex = i
		}
		name, ok := step["name"].(string)
		if !ok {
			continue
		}
		gotNames = append(gotNames, name)
		stepsByName[name] = step
		if name == "Prepare model audit diagnostics" {
			prepareIndex = i
		}
		if name == "Verify offline audit contracts" {
			verifyIndex = i
		}
	}
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("model audit named steps = %v, want %v", gotNames, wantNames)
	}
	if setupGoIndex < 0 || prepareIndex != setupGoIndex+1 || verifyIndex != prepareIndex+1 {
		t.Fatalf("model audit runner step positions: Set up Go=%d prepare=%d verify=%d", setupGoIndex, prepareIndex, verifyIndex)
	}
	prepare := stepsByName["Prepare model audit diagnostics"]
	if got := prepare["if"]; got != "always()" {
		t.Fatalf("prepare model audit diagnostics if = %v, want %q", got, "always()")
	}
	if got := strings.TrimSpace(prepare["run"].(string)); got != `mkdir -p "$RUNNER_TEMP/model-catalog-audit"
echo "AUDIT_REPORT_DIR=$RUNNER_TEMP/model-catalog-audit" >> "$GITHUB_ENV"` {
		t.Fatalf("prepare model audit diagnostics run = %v", got)
	}
	if _, ok := prepare["env"]; ok {
		t.Fatalf("runner step must not add step-level env: %v", prepare["env"])
	}
	if got := stepsByName["Verify offline audit contracts"]["run"]; got != "go test ./tools/model-catalog-audit -count=1" {
		t.Fatalf("offline audit contract command = %v", got)
	}
	if got := strings.TrimSpace(stepsByName["Audit public model evidence without modifying catalogs"]["run"].(string)); got != `set -euo pipefail
go run ./tools/model-catalog-audit check -cursor-models "$AUDIT_REPORT_DIR/cursor-cli-models.txt" > "$AUDIT_REPORT_DIR/report.json" 2> "$AUDIT_REPORT_DIR/audit.log"` {
		t.Fatalf("model audit command changed: %s", got)
	}
	if got := strings.TrimSpace(stepsByName["Publish model drift summary"]["run"].(string)); got != `set -euo pipefail
if [[ -s "$AUDIT_REPORT_DIR/report.json" ]]; then
  go run ./tools/model-catalog-audit summary < "$AUDIT_REPORT_DIR/report.json" >> "$GITHUB_STEP_SUMMARY"
else
  echo '## Model catalog audit failed before producing evidence' >> "$GITHUB_STEP_SUMMARY"
  echo 'No freshness conclusion is available. Inspect the failed step and audit log.' >> "$GITHUB_STEP_SUMMARY"
fi` {
		t.Fatalf("model audit summary command changed: %s", got)
	}
	if got := getMap(t, stepsByName["Upload model audit evidence"], "with")["path"]; got != "${{ runner.temp }}/model-catalog-audit/" {
		t.Fatalf("model audit artifact path = %v", got)
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

// TestMergeQueueTriggers keeps every workflow that reports a required check
// running on merge groups, so a queued pull request is never stuck waiting for
// a check that cannot report, and plans merge groups from their own commits.
func TestMergeQueueTriggers(t *testing.T) {
	for _, file := range []string{"ci.yml", "harness-compatibility.yml", "trufflehog.yml"} {
		t.Run(file, func(t *testing.T) {
			on := getMap(t, loadWorkflow(t, file), "on")
			if _, ok := on["merge_group"]; !ok {
				t.Fatalf("%s does not run on merge_group, so its required checks never report in the merge queue", file)
			}
		})
	}
	for _, tc := range []struct{ file, job string }{{"ci.yml", "changes"}, {"harness-compatibility.yml", "plan"}} {
		t.Run(tc.file+"/planner", func(t *testing.T) {
			jobs := getMap(t, loadWorkflow(t, tc.file), "jobs")
			for _, step := range getSlice(t, getMap(t, jobs, tc.job), "steps") {
				m := stepMap(t, step)
				if m["name"] != "Detect relevant changes" {
					continue
				}
				env := getMap(t, m, "env")
				if env["BASE_SHA"] != "${{ github.event.pull_request.base.sha || github.event.merge_group.base_sha }}" || env["HEAD_SHA"] != "${{ github.event.pull_request.head.sha || github.event.merge_group.head_sha }}" {
					t.Fatalf("planner does not take merge group commits: %v", env)
				}
				run, ok := m["run"].(string)
				if !ok || !strings.Contains(run, `"$EVENT_NAME" == merge_group`) {
					t.Fatalf("planner does not diff merge groups: %v", m["run"])
				}
				return
			}
			t.Fatal("planner step missing")
		})
	}
	t.Run("trufflehog.yml/range", func(t *testing.T) {
		jobs := getMap(t, loadWorkflow(t, "trufflehog.yml"), "jobs")
		for _, step := range getSlice(t, getMap(t, jobs, "trufflehog"), "steps") {
			m := stepMap(t, step)
			if m["name"] != "TruffleHog OSS" {
				continue
			}
			with := getMap(t, m, "with")
			if with["base"] != "${{ github.event.merge_group.base_sha }}" || with["head"] != "${{ github.event.merge_group.head_sha }}" {
				t.Fatalf("TruffleHog does not scan the merge group range: %v", with)
			}
			return
		}
		t.Fatal("TruffleHog step missing")
	})
}

func findStep(t *testing.T, doc map[string]any, jobName, stepName string) map[string]any {
	t.Helper()
	steps := getSlice(t, getMap(t, getMap(t, doc, "jobs"), jobName), "steps")
	index := slices.IndexFunc(steps, func(step any) bool { return stepMap(t, step)["name"] == stepName })
	if index < 0 {
		t.Fatalf("step %q not found in job %q", stepName, jobName)
	}
	return stepMap(t, steps[index])
}

func findStepRun(t *testing.T, doc map[string]any, jobName, stepName string) string {
	t.Helper()
	return findStep(t, doc, jobName, stepName)["run"].(string)
}

// spec: benchmark-reporting/documentation-only-pr-skips-unless-opted-in
func TestBenchmarkCondition(t *testing.T) {
	benchmark := getMap(t, getMap(t, loadWorkflow(t, "benchmark.yml"), "jobs"), "benchmark")
	if got, want := benchmark["if"], "github.event_name == 'pull_request' && (needs.changes.outputs.benchmark == 'true' || contains(github.event.pull_request.labels.*.name, 'run-benchmarks'))"; got != want {
		t.Errorf("benchmark condition = %v, want %v", got, want)
	}
}

// spec: benchmark-reporting/eligible-pr-starts-independent-benchmark-workflow
// spec: benchmark-reporting/failed-required-ci-can-be-retried-while-benchmark-runs
func TestBenchmarkWorkflowIndependence(t *testing.T) {
	ci := loadCIWorkflow(t)
	ciJobs := getMap(t, ci, "jobs")
	if _, ok := ciJobs["benchmark"]; ok {
		t.Fatal("CI still contains the benchmark job, coupling informational benchmarks to required CI")
	}
	if getMap(t, ciJobs, "e2e-gates-required")["name"] != "E2E Gates" {
		t.Fatal("required E2E check name changed")
	}

	benchmark := loadWorkflow(t, "benchmark.yml")
	if benchmark["name"] != "Benchmark" {
		t.Fatal("workflow name is not Benchmark")
	}
	on := getMap(t, benchmark, "on")
	if len(on) != 1 {
		t.Fatalf("Benchmark has non-pull-request triggers: %v", on)
	}
	pr := getMap(t, on, "pull_request")
	if !slices.Equal(toStringSlice(t, getSlice(t, pr, "types")), []string{"opened", "synchronize", "reopened", "labeled"}) {
		t.Fatalf("Benchmark pull_request types = %v", pr["types"])
	}
	concurrency := getMap(t, benchmark, "concurrency")
	if concurrency["group"] != "${{ github.workflow }}-pr-${{ github.event.pull_request.number }}" || concurrency["cancel-in-progress"] != true {
		t.Fatalf("Benchmark concurrency = %v", concurrency)
	}
	if permissions, ok := benchmark["permissions"].(map[string]any); !ok || len(permissions) != 0 {
		t.Fatalf("Benchmark workflow permissions must be empty; got %v", benchmark["permissions"])
	}

	jobs := getMap(t, benchmark, "jobs")
	if len(jobs) != 2 || jobs["changes"] == nil || jobs["benchmark"] == nil {
		t.Fatalf("Benchmark jobs = %v, want only changes and benchmark", jobs)
	}
	changes := getMap(t, jobs, "changes")
	outputs := getMap(t, changes, "outputs")
	if changes["name"] != "Benchmark Changes" || len(outputs) != 1 || outputs["benchmark"] != "${{ steps.changes.outputs.benchmark }}" {
		t.Fatalf("Benchmark changes job name or outputs are invalid: %v, %v", changes["name"], outputs)
	}
	planner := findStep(t, benchmark, "changes", "Detect relevant changes")
	if env := getMap(t, planner, "env"); env["BASE_SHA"] != "${{ github.event.pull_request.base.sha }}" || env["HEAD_SHA"] != "${{ github.event.pull_request.head.sha }}" {
		t.Fatalf("Benchmark planner SHA bindings = %v", env)
	}
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
		{"benchmark.yml", "changes", "Detect relevant changes"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			doc := loadWorkflow(t, tc.file)
			run := findStepRun(t, doc, tc.jobName, tc.stepName)
			if !strings.Contains(run, want) {
				t.Errorf("planner step in %s does not contain exact invocation %q:\n%s", tc.file, want, run)
			}
			if tc.file == "benchmark.yml" && !strings.Contains(run, `>> "$GITHUB_OUTPUT"`) {
				t.Errorf("benchmark planner does not append the output to GITHUB_OUTPUT:\n%s", run)
			}
		})
	}
}

// spec: benchmark-reporting/exact-base-and-head-are-benchmarked-serially
func TestBenchmarkSerialComparison(t *testing.T) {
	doc := loadWorkflow(t, "benchmark.yml")
	benchmark := getMap(t, getMap(t, doc, "jobs"), "benchmark")
	steps := getSlice(t, benchmark, "steps")
	baseCheckout := findStep(t, doc, "benchmark", "Checkout base")
	headCheckout := findStep(t, doc, "benchmark", "Checkout pull request")
	if got := getMap(t, baseCheckout, "with")["ref"]; got != "${{ github.event.pull_request.base.sha }}" {
		t.Fatalf("base checkout ref = %v", got)
	}
	if got := getMap(t, headCheckout, "with")["ref"]; got != "${{ github.event.pull_request.head.sha }}" {
		t.Fatalf("head checkout ref = %v", got)
	}
	for _, name := range []string{"Benchmark base", "Benchmark pull request"} {
		if run := findStepRun(t, doc, "benchmark", name); !strings.Contains(run, "-benchtime=200ms -count=6 -benchmem") {
			t.Errorf("%s sample flags changed: %s", name, run)
		}
	}
	report := findStep(t, doc, "benchmark", "Build benchmark report")
	reportEnv := getMap(t, report, "env")
	if reportEnv["BASE_SHA"] != "${{ github.event.pull_request.base.sha }}" || reportEnv["HEAD_SHA"] != "${{ github.event.pull_request.head.sha }}" {
		t.Errorf("report SHA bindings = %v", reportEnv)
	}
	reportRun := report["run"].(string)
	if !strings.Contains(reportRun, "benchstat -table '' benchmark-base-clean.txt benchmark-head-clean.txt") || !strings.Contains(reportRun, "go run ./tools/benchmark-report ../benchmark-base-clean.txt ../benchmark-head-clean.txt") {
		t.Errorf("report generation changed: %v", report["run"])
	}
	artifact := findStep(t, doc, "benchmark", "Upload benchmark evidence")
	artifactWith := getMap(t, artifact, "with")
	if artifactWith["name"] != "tui-performance-evidence" || artifactWith["path"] != "benchmark-*.txt\nbenchmark-comment.md\ntui-*.pprof\ntui-*.trace\n" {
		t.Errorf("artifact changed: %v", artifactWith)
	}
	index := func(name string) int {
		return slices.IndexFunc(steps, func(step any) bool { return stepMap(t, step)["name"] == name })
	}
	if index("Benchmark base") >= index("Benchmark pull request") || index("Benchmark pull request") >= index("Build benchmark report") || index("Build benchmark report") >= index("Upload benchmark evidence") {
		t.Fatal("base/head comparison, report, and artifact are not serial in one job")
	}
}

// spec: benchmark-reporting/fork-pr-does-not-attempt-a-privileged-comment
func TestBenchmarkForkCommentBoundary(t *testing.T) {
	doc := loadWorkflow(t, "benchmark.yml")
	jobs := getMap(t, doc, "jobs")
	changesPermissions := getMap(t, getMap(t, jobs, "changes"), "permissions")
	if len(changesPermissions) != 1 || changesPermissions["contents"] != "read" {
		t.Fatalf("planner permissions = %v, want contents: read", changesPermissions)
	}
	benchmarkPermissions := getMap(t, getMap(t, jobs, "benchmark"), "permissions")
	if len(benchmarkPermissions) != 2 || benchmarkPermissions["contents"] != "read" || benchmarkPermissions["pull-requests"] != "write" {
		t.Fatalf("benchmark permissions = %v, want contents: read and pull-requests: write", benchmarkPermissions)
	}
	comment := findStep(t, doc, "benchmark", "Update pull request comment")
	if comment["if"] != "github.event.pull_request.head.repo.full_name == github.repository" {
		t.Fatalf("comment guard = %v", comment["if"])
	}
}

func TestBenchmarkPackagesParity(t *testing.T) {
	doc := loadWorkflow(t, "benchmark.yml")

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
