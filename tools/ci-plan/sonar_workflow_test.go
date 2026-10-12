package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const sonarRepository = "Adyen/agent-marketplace"

var sonarCoverageArgs = []string{"test", "-short", "-timeout=50m", "-covermode=atomic", "-coverprofile=coverage.out", "./..."}

func sonarJob(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	return getMap(t, getMap(t, doc, "jobs"), name)
}

func sonarStep(t *testing.T, job map[string]any, name string) map[string]any {
	t.Helper()
	for _, raw := range getSlice(t, job, "steps") {
		step := stepMap(t, raw)
		if step["name"] == name {
			return step
		}
	}
	t.Fatalf("step %q not found", name)
	return nil
}

func sonarProperties(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "sonar-project.properties"))
	if err != nil {
		t.Fatal(err)
	}
	properties := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			properties[key] = value
		}
	}
	return properties
}

// spec: regression-gates/eligible-revisions-use-the-public-service
func TestSonarCloudWorkflow(t *testing.T) {
	doc := loadWorkflow(t, "sonar_cloud.yml")
	on := getMap(t, doc, "on")
	if got := toStringSlice(t, getSlice(t, getMap(t, on, "push"), "branches")); !slices.Equal(got, []string{"main"}) {
		t.Errorf("push branches = %v", got)
	}
	pr := getMap(t, on, "pull_request")
	if got := toStringSlice(t, getSlice(t, pr, "branches")); !slices.Equal(got, []string{"main"}) || !slices.Equal(toStringSlice(t, getSlice(t, pr, "types")), []string{"opened", "synchronize", "reopened"}) {
		t.Errorf("pull_request branches/types = %v/%v", pr["branches"], pr["types"])
	}
	if _, ok := on["workflow_dispatch"]; !ok {
		t.Error("manual trigger is missing")
	}
	for _, event := range []string{"pull_request_target", "workflow_run"} {
		if _, ok := on[event]; ok {
			t.Errorf("privileged trigger %q is configured", event)
		}
	}
	if concurrency := getMap(t, doc, "concurrency"); concurrency["group"] != "sonar-cloud-${{ github.ref }}" || concurrency["cancel-in-progress"] != true || len(getMap(t, doc, "permissions")) != 0 {
		t.Errorf("concurrency/permissions = %v/%v", concurrency, doc["permissions"])
	}
	jobs := getMap(t, doc, "jobs")
	if len(jobs) != 3 {
		t.Fatalf("jobs = %v, want configuration, coverage, and analysis", jobs)
	}
	configuration, coverage, analysis := sonarJob(t, doc, "configuration"), sonarJob(t, doc, "coverage"), sonarJob(t, doc, "analysis")
	for name, job := range map[string]map[string]any{"configuration": configuration, "coverage": coverage, "analysis": analysis} {
		if job["runs-on"] != "ubuntu-24.04" {
			t.Errorf("%s runner = %v", name, job["runs-on"])
		}
	}
	if len(getMap(t, configuration, "permissions")) != 0 {
		t.Errorf("configuration permissions = %v", configuration["permissions"])
	}
	for _, job := range []map[string]any{coverage, analysis} {
		permissions := getMap(t, job, "permissions")
		if len(permissions) != 1 || permissions["contents"] != "read" {
			t.Errorf("job permissions = %v, want contents: read", permissions)
		}
	}
	if steps := getSlice(t, configuration, "steps"); len(steps) != 1 || stepMap(t, steps[0])["uses"] != nil {
		t.Fatalf("configuration must be a single checkout-free token step: %v", configuration["steps"])
	}
	if getMap(t, configuration, "outputs")["enabled"] != "${{ steps.token.outputs.enabled }}" || coverage["needs"] != "configuration" || coverage["if"] != "needs.configuration.outputs.enabled == 'true'" || analysis["needs"] != "coverage" || analysis["if"] != nil {
		t.Errorf("eligibility outputs/dependencies are wrong: configuration=%v coverage=%v analysis=%v", configuration["outputs"], coverage["needs"], analysis["needs"])
	}
	if coverage["timeout-minutes"] != 60 {
		t.Errorf("coverage timeout = %v, want 60", coverage["timeout-minutes"])
	}
	coverageCheckout := getMap(t, sonarStep(t, coverage, "Checkout"), "with")
	if coverageCheckout["persist-credentials"] != false || coverageCheckout["ref"] != nil || coverageCheckout["fetch-depth"] != nil {
		t.Errorf("coverage checkout overrides default revision/credentials: %v", coverageCheckout)
	}
	analysisCheckout := getMap(t, sonarStep(t, analysis, "Checkout"), "with")
	if analysisCheckout["fetch-depth"] != 0 || analysisCheckout["persist-credentials"] != false || analysisCheckout["ref"] != nil {
		t.Errorf("analysis checkout options = %v", analysisCheckout)
	}
	if steps := getSlice(t, analysis, "steps"); len(steps) != 3 {
		t.Fatalf("analysis steps = %d, want checkout/download/scanner", len(steps))
	} else {
		for _, raw := range steps {
			if _, ok := stepMap(t, raw)["run"]; ok {
				t.Error("analysis job must not execute repository scripts")
			}
		}
	}
	scanner := sonarStep(t, analysis, "Analyze with SonarQube Cloud")
	if scanner["uses"] != "SonarSource/sonarqube-scan-action@d209202bc7d53ff1cc128f7f907dac145c9d6ae9" || getMap(t, scanner, "with")["args"] != "-Dsonar.scm.revision=${{ github.sha }}" {
		t.Errorf("scanner action/configuration = %v", scanner)
	}
	pins := []string{
		"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
		"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
		"actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02",
		"actions/download-artifact@d3f86a106a0bac45b974a628896c90dbdf5c8093",
		"SonarSource/sonarqube-scan-action@d209202bc7d53ff1cc128f7f907dac145c9d6ae9",
	}
	usedPins := make(map[string]bool)
	for jobName, rawJob := range jobs {
		for _, raw := range getSlice(t, stepMap(t, rawJob), "steps") {
			if uses, ok := stepMap(t, raw)["uses"].(string); ok {
				if !slices.Contains(pins, uses) {
					t.Errorf("%s uses unpinned action %q", jobName, uses)
				}
				usedPins[uses] = true
			}
		}
	}
	for _, pin := range pins {
		if !usedPins[pin] {
			t.Errorf("expected action pin %q is unused", pin)
		}
	}
	if _, ok := doc["env"]; ok {
		t.Error("workflow-level env must not receive secrets")
	}
	for name, job := range map[string]map[string]any{"configuration": configuration, "coverage": coverage, "analysis": analysis} {
		if env, ok := job["env"].(map[string]any); ok && env["SONAR_TOKEN"] != nil {
			t.Errorf("%s job-level env exposes SONAR_TOKEN", name)
		}
		for _, raw := range getSlice(t, job, "steps") {
			step := stepMap(t, raw)
			env, _ := step["env"].(map[string]any)
			_, hasToken := env["SONAR_TOKEN"]
			wantToken := (name == "configuration" && step["name"] == "Check analysis token availability") || (name == "analysis" && step["name"] == "Analyze with SonarQube Cloud")
			if hasToken != wantToken || hasToken && env["SONAR_TOKEN"] != "${{ secrets.SONAR_TOKEN }}" {
				t.Errorf("%s step %q has unexpected token scope", name, step["name"])
			}
		}
	}

	properties := sonarProperties(t)
	for key, want := range map[string]string{
		"sonar.host.url":                "https://sonarcloud.io",
		"sonar.organization":            "adyen",
		"sonar.projectKey":              "Adyen_agent-marketplace",
		"sonar.sourceEncoding":          "UTF-8",
		"sonar.go.coverage.reportPaths": "coverage.out",
	} {
		if properties[key] != want {
			t.Errorf("%s = %q, want %q", key, properties[key], want)
		}
	}
	roots := "cmd,internal,pkg,tools,scripts,web,site,api,deploy,e2e"
	if properties["sonar.sources"] != roots || properties["sonar.tests"] != roots {
		t.Errorf("source/test roots = %q/%q, want %q", properties["sonar.sources"], properties["sonar.tests"], roots)
	}
	tests := []string{"**/*_test.go", "**/*.test.*", "**/*.spec.*", "web/tests/**", "tools/tui-qa/tests/**", "e2e/**"}
	if got := strings.Split(properties["sonar.test.inclusions"], ","); !slices.Equal(got, tests) {
		t.Errorf("test inclusions = %v, want %v", got, tests)
	}
	for key, want := range map[string][]string{
		"sonar.exclusions":      append(tests, "**/testdata/**", "**/generated/**", "**/node_modules/**", "**/.nuxt/**", "**/.output/**", "**/dist/**", "**/.vitepress/cache/**", "**/.vitepress/dist/**"),
		"sonar.test.exclusions": {"**/testdata/**", "**/generated/**", "**/node_modules/**", "**/.nuxt/**", "**/.output/**", "**/dist/**", "**/.vitepress/cache/**", "**/.vitepress/dist/**"},
	} {
		got := strings.Split(properties[key], ",")
		for _, pattern := range want {
			if !slices.Contains(got, pattern) {
				t.Errorf("%s missing %q", key, pattern)
			}
		}
	}
	if _, ok := properties["sonar.coverage.exclusions"]; ok {
		t.Error("must not exclude unmeasured frontend coverage")
	}
}

