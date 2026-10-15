package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"gopkg.in/yaml.v3"
)

func TestEveryHarnessHasExplicitAuditCoverage(t *testing.T) {
	body, err := os.ReadFile("baselines.json")
	if err != nil {
		t.Fatal(err)
	}
	var baselines map[string]baseline
	if err := json.Unmarshal(body, &baselines); err != nil {
		t.Fatal(err)
	}
	if err := validateBaselines(baselines, sources); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range sources {
		if seen[s.Name] || (!target.IsValid(s.Harness) && s.Format == "") || !trustedURL(s.URL) {
			t.Fatalf("invalid source: %#v", s)
		}
		seen[s.Name] = true
	}
	for _, definition := range target.Definitions() {
		harness := string(definition.Target)
		if coverage[harness] == "" {
			t.Fatalf("no coverage declaration for %s", harness)
		}
		body, err := os.ReadFile("../../" + catalogPath(harness))
		if err != nil {
			t.Fatal(err)
		}
		var c catalog
		if err := yaml.Unmarshal(body, &c); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, s := range sources {
			found = found || (s.Domain == "" && s.Harness == harness)
		}
		if found != (len(c.Surfaces) > 0) {
			t.Fatalf("catalog surface and audit coverage disagree for %s", harness)
		}
	}
}

func TestNightlyAuditFailsAndPreservesReports(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/harness-images.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On   map[string]yaml.Node `yaml:"on"`
		Jobs map[string]struct {
			Needs       any               `yaml:"needs"`
			If          string            `yaml:"if"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Name     string `yaml:"name"`
				Run      string `yaml:"run"`
				If       string `yaml:"if"`
				Uses     string `yaml:"uses"`
				Continue bool   `yaml:"continue-on-error"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	job, exists := workflow.Jobs["model-catalog-audit"]
	if !exists {
		t.Fatal("nightly model catalog audit job missing")
	}
	if _, scheduled := workflow.On["schedule"]; !scheduled || job.Needs != nil || job.If != "github.ref == format('refs/heads/{0}', github.event.repository.default_branch)" {
		t.Fatal("audit must run on nightly/default-branch dispatch independently of image publication")
	}
	if len(job.Permissions) != 1 || job.Permissions["contents"] != "read" {
		t.Fatal("audit must not have repository write permissions")
	}
	check, summary, artifacts, offline := false, false, false, false
	for _, step := range job.Steps {
		if step.Continue {
			t.Fatal("audit failures must not be ignored")
		}
		if strings.Contains(step.Run, "./tools/model-catalog-audit check") {
			check = strings.Contains(step.Run, ">") && !strings.Contains(step.Run, "||") && !strings.Contains(step.Run, "| tee")
		}
		if strings.Contains(step.Run, "./tools/model-catalog-audit summary") {
			summary = step.If == "always()" && strings.Contains(step.Run, "GITHUB_STEP_SUMMARY")
		}
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
			artifacts = step.If == "always()"
		}
		offline = offline || strings.Contains(step.Run, "go test ./tools/model-catalog-audit")
	}
	if !check || !summary || !artifacts || !offline {
		t.Fatalf("nightly audit coverage: check=%t summary=%t artifacts=%t offline=%t", check, summary, artifacts, offline)
	}
}
