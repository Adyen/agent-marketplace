package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseV4NormalizesAllSourcesKindsAndSelectors(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v4
sources:
  shared:
    git: https://git.example/agents.git
    ref: main
    path: skills
    source_harness: claude
  local:
    directory: ./resources
  cloud:
    marketplace: HTTPS://Market.Example/api/
resources:
  shared:
    - skill/review
    - command/deploy
  local:
    include:
      - subagent/helper
      - hook/on-save
      - mcp/server
      - collection/team
    exclude: [command/*]
    required: true
    harnesses: [claude]
  cloud:
    include:
      - tools/skill/debug:
          revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      - tools/collection/onboarding
    tags: [platform]
    all: true
    registries: [tools]
    exclude: [tools/command/*, tools/skill/old]
`)
	cfg, err := ParseV4Config(data, "/repo/.agents/config.yaml", true)
	if err != nil {
		t.Fatalf("ParseV4Config() error = %v", err)
	}
	if len(cfg.Sources) != 3 || len(cfg.Requests) != 8 {
		t.Fatalf("sources/requests = %d/%d, want 3/8; %#v", len(cfg.Sources), len(cfg.Requests), cfg.Requests)
	}
	if cfg.Sources["cloud"].CanonicalURL != "https://market.example/api" {
		t.Fatalf("canonical marketplace URL = %q", cfg.Sources["cloud"].CanonicalURL)
	}
	if cfg.Sources["local"].URL != "/repo/.agents/resources" {
		t.Fatalf("relative directory URL = %q", cfg.Sources["local"].URL)
	}
	if got := cfg.Groups["cloud"].Include[0].Options.Revision; got == "" {
		t.Fatal("Marketplace revision was not retained")
	}
	if !cfg.Groups["cloud"].HasAll || !cfg.Groups["cloud"].HasTags || !cfg.Groups["cloud"].HasRegistries {
		t.Fatalf("dynamic selector presence was lost: %#v", cfg.Groups["cloud"])
	}
	if cfg.Groups["local"].Required == nil || !*cfg.Groups["local"].Required {
		t.Fatal("required selector was not retained")
	}
}

func TestV4RequiredExplicitRequestSurvivesExclusion(t *testing.T) {
	cfg, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources: {s: {git: https://example.test/a.git, ref: main}}
resources:
  s:
    include: [skill/protected]
    exclude: [skill/*]
    required: true
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Requests) != 1 || !cfg.Requests[0].Required {
		t.Fatalf("required request was excluded: %#v", cfg.Requests)
	}
}

func TestV4CompactAndExpandedFormsAreEquivalent(t *testing.T) {
	compact := []byte(`apiVersion: agent-marketplace/v4
sources: {s: {git: https://example.test/a.git, ref: main}}
resources: {s: [skill/a, command/b]}
`)
	expanded := []byte(`apiVersion: agent-marketplace/v4
sources: {s: {git: https://example.test/a.git, ref: main}}
resources:
  s:
    include: [skill/a, command/b]
`)
	a, err := ParseV4Config(compact, "config.yaml", true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseV4Config(expanded, "config.yaml", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Requests) != len(b.Requests) {
		t.Fatalf("request count differs: %d/%d", len(a.Requests), len(b.Requests))
	}
	for i := range a.Requests {
		if a.Requests[i].Locator != b.Requests[i].Locator || a.Requests[i].Options != b.Requests[i].Options {
			t.Fatalf("request %d differs: %#v/%#v", i, a.Requests[i], b.Requests[i])
		}
	}
}

func TestV4RejectsStrictInvalidInputs(t *testing.T) {
	cases := map[string]string{
		"missing version": `sources: {s: {git: https://example.test/a.git, ref: main}}`,
		"legacy key": `apiVersion: agent-marketplace/v4
registries: []`,
		"unknown key": `apiVersion: agent-marketplace/v4
sources: {s: {git: https://example.test/a.git, ref: main, nope: x}}`,
		"duplicate key": "apiVersion: agent-marketplace/v4\nsources:\n  s: {git: https://example.test/a.git, git: https://example.test/b.git, ref: main}\n",
		"alias":         "apiVersion: agent-marketplace/v4\nsources:\n  s: &source {git: https://example.test/a.git, ref: main}\n  t: *source\n",
		"merge key":     "apiVersion: agent-marketplace/v4\nsources:\n  base: &source {git: https://example.test/a.git, ref: main}\n  s: {<<: *source}\n",
		"wrong type": `apiVersion: agent-marketplace/v4
sources: []`,
		"undefined source": `apiVersion: agent-marketplace/v4
resources: {missing: [skill/a]}`,
		"duplicate request": `apiVersion: agent-marketplace/v4
sources: {s: {git: https://example.test/a.git, ref: main}}
resources: {s: [skill/a, skill/a]}`,
		"dynamic marketplace unbounded": `apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources: {m: {all: true}}`,
		"revision on git": `apiVersion: agent-marketplace/v4
sources: {s: {git: https://example.test/a.git, ref: main}}
resources: {s: [{skill/a: {revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}}]}`,
		"wildcard include": `apiVersion: agent-marketplace/v4
sources: {s: {git: https://example.test/a.git, ref: main}}
resources: {s: {include: [skill/*]}}`,
		"collection alias": `apiVersion: agent-marketplace/v4
sources: {s: {git: https://example.test/a.git, ref: main}}
resources: {s: [{collection/team: {as: renamed}}]}`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseV4Config([]byte(input), "config.yaml", true)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestV4IncludeTreeBindsSourcesAndPreservesPrecedence(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	child := filepath.Join(dir, "child.yaml")
	if err := os.WriteFile(child, []byte(`sources: {shared: {git: https://example.test/a.git, ref: main}}
resources:
  shared:
    include: [skill/old]
    exclude: [skill/blocked]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte(`apiVersion: agent-marketplace/v4
include: child.yaml
sources: {shared: {git: https://example.test/a.git, ref: main}}
resources:
  shared:
    include: [skill/blocked, skill/new]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadV4ConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Requests) != 3 {
		t.Fatalf("normalized requests = %d, want 3", len(cfg.Requests))
	}
	if len(cfg.Files) != 2 {
		t.Fatalf("files = %v", cfg.Files)
	}
	if len(cfg.Groups["shared"].Decisions) != 4 {
		t.Fatalf("decisions = %#v", cfg.Groups["shared"].Decisions)
	}
	if cfg.Groups["shared"].Decisions[0].Provenance.Precedence >= cfg.Groups["shared"].Decisions[2].Provenance.Precedence {
		t.Fatalf("include precedence did not preserve include-tree order: %#v", cfg.Groups["shared"].Decisions)
	}
}

func TestV4SharedIncludeDAGExpandsPerBranch(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared.yaml")
	branchA := filepath.Join(dir, "a.yaml")
	branchB := filepath.Join(dir, "b.yaml")
	root := filepath.Join(dir, "config.yaml")
	write := func(path, data string) {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(shared, `sources: {shared: {git: https://example.test/shared.git, ref: main}}
resources:
  shared:
    include: [skill/shared]
`)
	write(branchA, `include: shared.yaml
resources:
  shared:
    include: [skill/a]
`)
	write(branchB, `include: shared.yaml
resources:
  shared:
    include: [skill/b]
`)
	write(root, `apiVersion: agent-marketplace/v4
include: [a.yaml, b.yaml]
`)

	cfg, err := LoadV4ConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(cfg.Requests), 3; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
	if got, want := len(cfg.Files), 5; got != want {
		t.Fatalf("files = %v, want shared fragment loaded once per branch", cfg.Files)
	}
	decisions := cfg.Groups["shared"].Decisions
	if got, want := len(decisions), 4; got != want {
		t.Fatalf("decisions = %#v, want both branch contributions", decisions)
	}
	for i, want := range []struct {
		locator    string
		precedence int
	}{
		{locator: "skill/shared", precedence: 0},
		{locator: "skill/a", precedence: 1},
		{locator: "skill/shared", precedence: 2},
		{locator: "skill/b", precedence: 3},
	} {
		if decisions[i].Locator != want.locator || decisions[i].Provenance.Precedence != want.precedence {
			t.Fatalf("decision[%d] = %#v, want locator %q at precedence %d", i, decisions[i], want.locator, want.precedence)
		}
	}
}

func TestV4IncludeRejectsConflictingSourceBinding(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	child := filepath.Join(dir, "child.yaml")
	if err := os.WriteFile(child, []byte("sources: {shared: {git: https://example.test/one.git, ref: main}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("apiVersion: agent-marketplace/v4\ninclude: child.yaml\nsources: {shared: {git: https://example.test/two.git, ref: main}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadV4ConfigFromPath(root)
	if err == nil || !strings.Contains(err.Error(), "source-conflict") {
		t.Fatalf("error = %v, want source conflict", err)
	}
}

func TestV4DiagnosticsHaveStableCodes(t *testing.T) {
	_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nresources: {x: []}\n"))
	if err == nil {
		t.Fatal("expected undefined source error")
	}
	var diagnostic *V4ValidationError
	if !errors.As(err, &diagnostic) || diagnostic.Code != "v4.undefined-source" {
		t.Fatalf("diagnostic = %#v, error = %v", diagnostic, err)
	}
	if !strings.Contains(err.Error(), "undefined source") {
		t.Fatalf("source-binding context missing: %v", err)
	}
}

func TestV4IncludeReplacesEarlierRequestOptionsWholesale(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.yaml")
	child := filepath.Join(dir, "child.yaml")
	oldDigest := "sha256:" + strings.Repeat("a", 64)
	newDigest := "sha256:" + strings.Repeat("b", 64)
	if err := os.WriteFile(child, []byte("sources: {m: {marketplace: https://market.example}}\nresources:\n  m:\n    include:\n      - tools/skill/review: {as: child, revision: "+oldDigest+"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("apiVersion: agent-marketplace/v4\ninclude: child.yaml\nsources: {m: {marketplace: https://market.example}}\nresources:\n  m:\n    include:\n      - tools/skill/review: {as: root}\n      - tools/skill/deploy: {revision: "+newDigest+"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadV4ConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Requests) != 2 {
		t.Fatalf("requests = %#v", cfg.Requests)
	}
	var review, deploy V4ResourceRequest
	for _, request := range cfg.Requests {
		switch request.Locator {
		case "tools/skill/review":
			review = request
		case "tools/skill/deploy":
			deploy = request
		}
	}
	if review.Options.As != "root" || review.Options.Revision != "" {
		t.Fatalf("root options did not replace child options: %#v", review.Options)
	}
	if deploy.Options.As != "deploy" || deploy.Options.Revision != newDigest {
		t.Fatalf("request options = %#v", deploy.Options)
	}
	for _, decision := range cfg.Groups["m"].Decisions {
		if decision.Locator == "tools/skill/review" && decision.Action == V4SelectionExplicit && decision.Request != nil && decision.Provenance.Line == 7 {
			if decision.Request.Options.As != "root" || decision.Request.Options.Revision != "" {
				t.Fatalf("selected decision retained child options: %#v", decision.Request.Options)
			}
			return
		}
	}
	t.Fatalf("root review decision was not associated with its request: %#v", cfg.Groups["m"].Decisions)
}

func TestV4DecisionProvenanceIncludesExclusionLineAndSelectedRequest(t *testing.T) {
	file := "/configs/v4.yaml"
	cfg, err := ParseV4Config([]byte("apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: https://market.example}}\nresources:\n  m:\n    include:\n      - tools/skill/keep\n    exclude:\n      - tools/skill/blocked\n"), file, true)
	if err != nil {
		t.Fatal(err)
	}
	decisions := cfg.Groups["m"].Decisions
	if len(decisions) != 2 {
		t.Fatalf("decisions = %#v", decisions)
	}
	if decisions[0].Request == nil || decisions[0].Request.Provenance[0].File != file || decisions[0].Request.Provenance[0].Line != 6 {
		t.Fatalf("selected request provenance = %#v", decisions[0].Request)
	}
	if decisions[1].Action != V4SelectionExclude || decisions[1].Provenance.File != file || decisions[1].Provenance.Line != 8 {
		t.Fatalf("exclusion provenance = %#v", decisions[1].Provenance)
	}
}

func TestV4MarketplaceExclusionRejectsKindWildcard(t *testing.T) {
	_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: https://market.example}}\nresources: {m: {exclude: [skill/*]}}\n"))
	if err == nil || !strings.Contains(err.Error(), "registry/kind/*") {
		t.Fatalf("error = %v, want Marketplace exclusion grammar diagnostic", err)
	}
}

func TestV4MarketplaceExclusionDoesNotMatchSuffix(t *testing.T) {
	cfg, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include: [tools/skill/command]
    exclude: [skill/command/*]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Requests) != 1 || cfg.Requests[0].Locator != "tools/skill/command" {
		t.Fatalf("requests = %#v, want the non-matching registry to remain selected", cfg.Requests)
	}
}

func TestV4AliasUsesSafeFilenameRulesAndCollectionsRejectExplicitAs(t *testing.T) {
	cases := map[string]string{
		"reserved default":              "apiVersion: agent-marketplace/v4\nsources: {s: {git: https://example.test/a.git, ref: main}}\nresources: {s: [skill/CON]}\n",
		"collection explicit same name": "apiVersion: agent-marketplace/v4\nsources: {s: {git: https://example.test/a.git, ref: main}}\nresources: {s: {include: [{collection/team: {as: team}}]}}\n",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseV4([]byte(data)); err == nil {
				t.Fatal("expected invalid alias")
			}
		})
	}
}

func TestV4MarketplaceURLUsesCanonicalEndpointSafety(t *testing.T) {
	for _, value := range []string{
		"https://market.example/%2fprivate",
		"https://market.example/%5Cprivate",
		"https://user:secret@market.example/api",
		"https://market.example/api?token=secret",
		"https://market.example/api#fragment",
	} {
		t.Run(value, func(t *testing.T) {
			_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: " + value + "}}\n"))
			if err == nil {
				t.Fatal("expected unsafe endpoint to be rejected")
			}
		})
	}
}

func TestV4HarnessValidationAndEmptyOptOut(t *testing.T) {
	for name, data := range map[string]string{
		"source": "apiVersion: agent-marketplace/v4\nsources: {s: {git: https://example.test/a.git, ref: main, source_harness: nope}}\n",
		"group":  "apiVersion: agent-marketplace/v4\nsources: {s: {git: https://example.test/a.git, ref: main}}\nresources: {s: {include: [skill/a], harnesses: [nope]}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseV4([]byte(data)); err == nil {
				t.Fatal("expected invalid harness")
			}
		})
	}
	cfg, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources: {s: {git: https://example.test/a.git, ref: main}}\nresources: {s: {include: [skill/a], harnesses: []}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Groups["s"].HasHarnesses || len(cfg.Requests) != 1 || !cfg.Requests[0].HasHarnesses || len(cfg.Requests[0].Harnesses) != 0 {
		t.Fatalf("empty harness opt-out was not retained: %#v", cfg)
	}
}

func TestV4DirectorySourceSafety(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	outside := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"traversal":      "../outside",
		"symlink escape": "link",
	} {
		t.Run(name, func(t *testing.T) {
			data := []byte("apiVersion: agent-marketplace/v4\nsources: {s: {directory: " + source + "}}\n")
			if err := os.WriteFile(root, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadV4ConfigFromPath(root); err == nil {
				t.Fatal("expected unsafe directory source to be rejected")
			}
		})
	}
	if _, err := ParseV4Config([]byte("apiVersion: agent-marketplace/v4\nsources: {s: {directory: "+outside+"}}\n"), root, true); err != nil {
		t.Fatalf("absolute directory source was rejected: %v", err)
	}
}

func TestV4GitSupportsHostlessFileURLOnly(t *testing.T) {
	for name, origin := range map[string]string{
		"hostless file": "file:///tmp/registry.git",
		"remote":        "https://example.test/registry.git",
	} {
		t.Run(name, func(t *testing.T) {
			data := []byte("apiVersion: agent-marketplace/v4\nsources: {s: {git: " + origin + ", ref: main}}\n")
			if _, err := ParseV4(data); err != nil {
				t.Fatalf("supported origin rejected: %v", err)
			}
		})
	}
	for _, origin := range []string{"file://localhost/tmp/registry.git", "git://example.test/registry.git", "../registry.git"} {
		t.Run(origin, func(t *testing.T) {
			data := []byte("apiVersion: agent-marketplace/v4\nsources: {s: {git: " + origin + ", ref: main}}\n")
			if _, err := ParseV4(data); err == nil {
				t.Fatal("unsupported origin accepted")
			}
		})
	}
}

func TestV4GitRefsUseHardenedSharedValidation(t *testing.T) {
	valid := []string{"main", "feature/release-1.2", "refs/tags/v1.0.0", "deadbeef"}
	for _, ref := range valid {
		t.Run("valid/"+ref, func(t *testing.T) {
			data := []byte("apiVersion: agent-marketplace/v4\nsources: {s: {git: https://example.test/repo.git, ref: " + ref + "}}\n")
			if _, err := ParseV4(data); err != nil {
				t.Fatalf("valid ref rejected: %v", err)
			}
		})
	}
	invalid := []string{"-force", "/etc/passwd", "~main", "../main", "main@{1}", "main.lock", "main.", "main/", ".hidden", "foo//bar", "foo.lock/bar", "main with space", "main\\tbranch"}
	for _, ref := range invalid {
		t.Run("invalid/"+ref, func(t *testing.T) {
			data := []byte("apiVersion: agent-marketplace/v4\nsources: {s: {git: https://example.test/repo.git, ref: " + ref + "}}\n")
			if _, err := ParseV4(data); err == nil {
				t.Fatal("unsafe ref accepted")
			}
		})
	}
	for name, data := range map[string]string{
		"git leading whitespace":  "apiVersion: agent-marketplace/v4\nsources: {s: {git: \" https://example.test/repo.git\", ref: main}}\n",
		"git trailing whitespace": "apiVersion: agent-marketplace/v4\nsources: {s: {git: \"https://example.test/repo.git \", ref: main}}\n",
		"ref leading whitespace":  "apiVersion: agent-marketplace/v4\nsources: {s: {git: https://example.test/repo.git, ref: \" main\"}}\n",
		"ref trailing whitespace": "apiVersion: agent-marketplace/v4\nsources: {s: {git: https://example.test/repo.git, ref: \"main \"}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseV4([]byte(data)); err == nil {
				t.Fatal("whitespace-padded Git source accepted")
			}
		})
	}
}

func TestV4GitScpOriginsRejectOptionPathsAndBackslashes(t *testing.T) {
	for _, origin := range []string{"example.com:org/repo.git", "git@example.com:org/repo.git"} {
		t.Run("valid/"+origin, func(t *testing.T) {
			data := []byte("apiVersion: agent-marketplace/v4\nsources: {s: {git: " + origin + ", ref: main}}\n")
			if _, err := ParseV4(data); err != nil {
				t.Fatalf("valid scp origin rejected: %v", err)
			}
		})
	}
	for _, origin := range []string{
		"example.com:-oProxyCommand=evil",
		"git@example.com:-oProxyCommand=evil",
		`example.com:org\\repo.git`,
		`git@example.com:org\\repo.git`,
	} {
		t.Run("invalid/"+origin, func(t *testing.T) {
			data := []byte("apiVersion: agent-marketplace/v4\nsources: {s: {git: " + origin + ", ref: main}}\n")
			if _, err := ParseV4(data); err == nil {
				t.Fatal("unsafe scp origin accepted")
			}
		})
	}
}

func TestV4TelemetryAuxiliaryValuesValidateAtParseTime(t *testing.T) {
	for name, telemetry := range map[string]string{
		"endpoint":         "endpoint: ftp://telemetry.example",
		"retention_policy": "retention_policy: not-a-url",
		"header":           "headers: {Authorization: {env: TOKEN, file: secret.txt}}",
	} {
		t.Run(name, func(t *testing.T) {
			data := []byte("apiVersion: agent-marketplace/v4\ntelemetry:\n  " + telemetry + "\n")
			if _, err := ParseV4(data); err == nil {
				t.Fatal("malformed telemetry value accepted")
			}
		})
	}
	caDir := t.TempDir()
	if _, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\ntelemetry:\n  ca_file: " + caDir + "\n")); err == nil {
		t.Fatal("directory CA file accepted")
	}
	data := []byte("apiVersion: agent-marketplace/v4\ntelemetry:\n  endpoint: http://telemetry.example\n  retention_policy: https://policy.example/retention\n  headers: {Authorization: {env: TOKEN}}\n")
	if _, err := ParseV4(data); err != nil {
		t.Fatalf("parser-safe telemetry validation unexpectedly rejected valid syntax: %v", err)
	}
}

func TestV4IncludeMergesAuxiliaryPolicyByField(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.yaml")
	child := filepath.Join(dir, "child.yaml")
	if err := os.WriteFile(child, []byte(`backup: {enabled: true, max_days: 7}
logging: {level: debug, file: child.log, max_size_mb: 4}
allowed_registries: [child-registry]
allowed_marketplaces: [child-marketplace]
materialization: {mode: file, targets: {devin: file, factory: file}}
telemetry:
  level: basic
  endpoint: https://child.example/otlp
  retention_policy: https://child.example/retention
  headers: {Child: {env: CHILD_TOKEN}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte(`apiVersion: agent-marketplace/v4
include: child.yaml
backup: {max_per_day: 3}
logging: {max_age_days: 5}
allowed_registries: [root-registry]
allowed_marketplaces: [root-marketplace]
materialization: {mode: symlink, targets: {factory: symlink}}
telemetry:
  endpoint: https://root.example/otlp
  community_reporting: false
  headers: {Root: {env: ROOT_TOKEN}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadV4ConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backup == nil || cfg.Backup.Enabled == nil || !*cfg.Backup.Enabled || cfg.Backup.MaxDays == nil || *cfg.Backup.MaxDays != 7 || cfg.Backup.MaxPerDay == nil || *cfg.Backup.MaxPerDay != 3 {
		t.Fatalf("backup fields were not composed: %#v", cfg.Backup)
	}
	if cfg.Logging == nil || cfg.Logging.Level == nil || *cfg.Logging.Level != "debug" || cfg.Logging.File == nil || *cfg.Logging.File != "child.log" || cfg.Logging.MaxSizeMB == nil || *cfg.Logging.MaxSizeMB != 4 || cfg.Logging.MaxAgeDays == nil || *cfg.Logging.MaxAgeDays != 5 {
		t.Fatalf("logging fields were not composed: %#v", cfg.Logging)
	}
	if cfg.AllowedRegistries == nil || (*cfg.AllowedRegistries)[0] != "child-registry" || cfg.AllowedMarketplaces == nil || (*cfg.AllowedMarketplaces)[0] != "child-marketplace" {
		t.Fatalf("allowlist authority was not preserved: %#v/%#v", cfg.AllowedRegistries, cfg.AllowedMarketplaces)
	}
	if cfg.Materialization == nil || cfg.Materialization.Mode != "symlink" {
		t.Fatalf("materialization mode was not overridden: %#v", cfg.Materialization)
	}
	if got := cfg.Materialization.Targets; len(got) != 2 || got["devin"] != "file" || got["factory"] != "symlink" {
		t.Fatalf("materialization target map was not composed field-wise: %#v", got)
	}
	if cfg.Telemetry == nil || cfg.Telemetry.Level != "basic" || cfg.Telemetry.Endpoint != "https://root.example/otlp" || cfg.Telemetry.RetentionPolicy != "https://child.example/retention" || cfg.Telemetry.CommunityReporting == nil || *cfg.Telemetry.CommunityReporting || len(cfg.Telemetry.Headers) != 2 {
		t.Fatalf("telemetry fields were not composed: %#v", cfg.Telemetry)
	}
}

func TestV4AuxiliaryFieldsAreRetainedAndStrict(t *testing.T) {
	cfg, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
harnesses: []
allowed_registries: ["example"]
backup: {enabled: false, max_days: 3, max_per_day: 2}
logging: {level: debug, max_size_mb: 4}
materialization: {mode: file}
resources: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HasHarnesses || cfg.Backup == nil || cfg.Logging == nil || cfg.Materialization == nil || cfg.AllowedRegistries == nil || (*cfg.AllowedRegistries)[0] != "example" {
		t.Fatalf("auxiliary fields were discarded: %#v", cfg)
	}
	_, err = ParseV4([]byte("apiVersion: agent-marketplace/v4\nbackup: {unsupported: true}\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want strict auxiliary diagnostic", err)
	}
}
