package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type workflowStep struct {
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]any    `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

type workflowJob struct {
	If    string         `yaml:"if"`
	Needs any            `yaml:"needs"`
	Steps []workflowStep `yaml:"steps"`
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func specsJob(t *testing.T) workflowJob {
	t.Helper()
	var doc struct {
		Jobs map[string]workflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(readRepoFile(t, ".github/workflows/ci.yml")), &doc); err != nil {
		t.Fatalf("parse ci.yml: %v", err)
	}
	job, ok := doc.Jobs["specs"]
	if !ok {
		t.Fatal("ci.yml has no specs job")
	}
	return job
}

// makeRecipe returns the command lines of a Makefile target.
func makeRecipe(t *testing.T, target string) []string {
	t.Helper()
	var lines []string
	in := false
	for _, line := range strings.Split(readRepoFile(t, "Makefile"), "\n") {
		switch {
		case strings.HasPrefix(line, target+":"):
			in = true
		case in && strings.HasPrefix(line, "\t"):
			lines = append(lines, strings.TrimSpace(line))
		case in:
			return lines
		}
	}
	return lines
}

// spec: spec-governance/invalid-spec-fails-ci
func TestSpecsJobRunsStrictValidationOnEveryEvent(t *testing.T) {
	job := specsJob(t)
	if job.If != "" || job.Needs != nil {
		t.Fatalf("specs job is conditional (if=%q needs=%v); it must run on every pull request", job.If, job.Needs)
	}
	var checkout, specCheck, gate *workflowStep
	for i := range job.Steps {
		s := &job.Steps[i]
		switch {
		case strings.HasPrefix(s.Uses, "actions/checkout@"):
			checkout = s
		case s.Run == "make spec-check" && s.If == "":
			specCheck = s
		case strings.Contains(s.Run, "go run ./tools/specaudit gate"):
			gate = s
		}
	}
	if checkout == nil || checkout.With["fetch-depth"] != 0 {
		t.Errorf("checkout step = %+v, want fetch-depth: 0 for the gate's commit range", checkout)
	}
	if specCheck == nil {
		t.Error("specs job does not run make spec-check unconditionally")
	}
	if gate == nil || gate.If != "github.event_name == 'pull_request'" ||
		!strings.Contains(gate.Run, `-base "$BASE_SHA" -head "$HEAD_SHA"`) ||
		gate.Env["BASE_SHA"] != "${{ github.event.pull_request.base.sha }}" ||
		gate.Env["HEAD_SHA"] != "${{ github.event.pull_request.head.sha }}" {
		t.Errorf("gate step = %+v, want pull-request-only specaudit gate over base..head", gate)
	}

	recipe := strings.Join(makeRecipe(t, "spec-check"), "\n")
	for _, want := range []string{"validate --all --strict --no-interactive", "./tools/specaudit trace", "./tools/specaudit archived"} {
		if !strings.Contains(recipe, want) {
			t.Errorf("make spec-check recipe does not contain %q:\n%s", want, recipe)
		}
	}
}

// spec: spec-governance/cli-version-is-pinned
func TestOpenSpecCLIVersionIsPinned(t *testing.T) {
	m := regexp.MustCompile(`(?m)^OPENSPEC_VERSION \?= (\S+)$`).FindStringSubmatch(readRepoFile(t, "Makefile"))
	if m == nil || !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(m[1]) {
		t.Fatalf("OPENSPEC_VERSION = %v, want an exact x.y.z version", m)
	}
	recipe := strings.Join(makeRecipe(t, "spec-check"), "\n")
	if !strings.Contains(recipe, "@fission-ai/openspec@$(OPENSPEC_VERSION) ") {
		t.Errorf("spec-check does not install the CLI at $(OPENSPEC_VERSION):\n%s", recipe)
	}
	if strings.Contains(readRepoFile(t, ".github/workflows/ci.yml"), "openspec@") {
		t.Error("ci.yml references an OpenSpec version directly; use make spec-check so the pin lives in one place")
	}
}