// spec: regression-gates/untrusted-requests-receive-no-analysis-credentials
func TestSonarCloudEligibility(t *testing.T) {
	condition := sonarJob(t, loadWorkflow(t, "sonar_cloud.yml"), "configuration")["if"].(string)
	want := "github.actor != 'dependabot[bot]' && (github.event_name == 'push' || (github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/main') || (github.event_name == 'pull_request' && github.event.pull_request.head.repo.full_name == github.repository && github.event.pull_request.user.login != 'dependabot[bot]'))"
	if strings.Join(strings.Fields(condition), " ") != want {
		t.Fatalf("eligibility expression = %q, want %q", condition, want)
	}
	for _, tc := range []struct {
		name, actor, event, ref, headRepo, prAuthor string
		want                                        bool
	}{
		{"main push", "maintainer", "push", "refs/heads/main", sonarRepository, "maintainer", true},
		{"same-repository pull request", "contributor", "pull_request", "refs/pull/1/merge", sonarRepository, "contributor", true},
		{"fork pull request", "contributor", "pull_request", "refs/pull/2/merge", "person/agent-marketplace", "contributor", false},
		{"Dependabot actor", "dependabot[bot]", "push", "refs/heads/main", sonarRepository, "maintainer", false},
		{"Dependabot pull request author", "maintainer", "pull_request", "refs/pull/3/merge", sonarRepository, "dependabot[bot]", false},
		{"manual main dispatch", "maintainer", "workflow_dispatch", "refs/heads/main", sonarRepository, "maintainer", true},
		{"manual non-main dispatch", "maintainer", "workflow_dispatch", "refs/heads/feature", sonarRepository, "maintainer", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expression := strings.NewReplacer(
				"github.event.pull_request.head.repo.full_name", strconv.Quote(tc.headRepo),
				"github.event.pull_request.user.login", strconv.Quote(tc.prAuthor),
				"github.event_name", strconv.Quote(tc.event),
				"github.repository", strconv.Quote(sonarRepository),
				"github.actor", strconv.Quote(tc.actor),
				"github.ref", strconv.Quote(tc.ref),
				"'", `"`,
			).Replace(condition)
			cmd := exec.Command("bash", "--noprofile", "--norc", "-c", "[[ "+expression+" ]]")
			cmd.Env = []string{}
			err := cmd.Run()
			code := 0
			if err != nil {
				exit, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("evaluate eligibility: %v", err)
				}
				code = exit.ExitCode()
			}
			if code > 1 || (code == 0) != tc.want {
				t.Errorf("eligibility exit = %d, want %v", code, tc.want)
			}
		})
	}
}

