package main

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const pagesPublishCondition = "github.ref == 'refs/heads/main' && github.event_name != 'pull_request'"

type pagesStep struct {
	workflowStep `yaml:",inline"`
	ID           string `yaml:"id"`
}

type pagesJob struct {
	If          string            `yaml:"if"`
	Needs       string            `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Environment map[string]string `yaml:"environment"`
	Steps       []pagesStep       `yaml:"steps"`
}

type pagesWorkflow struct {
	On map[string]struct {
		Branches []string `yaml:"branches"`
		Paths    []string `yaml:"paths"`
	} `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Concurrency map[string]any    `yaml:"concurrency"`
	Jobs        map[string]pagesJob
}

func readPagesWorkflow(t *testing.T) pagesWorkflow {
	t.Helper()
	var workflow pagesWorkflow
	if err := yaml.Unmarshal([]byte(readRepoFile(t, ".github/workflows/site.yml")), &workflow); err != nil {
		t.Fatalf("parse site.yml: %v", err)
	}
	return workflow
}

// spec: site-deployment-profiles/main-publishes-the-validated-public-artifact
func TestSiteWorkflowPublishesValidatedArtifact(t *testing.T) {
	workflow := readPagesWorkflow(t)
	for _, event := range []string{"push", "pull_request"} {
		trigger := workflow.On[event]
		if !slices.Equal(trigger.Branches, []string{"main"}) {
			t.Errorf("%s branches = %v, want main only", event, trigger.Branches)
		}
		for _, path := range []string{"site/**", "design/**", "Makefile", ".github/workflows/site.yml"} {
			if !slices.Contains(trigger.Paths, path) {
				t.Errorf("%s does not watch %s", event, path)
			}
		}
	}
	if _, ok := workflow.On["workflow_dispatch"]; !ok {
		t.Error("manual website publication is missing")
	}
	if workflow.Concurrency["cancel-in-progress"] != "${{ github.ref != 'refs/heads/main' }}" ||
		workflow.Concurrency["group"] != "${{ github.workflow }}-${{ github.ref }}" {
		t.Errorf("main runs must serialize without cancelling deployments: %v", workflow.Concurrency)
	}

	build, deploy := workflow.Jobs["build"], workflow.Jobs["deploy"]
	if build.If != "" || build.Needs != "" {
		t.Error("website validation must run without a deployment prerequisite")
	}
	if deploy.If != pagesPublishCondition || deploy.Needs != "build" {
		t.Errorf("deploy must require a successful main build: %+v", deploy)
	}
	if deploy.Environment["name"] != "github-pages" ||
		deploy.Environment["url"] != "${{ steps.deployment.outputs.page_url }}" {
		t.Errorf("unexpected Pages environment: %v", deploy.Environment)
	}

	configure, validate, upload := -1, -1, -1
	pinnedAction := regexp.MustCompile(`@[0-9a-f]{40}$`)
	for jobName, job := range workflow.Jobs {
		for i, step := range job.Steps {
			if step.Uses != "" && !pinnedAction.MatchString(step.Uses) {
				t.Errorf("%s action is not commit-pinned: %s", jobName, step.Uses)
			}
			switch {
			case strings.HasPrefix(step.Uses, "actions/configure-pages@"):
				configure = i
				if jobName != "build" || step.ID != "pages" || step.If != pagesPublishCondition {
					t.Errorf("Pages configuration must be main-only: %+v", step)
				}
			case strings.Contains(step.Run, "make site-build"):
				validate = i
				wantBase := "${{ steps.pages.outcome == 'success' && format('{0}/', steps.pages.outputs.base_path) || '/agent-marketplace/' }}"
				if step.If != "" || step.Env["SITE_BASE"] != wantBase {
					t.Errorf("build must use configured Pages base, including an empty root base: %+v", step)
				}
				if step.Env["SITE_CLOUDFLARE_ANALYTICS_TOKEN"] != "${{ steps.pages.outcome == 'success' && vars.CLOUDFLARE_WEB_ANALYTICS_TOKEN || '' }}" {
					t.Errorf("only the publishing build may receive the analytics token: %+v", step)
				}
				commands := []string{
					"make site-build",
					"node --test tests/analytics.test.ts",
					"node --test tests/public-site.test.ts",
					"pnpm run build:docs",
					"node --test tests/docs-profile.test.ts",
				}
				previous := -1
				for _, command := range commands {
					index := strings.Index(step.Run, command)
					if index <= previous {
						t.Errorf("missing or out-of-order validation command %q: %s", command, step.Run)
					}
					previous = index
				}
			case strings.HasPrefix(step.Uses, "actions/upload-pages-artifact@"):
				upload = i
				if jobName != "build" || step.If != pagesPublishCondition ||
					step.With["path"] != "site/.vitepress/dist" {
					t.Errorf("only the validated public artifact may be uploaded from main: %+v", step)
				}
			}
		}
	}
	if configure < 0 || validate <= configure || upload <= validate {
		t.Errorf("expected configure < validate < upload, got %d, %d, %d", configure, validate, upload)
	}
	if len(deploy.Steps) != 1 || !strings.HasPrefix(deploy.Steps[0].Uses, "actions/deploy-pages@") ||
		deploy.Steps[0].ID != "deployment" {
		t.Errorf("deployment must use the native Pages action: %+v", deploy.Steps)
	}
}

// spec: site-deployment-profiles/other-branches-and-pull-requests-never-publish
func TestSiteWorkflowLimitsPublicationPermissions(t *testing.T) {
	workflow := readPagesWorkflow(t)
	if len(workflow.Permissions) != 0 {
		t.Errorf("workflow must not grant global permissions: %v", workflow.Permissions)
	}
	want := map[string]map[string]string{
		"build":  {"contents": "read", "pages": "read"},
		"deploy": {"pages": "write", "id-token": "write"},
	}
	if len(workflow.Jobs) != len(want) {
		t.Fatalf("unexpected website jobs: %v", workflow.Jobs)
	}
	for name, permissions := range want {
		if got := workflow.Jobs[name].Permissions; !maps.Equal(got, permissions) {
			t.Errorf("%s permissions = %v, want %v", name, got, permissions)
		}
	}
	if workflow.Jobs["deploy"].If != pagesPublishCondition {
		t.Error("pull requests and manually selected non-main branches must never deploy")
	}
	for _, step := range workflow.Jobs["build"].Steps {
		if strings.HasPrefix(step.Uses, "actions/configure-pages@") ||
			strings.HasPrefix(step.Uses, "actions/upload-pages-artifact@") {
			if step.If != pagesPublishCondition {
				t.Errorf("validation-only runs must not configure or upload Pages: %+v", step)
			}
		}
	}
}
