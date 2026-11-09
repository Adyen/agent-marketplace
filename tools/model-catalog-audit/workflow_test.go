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
		if seen[s.Name] || !target.IsValid(s.Harness) || !trustedURL(s.URL) {
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

// spec: model-catalog-audit/cursor-cli-output-lists-model-identifiers
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
				Name     string            `yaml:"name"`
				Run      string            `yaml:"run"`
				If       string            `yaml:"if"`
				Uses     string            `yaml:"uses"`
				With     map[string]string `yaml:"with"`
				Env      map[string]string `yaml:"env"`
				Continue bool              `yaml:"continue-on-error"`
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
	if len(job.Permissions) != 2 || job.Permissions["contents"] != "read" || job.Permissions["packages"] != "read" {
		t.Fatalf("audit permissions = %v, want only contents:read and packages:read", job.Permissions)
	}
	loginUses := map[string]int{}
	for _, otherJob := range workflow.Jobs {
		for _, step := range otherJob.Steps {
			if strings.HasPrefix(step.Uses, "docker/login-action@") {
				loginUses[step.Uses]++
			}
		}
	}
	check, summary, artifacts, offline := false, false, false, false
	prepareIndex, loginIndex, captureIndex, checkIndex := -1, -1, -1, -1
	for i, step := range job.Steps {
		switch step.Name {
		case "Prepare model audit diagnostics":
			prepareIndex = i
			if !strings.Contains(step.Run, `mkdir -p "$RUNNER_TEMP/model-catalog-audit"`) {
				t.Fatal("audit report directory must exist before the Cursor capture")
			}
		case "Log in to GitHub Container Registry":
			loginIndex = i
			if !step.Continue || !strings.HasPrefix(step.Uses, "docker/login-action@") || loginUses[step.Uses] < 2 ||
				step.With["registry"] != "ghcr.io" || step.With["username"] != "${{ github.actor }}" ||
				step.With["password"] != "${{ secrets.GITHUB_TOKEN }}" {
				t.Fatalf("invalid GHCR login step or unshared pin: %#v (login uses: %v)", step, loginUses)
			}
		case "Capture Cursor CLI model inventory":
			captureIndex = i
			if !step.Continue || !strings.Contains(step.Run, `docker run --rm ghcr.io/adyen/agent-marketplace/harness-cursor:ci agent models`) ||
				!strings.Contains(step.Run, `> "$AUDIT_REPORT_DIR/cursor-cli-models.txt" 2> "$AUDIT_REPORT_DIR/cursor-cli-models.log"`) ||
				strings.Contains(step.Run, "CURSOR_API_KEY") || strings.Contains(step.Run, "secrets.") || len(step.Env) != 0 {
				t.Fatalf("invalid keyless Cursor inventory capture step: %#v", step)
			}
		}
		if step.Continue && step.Name != "Log in to GitHub Container Registry" && step.Name != "Capture Cursor CLI model inventory" {
			t.Fatal("audit failures must not be ignored")
		}
		if strings.Contains(step.Run, "./tools/model-catalog-audit check") {
			checkIndex = i
			check = strings.Contains(step.Run, `-cursor-models "$AUDIT_REPORT_DIR/cursor-cli-models.txt"`) &&
				strings.Contains(step.Run, ">") && !strings.Contains(step.Run, "||") && !strings.Contains(step.Run, "| tee")
		}
		if strings.Contains(step.Run, "./tools/model-catalog-audit summary") {
			summary = step.If == "always()" && strings.Contains(step.Run, "GITHUB_STEP_SUMMARY")
		}
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
			artifacts = step.If == "always()"
		}
		offline = offline || strings.Contains(step.Run, "go test ./tools/model-catalog-audit")
	}
	if !check || !summary || !artifacts || !offline || prepareIndex < 0 || loginIndex <= prepareIndex || captureIndex <= loginIndex || checkIndex <= captureIndex {
		t.Fatalf("nightly audit coverage: check=%t summary=%t artifacts=%t offline=%t prepare=%d login=%d capture=%d check=%d",
			check, summary, artifacts, offline, prepareIndex, loginIndex, captureIndex, checkIndex)
	}
}

func TestNightlyDriftProposalOpensReviewablePR(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/harness-images.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Needs           any               `yaml:"needs"`
			If              string            `yaml:"if"`
			ContinueOnError bool              `yaml:"continue-on-error"`
			Permissions     map[string]string `yaml:"permissions"`
			Steps           []struct {
				Run      string            `yaml:"run"`
				Uses     string            `yaml:"uses"`
				With     map[string]string `yaml:"with"`
				Continue bool              `yaml:"continue-on-error"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	job, exists := workflow.Jobs["model-catalog-drift-pr"]
	if !exists {
		t.Fatal("drift proposal job missing")
	}
	if job.Needs != "model-catalog-audit" || job.If != "always() && needs.model-catalog-audit.result == 'failure' && github.ref == format('refs/heads/{0}', github.event.repository.default_branch)" || job.ContinueOnError {
		t.Fatalf("drift proposal gating: needs=%v if=%q continue-on-error=%t", job.Needs, job.If, job.ContinueOnError)
	}
	if len(job.Permissions) != 2 || job.Permissions["contents"] != "write" || job.Permissions["pull-requests"] != "write" {
		t.Fatalf("drift proposal permissions = %v, want exactly contents and pull-requests write", job.Permissions)
	}
	propose, download, humanCheck, lease := false, false, false, false
	for _, step := range job.Steps {
		if step.Continue {
			t.Fatal("drift proposal must not ignore step failures")
		}
		propose = propose || strings.Contains(step.Run, "./tools/model-catalog-audit propose")
		humanCheck = humanCheck || strings.Contains(step.Run, `"$(git log -1 --format='%ae %ce' FETCH_HEAD)" != "$bot_email $bot_email"`)
		lease = lease || strings.Contains(step.Run, `git push --force-with-lease="refs/heads/$branch:$remote_sha" origin "HEAD:refs/heads/$branch"`)
		if strings.Contains(step.Run, "git push --force origin") {
			t.Fatal("drift proposal must push with a lease, not an unconditional force")
		}
		if strings.HasPrefix(step.Uses, "actions/download-artifact@") {
			download = step.With["name"] == "model-catalog-audit-${{ github.run_id }}"
		}
	}
	if !propose || !download || !humanCheck || !lease {
		t.Fatalf("drift proposal steps: propose=%t download=%t author-and-committer-check=%t lease-push=%t", propose, download, humanCheck, lease)
	}
}