// spec: regression-gates/missing-credentials-produce-an-explicit-skip
func TestSonarCloudTokenShell(t *testing.T) {
	run := findStepRun(t, loadWorkflow(t, "sonar_cloud.yml"), "configuration", "Check analysis token availability")
	if strings.Contains(run, "sonar-scanner") {
		t.Error("token check must not invoke a scanner")
	}
	for _, tc := range []struct {
		name, token, output string
		notice              bool
	}{{"missing token", "", "enabled=false\n", true}, {"configured token", "dummy-sonar-token-value", "enabled=true\n", false}} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "github-output")
			cmd := exec.Command("bash", "--noprofile", "--norc", "-c", run)
			cmd.Env = []string{"SONAR_TOKEN=" + tc.token, "GITHUB_OUTPUT=" + path}
			stdout, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("token shell: %v\n%s", err, stdout)
			}
			output, err := os.ReadFile(path)
			if err != nil || string(output) != tc.output {
				t.Fatalf("GITHUB_OUTPUT = %q, err %v; want %q", output, err, tc.output)
			}
			if strings.Contains(string(stdout), "SonarQube Cloud analysis skipped") != tc.notice || tc.token != "" && (strings.Contains(string(stdout), tc.token) || strings.Contains(string(output), tc.token)) {
				t.Errorf("unexpected eligibility output: %q, %q", stdout, output)
			}
		})
	}
}

