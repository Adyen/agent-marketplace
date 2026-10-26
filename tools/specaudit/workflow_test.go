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
	ID   string            `yaml:"id"`
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

func ciJob(t *testing.T, name string) workflowJob {
	t.Helper()
	var doc struct {
		Jobs map[string]workflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(readRepoFile(t, ".github/workflows/ci.yml")), &doc); err != nil {
		t.Fatalf("parse ci.yml: %v", err)
	}
	job, ok := doc.Jobs[name]
	if !ok {
		t.Fatalf("ci.yml has no %s job", name)
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
	job := ciJob(t, "specs")
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

func TestReleaseJobRendersCuratedNotes(t *testing.T) {
	steps := ciJob(t, "release").Steps
	stepIndex := func(match func(workflowStep) bool) int {
		for i, s := range steps {
			if match(s) {
				return i
			}
		}
		return -1
	}
	build := stepIndex(func(s workflowStep) bool { return s.Run == "make release" })
	notesAt := stepIndex(func(s workflowStep) bool { return s.ID == "notes" })
	publish := stepIndex(func(s workflowStep) bool { return strings.HasPrefix(s.Uses, "softprops/action-gh-release@") })
	if build < 0 || notesAt <= build || publish <= notesAt {
		t.Fatalf("release steps out of order: build=%d notes=%d publish=%d", build, notesAt, publish)
	}

	notes := steps[notesAt]
	target := regexp.MustCompile(`-o (\S+)`).FindStringSubmatch(notes.Run)
	if !strings.Contains(notes.Run, "./tools/specaudit release-notes") || target == nil || strings.HasPrefix(target[1], "dist/") {
		t.Errorf("notes step = %q, want specaudit release-notes writing outside dist/", notes.Run)
	}
	if !strings.Contains(notes.Run, `grep -m1 -E '^v[0-9]+\.[0-9]+\.[0-9]+$'`) || !strings.Contains(notes.Run, `"$VERSION^"`) {
		t.Errorf("notes step does not select the previous stable tag before this release:\n%s", notes.Run)
	}

	publishStep := steps[publish]
	for key, want := range map[string]string{
		"body_path":    "${{ steps.notes.outputs.body_path }}",
		"previous_tag": "${{ steps.notes.outputs.previous_tag }}",
		"files":        "dist/*",
	} {
		if publishStep.With[key] != want {
			t.Errorf("release action %s = %v, want %q", key, publishStep.With[key], want)
		}
	}
	if publishStep.With["generate_release_notes"] != true {
		t.Errorf("release action generate_release_notes = %v, want true", publishStep.With["generate_release_notes"])
	}
}

func TestReleaseNotesMakeTargetPreviewsStableRange(t *testing.T) {
	recipe := strings.Join(makeRecipe(t, "release-notes"), "\n")
	if !strings.Contains(recipe, "./tools/specaudit release-notes") || !strings.Contains(recipe, "-head HEAD") {
		t.Errorf("make release-notes does not render the stable tag through HEAD:\n%s", recipe)
	}
	if !strings.Contains(recipe, `grep -m1 -E '^v[0-9]+\.[0-9]+\.[0-9]+$$'`) || !strings.Contains(recipe, "--merged HEAD^ ") {
		t.Errorf("make release-notes does not select the previous stable tag before HEAD:\n%s", recipe)
	}
}
