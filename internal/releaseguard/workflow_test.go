package releaseguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	workflowRelPath = ".github/workflows/ci.yml"
	guardJobID      = "release-tag-guard"
	releaseJobID    = "release"
)

type workflowFile struct {
	On struct {
		Push struct {
			Branches []string `yaml:"branches"`
			Tags     []string `yaml:"tags"`
		} `yaml:"push"`
	} `yaml:"on"`
	Permissions *map[string]string     `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Name        string             `yaml:"name"`
	If          string             `yaml:"if"`
	Needs       []string           `yaml:"needs"`
	Environment string             `yaml:"environment"`
	Permissions *map[string]string `yaml:"permissions"`
	Steps       []workflowStep     `yaml:"steps"`
	Env         map[string]string  `yaml:"env"`
}

type workflowStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
}

func loadWorkflow(t *testing.T) workflowFile {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), workflowRelPath))
	if err != nil {
		t.Fatalf("read %s: %v", workflowRelPath, err)
	}

	var wf workflowFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("parse %s: %v", workflowRelPath, err)
	}
	if len(wf.Jobs) == 0 {
		t.Fatalf("parsed %s but found no jobs", workflowRelPath)
	}
	if len(wf.On.Push.Tags) == 0 {
		t.Fatalf("parsed %s but found no push tag filters", workflowRelPath)
	}
	return wf
}

// TestWorkflowRestrictsReleaseTagTrigger keeps the privileged workflow from
// firing on arbitrary tags: the old '*' filter meant any tag name — including a
// shell-bearing one — started the release path.
func TestWorkflowRestrictsReleaseTagTrigger(t *testing.T) {
	t.Parallel()

	wf := loadWorkflow(t)

	for _, pattern := range wf.On.Push.Tags {
		if pattern == "*" || pattern == "**" {
			t.Fatalf("tag trigger %q matches every tag", pattern)
		}
		if !strings.HasPrefix(pattern, "v[0-9]+.[0-9]+.[0-9]+") {
			t.Errorf("tag trigger %q is broader than the release tag shape", pattern)
		}
	}
}

func TestWorkflowValidatesTagBeforePrivilegedJob(t *testing.T) {
	t.Parallel()

	wf := loadWorkflow(t)

	guard, ok := wf.Jobs[guardJobID]
	if !ok {
		t.Fatalf("workflow has no %q job", guardJobID)
	}
	if !strings.Contains(guard.If, "refs/tags/") {
		t.Errorf("%s job must be conditional on a tag ref, got if: %q", guardJobID, guard.If)
	}
	if !jobRunsValidator(guard) {
		t.Errorf("%s job does not run %s", guardJobID, validateScriptRelPath)
	}
	if perms := guard.Permissions; perms == nil || (*perms)["contents"] != "read" {
		t.Errorf("%s job must declare contents: read, got %v", guardJobID, perms)
	}

	release, ok := wf.Jobs[releaseJobID]
	if !ok {
		t.Fatalf("workflow has no %q job", releaseJobID)
	}
	if !contains(release.Needs, guardJobID) {
		t.Errorf("%s job must need %s, got needs: %v", releaseJobID, guardJobID, release.Needs)
	}
	if release.Environment == "" {
		t.Errorf("%s job must run in a protected environment to restrict who can publish", releaseJobID)
	}

	validateAt, makeAt := -1, -1
	for i, step := range release.Steps {
		if strings.Contains(step.Run, validateScriptRelPath) && validateAt < 0 {
			validateAt = i
		}
		if strings.Contains(step.Run, "make release") && makeAt < 0 {
			makeAt = i
		}
	}
	switch {
	case validateAt < 0:
		t.Fatalf("%s job does not re-validate the tag with %s", releaseJobID, validateScriptRelPath)
	case makeAt < 0:
		t.Fatalf("%s job does not run `make release`", releaseJobID)
	case validateAt > makeAt:
		t.Fatalf("%s job validates the tag at step %d, after `make release` at step %d", releaseJobID, validateAt, makeAt)
	}
}

// TestWorkflowNeverInterpolatesExpressionsIntoShell asserts that no `run:` block
// splices a `${{ }}` expression into a shell script: attacker-controlled values
// such as a tag name must arrive through the environment and be quoted.
func TestWorkflowNeverInterpolatesExpressionsIntoShell(t *testing.T) {
	t.Parallel()

	wf := loadWorkflow(t)

	for jobID, job := range wf.Jobs {
		for i, step := range job.Steps {
			if strings.Contains(step.Run, "${{") {
				t.Errorf("job %s step %d interpolates an expression into a shell script: %q", jobID, i, step.Run)
			}
			for name, value := range step.Env {
				if !strings.Contains(value, "${{") {
					continue
				}
				if !strings.Contains(step.Run, `"$`+name+`"`) {
					t.Errorf("job %s step %d passes %s from an expression but does not quote \"$%s\" in run", jobID, i, name, name)
				}
			}
		}
		for name, value := range job.Env {
			if !strings.Contains(value, "${{") {
				continue
			}
			for i, step := range job.Steps {
				if strings.Contains(step.Run, "$"+name) && !strings.Contains(step.Run, `"$`+name+`"`) {
					t.Errorf("job %s step %d uses unquoted $%s derived from an expression: %q", jobID, i, name, step.Run)
				}
			}
		}
	}
}

// TestWorkflowKeepsWritePermissionOnReleaseJobOnly documents and enforces the
// permission model: the workflow grants nothing by default and only the
// publishing job may write.
func TestWorkflowKeepsWritePermissionOnReleaseJobOnly(t *testing.T) {
	t.Parallel()

	wf := loadWorkflow(t)

	if wf.Permissions == nil {
		t.Fatalf("workflow must declare top-level permissions")
	}
	if len(*wf.Permissions) != 0 {
		t.Errorf("workflow top-level permissions must be empty, got %v", *wf.Permissions)
	}

	for jobID, job := range wf.Jobs {
		perms := job.Permissions
		if perms == nil {
			t.Errorf("job %s does not declare permissions", jobID)
			continue
		}
		for scope, level := range *perms {
			if level != "write" {
				continue
			}
			if jobID != releaseJobID {
				t.Errorf("job %s requests %s: write; only %s may write", jobID, scope, releaseJobID)
			}
			if scope != "contents" {
				t.Errorf("job %s requests %s: write; only contents: write is expected", jobID, scope)
			}
		}
	}
}

func jobRunsValidator(job workflowJob) bool {
	for _, step := range job.Steps {
		if strings.Contains(step.Run, validateScriptRelPath) {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