func runFakeSonarGo(t *testing.T, run string, exitCode int) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	fakeGo := `#!/bin/sh
actual=$(printf '%s\n' "$@")
[ "$actual" = "$EXPECTED_GO_ARGS" ] || { printf 'unexpected go args:\n%s\n' "$actual" >&2; exit 41; }
[ "${CGO_ENABLED:-}" = 0 ] || exit 42
[ "${FAKE_GO_EXIT:-0}" -eq 0 ] || exit "$FAKE_GO_EXIT"
printf 'fake coverage report\n' > coverage.out
`
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(fakeGo), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", run)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "CGO_ENABLED=0", "EXPECTED_GO_ARGS=" + strings.Join(sonarCoverageArgs, "\n"), "FAKE_GO_EXIT=" + strconv.Itoa(exitCode)}
	output, err := cmd.CombinedOutput()
	return dir, string(output), err
}

// spec: regression-gates/analysis-imports-fresh-matching-coverage
func TestSonarCloudCoverage(t *testing.T) {
	coverage := sonarJob(t, loadWorkflow(t, "sonar_cloud.yml"), "coverage")
	step := sonarStep(t, coverage, "Generate Go coverage")
	if step["run"] != "go "+strings.Join(sonarCoverageArgs, " ") || getMap(t, step, "env")["CGO_ENABLED"] != "0" {
		t.Fatalf("coverage command/env = %v/%v", step["run"], step["env"])
	}
	for _, tc := range []struct {
		name string
		code int
	}{{"success", 0}, {"go test failure", 17}} {
		t.Run(tc.name, func(t *testing.T) {
			dir, output, err := runFakeSonarGo(t, step["run"].(string), tc.code)
			if tc.code == 0 {
				if err != nil {
					t.Fatalf("coverage command: %v\n%s", err, output)
				}
				report, readErr := os.ReadFile(filepath.Join(dir, "coverage.out"))
				if readErr != nil || string(report) != "fake coverage report\n" {
					t.Fatalf("coverage report = %q, err %v", report, readErr)
				}
				return
			}
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != tc.code {
				t.Fatalf("coverage failure = %v, want exit %d; output:\n%s", err, tc.code, output)
			}
			if _, err := os.Stat(filepath.Join(dir, "coverage.out")); !os.IsNotExist(err) {
				t.Errorf("failed go test left coverage report: %v", err)
			}
		})
	}
	upload, analysis := sonarStep(t, coverage, "Upload Go coverage"), sonarJob(t, loadWorkflow(t, "sonar_cloud.yml"), "analysis")
	download := sonarStep(t, analysis, "Download matching Go coverage")
	if getMap(t, upload, "with")["path"] != "coverage.out" || getMap(t, upload, "with")["name"] != "sonar-go-coverage-${{ github.sha }}" || getMap(t, download, "with")["name"] != getMap(t, upload, "with")["name"] {
		t.Errorf("coverage upload/download config = %v/%v", upload["with"], download["with"])
	}
	for _, key := range []string{"run-id", "repository", "pattern"} {
		if _, ok := getMap(t, download, "with")[key]; ok {
			t.Errorf("coverage download uses fallback selector %q", key)
		}
	}
}

// spec: regression-gates/analysis-failures-remain-visible
func TestSonarCloudFailures(t *testing.T) {
	coverage, analysis := sonarJob(t, loadWorkflow(t, "sonar_cloud.yml"), "coverage"), sonarJob(t, loadWorkflow(t, "sonar_cloud.yml"), "analysis")
	steps := []map[string]any{sonarStep(t, coverage, "Generate Go coverage"), sonarStep(t, coverage, "Upload Go coverage"), sonarStep(t, analysis, "Download matching Go coverage"), sonarStep(t, analysis, "Analyze with SonarQube Cloud")}
	if getMap(t, steps[1], "with")["if-no-files-found"] != "error" {
		t.Errorf("missing coverage report behavior = %v, want error", getMap(t, steps[1], "with")["if-no-files-found"])
	}
	for _, step := range steps {
		if _, ok := step["continue-on-error"]; ok {
			t.Errorf("step %q masks failure", step["name"])
		}
	}
}
