package resolver

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func ks(v ...string) config.KindSelector {
	if len(v) == 0 {
		return config.KindSelector{}
	}
	return config.KindSelector{Include: v}
}
func kse(include, exclude []string) config.KindSelector {
	return config.KindSelector{Include: include, Exclude: exclude}
}

// setCatalogKeys sets CatalogKey on each registry entry, matching the
// production behavior where catalog loading populates this field.
func setCatalogKeys(cfg *config.Config) {
	for i := range cfg.Registries {
		entry := &cfg.Registries[i]
		if entry.IsLocal() {
			path, _ := entry.ResolveLocalPath("")
			entry.CatalogKey = entry.BundlePath(path)
		} else {
			entry.CatalogKey = cache.CatalogKeyForEntry(entry.URL, entry.Ref, entry.Path)
		}
	}
}

func ck(url, ref string, extraPath ...string) string {
	p := ""
	if len(extraPath) > 0 {
		p = extraPath[0]
	}
	return cache.CatalogKeyForEntry(url, ref, p)
}

func TestResolveBasicResolution(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:infra.git",
			Ref:    "main",
			Skills: kse([]string{"vm-lifecycle"}, []string{"firewall-rule-lifecycle"}),
			Tags:   []string{"k8s"},
		}},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:infra.git", "main"): {
			{Name: "vm-lifecycle", Tags: []string{"compute"}, Path: "/cache/infra/vm-lifecycle"},
			{Name: "k8s-app-onboarding", Tags: []string{"k8s", "helm"}, Path: "/cache/infra/k8s-app-onboarding"},
			{Name: "firewall-rule-lifecycle", Tags: []string{"k8s", "security"}, Path: "/cache/infra/firewall-rule-lifecycle"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no resolution errors, got %v", errs)
	}

	want := []ResolvedArtifact{
		{
			Entry:       catalog.Entry{Name: "k8s-app-onboarding", Tags: []string{"k8s", "helm"}, Path: "/cache/infra/k8s-app-onboarding"},
			RegistryURL: "git@example.com:infra.git",
			Ref:         "main",
			CatalogKey:  ck("git@example.com:infra.git", "main"),
		},
		{
			Entry:       catalog.Entry{Name: "vm-lifecycle", Tags: []string{"compute"}, Path: "/cache/infra/vm-lifecycle"},
			RegistryURL: "git@example.com:infra.git",
			Ref:         "main",
			CatalogKey:  ck("git@example.com:infra.git", "main"),
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected resolved skills %#v, got %#v", want, got)
	}
}

func TestResolveReportsUnmetRequirement(t *testing.T) {
	type warningExpectation struct {
		message     string
		remediation string
	}
	tests := []struct {
		name     string
		selected []string
		entries  []catalog.Entry
		want     []warningExpectation
	}{
		{
			name:     "single requirement",
			selected: []string{"smoke-test-kb"},
			entries: []catalog.Entry{
				{Kind: ir.KindSkill, Name: "smoke-test-kb", Resource: requirementTestSkill()},
				{Kind: ir.KindMcpServer, Name: "genai-atlassian"},
			},
			want: []warningExpectation{
				{
					message:     `skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`,
					remediation: `agent-marketplace install "genai-atlassian" --kind mcp`,
				},
			},
		},
		{
			name:     "plain artifact before multiple requirements",
			selected: []string{"plain-skill", "smoke-test-kb", "incident-kb"},
			entries: []catalog.Entry{
				{Kind: ir.KindSkill, Name: "plain-skill", Resource: ir.NewSkill("plain-skill", "", "", nil, nil, ir.SourceRef{})},
				{Kind: ir.KindSkill, Name: "smoke-test-kb", Resource: requirementTestSkill()},
				{Kind: ir.KindSkill, Name: "incident-kb", Resource: requirementTestSkillNamed("incident-kb", "genai-incident")},
				{Kind: ir.KindMcpServer, Name: "genai-atlassian"},
				{Kind: ir.KindMcpServer, Name: "genai-incident"},
			},
			want: []warningExpectation{
				{
					message:     `skill "incident-kb" requires mcp "genai-incident", but it is not selected`,
					remediation: `agent-marketplace install "genai-incident" --kind mcp`,
				},
				{
					message:     `skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`,
					remediation: `agent-marketplace install "genai-atlassian" --kind mcp`,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Registries: []config.RegistryEntry{{
				URL:    "git@example.com:skills.git",
				Ref:    "main",
				Skills: ks(tt.selected...),
			}}}
			setCatalogKeys(cfg)
			catalogs := map[string][]catalog.Entry{
				ck("git@example.com:skills.git", "main"): tt.entries,
			}

			resolved, errs := Resolve(cfg, catalogs, nil)
			if len(resolved) != len(tt.selected) {
				t.Fatalf("Resolve() returned %d resources, want %d", len(resolved), len(tt.selected))
			}
			if len(errs) != len(tt.want) {
				t.Fatalf("Resolve() errors = %v, want %d unmet requirement warnings", errs, len(tt.want))
			}
			for i, want := range tt.want {
				if !IsWarning(errs[i]) || !IsUnmetRequirement(errs[i]) {
					t.Fatalf("Resolve() error %d = %v, want unmet requirement warning", i, errs[i])
				}
				if got := errs[i].Error(); got != want.message {
					t.Fatalf("warning %d Error() = %q, want %q", i, got, want.message)
				}
				var warning *UnmetRequirementWarning
				if !errors.As(errs[i], &warning) {
					t.Fatalf("Resolve() error %d = %T, want *UnmetRequirementWarning", i, errs[i])
				}
				if got := warning.Remediation(); got != want.remediation {
					t.Fatalf("warning %d Remediation() = %q, want %q", i, got, want.remediation)
				}
			}
		})
	}
}

func TestUserFacingWarningsRedactURLCredentials(t *testing.T) {
	const secretURL = "https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.com/registry.git"
	warnings := []struct {
		name string
		text string
	}{
		{
			name: "requirement remediation",
			text: (&UnmetRequirementWarning{
				ResourceKind: ir.KindSkill,
				ResourceName: "smoke-test-kb",
				Requirement:  catalog.Requirement{Kind: ir.KindMcpServer, Name: "genai-atlassian", Registry: secretURL},
			}).Remediation(),
		},
		{
			name: "collision error",
			text: (&NameCollisionError{Kind: ir.KindSkill, Name: "shared", Sources: []string{secretURL}}).Error(),
		},
		{
			name: "collision remediation",
			text: (&NameCollisionError{Kind: ir.KindSkill, Name: "shared", Sources: []string{secretURL}}).Remediation(),
		},
		{
			name: "collision warning",
			text: (&NameCollisionWarning{Kind: ir.KindSkill, Name: "shared", Winner: secretURL, RunnersUp: []string{secretURL}}).Error(),
		},
		{
			name: "selected not found",
			text: (&SelectedNotFoundError{Kind: ir.KindSkill, Name: "missing", URL: secretURL, Ref: "main"}).Error(),
		},
	}

	for _, tt := range warnings {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(tt.text, "YOUR_TOKEN_HERE") {
				t.Fatalf("warning leaked credentials: %s", tt.text)
			}
			if !strings.Contains(tt.text, "https://***@example.com/registry.git") {
				t.Fatalf("warning did not preserve redacted URL: %s", tt.text)
			}
		})
	}
}

func TestResolveAcceptsSelectedRequirement(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:    "git@example.com:skills.git",
		Ref:    "main",
		Skills: ks("smoke-test-kb"),
		MCP:    ks("genai-atlassian"),
	}}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{
				Kind:     ir.KindSkill,
				Name:     "smoke-test-kb",
				Resource: requirementTestSkill(),
			},
			{Kind: ir.KindMcpServer, Name: "genai-atlassian"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(resolved) != 2 {
		t.Fatalf("Resolve() returned %d resources, want 2", len(resolved))
	}
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
}

func TestResolveRequiredExcludedPrerequisiteRemainsSelected(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{
			URL:    "git@example.com:skills.git",
			Ref:    "main",
			Skills: ks("smoke-test-kb"),
		},
		{
			URL:      "git@example.com:required.git",
			Ref:      "main",
			Required: true,
			MCP:      kse(nil, []string{"genai-atlassian"}),
		},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{Kind: ir.KindSkill, Name: "smoke-test-kb", Resource: requirementTestSkill()},
		},
		ck("git@example.com:required.git", "main"): {
			{Kind: ir.KindMcpServer, Name: "genai-atlassian"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	if len(resolved) != 2 {
		t.Fatalf("Resolve() returned %d resources, want 2", len(resolved))
	}
	if resolved[1].Kind != ir.KindMcpServer || resolved[1].Name != "genai-atlassian" {
		t.Fatalf("Resolve() prerequisite = %s:%s, want mcp:genai-atlassian", resolved[1].Kind, resolved[1].Name)
	}
}

func requirementTestSkill() ir.Resource {
	return requirementTestSkillNamed("smoke-test-kb", "genai-atlassian")
}

func requirementTestSkillNamed(name, requirement string) ir.Resource {
	return ir.NewSkill(
		name,
		"Smoke test knowledge",
		"",
		nil,
		map[string]any{
			"metadata": map[string]any{
				"requires": []any{"mcp:" + requirement},
			},
		},
		ir.SourceRef{},
	)
}

func TestResolveSelectsAllResourceKinds(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:       "git@example.com:infra.git",
			Ref:       "main",
			Skills:    ks("skill-a"),
			Commands:  ks("command-a"),
			Subagents: ks("subagent-a"),
			Hooks:     ks("hook-a"),
			MCP:       ks("mcp-a"),
		}},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:infra.git", "main"): {
			{Kind: ir.KindSkill, Name: "skill-a"},
			{Kind: ir.KindCommand, Name: "command-a"},
			{Kind: ir.KindSubagent, Name: "subagent-a"},
			{Kind: ir.KindHook, Name: "hook-a"},
			{Kind: ir.KindMcpServer, Name: "mcp-a"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 resources, got %d: %#v", len(got), got)
	}

	want := map[string]bool{
		skill.ResourceKey(ir.KindSkill, "skill-a"):       false,
		skill.ResourceKey(ir.KindCommand, "command-a"):   false,
		skill.ResourceKey(ir.KindSubagent, "subagent-a"): false,
		skill.ResourceKey(ir.KindHook, "hook-a"):         false,
		skill.ResourceKey(ir.KindMcpServer, "mcp-a"):     false,
	}
	for _, r := range got {
		key := skill.ResourceKey(r.Kind, r.Name)
		if _, ok := want[key]; !ok {
			t.Fatalf("unexpected resolved resource %s/%s", skill.KindLabel(r.Kind), r.Name)
		}
		want[key] = true
	}
	for key, seen := range want {
		if !seen {
			t.Fatalf("expected resource %q to be resolved", key)
		}
	}
}

func TestResolveAllowsSameNameAcrossKinds(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:infra.git",
			Ref:      "main",
			Skills:   ks("shared"),
			Commands: ks("shared"),
		}},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:infra.git", "main"): {
			{Kind: ir.KindSkill, Name: "shared"},
			{Kind: ir.KindCommand, Name: "shared"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(got) != 2 {
		t.Fatalf("expected both same-named resources to resolve, got %#v", got)
	}
}

func TestResolvePerEntryOverridesTopLevel(t *testing.T) {
	repo := &config.Config{
		Tags: []string{"ops"},
		Registries: []config.RegistryEntry{
			{
				URL:    "git@example.com:inherit.git",
				Ref:    "main",
				Skills: kse([]string{"default-skill"}, []string{"ops-skill"}),
			},
			{
				URL:    "git@example.com:override.git",
				Ref:    "main",
				Skills: kse([]string{"explicit-skill"}, []string{"network-skip"}),
				Tags:   []string{"network"},
			},
		},
	}

	merged := config.Merge(nil, repo)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:inherit.git", "main"): {
			{Name: "default-skill", Path: "/cache/inherit/default-skill"},
			{Name: "ops-skill", Tags: []string{"ops"}, Path: "/cache/inherit/ops-skill"},
		},
		ck("git@example.com:override.git", "main"): {
			{Name: "default-skill", Tags: []string{"ops"}, Path: "/cache/override/default-skill"},
			{Name: "explicit-skill", Path: "/cache/override/explicit-skill"},
			{Name: "network-picked", Tags: []string{"network"}, Path: "/cache/override/network-picked"},
			{Name: "network-skip", Tags: []string{"network"}, Path: "/cache/override/network-skip"},
		},
	}

	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no resolution errors, got %v", errs)
	}

	want := []ResolvedArtifact{
		{
			Entry:       catalog.Entry{Name: "default-skill", Path: "/cache/inherit/default-skill"},
			RegistryURL: "git@example.com:inherit.git",
			Ref:         "main",
			Source:      config.RepoCommitted,
			CatalogKey:  ck("git@example.com:inherit.git", "main"),
		},
		{
			Entry:       catalog.Entry{Name: "explicit-skill", Path: "/cache/override/explicit-skill"},
			RegistryURL: "git@example.com:override.git",
			Ref:         "main",
			Source:      config.RepoCommitted,
			CatalogKey:  ck("git@example.com:override.git", "main"),
		},
		{
			Entry:       catalog.Entry{Name: "network-picked", Tags: []string{"network"}, Path: "/cache/override/network-picked"},
			RegistryURL: "git@example.com:override.git",
			Ref:         "main",
			Source:      config.RepoCommitted,
			CatalogKey:  ck("git@example.com:override.git", "main"),
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected resolved skills %#v, got %#v", want, got)
	}
}

func TestResolveDetectsNameCollisionAcrossDifferentRegistries(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:registry-a.git", Ref: "main", Skills: ks("shared-skill", "only-a")},
			{URL: "git@example.com:registry-b.git", Ref: "main", Skills: ks("shared-skill", "only-b")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:registry-a.git", "main"): {
			{Name: "shared-skill", Path: "/cache/a/shared-skill"},
			{Name: "only-a", Path: "/cache/a/only-a"},
		},
		ck("git@example.com:registry-b.git", "main"): {
			{Name: "shared-skill", Path: "/cache/b/shared-skill"},
			{Name: "only-b", Path: "/cache/b/only-b"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected one collision warning, got %v", errs)
	}
	if !IsNameCollision(errs[0]) {
		t.Fatalf("expected collision type, got %T: %v", errs[0], errs[0])
	}
	if !IsWarning(errs[0]) {
		t.Fatalf("expected collision to be a warning, got %T: %v", errs[0], errs[0])
	}

	if !strings.Contains(errs[0].Error(), "shared-skill") || !strings.Contains(errs[0].Error(), "registry-a") || !strings.Contains(errs[0].Error(), "registry-b") {
		t.Fatalf("expected collision warning to mention shared skill and both sources, got %v", errs[0])
	}

	// The colliding skill is now resolved (winner picked) rather than skipped.
	// alphabetically-first URL (registry-a) wins as tiebreaker since both have same precedence.
	gotByName := make(map[string]ResolvedArtifact)
	for _, a := range got {
		gotByName[a.Name] = a
	}

	if _, ok := gotByName["shared-skill"]; !ok {
		names := make([]string, len(got))
		for i, a := range got {
			names[i] = a.Name
		}
		t.Fatalf("expected colliding shared-skill to be resolved, got %v", names)
	}
	if gotByName["shared-skill"].RegistryURL != "git@example.com:registry-a.git" {
		t.Fatalf("expected colliding shared-skill to come from registry-a (alpha winner), got %s", gotByName["shared-skill"].RegistryURL)
	}

	// Verify the warning mentions the normalized winner URL and runner-up.
	var warn *NameCollisionWarning
	if !errors.As(errs[0], &warn) {
		t.Fatalf("expected NameCollisionWarning, got %T", errs[0])
	}
	if warn.Winner != "git@example.com:registry-a" {
		t.Fatalf("expected winner to be normalized registry-a, got %s", warn.Winner)
	}
	if len(warn.RunnersUp) != 1 || warn.RunnersUp[0] != "git@example.com:registry-b" {
		t.Fatalf("expected runners-up [git@example.com:registry-b], got %v", warn.RunnersUp)
	}
	if _, ok := gotByName["only-a"]; !ok {
		t.Fatal("expected only-a to be resolved")
	}
	if _, ok := gotByName["only-b"]; !ok {
		t.Fatal("expected only-b to be resolved")
	}
}

func TestResolveSameURLDifferentRefsIsCollision(t *testing.T) {
	// Same URL but different refs are different catalog sources.
	// If both select the same skill name, it's a collision — but the resolver
	// now picks a deterministic winner and emits a warning.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:registry.git", Ref: "v1.0", Skills: ks("shared-skill")},
			{URL: "git@example.com:registry.git", Ref: "v2.0", Skills: ks("shared-skill", "newer-only")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:registry.git", "v1.0"): {
			{Name: "shared-skill", Path: "/cache/registry/v1/shared-skill"},
		},
		ck("git@example.com:registry.git", "v2.0"): {
			{Name: "shared-skill", Path: "/cache/registry/v2/shared-skill"},
			{Name: "newer-only", Path: "/cache/registry/v2/newer-only"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected one collision warning for same URL different refs, got %v", errs)
	}
	if !IsNameCollision(errs[0]) {
		t.Fatalf("expected collision type, got %T: %v", errs[0], errs[0])
	}

	if !strings.Contains(errs[0].Error(), "shared-skill") {
		t.Fatalf("expected collision warning to mention shared-skill, got %v", errs[0])
	}

	// "shared-skill" is now resolved (winner picked) alongside "newer-only"
	names := make(map[string]bool)
	for _, a := range got {
		names[a.Name] = true
	}
	if !names["shared-skill"] {
		t.Fatalf("expected colliding shared-skill to be resolved from winning source, got %#v", got)
	}
	if !names["newer-only"] {
		t.Fatalf("expected newer-only to survive, got %#v", got)
	}
}

func TestResolveCollisionWarningNamesRefsForSameURLAndPath(t *testing.T) {
	const url = "git@example.com:registry.git"
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: url, Ref: "v1.0", Path: "skills", Skills: ks("shared")},
		{URL: url, Ref: "v2.0", Path: "skills", Skills: ks("shared")},
	}}
	setCatalogKeys(cfg)
	entry := catalog.Entry{Name: "shared", Path: "/cache/shared"}
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {entry},
		cfg.Registries[1].CatalogKey: {entry},
	}

	_, errs := Resolve(cfg, catalogs, nil)
	var warning *NameCollisionWarning
	for _, err := range errs {
		if errors.As(err, &warning) {
			break
		}
	}
	if warning == nil {
		t.Fatalf("expected collision warning, got %v", errs)
	}
	normalizedURL := config.NormalizeGitURL(url)
	if warning.Winner != normalizedURL+" (skills) @v1.0" {
		t.Fatalf("winner = %q, want URL, path, and ref", warning.Winner)
	}
	wantRunnersUp := []string{normalizedURL + " (skills) @v2.0"}
	if !slices.Equal(warning.RunnersUp, wantRunnersUp) {
		t.Fatalf("runners-up = %#v, want %#v", warning.RunnersUp, wantRunnersUp)
	}
	if got := warning.Remediation(); !strings.Contains(got, "--from "+normalizedURL) || !strings.Contains(got, "--ref v1.0") || strings.Contains(got, "@v1.0") {
		t.Fatalf("remediation should retain the winning ref as a separate flag: %q", got)
	}
}

func TestNameCollisionWarningRemediationPreservesStructuredPathAndRef(t *testing.T) {
	const (
		url  = "git@example.com:registry.git"
		path = "team (prod) @v1"
		ref  = "feature) @fast path"
	)
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: url, Ref: ref, Path: path, Skills: ks("shared")},
		{URL: url, Ref: "other", Path: path, Skills: ks("shared")},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Name: "shared", Path: "/cache/first/shared"}},
		cfg.Registries[1].CatalogKey: {{Name: "shared", Path: "/cache/second/shared"}},
	}

	_, errs := Resolve(cfg, catalogs, nil)
	var warning *NameCollisionWarning
	for _, err := range errs {
		if errors.As(err, &warning) {
			break
		}
	}
	if warning == nil {
		t.Fatalf("expected collision warning, got %v", errs)
	}
	if warning.WinnerURL != url || warning.WinnerPath != path || warning.WinnerRef != ref {
		t.Fatalf("structured winner location = (%q, %q, %q), want (%q, %q, %q)", warning.WinnerURL, warning.WinnerPath, warning.WinnerRef, url, path, ref)
	}
	remediation := warning.Remediation()
	for _, want := range []string{
		"--from " + url,
		"--path 'team (prod) @v1'",
		"--ref 'feature) @fast path'",
	} {
		if !strings.Contains(remediation, want) {
			t.Fatalf("remediation = %q, want %q", remediation, want)
		}
	}
}

func TestNameCollisionWarningRemediationQuotesApostrophesForPOSIXShell(t *testing.T) {
	warning := &NameCollisionWarning{
		Name:       "owner's skill",
		Winner:     "https://example.com/registry.git",
		RunnersUp:  []string{"https://example.com/other.git"},
		WinnerURL:  "https://example.com/registry.git",
		WinnerPath: "team's skills",
		WinnerRef:  "feature's branch",
	}

	remediation := warning.Remediation()
	const want = `to prefer a different source, exclude the winner: agent-marketplace remove 'owner'\''s skill' --from https://example.com/registry.git --path 'team'\''s skills' --ref 'feature'\''s branch'`
	if remediation != want {
		t.Fatalf("Remediation() = %q, want %q", remediation, want)
	}
}

func TestResolveSameURLSameRefDeduplicates(t *testing.T) {
	// Same URL and same ref produce the same catalog key, so dedup applies.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:registry.git", Ref: "v1.0", Skills: ks("shared-skill")},
			{URL: "git@example.com:registry.git", Ref: "v1.0", Skills: ks("shared-skill", "extra")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:registry.git", "v1.0"): {
			{Name: "shared-skill", Path: "/cache/registry/v1/shared-skill"},
			{Name: "extra", Path: "/cache/registry/v1/extra"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no collision for same URL+ref, got %v", errs)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 resolved skills, got %d", len(got))
	}

	names := make(map[string]bool)
	for _, resolved := range got {
		names[resolved.Name] = true
	}
	if !names["shared-skill"] || !names["extra"] {
		t.Fatalf("expected shared-skill and extra, got %v", got)
	}
}

func TestResolveCollisionWarningNamesEveryPathAndRegistry(t *testing.T) {
	const sharedURL = "git@example.com:shared.git"
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: sharedURL, Ref: "main", Path: "teams/a", Skills: ks("shared")},
		{URL: sharedURL, Ref: "main", Path: "teams/b", Skills: ks("shared")},
		{URL: "git@example.com:other.git", Ref: "main", Path: "teams/c", Skills: ks("shared")},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Name: "shared", Path: "/cache/a/shared"}},
		cfg.Registries[1].CatalogKey: {{Name: "shared", Path: "/cache/b/shared"}},
		cfg.Registries[2].CatalogKey: {{Name: "shared", Path: "/cache/c/shared"}},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(resolved) != 1 || resolved[0].Name != "shared" {
		t.Fatalf("expected one deterministic materialized winner, got %#v", resolved)
	}
	var warning *NameCollisionWarning
	for _, err := range errs {
		if errors.As(err, &warning) {
			break
		}
	}
	if warning == nil {
		t.Fatalf("expected collision warning, got %v", errs)
	}
	if warning.Winner != "git@example.com:other (teams/c)" {
		t.Fatalf("warning winner = %q, want the alphabetically first normalized URL", warning.Winner)
	}
	wantRunnersUp := []string{
		"git@example.com:shared (teams/a)",
		"git@example.com:shared (teams/b)",
	}
	if !slices.Equal(warning.RunnersUp, wantRunnersUp) {
		t.Fatalf("warning runners-up = %#v, want %#v", warning.RunnersUp, wantRunnersUp)
	}
}

func TestResolveRequiredRegistriesIgnoreRepoExcludes(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:security.git",
			Ref:      "main",
			Required: true,
			Skills:   ks("no-curl-bash"),
		}},
	}

	repo := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:infra.git",
			Ref:    "main",
			Skills: kse([]string{"vm-lifecycle"}, []string{"no-curl-bash"}),
		}},
	}

	merged := config.Merge(global, repo)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {
			{Name: "no-curl-bash", Path: "/cache/security/no-curl-bash"},
		},
		ck("git@example.com:infra.git", "main"): {
			{Name: "vm-lifecycle", Path: "/cache/infra/vm-lifecycle"},
		},
	}

	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected one warning for ineffective repo exclude, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), `exclude "no-curl-bash" in repo config is ineffective`) {
		t.Fatalf("expected ineffective repo exclude warning, got %v", errs[0])
	}

	want := []ResolvedArtifact{
		{
			Entry:       catalog.Entry{Name: "no-curl-bash", Path: "/cache/security/no-curl-bash"},
			RegistryURL: "git@example.com:security.git",
			Ref:         "main",
			Source:      config.UserPersonal,
			Required:    true,
			CatalogKey:  ck("git@example.com:security.git", "main"),
		},
		{
			Entry:       catalog.Entry{Name: "vm-lifecycle", Path: "/cache/infra/vm-lifecycle"},
			RegistryURL: "git@example.com:infra.git",
			Ref:         "main",
			Source:      config.RepoCommitted,
			CatalogKey:  ck("git@example.com:infra.git", "main"),
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected required skill to survive repo excludes, want %#v got %#v", want, got)
	}
}

func TestResolveEmptyConfig(t *testing.T) {
	got, errs := Resolve(&config.Config{}, nil, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors for empty config, got %v", errs)
	}

	if len(got) != 0 {
		t.Fatalf("expected no resolved skills for empty config, got %#v", got)
	}
}

func TestResolveRequiredRegistryAutoSelectsAllSkills(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:security.git",
			Ref:      "main",
			Required: true,
		}},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {
			{Name: "no-curl-bash", Tags: []string{"security"}, Path: "/cache/security/no-curl-bash"},
			{Name: "secret-scanning", Tags: []string{"security"}, Path: "/cache/security/secret-scanning"},
			{Name: "sast-rules", Tags: []string{"sast"}, Path: "/cache/security/sast-rules"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no resolution errors, got %v", errs)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 skills from required registry, got %d", len(got))
	}

	for _, resolved := range got {
		if resolved.Source.IsMachineScope() {
			t.Fatalf("expected repo-required registry skill %q to NOT be machine-scoped (Source is set by merge, not Required)", resolved.Name)
		}
	}
}

func TestResolveRequiredRegistryWithExplicitSkillsHonorsSelection(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:security.git",
			Ref:      "main",
			Required: true,
			Skills:   ks("no-curl-bash"),
		}},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {
			{Name: "no-curl-bash", Tags: []string{"security"}, Path: "/cache/security/no-curl-bash"},
			{Name: "secret-scanning", Tags: []string{"security"}, Path: "/cache/security/secret-scanning"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no resolution errors, got %v", errs)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 skill when explicit skills specified on required entry, got %d", len(got))
	}

	if got[0].Name != "no-curl-bash" {
		t.Fatalf("expected only no-curl-bash, got %q", got[0].Name)
	}
}

func TestResolveRequiredRegistryExcludesAreIgnored(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:security.git",
			Ref:      "main",
			Required: true,
		}},
	}

	repo := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:infra.git",
			Ref:    "main",
			Skills: kse(nil, []string{"no-curl-bash"}),
		}},
	}

	merged := config.Merge(global, repo)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {
			{Name: "no-curl-bash", Tags: []string{"security"}, Path: "/cache/security/no-curl-bash"},
			{Name: "secret-scanning", Tags: []string{"security"}, Path: "/cache/security/secret-scanning"},
		},
		ck("git@example.com:infra.git", "main"): {
			{Name: "vm-lifecycle", Path: "/cache/infra/vm-lifecycle"},
		},
	}

	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected one warning for ineffective repo exclude, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), `exclude "no-curl-bash" in repo config is ineffective`) {
		t.Fatalf("expected ineffective repo exclude warning, got %v", errs[0])
	}

	names := make(map[string]bool)
	for _, resolved := range got {
		names[resolved.Name] = true
	}

	if !names["no-curl-bash"] {
		t.Fatalf("expected required skill no-curl-bash to survive repo excludes")
	}
}

func TestResolveDeduplicatesSameCatalogKey(t *testing.T) {
	// When two entries resolve to the same catalog key (same URL+ref),
	// the skill from the second entry is deduplicated.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@host:repo", Ref: "main", Skills: ks("agent-marketplace")},
			{URL: "git@host:repo", Ref: "main", Skills: ks("agent-marketplace")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@host:repo", "main"): {
			{Name: "agent-marketplace", Path: "/cache/repo/agent-marketplace"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors for same catalog key, got %v", errs)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 deduplicated skill, got %d", len(got))
	}

	if got[0].Name != "agent-marketplace" {
		t.Fatalf("expected agent-marketplace, got %q", got[0].Name)
	}
}

func TestResolveDeduplicatePrefersHigherPrecedence(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@host:repo.git", Ref: "main", Skills: ks("shared-skill")},
		},
	}

	repo := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@host:repo.git", Ref: "main", Skills: ks("shared-skill")},
		},
	}

	merged := config.Merge(global, repo)
	catalogs := map[string][]catalog.Entry{
		ck("git@host:repo.git", "main"): {
			{Name: "shared-skill", Path: "/cache/repo/shared-skill"},
		},
	}

	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors for deduplicated same registry, got %v", errs)
	}

	if len(got) != 2 {
		t.Fatalf("expected machine and repo occurrences to remain scope-separated, got %d", len(got))
	}
	var machine, repoScoped bool
	for _, artifact := range got {
		machine = machine || artifact.IsGlobal()
		repoScoped = repoScoped || !artifact.IsGlobal()
	}
	if !machine || !repoScoped {
		t.Fatalf("expected one machine and one repo occurrence, got %+v", got)
	}
}

func TestResolveDeduplicateKeepsRequiredOccurrence(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@host:repo.git", Ref: "main", Required: true, Skills: ks("shared-skill")},
		},
	}

	repo := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@host:repo.git", Ref: "main", Skills: ks("shared-skill")},
		},
	}

	merged := config.Merge(global, repo)
	catalogs := map[string][]catalog.Entry{
		ck("git@host:repo.git", "main"): {
			{Name: "shared-skill", Path: "/cache/repo/shared-skill"},
		},
	}

	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors for deduplicated same registry, got %v", errs)
	}
	if len(got) != 2 {
		t.Fatalf("expected machine and repo occurrences to remain scope-separated, got %d", len(got))
	}
	var requiredMachine, repoScoped bool
	for _, artifact := range got {
		if artifact.IsGlobal() {
			requiredMachine = artifact.Required
		} else {
			repoScoped = true
		}
	}
	if !requiredMachine {
		t.Fatalf("expected required machine-scope occurrence to remain required: %+v", got)
	}
	if !repoScoped {
		t.Fatalf("expected repo-scope occurrence to remain materialized: %+v", got)
	}
}

func TestResolveGlobalConfigEntriesAreMarkedIsGlobal(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:    "git@example.com:global-optional.git",
				Ref:    "main",
				Skills: ks("global-optional-skill"),
			},
			{
				URL:      "git@example.com:global-required.git",
				Ref:      "main",
				Required: true,
			},
		},
	}

	repo := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:    "git@example.com:repo.git",
				Ref:    "main",
				Skills: ks("repo-skill"),
			},
		},
	}

	merged := config.Merge(global, repo)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:global-optional.git", "main"): {
			{Name: "global-optional-skill", Path: "/cache/global-optional/global-optional-skill"},
		},
		ck("git@example.com:global-required.git", "main"): {
			{Name: "global-required-skill", Path: "/cache/global-required/global-required-skill"},
		},
		ck("git@example.com:repo.git", "main"): {
			{Name: "repo-skill", Path: "/cache/repo/repo-skill"},
		},
	}

	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no resolution errors, got %v", errs)
	}

	for _, resolved := range got {
		if resolved.Name == "repo-skill" && resolved.IsGlobal() {
			t.Fatalf("expected repo skill %q to NOT be machine-scoped", resolved.Name)
		}
		if strings.HasPrefix(resolved.Name, "global-") && !resolved.IsGlobal() {
			t.Fatalf("expected global config skill %q to be machine-scoped", resolved.Name)
		}
	}
}

func TestResolveLocalExcludesFiltersNonGlobalSkills(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:repo.git", Ref: "main", Skills: ks("repo-skill-a", "repo-skill-b")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:repo.git", "main"): {
			{Name: "repo-skill-a", Path: "/cache/repo/repo-skill-a"},
			{Name: "repo-skill-b", Path: "/cache/repo/repo-skill-b"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, &config.LocalConfig{Skills: kse(nil, []string{"repo-skill-a"})})
	if len(errs) != 0 {
		t.Fatalf("expected no resolution errors, got %v", errs)
	}

	if len(got) != 1 || got[0].Name != "repo-skill-b" {
		t.Fatalf("expected only repo-skill-b after local exclude, got %#v", got)
	}
}

type staticMarketplaceResolver struct {
	artifacts []ResolvedArtifact
}

func (r staticMarketplaceResolver) Resolve(context.Context, *config.Config, []hcc.Target, MarketplaceResolveMode) ([]ResolvedArtifact, []error) {
	return r.artifacts, nil
}

func TestResolveMarketplaceCollisionsPreserveInstanceAndSourceIdentity(t *testing.T) {
	const endpoint = "https://market.example"

	for _, tt := range []struct {
		name       string
		first      ResolvedArtifact
		second     ResolvedArtifact
		wantFirst  string
		wantSecond string
	}{
		{
			name: "same source at distinct instances",
			first: ResolvedArtifact{
				Entry:      catalog.Entry{Kind: ir.KindSkill, Name: "review"},
				CatalogKey: "intentionally-shared-catalog-key", Ref: "refs/heads/main",
				IsMarketplace: true, MarketplaceURL: endpoint, MarketplaceInstanceID: "instance-one", MarketplaceRegistryID: "source",
			},
			second: ResolvedArtifact{
				Entry:      catalog.Entry{Kind: ir.KindSkill, Name: "review"},
				CatalogKey: "intentionally-shared-catalog-key", Ref: "refs/heads/main",
				IsMarketplace: true, MarketplaceURL: endpoint, MarketplaceInstanceID: "instance-two", MarketplaceRegistryID: "source",
			},
			wantFirst: "instance-one", wantSecond: "instance-two",
		},
		{
			name: "distinct sources at one instance",
			first: ResolvedArtifact{
				Entry:      catalog.Entry{Kind: ir.KindSkill, Name: "review"},
				CatalogKey: "intentionally-shared-catalog-key", Ref: "refs/heads/main",
				IsMarketplace: true, MarketplaceURL: endpoint, MarketplaceInstanceID: "instance", MarketplaceRegistryID: "source-one",
			},
			second: ResolvedArtifact{
				Entry:      catalog.Entry{Kind: ir.KindSkill, Name: "review"},
				CatalogKey: "intentionally-shared-catalog-key", Ref: "refs/heads/main",
				IsMarketplace: true, MarketplaceURL: endpoint, MarketplaceInstanceID: "instance", MarketplaceRegistryID: "source-two",
			},
			wantFirst: "source-one", wantSecond: "source-two",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mp := staticMarketplaceResolver{artifacts: []ResolvedArtifact{tt.first, tt.second}}
			cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: endpoint, InstanceID: "configured"}}}
			resolved, errs := Resolve(cfg, nil, nil, WithMarketplaceResolver(mp))
			if len(resolved) != 1 {
				t.Fatalf("resolved %d artifacts, want one deterministic winner: %#v", len(resolved), resolved)
			}
			if len(errs) != 1 || !IsNameCollision(errs[0]) {
				t.Fatalf("errors = %v, want one Marketplace collision warning", errs)
			}
			var warning *NameCollisionWarning
			if !errors.As(errs[0], &warning) {
				t.Fatalf("error = %T, want NameCollisionWarning", errs[0])
			}
			for _, label := range []string{endpoint, tt.wantFirst, tt.wantSecond} {
				if !strings.Contains(warning.Error(), label) {
					t.Errorf("warning %q does not identify %q", warning.Error(), label)
				}
			}
			if len(warning.RunnersUp) != 1 {
				t.Fatalf("runners-up = %v, want one distinct Marketplace participant", warning.RunnersUp)
			}
			if !strings.Contains(warning.Winner, tt.wantFirst) {
				t.Fatalf("winner = %q, want identity %q", warning.Winner, tt.wantFirst)
			}
			if !strings.Contains(warning.RunnersUp[0], tt.wantSecond) {
				t.Fatalf("runners-up = %v, want identity %q", warning.RunnersUp, tt.wantSecond)
			}
		})
	}
}

func TestResolveLocalExcludeRespectsMarketplaceRequiredOwnership(t *testing.T) {
	for _, tt := range []struct {
		name         string
		source       config.Source
		required     bool
		wantResolved bool
		wantWarnings int
	}{
		{name: "organization pin is protected", source: config.Organization, required: true, wantResolved: true, wantWarnings: 1},
		{name: "user pin remains machine scoped", source: config.UserPersonal, required: false, wantResolved: true, wantWarnings: 1},
		{name: "repo pin can be excluded", source: config.RepoCommitted, required: false, wantResolved: false, wantWarnings: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "mp"}}}
			mp := staticMarketplaceResolver{artifacts: []ResolvedArtifact{{
				Entry:  catalog.Entry{Kind: ir.KindSkill, Name: "review"},
				Source: tt.source, Required: tt.required, IsMarketplace: true,
				MarketplaceURL: "https://market.example", MarketplaceRegistryID: "src",
			}}}
			resolved, errs := Resolve(cfg, nil, &config.LocalConfig{Skills: kse(nil, []string{"review"})}, WithMarketplaceResolver(mp))
			if (len(resolved) > 0) != tt.wantResolved {
				t.Fatalf("resolved = %#v, want present=%t", resolved, tt.wantResolved)
			}
			if len(errs) != tt.wantWarnings {
				t.Fatalf("errors = %v, want %d local-exclude warning(s)", errs, tt.wantWarnings)
			}
			for _, err := range errs {
				if !IsWarning(err) || !strings.Contains(err.Error(), `exclude "review"`) {
					t.Fatalf("error = %v, want ineffective-exclude warning", err)
				}
			}
		})
	}
}

// TestResolvePersonalExcludesApplyToNonRequiredGlobalSkills verifies that in
// Phase 2 semantics, personal excludes (config.local.yaml) affect non-required
// global/user skills. Only required:true skills are immune.
func TestResolvePersonalExcludesDoNotApplyToNonRequiredGlobalSkills(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:security.git",
			Ref:    "main",
			Skills: ks("no-curl-bash"),
		}},
	}

	repo := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:infra.git",
			Ref:    "main",
			Skills: ks("vm-lifecycle"),
		}},
	}

	merged := config.Merge(global, repo)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {
			{Name: "no-curl-bash", Path: "/cache/security/no-curl-bash"},
		},
		ck("git@example.com:infra.git", "main"): {
			{Name: "vm-lifecycle", Path: "/cache/infra/vm-lifecycle"},
		},
	}

	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, &config.LocalConfig{Skills: kse(nil, []string{"no-curl-bash", "vm-lifecycle"})})
	if len(errs) != 1 {
		t.Fatalf("expected one warning for ineffective local exclude, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), `exclude "no-curl-bash" in local config is ineffective`) {
		t.Fatalf("expected ineffective local exclude warning, got %v", errs[0])
	}

	// Local exclude applies to repo-scoped resources only.
	names := make(map[string]bool)
	for _, resolved := range got {
		names[resolved.Name] = true
	}
	if !names["no-curl-bash"] {
		t.Fatalf("expected non-required global skill no-curl-bash to remain enabled")
	}
	if names["vm-lifecycle"] {
		t.Fatalf("expected repo skill vm-lifecycle to be excluded by personal config")
	}
}

// TestResolveRequiredSkillsImmuneToPersonalExclude verifies that required:true
// skills cannot be removed by the personal exclude list.
func TestResolveRequiredSkillsImmuneToPersonalExclude(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:security.git",
			Ref:      "main",
			Required: true,
			Skills:   ks("no-curl-bash"),
		}},
	}

	merged := config.Merge(global, nil)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {
			{Name: "no-curl-bash", Path: "/cache/security/no-curl-bash"},
		},
	}
	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, &config.LocalConfig{Skills: kse(nil, []string{"no-curl-bash"})})
	if len(errs) != 1 {
		t.Fatalf("expected one warning for ineffective local exclude, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), `exclude "no-curl-bash" in local config is ineffective`) {
		t.Fatalf("expected ineffective local exclude warning, got %v", errs[0])
	}

	names := make(map[string]bool)
	for _, r := range got {
		names[r.Name] = true
	}
	if !names["no-curl-bash"] {
		t.Fatalf("required skill must be immune to personal exclude")
	}
}

func TestResolveWarnsOnIneffectiveLocalExcludeForMachineScopeSkill(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:security.git",
			Ref:    "main",
			Skills: ks("no-curl-bash"),
		}},
	}
	merged := config.Merge(global, nil)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {
			{Name: "no-curl-bash", Path: "/cache/security/no-curl-bash"},
		},
	}
	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, &config.LocalConfig{Skills: kse(nil, []string{"no-curl-bash"})})
	if len(got) != 1 || got[0].Name != "no-curl-bash" {
		t.Fatalf("expected machine-scope skill to remain enabled, got %#v", got)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `exclude "no-curl-bash" in local config is ineffective`) {
		t.Fatalf("expected ineffective local exclude warning, got %v", errs)
	}
}

func TestResolveWarnsOnIneffectiveRepoExcludeForMachineScopeSkill(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:security.git",
			Ref:    "main",
			Skills: ks("no-curl-bash"),
		}},
	}
	repo := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:repo.git",
			Ref:    "main",
			Skills: kse(nil, []string{"no-curl-bash"}),
		}},
	}
	merged := config.Merge(global, repo)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {
			{Name: "no-curl-bash", Path: "/cache/security/no-curl-bash"},
		},
		ck("git@example.com:repo.git", "main"): {
			{Name: "repo-skill", Path: "/cache/repo/repo-skill"},
		},
	}
	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, nil)
	names := make(map[string]bool)
	for _, r := range got {
		names[r.Name] = true
	}
	if !names["no-curl-bash"] {
		t.Fatalf("expected machine-scope skill to remain enabled")
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `exclude "no-curl-bash" in repo config is ineffective`) {
		t.Fatalf("expected ineffective repo exclude warning, got %v", errs)
	}
}

func TestResolveLocalRegistryEntry(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "/local/skills", Skills: ks("local-skill")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		"/local/skills": {
			{Name: "local-skill", Path: "/local/skills/local-skill"},
			{Name: "other-skill", Path: "/local/skills/other-skill"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no resolution errors, got %v", errs)
	}

	if len(got) != 1 || got[0].Name != "local-skill" {
		t.Fatalf("expected only local-skill, got %#v", got)
	}

	if got[0].CatalogKey != "/local/skills" {
		t.Fatalf("expected CatalogKey to be the local path, got %q", got[0].CatalogKey)
	}

	if got[0].Ref != "" {
		t.Fatalf("expected Ref to be empty for local registry, got %q", got[0].Ref)
	}
}

func TestResolveMixedLocalAndGitRegistries(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "/local/skills", Skills: ks("local-skill")},
			{URL: "git@example.com:remote.git", Ref: "main", Skills: ks("remote-skill")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		"/local/skills": {
			{Name: "local-skill", Path: "/local/skills/local-skill"},
		},
		ck("git@example.com:remote.git", "main"): {
			{Name: "remote-skill", Path: "/cache/remote/remote-skill"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no resolution errors, got %v", errs)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 resolved skills, got %d", len(got))
	}

	names := make(map[string]bool)
	for _, resolved := range got {
		names[resolved.Name] = true
	}
	if !names["local-skill"] || !names["remote-skill"] {
		t.Fatalf("expected both local-skill and remote-skill, got %v", got)
	}
}

func TestResolveDeduplicatesNotFoundErrorForSameRegistry(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main", Skills: ks("missing-a", "missing-b")},
			{URL: "git@example.com:infra.git", Ref: "main", Skills: ks("missing-a", "missing-b", "found-skill")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:infra.git", "main"): {
			{Name: "found-skill", Path: "/cache/infra/found-skill"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(got) != 1 || got[0].Name != "found-skill" {
		t.Fatalf("expected only found-skill resolved, got %#v", got)
	}

	if len(errs) != 2 {
		t.Fatalf("expected 2 deduplicated not-found errors (one per missing skill), got %d: %v", len(errs), errs)
	}

	for _, err := range errs {
		if !strings.Contains(err.Error(), "was selected but not found") {
			t.Fatalf("expected not-found error, got %v", err)
		}
	}

	errorMessages := make(map[string]int)
	for _, err := range errs {
		errorMessages[err.Error()]++
	}
	for msg, count := range errorMessages {
		if count > 1 {
			t.Fatalf("expected each not-found error to appear only once, but %q appeared %d times", msg, count)
		}
	}

	// Verify that the errors are SelectedNotFoundError instances
	for _, err := range errs {
		if !IsSelectedNotFound(err) {
			t.Fatalf("expected SelectedNotFoundError, got %T: %v", err, err)
		}
		var snf *SelectedNotFoundError
		if !errors.As(err, &snf) {
			t.Fatalf("expected errors.As to unwrap SelectedNotFoundError, got %T", err)
		}
		if snf.Name == "missing-a" || snf.Name == "missing-b" {
			if snf.URL != "git@example.com:infra.git" || snf.Ref != "main" {
				t.Fatalf("expected URL/Ref to be set, got URL=%s Ref=%s", snf.URL, snf.Ref)
			}
			if snf.Required {
				t.Fatal("expected Required=false for non-required registry entry")
			}
			if !strings.Contains(snf.Remediation(), "agent-marketplace remove") {
				t.Fatalf("expected remediation to mention 'agent-marketplace remove', got %q", snf.Remediation())
			}
		}
	}
}

func TestIsSelectedNotFoundRejectsOtherErrors(t *testing.T) {
	if IsSelectedNotFound(fmt.Errorf("some other error")) {
		t.Fatal("IsSelectedNotFound should return false for generic errors")
	}
	if IsSelectedNotFound(&NameCollisionError{Name: "x", Sources: []string{"a", "b"}}) {
		t.Fatal("IsSelectedNotFound should return false for NameCollisionError")
	}
	if IsSelectedNotFound(&IneffectiveExcludeWarning{Scope: "local", Name: "x"}) {
		t.Fatal("IsSelectedNotFound should return false for IneffectiveExcludeWarning")
	}
	if !IsSelectedNotFound(&SelectedNotFoundError{Name: "x", URL: "u", Ref: "r"}) {
		t.Fatal("IsSelectedNotFound should return true for SelectedNotFoundError")
	}
}

func TestIsSoftSelectedNotFoundRespectsRequired(t *testing.T) {
	nonRequired := &SelectedNotFoundError{Name: "x", URL: "u", Ref: "r", Required: false}
	required := &SelectedNotFoundError{Name: "y", URL: "u", Ref: "r", Required: true}

	if !IsSoftSelectedNotFound(nonRequired) {
		t.Fatal("IsSoftSelectedNotFound should return true for non-required SelectedNotFoundError")
	}
	if IsSoftSelectedNotFound(required) {
		t.Fatal("IsSoftSelectedNotFound should return false for required SelectedNotFoundError")
	}
	if IsSoftSelectedNotFound(fmt.Errorf("other")) {
		t.Fatal("IsSoftSelectedNotFound should return false for non-SelectedNotFoundError")
	}
	if IsSoftSelectedNotFound(&NameCollisionError{Name: "x"}) {
		t.Fatal("IsSoftSelectedNotFound should return false for NameCollisionError")
	}
}

func TestResolveSelectedNotFoundFromRequiredRegistryIsHardError(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:security.git", Ref: "main", Required: true, Skills: ks("mandatory-skill")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}

	var snf *SelectedNotFoundError
	if !errors.As(errs[0], &snf) {
		t.Fatalf("expected SelectedNotFoundError, got %T: %v", errs[0], errs[0])
	}
	if !snf.Required {
		t.Fatal("expected Required=true on SelectedNotFoundError from required registry")
	}
	if IsSoftSelectedNotFound(errs[0]) {
		t.Fatal("IsSoftSelectedNotFound should return false for required registry's SelectedNotFoundError")
	}
}

// TestResolveCollectionNotFoundSuppressedWhenFoundInAnotherRegistry
// verifies that when propagateTopLevelCollections adds a collection
// include to every registry entry, "collection not found" errors from
// registries that don't have the collection are suppressed if the
// collection exists in at least one other registry.
func TestResolveCollectionNotFoundSuppressedWhenFoundInAnotherRegistry(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:local.git", Ref: "main", Path: "skills"},
			{URL: "git@example.com:remote.git", Ref: "main", Path: ".factory/skills"},
		},
		Collections: config.KindSelector{Include: []string{"agent-marketplace-tools"}},
	}

	// The local registry has the collection, the remote one does not.
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:local.git", "main", "skills"): {
			{Name: "agent-marketplace-tools", Kind: ir.KindCollection, Path: "/cache/local/collections/agent-marketplace-tools"},
		},
		ck("git@example.com:remote.git", "main", ".factory/skills"): {
			{Name: "some-skill", Kind: ir.KindSkill, Path: "/cache/remote/skills/some-skill"},
		},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		var snf *SelectedNotFoundError
		if errors.As(err, &snf) && snf.Kind == ir.KindCollection && snf.Name == "agent-marketplace-tools" {
			t.Fatalf("collection 'agent-marketplace-tools' exists in the local registry; 'not found in remote' error should be suppressed, got: %v", err)
		}
	}
}

// TestResolveCollectionNotFoundErrorWhenNotInAnyRegistry verifies that
// a hard error is still produced when a collection is not found in ANY
// registry (i.e., the collection truly doesn't exist).
func TestResolveCollectionNotFoundErrorWhenNotInAnyRegistry(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:local.git", Ref: "main", Path: "skills"},
			{URL: "git@example.com:remote.git", Ref: "main", Path: ".factory/skills"},
		},
		Collections: config.KindSelector{Include: []string{"nonexistent-collection"}},
	}

	// Neither registry has the collection.
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:local.git", "main", "skills"): {
			{Name: "some-skill", Kind: ir.KindSkill, Path: "/cache/local/skills/some-skill"},
		},
		ck("git@example.com:remote.git", "main", ".factory/skills"): {
			{Name: "other-skill", Kind: ir.KindSkill, Path: "/cache/remote/skills/other-skill"},
		},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil)
	found := false
	for _, err := range errs {
		var snf *SelectedNotFoundError
		if errors.As(err, &snf) && snf.Kind == ir.KindCollection && snf.Name == "nonexistent-collection" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected SelectedNotFoundError for collection 'nonexistent-collection' not found in any registry, got errors: %v", errs)
	}
}

func TestResolveLocalSkillsEnableFromAnyRegistry(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:security.git", Ref: "main", Skills: ks("no-curl-bash")},
			{URL: "git@example.com:infra.git", Ref: "main", Skills: ks("vm-lifecycle")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {
			{Name: "no-curl-bash", Path: "/cache/security/no-curl-bash"},
			{Name: "secret-scanning", Path: "/cache/security/secret-scanning"},
		},
		ck("git@example.com:infra.git", "main"): {
			{Name: "vm-lifecycle", Path: "/cache/infra/vm-lifecycle"},
			{Name: "k8s-deploy", Path: "/cache/infra/k8s-deploy"},
		},
	}

	setCatalogKeys(cfg)

	localCfg := &config.LocalConfig{Skills: ks("secret-scanning", "k8s-deploy")}
	got, errs := Resolve(cfg, catalogs, localCfg)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	names := make(map[string]ResolvedArtifact)
	for _, r := range got {
		names[r.Name] = r
	}

	if _, ok := names["secret-scanning"]; !ok {
		t.Fatalf("expected secret-scanning enabled via local config")
	}
	if _, ok := names["k8s-deploy"]; !ok {
		t.Fatalf("expected k8s-deploy enabled via local config")
	}
	if names["secret-scanning"].IsGlobal() {
		t.Fatalf("local-enabled skill must not be machine-scoped")
	}
	if names["k8s-deploy"].IsGlobal() {
		t.Fatalf("local-enabled skill must not be machine-scoped")
	}
	if names["secret-scanning"].Required {
		t.Fatalf("local-enabled skill must have Required=false")
	}
}

func TestResolveLocalSkillAlreadyEnabledBecomesRepoPersonal(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:repo.git", Ref: "main", Skills: ks("repo-skill")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:repo.git", "main"): {
			{Name: "repo-skill", Path: "/cache/repo/repo-skill"},
		},
	}

	setCatalogKeys(cfg)

	// Local also enables the same skill
	localCfg := &config.LocalConfig{Skills: ks("repo-skill")}
	got, errs := Resolve(cfg, catalogs, localCfg)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	if len(got) != 1 {
		t.Fatalf("expected skill to appear once (not duplicated by local enablement), got %d: %#v", len(got), got)
	}
	if got[0].Source != config.RepoPersonal {
		t.Fatalf("expected local enablement to force repo-personal source, got %v", got[0].Source)
	}
	if got[0].IsGlobal() {
		t.Fatalf("expected local enablement to keep skill repo-scoped")
	}
}

func TestResolveLocalSkillNotInAnyCatalogEmitsSoftError(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:repo.git", Ref: "main", Skills: ks("repo-skill")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:repo.git", "main"): {
			{Name: "repo-skill", Path: "/cache/repo/repo-skill"},
		},
	}

	setCatalogKeys(cfg)

	localCfg := &config.LocalConfig{Skills: ks("phantom-skill")}
	got, errs := Resolve(cfg, catalogs, localCfg)

	if len(errs) != 1 {
		t.Fatalf("expected 1 soft error for missing local-enabled skill, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "phantom-skill") {
		t.Fatalf("expected error to mention phantom-skill, got %v", errs[0])
	}
	if !strings.Contains(errs[0].Error(), "local config") {
		t.Fatalf("expected error to mention local config, got %v", errs[0])
	}

	if len(got) != 1 || got[0].Name != "repo-skill" {
		t.Fatalf("expected resolution to continue despite soft error, got %#v", got)
	}
}

func TestResolveLocalSkillPlusExcludeFavorsExclude(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:repo.git", Ref: "main", Skills: ks("repo-skill")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:repo.git", "main"): {
			{Name: "extra-skill", Path: "/cache/repo/extra-skill"},
			{Name: "repo-skill", Path: "/cache/repo/repo-skill"},
		},
	}

	setCatalogKeys(cfg)

	// User locally enables extra-skill AND excludes it — exclude wins.
	localCfg := &config.LocalConfig{
		Skills: kse([]string{"extra-skill"}, []string{"extra-skill"}),
	}
	got, errs := Resolve(cfg, catalogs, localCfg)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	for _, r := range got {
		if r.Name == "extra-skill" {
			t.Fatalf("expected extra-skill to be excluded despite local enablement")
		}
	}
}

func TestResolveSameSkillInMachineAndRepoScopeProducesBothArtifacts(t *testing.T) {
	// Regression: when a skill appears in both a UserPersonal registry
	// (machine-scope, path: .factory) and a RepoCommitted registry (repo-scope,
	// path: .factory/skills) pointing at the same remote, the old dedup logic
	// merged both path variants into one group and picked the higher-precedence
	// RepoCommitted winner. This silently removed the UserPersonal machine-scope
	// artifact, causing the skill to be pruned from ~/.factory/skills/ every
	// time the user ran am sync from ~/ (where the repo config is absent).
	//
	// Fix: normKey includes path, so different-path entries stay in separate
	// groups. pickWinners then emits both when they span different scopes
	// (scopesSeparated), giving the materializer both artifacts to place in
	// their respective directories.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:    "git@example.com:awesome-ai.git",
				Ref:    "main",
				Path:   ".factory",
				Source: config.UserPersonal,
				Skills: config.KindSelector{Include: []string{"openspec-apply-change"}},
			},
			{
				URL:    "git@example.com:awesome-ai.git",
				Ref:    "main",
				Path:   ".factory/skills",
				Source: config.RepoCommitted,
				Skills: config.KindSelector{Include: []string{"openspec-apply-change"}},
			},
		},
	}
	setCatalogKeys(cfg)

	// Each path-specific entry needs its own catalog, since CatalogKey
	// now incorporates the path.
	entry := catalog.Entry{Name: "openspec-apply-change", Path: "/cache/awesome-ai/openspec-apply-change"}
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {entry},
		cfg.Registries[1].CatalogKey: {entry},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 resolved artifacts (one per scope), got %d: %#v", len(got), got)
	}

	var machine, repo *ResolvedArtifact
	for i := range got {
		if got[i].Source.IsMachineScope() {
			machine = &got[i]
		} else {
			repo = &got[i]
		}
	}
	if machine == nil {
		t.Fatal("expected a UserPersonal (machine-scope) artifact — machine-scope skills must not be pruned by a same-remote repo entry")
	}
	if repo == nil {
		t.Fatal("expected a RepoCommitted (repo-scope) artifact")
	}
	if machine.Source != config.UserPersonal {
		t.Fatalf("machine-scope artifact source = %v, want UserPersonal", machine.Source)
	}
	if repo.Source != config.RepoCommitted {
		t.Fatalf("repo-scope artifact source = %v, want RepoCommitted", repo.Source)
	}
}

func TestResolveSameURLDifferentPathSameScopeKeepsBoth(t *testing.T) {
	// Regression: when a skill name appears from two entries of
	// the same repo (same URL, different paths) and both sources are
	// machine-scoped (e.g. Organization + UserPersonal), the old pickWinners
	// logic silently dropped the second occurrence because neither
	// hasMultiRegistryCollision nor scopesSeparated detected the case.
	//
	// Fix: the pathsDiffer check recognises same-URL-different-path entries
	// as distinct and keeps all of them, so the user sees both in
	// status/TUI instead of one being silently dropped.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:     "git@gitlab.example.com:example-org/example-resources.git",
				Ref:     "main",
				Path:    "teams/example-team/plugins",
				Source:  config.Organization,
				Default: true,
			},
			{
				URL:     "git@gitlab.example.com:example-org/example-resources.git",
				Ref:     "main",
				Path:    "teams/example-product",
				Source:  config.UserPersonal,
				Default: true,
			},
		},
	}
	setCatalogKeys(cfg)

	entry := catalog.Entry{Name: "shared-workflow", Path: "/cache/awesome-ai/shared-workflow"}
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {entry},
		cfg.Registries[1].CatalogKey: {entry},
	}

	got, errs := Resolve(cfg, catalogs, nil)

	// Both entries should be kept (visibility), but a collision warning
	// should be emitted because the materializer can only install one to
	// the shared target directory.
	if len(got) != 2 {
		t.Fatalf("expected 2 resolved artifacts (one per path), got %d: %#v", len(got), got)
	}

	if len(errs) != 1 {
		t.Fatalf("expected 1 collision warning, got %d: %v", len(errs), errs)
	}
	var warn *NameCollisionWarning
	if !errors.As(errs[0], &warn) {
		t.Fatalf("expected NameCollisionWarning, got %T: %v", errs[0], errs[0])
	}
	if warn.Name != "shared-workflow" {
		t.Fatalf("warning name = %q, want shared-workflow", warn.Name)
	}
	// Winner should be UserPersonal (higher precedence than Organization)
	if !strings.Contains(warn.Winner, "teams/example-product") {
		t.Fatalf("warning winner = %q, want to contain teams/example-product", warn.Winner)
	}
	if len(warn.RunnersUp) != 1 || !strings.Contains(warn.RunnersUp[0], "teams/example-team/plugins") {
		t.Fatalf("warning runners-up = %v, want [teams/example-team/plugins]", warn.RunnersUp)
	}

	var org, user *ResolvedArtifact
	for i := range got {
		switch got[i].Source {
		case config.Organization:
			org = &got[i]
		case config.UserPersonal:
			user = &got[i]
		}
	}
	if org == nil {
		t.Fatal("expected an Organization artifact — same-URL different-path entries must not be silently dropped")
	}
	if user == nil {
		t.Fatal("expected a UserPersonal artifact — same-URL different-path entries must not be silently dropped")
	}
	if org.RegistryPath != "teams/example-team/plugins" {
		t.Fatalf("org artifact path = %q, want teams/example-team/plugins", org.RegistryPath)
	}
	if user.RegistryPath != "teams/example-product" {
		t.Fatalf("user artifact path = %q, want teams/example-product", user.RegistryPath)
	}
}

func TestResolveScopesSeparatedWithWithinScopePathCollision(t *testing.T) {
	// When entries span both machine and repo scope (scopesSeparated=true)
	// but also have within-scope path collisions, the resolver must still
	// emit collision warnings for the contending scope group(s) rather than
	// silently keeping all without informing the user.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:     "git@gitlab.example.com:example-org/example-resources.git",
				Ref:     "main",
				Path:    "teams/example-team/plugins",
				Source:  config.Organization,
				Default: true,
			},
			{
				URL:     "git@gitlab.example.com:example-org/example-resources.git",
				Ref:     "main",
				Path:    "teams/example-product",
				Source:  config.UserPersonal,
				Default: true,
			},
			{
				URL:     "git@gitlab.example.com:example-org/example-resources.git",
				Ref:     "main",
				Path:    "teams/example-team/repo-plugins",
				Source:  config.RepoCommitted,
				Default: true,
			},
		},
	}
	setCatalogKeys(cfg)

	entry := catalog.Entry{Name: "shared-workflow", Path: "/cache/awesome-ai/shared-workflow"}
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {entry},
		cfg.Registries[1].CatalogKey: {entry},
		cfg.Registries[2].CatalogKey: {entry},
	}

	got, errs := Resolve(cfg, catalogs, nil)

	// All three entries should be kept (scopesSeparated keeps all).
	if len(got) != 3 {
		t.Fatalf("expected 3 resolved artifacts, got %d: %#v", len(got), got)
	}

	// But the two machine-scoped entries (Organization + UserPersonal)
	// have different paths → collision warning for the machine-scope group.
	if len(errs) != 1 {
		t.Fatalf("expected 1 collision warning, got %d: %v", len(errs), errs)
	}
	var warn *NameCollisionWarning
	if !errors.As(errs[0], &warn) {
		t.Fatalf("expected NameCollisionWarning, got %T: %v", errs[0], errs[0])
	}
	if warn.Name != "shared-workflow" {
		t.Fatalf("warning name = %q, want shared-workflow", warn.Name)
	}
	// The repo-scoped entry has the same path as the Organization entry,
	// so it's not in the collision (no path difference within repo scope).
	// The collision is within the machine-scope group only.
	if !strings.Contains(warn.Winner, "teams/") {
		t.Fatalf("warning winner = %q, should contain path info", warn.Winner)
	}
}

func TestResolveDeduplicateIsDeterministicAcrossRuns(t *testing.T) {
	// When the same skill appears from the same normalized URL+ref but
	// with different catalog keys (e.g. different path: values in the
	// same registry), the deduplicateOccurrences function must produce
	// identical results on every call. Without deterministic sorting of
	// map keys, the surviving CatalogKey varies across runs, causing the
	// symlink target to oscillate (bug: Installed count flaps between
	// two values on repeated syncs).
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@host:repo.git", Ref: "main", Path: "skills", Skills: ks("shared-skill")},
			{URL: "git@host:repo.git", Ref: "main", Path: ".factory/skills", Skills: ks("shared-skill")},
			{URL: "git@host:repo", Ref: "main", Path: "skills", Skills: ks("shared-skill")},
		},
	}

	setCatalogKeys(cfg)

	// Each path-specific entry now has its own catalog key.
	entry := catalog.Entry{Name: "shared-skill", Path: "/cache/repo/shared-skill"}
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {entry},
		cfg.Registries[1].CatalogKey: {entry},
		cfg.Registries[2].CatalogKey: {entry},
	}

	// Run Resolve 100 times; all outputs must be identical.
	var first []ResolvedArtifact
	var firstErrs []error
	for i := 0; i < 100; i++ {
		got, errs := Resolve(cfg, catalogs, nil)
		// Same-URL-different-path entries now emit a collision warning;
		// accept it but verify it is deterministic across runs.
		if i == 0 {
			first = got
			firstErrs = errs
			continue
		}
		if len(got) != len(first) {
			t.Fatalf("run %d: expected %d resolved skills, got %d", i, len(first), len(got))
		}
		if len(errs) != len(firstErrs) {
			t.Fatalf("run %d: expected %d errors, got %d", i, len(firstErrs), len(errs))
		}
		for j := range got {
			if got[j].CatalogKey != first[j].CatalogKey {
				t.Fatalf("run %d skill %d: CatalogKey differs: %q vs %q",
					i, j, got[j].CatalogKey, first[j].CatalogKey)
			}
			if got[j].Name != first[j].Name {
				t.Fatalf("run %d skill %d: Name differs: %q vs %q",
					i, j, got[j].Name, first[j].Name)
			}
		}
		for j := range errs {
			if errs[j].Error() != firstErrs[j].Error() {
				t.Fatalf("run %d error %d: differs: %q vs %q",
					i, j, errs[j].Error(), firstErrs[j].Error())
			}
		}
	}
}

func TestDeduplicateOverlappingPathsPreferDepthBeforeAuthority(t *testing.T) {
	const url = "git@example.com:shared.git"
	occurrences := []skillOccurrence{
		{resolved: ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: url, RegistryPath: "parent", Ref: "main", Source: config.Organization, CatalogKey: "parent"}, url: url, order: 0},
		{resolved: ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: url, RegistryPath: "parent/child", Ref: "main", Source: config.UserPersonal, CatalogKey: "child"}, url: url, order: 1},
	}

	got := deduplicateOccurrences(occurrences)
	if len(got) != 1 {
		t.Fatalf("expected one same-scope overlapping occurrence, got %#v", got)
	}
	if got[0].resolved.RegistryPath != "parent/child" {
		t.Fatalf("deeper path must beat higher-authority parent, got %q", got[0].resolved.RegistryPath)
	}
}

func TestDeduplicateOverlappingPathsIsDeterministic(t *testing.T) {
	const url = "git@example.com:shared.git"
	occurrences := []skillOccurrence{
		{resolved: ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: url, RegistryPath: "", Ref: "main", Source: config.RepoCommitted, CatalogKey: "root"}, url: url, order: 0},
		{resolved: ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: url, RegistryPath: "root/a", Ref: "main", Source: config.RepoCommitted, CatalogKey: "root-a"}, url: url, order: 1},
		{resolved: ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: url, RegistryPath: "root/a/b", Ref: "main", Source: config.RepoCommitted, CatalogKey: "root-b"}, url: url, order: 2},
	}

	var first []skillOccurrence
	for i := 0; i < 100; i++ {
		got := deduplicateOccurrences(occurrences)
		if len(got) != 1 {
			t.Fatalf("run %d: got %d occurrences, want one", i, len(got))
		}
		if i == 0 {
			first = got
			continue
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs from first result: %#v vs %#v", i, got, first)
		}
	}
	if first[0].resolved.RegistryPath != "root/a/b" {
		t.Fatalf("surviving path = %q, want deepest overlapping path", first[0].resolved.RegistryPath)
	}
}

// --- Cross-kind ambiguity tests ---

func TestSkillExcludeDoesNotSuppressCommandWithSameName(t *testing.T) {
	// A skill exclude should NOT suppress a command with the same name.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:      "git@example.com:team.git",
				Ref:      "main",
				Source:   config.UserPersonal,
				Skills:   config.KindSelector{Include: []string{"deploy"}, Exclude: []string{"deploy"}},
				Commands: config.KindSelector{Include: []string{"deploy"}},
			},
		},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {
			{Kind: ir.KindSkill, Name: "deploy", Tags: nil},
			{Kind: ir.KindCommand, Name: "deploy", Tags: nil},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no resolver errors, got %d: %v", len(errs), errs)
	}

	// The command "deploy" must survive the skill exclude.
	var foundSkill, foundCommand bool
	for _, r := range resolved {
		if r.Name == "deploy" {
			if r.Kind == ir.KindSkill {
				foundSkill = true
			}
			if r.Kind == ir.KindCommand {
				foundCommand = true
			}
		}
	}
	if foundSkill {
		t.Fatalf("skill 'deploy' should be excluded by Skills.Exclude")
	}
	if !foundCommand {
		t.Fatalf("command 'deploy' should NOT be excluded by Skills.Exclude; resolved=%v", resolved)
	}
}

func TestLocalSkillExcludeDoesNotSuppressCommandWithSameName(t *testing.T) {
	// A per-kind local exclude (Skills.Exclude) should only suppress skills,
	// not commands with the same name.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:      "git@example.com:team.git",
				Ref:      "main",
				Source:   config.RepoCommitted,
				Skills:   config.KindSelector{Include: []string{"deploy"}},
				Commands: config.KindSelector{Include: []string{"deploy"}},
			},
		},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {
			{Kind: ir.KindSkill, Name: "deploy", Tags: nil},
			{Kind: ir.KindCommand, Name: "deploy", Tags: nil},
		},
	}

	localCfg := &config.LocalConfig{
		Skills: config.KindSelector{Exclude: []string{"deploy"}},
	}

	resolved, errs := Resolve(cfg, catalogs, localCfg)
	for _, e := range errs {
		t.Logf("warning/error: %v", e)
	}

	var foundSkill, foundCommand bool
	for _, r := range resolved {
		if r.Name == "deploy" {
			if r.Kind == ir.KindSkill {
				foundSkill = true
			}
			if r.Kind == ir.KindCommand {
				foundCommand = true
			}
		}
	}
	if foundSkill {
		t.Fatalf("skill 'deploy' should be excluded by local Skills.Exclude")
	}
	if !foundCommand {
		t.Fatalf("command 'deploy' should NOT be excluded by local Skills.Exclude; resolved=%v", resolved)
	}
}

func TestCrossLayerSkillExcludeDoesNotSuppressCommandWithSameName(t *testing.T) {
	// A higher-precedence skill exclude should NOT suppress a command with the
	// same name from a lower-precedence layer.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:      "git@example.com:team.git",
				Ref:      "main",
				Source:   config.RepoCommitted,
				Skills:   config.KindSelector{Include: []string{"deploy"}},
				Commands: config.KindSelector{Include: []string{"deploy"}},
			},
			{
				URL:    "git@example.com:other.git",
				Ref:    "main",
				Source: config.RepoPersonal,
				Skills: config.KindSelector{Exclude: []string{"deploy"}},
			},
		},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{}
	for _, e := range cfg.Registries {
		if _, ok := catalogs[e.CatalogKey]; ok {
			continue
		}
		catalogs[e.CatalogKey] = []catalog.Entry{
			{Kind: ir.KindSkill, Name: "deploy"},
			{Kind: ir.KindCommand, Name: "deploy"},
		}
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	for _, e := range errs {
		t.Logf("warning/error: %v", e)
	}

	var foundSkill, foundCommand bool
	for _, r := range resolved {
		if r.Name == "deploy" {
			if r.Kind == ir.KindSkill {
				foundSkill = true
			}
			if r.Kind == ir.KindCommand {
				foundCommand = true
			}
		}
	}
	if foundSkill {
		t.Fatalf("skill 'deploy' should be excluded by higher-precedence Skills.Exclude")
	}
	if !foundCommand {
		t.Fatalf("command 'deploy' should NOT be excluded by Skills.Exclude; resolved=%v", resolved)
	}
}

func TestSkillsIncludeForWrongKindGivesHint(t *testing.T) {
	// When skills: include: [X] references a name that exists under a different
	// kind, the error must contain a hint with the correct selector key.
	cases := []struct {
		name           string
		actualKind     ir.ResourceKind
		expectedLabel  string
		expectedSelKey string
	}{
		{"command kind", ir.KindCommand, "command", "commands"},
		{"subagent kind", ir.KindSubagent, "subagent", "subagents"},
		{"hook kind", ir.KindHook, "hook", "hooks"},
		{"mcp kind", ir.KindMcpServer, "mcp", "mcp"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				Registries: []config.RegistryEntry{
					{
						URL:    "git@example.com:team.git",
						Ref:    "main",
						Source: config.RepoCommitted,
						Skills: config.KindSelector{Include: []string{"deploy-res"}},
					},
				},
			}
			setCatalogKeys(cfg)

			catalogs := map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {
					{Kind: tc.actualKind, Name: "deploy-res", Tags: nil},
				},
			}

			_, errs := Resolve(cfg, catalogs, nil)
			if len(errs) == 0 {
				t.Fatalf("expected an error for skills: include: referencing a %s-kind resource", tc.expectedLabel)
			}

			errMsg := errs[0].Error()
			if !strings.Contains(errMsg, "was selected but not found") {
				t.Fatalf("expected 'was selected but not found' in error, got: %s", errMsg)
			}
			wantLabel := "resource exists as " + tc.expectedLabel
			if !strings.Contains(errMsg, wantLabel) {
				t.Fatalf("expected %q in error, got: %s", wantLabel, errMsg)
			}
			wantKey := tc.expectedSelKey + ": include:"
			if !strings.Contains(errMsg, wantKey) {
				t.Fatalf("expected %q selector hint in error, got: %s", wantKey, errMsg)
			}
		})
	}
}

// TestResolve_PerPathCatalogsDontLeakSelections verifies that when a single
// repo produces multiple per-path catalogs (same URL+ref, different paths),
// a skill selection in one entry does not cause the skill to appear in
// another entry's resolved artifacts. This is a regression guard for the
// path-aware CatalogKey change.
func TestResolve_PerPathCatalogsDontLeakSelections(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:    "git@example.com:awesome-ai.git",
				Ref:    "main",
				Path:   "teams/pm",
				Source: config.UserPersonal,
				Skills: config.KindSelector{Include: []string{"pm-skill"}},
			},
			{
				URL:     "git@example.com:awesome-ai.git",
				Ref:     "main",
				Path:    "teams/eng",
				Source:  config.UserPersonal,
				Default: true,
			},
		},
	}
	setCatalogKeys(cfg)

	pmEntry := catalog.Entry{Name: "pm-skill", Kind: ir.KindSkill, Path: "/cache/awesome-ai/teams/pm/pm-skill"}
	engEntry := catalog.Entry{Name: "eng-skill", Kind: ir.KindSkill, Path: "/cache/awesome-ai/teams/eng/eng-skill"}

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {pmEntry},
		cfg.Registries[1].CatalogKey: {engEntry},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// pm-skill should only be resolved from the teams/pm entry
	// eng-skill should only be resolved from the teams/eng entry (default:true)
	pmCount := 0
	engCount := 0
	for _, a := range got {
		if a.Name == "pm-skill" {
			pmCount++
			if a.CatalogKey != cfg.Registries[0].CatalogKey {
				t.Errorf("pm-skill resolved from wrong catalog key %q, expected %q",
					a.CatalogKey, cfg.Registries[0].CatalogKey)
			}
		}
		if a.Name == "eng-skill" {
			engCount++
			if a.CatalogKey != cfg.Registries[1].CatalogKey {
				t.Errorf("eng-skill resolved from wrong catalog key %q, expected %q",
					a.CatalogKey, cfg.Registries[1].CatalogKey)
			}
		}
	}
	if pmCount != 1 {
		t.Errorf("pm-skill resolved %d times, expected 1", pmCount)
	}
	if engCount != 1 {
		t.Errorf("eng-skill resolved %d times, expected 1", engCount)
	}
}

// TestResolve_PerPathExcludesDontLeak verifies that excluding a skill in one
// per-path entry does not exclude it from another entry with the same URL+ref
// but a different path.
func TestResolve_PerPathExcludesDontLeak(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:     "git@example.com:repo.git",
				Ref:     "main",
				Path:    "team-a",
				Source:  config.UserPersonal,
				Skills:  config.KindSelector{Exclude: []string{"shared-skill"}},
				Default: true,
			},
			{
				URL:     "git@example.com:repo.git",
				Ref:     "main",
				Path:    "team-b",
				Source:  config.UserPersonal,
				Default: true,
			},
		},
	}
	setCatalogKeys(cfg)

	sharedEntry := catalog.Entry{Name: "shared-skill", Kind: ir.KindSkill, Path: "/cache/repo/shared-skill"}

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {sharedEntry},
		cfg.Registries[1].CatalogKey: {sharedEntry},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// shared-skill should be excluded from team-a but present from team-b
	names := make(map[string]int)
	for _, a := range got {
		names[a.Name]++
	}
	if names["shared-skill"] != 1 {
		t.Errorf("shared-skill should be resolved exactly once (from team-b, excluded from team-a), got %d", names["shared-skill"])
	}
}

func TestPathsDiffer(t *testing.T) {
	tests := []struct {
		name string
		occs []skillOccurrence
		want bool
	}{
		{
			name: "single occurrence",
			occs: []skillOccurrence{{resolved: ResolvedArtifact{RegistryPath: "teams/dev"}}},
			want: false,
		},
		{
			name: "same path",
			occs: []skillOccurrence{
				{resolved: ResolvedArtifact{RegistryPath: "teams/dev"}},
				{resolved: ResolvedArtifact{RegistryPath: "teams/dev"}},
			},
			want: false,
		},
		{
			name: "different paths",
			occs: []skillOccurrence{
				{resolved: ResolvedArtifact{RegistryPath: "teams/dev"}},
				{resolved: ResolvedArtifact{RegistryPath: "teams/pm"}},
			},
			want: true,
		},
		{
			name: "empty vs non-empty path",
			occs: []skillOccurrence{
				{resolved: ResolvedArtifact{RegistryPath: ""}},
				{resolved: ResolvedArtifact{RegistryPath: "teams/dev"}},
			},
			want: true,
		},
		{
			name: "three occurrences two differ",
			occs: []skillOccurrence{
				{resolved: ResolvedArtifact{RegistryPath: "a"}},
				{resolved: ResolvedArtifact{RegistryPath: "a"}},
				{resolved: ResolvedArtifact{RegistryPath: "b"}},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathsDiffer(tt.occs); got != tt.want {
				t.Errorf("pathsDiffer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsPathPrefix(t *testing.T) {
	tests := []struct {
		parent string
		child  string
		want   bool
	}{
		{"", "", false},                         // same path
		{"", "teams/dev", true},                 // root is prefix of everything
		{"teams/dev", "teams/dev", false},       // same path
		{"teams/dev", "teams/dev/eng", true},    // component-wise prefix
		{"teams/dev", "teams/dev-tools", false}, // NOT a prefix (hyphen, not slash)
		{"teams/dev", "teams", false},           // parent is longer, can't be prefix
		{"teams/dev", "", false},                // non-root can't be prefix of root
		{"a", "a/b", true},                      // single-component prefix
		{"a/b", "a/b/c/d", true},                // multi-level prefix
		{"a/b", "a/bc", false},                  // not a component boundary
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q_%q", tt.parent, tt.child), func(t *testing.T) {
			if got := isPathPrefix(tt.parent, tt.child); got != tt.want {
				t.Errorf("isPathPrefix(%q, %q) = %v, want %v", tt.parent, tt.child, got, tt.want)
			}
		})
	}
}

func TestGroupByScope(t *testing.T) {
	occs := []skillOccurrence{
		{resolved: ResolvedArtifact{Source: config.Organization, RegistryPath: "a"}},
		{resolved: ResolvedArtifact{Source: config.UserPersonal, RegistryPath: "b"}},
		{resolved: ResolvedArtifact{Source: config.RepoCommitted, RegistryPath: "c"}},
		{resolved: ResolvedArtifact{Source: config.RepoPersonal, RegistryPath: "d"}},
	}

	groups := groupByScope(occs)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups (machine + repo), got %d", len(groups))
	}

	machine := groups[0]
	if len(machine) != 2 {
		t.Fatalf("expected 2 machine-scoped entries, got %d", len(machine))
	}
	for _, occ := range machine {
		if !occ.resolved.Source.IsMachineScope() {
			t.Fatalf("expected machine scope, got %v", occ.resolved.Source)
		}
	}

	repo := groups[1]
	if len(repo) != 2 {
		t.Fatalf("expected 2 repo-scoped entries, got %d", len(repo))
	}
	for _, occ := range repo {
		if occ.resolved.Source.IsMachineScope() {
			t.Fatalf("expected repo scope, got %v", occ.resolved.Source)
		}
	}
}

func TestResolve_OverlappingPathNoFalseCollision(t *testing.T) {
	// Regression: when two registry entries share the same
	// URL+ref and one path is a component-wise prefix of the other
	// (e.g. "" and "teams/example-team/plugins", or "teams/dev" and
	// "teams/dev/eng"), the parent path discovers skills that also appear
	// in the child path's catalog. These are NOT genuine collisions —
	// they are the same physical skill from the same source. The resolver
	// must merge overlapping-path occurrences without emitting a
	// NameCollisionWarning.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:     "git@gitlab.example.com:example-org/example-resources.git",
				Ref:     "main",
				Path:    "", // parent: discovers everything including sub-directories
				Source:  config.Organization,
				Default: true,
			},
			{
				URL:     "git@gitlab.example.com:example-org/example-resources.git",
				Ref:     "main",
				Path:    "teams/example-team/plugins", // child: explicit sub-directory
				Source:  config.Organization,
				Default: true,
			},
		},
	}
	setCatalogKeys(cfg)

	// The same skill appears in both catalogs (parent discovers it via
	// recursive sub-bundle discovery; child discovers it directly).
	pluginSkill := catalog.Entry{Name: "shared-workflow", Path: "/cache/awesome-ai/teams/example-team/plugins/shared-workflow"}
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {pluginSkill},
		cfg.Registries[1].CatalogKey: {pluginSkill},
	}

	got, errs := Resolve(cfg, catalogs, nil)

	// The skill should be resolved exactly once (not duplicated).
	if len(got) != 1 {
		t.Fatalf("expected 1 resolved artifact, got %d: %#v", len(got), got)
	}

	// No collision warning — these are the same physical skill from the
	// same repository, just discovered via overlapping paths.
	for _, err := range errs {
		if IsNameCollision(err) {
			t.Errorf("unexpected NameCollisionWarning for overlapping paths: %v", err)
		}
	}

	// The winner should come from the more-specific (child) path, since
	// the child entry explicitly targets the sub-directory.
	if got[0].RegistryPath != "teams/example-team/plugins" {
		t.Errorf("expected winner from child path teams/example-team/plugins, got %q", got[0].RegistryPath)
	}
}

// --- Command-specific resolver tests ---

func TestResolveCommandCollisionAcrossRegistries(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:registry-a.git", Ref: "main", Commands: ks("deploy")},
			{URL: "git@example.com:registry-b.git", Ref: "main", Commands: ks("deploy")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:registry-a.git", "main"): {
			{Kind: ir.KindCommand, Name: "deploy", Path: "/cache/a/deploy"},
		},
		ck("git@example.com:registry-b.git", "main"): {
			{Kind: ir.KindCommand, Name: "deploy", Path: "/cache/b/deploy"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected one collision warning, got %v", errs)
	}
	if !IsNameCollision(errs[0]) {
		t.Fatalf("expected collision type, got %T: %v", errs[0], errs[0])
	}
	if !strings.Contains(errs[0].Error(), "deploy") {
		t.Fatalf("expected collision warning to mention deploy, got %v", errs[0])
	}

	// The colliding command should still be resolved (winner picked).
	names := make(map[string]bool)
	for _, a := range got {
		names[a.Name] = true
	}
	if !names["deploy"] {
		t.Fatalf("expected colliding command 'deploy' to be resolved, got %v", got)
	}
}

func TestResolveCommandTagSelection(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:     "git@example.com:team.git",
			Ref:     "main",
			Tags:    []string{"k8s"},
			Default: true,
		}},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:team.git", "main"): {
			{Kind: ir.KindCommand, Name: "k8s-rollout", Tags: []string{"k8s", "infra"}},
			{Kind: ir.KindCommand, Name: "python-lint", Tags: []string{"python"}},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	names := make(map[string]bool)
	for _, a := range got {
		names[a.Name] = true
	}
	if !names["k8s-rollout"] {
		t.Fatal("expected k8s-tagged command to be selected")
	}
	if names["python-lint"] {
		t.Fatal("expected python-tagged command to NOT be selected by k8s tag")
	}
}

func TestResolveCommandExcludes(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:team.git",
			Ref:      "main",
			Commands: kse([]string{"deploy", "debug"}, []string{"deploy"}),
		}},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:team.git", "main"): {
			{Kind: ir.KindCommand, Name: "deploy", Path: "/cache/deploy"},
			{Kind: ir.KindCommand, Name: "debug", Path: "/cache/debug"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	names := make(map[string]bool)
	for _, a := range got {
		names[a.Name] = true
	}
	if names["deploy"] {
		t.Fatal("expected 'deploy' command to be excluded")
	}
	if !names["debug"] {
		t.Fatal("expected 'debug' command to be resolved")
	}
}

func TestResolveCommandRequiredImmuneToExcludes(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:security.git",
			Ref:      "main",
			Required: true,
			Commands: ks("security-scan"),
		}},
	}

	repo := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:infra.git",
			Ref:      "main",
			Commands: kse(nil, []string{"security-scan"}),
		}},
	}

	merged := config.Merge(global, repo)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {
			{Kind: ir.KindCommand, Name: "security-scan", Path: "/cache/security-scan"},
		},
		ck("git@example.com:infra.git", "main"): {},
	}

	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected one warning for ineffective repo exclude, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), `exclude "security-scan"`) {
		t.Fatalf("expected ineffective exclude warning, got %v", errs[0])
	}

	names := make(map[string]bool)
	for _, a := range got {
		names[a.Name] = true
	}
	if !names["security-scan"] {
		t.Fatal("required command must be immune to repo excludes")
	}
}

func TestResolveCommandLocalExcludes(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:repo.git",
			Ref:      "main",
			Commands: ks("cmd-a", "cmd-b"),
		}},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:repo.git", "main"): {
			{Kind: ir.KindCommand, Name: "cmd-a", Path: "/cache/cmd-a"},
			{Kind: ir.KindCommand, Name: "cmd-b", Path: "/cache/cmd-b"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, &config.LocalConfig{Commands: kse(nil, []string{"cmd-a"})})
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	if len(got) != 1 || got[0].Name != "cmd-b" {
		t.Fatalf("expected only cmd-b after local exclude, got %#v", got)
	}
}

func BenchmarkResolve_OrganizationCrossEntryExclude(b *testing.B) {
	url := "git@example.com:shared.git"
	devCK := cache.CatalogKeyForEntry(url, "main", "teams/dev")
	oncallCK := cache.CatalogKeyForEntry(url, "main", "teams/oncall")

	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:     url,
				Ref:     "main",
				Path:    "teams/dev",
				Source:  config.Organization,
				Default: true,
				Skills:  kse([]string{"gitlab-info", "caveman"}, []string{"pagerduty-triage"}),
			},
			{
				URL:     url,
				Ref:     "main",
				Path:    "teams/oncall",
				Source:  config.Organization,
				Default: true,
				Skills:  kse([]string{"pagerduty-triage", "brainstorming"}, []string{"gitlab-info"}),
			},
		},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		devCK:    {{Name: "gitlab-info"}, {Name: "caveman"}},
		oncallCK: {{Name: "pagerduty-triage"}, {Name: "brainstorming"}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Resolve(cfg, catalogs, nil)
	}
}

// --- MCP-specific resolver tests ---

func TestResolveMcpServerCollisionAcrossRegistries(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:registry-a.git", Ref: "main", MCP: ks("youtrack")},
			{URL: "git@example.com:registry-b.git", Ref: "main", MCP: ks("youtrack")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:registry-a.git", "main"): {
			{Kind: ir.KindMcpServer, Name: "youtrack", Path: "/cache/a/youtrack"},
		},
		ck("git@example.com:registry-b.git", "main"): {
			{Kind: ir.KindMcpServer, Name: "youtrack", Path: "/cache/b/youtrack"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected one collision warning, got %v", errs)
	}
	if !IsNameCollision(errs[0]) {
		t.Fatalf("expected collision type, got %T: %v", errs[0], errs[0])
	}

	names := make(map[string]bool)
	for _, a := range got {
		names[a.Name] = true
	}
	if !names["youtrack"] {
		t.Fatalf("expected colliding MCP server 'youtrack' to be resolved, got %v", got)
	}
}

func TestResolveMcpServerExcludes(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL: "git@example.com:team.git",
			Ref: "main",
			MCP: kse([]string{"youtrack", "grafana"}, []string{"grafana"}),
		}},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:team.git", "main"): {
			{Kind: ir.KindMcpServer, Name: "youtrack", Path: "/cache/youtrack"},
			{Kind: ir.KindMcpServer, Name: "grafana", Path: "/cache/grafana"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	names := make(map[string]bool)
	for _, a := range got {
		names[a.Name] = true
	}
	if names["grafana"] {
		t.Fatal("expected 'grafana' MCP server to be excluded")
	}
	if !names["youtrack"] {
		t.Fatal("expected 'youtrack' MCP server to be resolved")
	}
}

func TestResolveMcpServerRequiredImmuneToExcludes(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:security.git",
			Ref:      "main",
			Required: true,
			MCP:      ks("security-mcp"),
		}},
	}

	repo := &config.Config{
		Registries: []config.RegistryEntry{{
			URL: "git@example.com:infra.git",
			Ref: "main",
			MCP: kse(nil, []string{"security-mcp"}),
		}},
	}

	merged := config.Merge(global, repo)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:security.git", "main"): {
			{Kind: ir.KindMcpServer, Name: "security-mcp", Path: "/cache/security-mcp"},
		},
		ck("git@example.com:infra.git", "main"): {},
	}

	setCatalogKeys(merged)

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected one warning for ineffective repo exclude, got %v", errs)
	}

	names := make(map[string]bool)
	for _, a := range got {
		names[a.Name] = true
	}
	if !names["security-mcp"] {
		t.Fatal("required MCP server must be immune to repo excludes")
	}
}

func TestResolveMcpServerTagSelection(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:     "git@example.com:team.git",
			Ref:     "main",
			Tags:    []string{"monitoring"},
			Default: true,
		}},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:team.git", "main"): {
			{Kind: ir.KindMcpServer, Name: "prometheus-mcp", Tags: []string{"monitoring", "infra"}},
			{Kind: ir.KindMcpServer, Name: "slack-mcp", Tags: []string{"comms"}},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	names := make(map[string]bool)
	for _, a := range got {
		names[a.Name] = true
	}
	if !names["prometheus-mcp"] {
		t.Fatal("expected monitoring-tagged MCP server to be selected")
	}
	if names["slack-mcp"] {
		t.Fatal("expected comms-tagged MCP server to NOT be selected by monitoring tag")
	}
}

func TestResolveMcpServerLocalExcludes(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL: "git@example.com:repo.git",
			Ref: "main",
			MCP: ks("mcp-a", "mcp-b"),
		}},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:repo.git", "main"): {
			{Kind: ir.KindMcpServer, Name: "mcp-a", Path: "/cache/mcp-a"},
			{Kind: ir.KindMcpServer, Name: "mcp-b", Path: "/cache/mcp-b"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, &config.LocalConfig{MCP: kse(nil, []string{"mcp-a"})})
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	if len(got) != 1 || got[0].Name != "mcp-b" {
		t.Fatalf("expected only mcp-b after local exclude, got %#v", got)
	}
}

func TestResolveMcpServerCrossKindNoCollision(t *testing.T) {
	// A skill named "deploy" and an MCP server named "deploy" should both
	// resolve without collision.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:     "git@example.com:team.git",
			Ref:     "main",
			Skills:  ks("deploy"),
			MCP:     ks("deploy"),
			Default: true,
		}},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:team.git", "main"): {
			{Kind: ir.KindSkill, Name: "deploy", Path: "/cache/deploy-skill"},
			{Kind: ir.KindMcpServer, Name: "deploy", Path: "/cache/deploy-mcp"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors for cross-kind same name, got %v", errs)
	}

	if len(got) != 2 {
		t.Fatalf("expected two resolved artifacts (skill + MCP server), got %d", len(got))
	}
}

func TestResolveMcpServerDefaultAutoSelects(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:     "git@example.com:team.git",
			Ref:     "main",
			Default: true,
		}},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:team.git", "main"): {
			{Kind: ir.KindMcpServer, Name: "auto-mcp", Path: "/cache/auto-mcp", Tags: []string{"infra"}},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	names := make(map[string]bool)
	for _, a := range got {
		names[a.Name] = true
	}
	if !names["auto-mcp"] {
		t.Fatal("expected default:true to auto-select MCP server")
	}
}

// --- Local config registries (RepoPersonal source) tests ---

// TestLocalRepoPersonalSourceResolves verifies that a registry entry with
// Source=RepoPersonal (contributed by .agents/config.local.yaml registries)
// resolves correctly and the resulting artifact carries the RepoPersonal source.
func TestLocalRepoPersonalSourceResolves(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:personal-infra.git",
			Ref:    "main",
			Source: config.RepoPersonal,
			Skills: ks("local-only-skill"),
		}},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {
			{Name: "local-only-skill", Path: "/cache/personal-infra/local-only-skill"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no resolution errors, got %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 resolved artifact, got %d: %#v", len(got), got)
	}
	if got[0].Name != "local-only-skill" {
		t.Fatalf("expected local-only-skill, got %q", got[0].Name)
	}
	if got[0].Source != config.RepoPersonal {
		t.Fatalf("expected Source=RepoPersonal, got %v", got[0].Source)
	}
	if got[0].IsGlobal() {
		t.Fatalf("RepoPersonal resources must not be machine-scoped")
	}
}

// TestLocalRepoPersonalCollisionWithRepoCommitted verifies that when a
// RepoPersonal registry and a RepoCommitted registry both provide the same
// skill name, the resolver detects a name collision and emits a warning.
// The RepoPersonal source has higher precedence and wins the tiebreak.
func TestLocalRepoPersonalCollisionWithRepoCommitted(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:    "git@example.com:repo.git",
				Ref:    "main",
				Source: config.RepoCommitted,
				Skills: ks("shared-skill"),
			},
			{
				URL:    "git@example.com:personal.git",
				Ref:    "main",
				Source: config.RepoPersonal,
				Skills: ks("shared-skill"),
			},
		},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {
			{Name: "shared-skill", Path: "/cache/repo/shared-skill"},
		},
		cfg.Registries[1].CatalogKey: {
			{Name: "shared-skill", Path: "/cache/personal/shared-skill"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected 1 collision warning, got %d: %v", len(errs), errs)
	}
	if !IsNameCollision(errs[0]) {
		t.Fatalf("expected collision type, got %T: %v", errs[0], errs[0])
	}
	if !strings.Contains(errs[0].Error(), "shared-skill") {
		t.Fatalf("expected collision warning to mention shared-skill, got %v", errs[0])
	}

	// The RepoPersonal entry has higher precedence, so it should win.
	names := make(map[string]ResolvedArtifact)
	for _, a := range got {
		names[a.Name] = a
	}
	if _, ok := names["shared-skill"]; !ok {
		t.Fatal("expected colliding shared-skill to be resolved")
	}
	if names["shared-skill"].Source != config.RepoPersonal {
		t.Fatalf("expected RepoPersonal to win (higher precedence), got %v", names["shared-skill"].Source)
	}
}

// TestLocalExcludeSuppressesRepoPersonalResource verifies that a top-level
// local config exclude (from .agents/config.local.yaml Skills.Exclude)
// suppresses a RepoPersonal-sourced resource. This confirms the dual-path:
// local config registries feed entries into the merged config with
// Source=RepoPersonal, and the top-level local exclude still applies.
func TestLocalExcludeSuppressesRepoPersonalResource(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:    "git@example.com:personal-infra.git",
				Ref:    "main",
				Source: config.RepoPersonal,
				Skills: ks("local-only-skill", "other-skill"),
			},
		},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {
			{Name: "local-only-skill", Path: "/cache/personal-infra/local-only-skill"},
			{Name: "other-skill", Path: "/cache/personal-infra/other-skill"},
		},
	}

	got, errs := Resolve(cfg, catalogs, &config.LocalConfig{
		Skills: kse(nil, []string{"local-only-skill"}),
	})
	if len(errs) != 0 {
		t.Fatalf("expected no resolution errors, got %v", errs)
	}

	names := make(map[string]bool)
	for _, r := range got {
		names[r.Name] = true
	}
	if names["local-only-skill"] {
		t.Fatal("expected local-only-skill to be suppressed by local exclude")
	}
	if !names["other-skill"] {
		t.Fatal("expected other-skill to remain (not excluded)")
	}
}

// TestLocalCrossLayerExcludeValidationForRepoPersonal verifies that
// validateCrossLayerExcludes correctly handles RepoPersonal-sourced registry
// entries. A RepoPersonal entry that tries to exclude a machine-scope
// (UserPersonal) skill produces an IneffectiveExcludeWarning with Scope="repo",
// because repo-scope entries cannot suppress machine-scope resources.
func TestLocalCrossLayerExcludeValidationForRepoPersonal(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:    "git@example.com:security.git",
				Ref:    "main",
				Source: config.UserPersonal,
				Skills: ks("mandatory-security-skill"),
			},
			{
				URL:    "git@example.com:personal-infra.git",
				Ref:    "main",
				Source: config.RepoPersonal,
				Skills: kse(nil, []string{"mandatory-security-skill"}),
			},
		},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {
			{Name: "mandatory-security-skill", Path: "/cache/security/mandatory-security-skill"},
		},
		cfg.Registries[1].CatalogKey: {},
	}

	got, errs := Resolve(cfg, catalogs, nil)

	// The machine-scope skill must survive the RepoPersonal exclude.
	names := make(map[string]bool)
	for _, r := range got {
		names[r.Name] = true
	}
	if !names["mandatory-security-skill"] {
		t.Fatal("expected machine-scope skill to survive RepoPersonal exclude")
	}

	// An IneffectiveExcludeWarning must be produced for the repo-scope exclude
	// targeting a machine-scope resource.
	var foundIneffective bool
	for _, err := range errs {
		var warn *IneffectiveExcludeWarning
		if errors.As(err, &warn) {
			if warn.Name == "mandatory-security-skill" && warn.Scope == "repo" {
				foundIneffective = true
				break
			}
		}
	}
	if !foundIneffective {
		t.Fatalf("expected IneffectiveExcludeWarning for RepoPersonal exclude of machine-scope skill, got errors: %v", errs)
	}
}

// ---------------------------------------------------------------------------
// ExcludeAll tests
// ---------------------------------------------------------------------------

func TestResolveSelectedResources_ExcludeAllRemovesAllOfKind(t *testing.T) {
	entry := config.RegistryEntry{
		URL:     "git@example.com:infra.git",
		Ref:     "main",
		Default: true,
		Skills:  config.KindSelector{ExcludeAll: true},
	}
	entry.CatalogKey = ck("git@example.com:infra.git", "main")

	cat := []catalog.Entry{
		{Kind: ir.KindSkill, Name: "skill-a"},
		{Kind: ir.KindSkill, Name: "skill-b"},
		{Kind: ir.KindCommand, Name: "command-a"},
	}

	selected := resolveSelectedResources(entry, cat)

	// All skills should be excluded; command should remain.
	for _, s := range selected {
		if s.kind == ir.KindSkill {
			t.Fatalf("expected no skills after ExcludeAll, got %s", s.name)
		}
	}
	if len(selected) != 1 || selected[0].kind != ir.KindCommand || selected[0].name != "command-a" {
		t.Fatalf("expected only command-a, got %#v", selected)
	}
}

func TestResolveSelectedResources_ExcludeAllRespectsRequired(t *testing.T) {
	entry := config.RegistryEntry{
		URL:      "git@example.com:infra.git",
		Ref:      "main",
		Required: true,
		Skills:   config.KindSelector{ExcludeAll: true},
	}
	entry.CatalogKey = ck("git@example.com:infra.git", "main")

	cat := []catalog.Entry{
		{Kind: ir.KindSkill, Name: "skill-a"},
		{Kind: ir.KindSkill, Name: "skill-b"},
	}

	selected := resolveSelectedResources(entry, cat)
	// Required entries are immune to excludes, so all skills should remain.
	if len(selected) != 2 {
		t.Fatalf("expected 2 skills (Required immune to ExcludeAll), got %d: %#v", len(selected), selected)
	}
}

func TestResolveLocalConfigSkillsExcludeAll(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:repo.git", Ref: "main", Default: true},
		},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {
			{Kind: ir.KindSkill, Name: "repo-skill-a"},
			{Kind: ir.KindSkill, Name: "repo-skill-b"},
			{Kind: ir.KindCommand, Name: "repo-command-a"},
		},
	}

	localCfg := &config.LocalConfig{
		Skills: config.KindSelector{ExcludeAll: true},
	}

	got, errs := Resolve(cfg, catalogs, localCfg)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	// All repo-scoped skills should be excluded; command should remain.
	for _, r := range got {
		if r.Kind == ir.KindSkill {
			t.Fatalf("expected no skills after local skills.exclude_all, got %s", r.Name)
		}
	}
	if len(got) != 1 || got[0].Name != "repo-command-a" {
		t.Fatalf("expected only repo-command-a, got %#v", got)
	}
}

func TestResolveLocalConfigSkillsExcludeAllDoesNotAffectMachineScope(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:global.git", Ref: "main", Default: true},
		},
	}
	repo := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:repo.git", Ref: "main", Default: true},
		},
	}
	merged := config.Merge(global, repo)
	setCatalogKeys(merged)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:global.git", "main"): {
			{Kind: ir.KindSkill, Name: "global-skill"},
		},
		ck("git@example.com:repo.git", "main"): {
			{Kind: ir.KindSkill, Name: "repo-skill"},
		},
	}

	localCfg := &config.LocalConfig{
		Skills: config.KindSelector{ExcludeAll: true},
	}

	got, errs := Resolve(merged, catalogs, localCfg)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	// Machine-scope (global) skill should survive; repo-scope skill should be excluded.
	names := resolvedNames(got)
	if !names["global-skill"] {
		t.Fatal("expected global-skill to survive local skills.exclude_all (machine-scope immune)")
	}
	if names["repo-skill"] {
		t.Fatal("expected repo-skill to be excluded by local skills.exclude_all")
	}
}

func TestResolvePerRegistryExcludeAllScopedByURL(t *testing.T) {
	// Repo config has two registries with default:true.
	repo := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:plugins-testing.git", Ref: "main", Default: true},
			{URL: "git@example.com:plugins-review.git", Ref: "main", Default: true},
		},
	}
	// Local config adds entries with ExcludeAll on testing only.
	local := &config.LocalConfig{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:plugins-testing.git", Ref: "main", Skills: config.KindSelector{ExcludeAll: true}},
		},
	}
	merged := config.MergeLayers([]config.Layer{
		{Source: config.RepoCommitted, Config: repo},
		{Source: config.RepoPersonal, Config: &config.Config{Registries: local.Registries}},
	})
	setCatalogKeys(merged)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:plugins-testing.git", "main"): {
			{Kind: ir.KindSkill, Name: "testing-skill-a"},
			{Kind: ir.KindSkill, Name: "testing-skill-b"},
		},
		ck("git@example.com:plugins-review.git", "main"): {
			{Kind: ir.KindSkill, Name: "review-skill-a"},
		},
	}

	got, errs := Resolve(merged, catalogs, local)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	names := resolvedNames(got)
	// Testing skills should be excluded.
	if names["testing-skill-a"] || names["testing-skill-b"] {
		t.Fatal("expected testing skills to be excluded by per-registry ExcludeAll")
	}
	// Review skills should remain (URL-scoped, no cross-registry bleed).
	if !names["review-skill-a"] {
		t.Fatal("expected review-skill-a to survive (ExcludeAll is scoped by registry URL)")
	}
}

func TestResolveRegistryEntryExcludeAllSugarPropagatesToAllKinds(t *testing.T) {
	// Test that ExcludeAll on RegistryEntry is propagated to all per-kind
	// selectors during merge resolution (the sugar mechanism).
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main", Default: true, ExcludeAll: true},
		},
	}
	// Must go through MergeLayers to trigger resolveRegistryEntry which
	// propagates the ExcludeAll sugar to per-kind selectors.
	merged := config.MergeLayers([]config.Layer{
		{Source: config.RepoCommitted, Config: cfg},
	})
	setCatalogKeys(merged)

	catalogs := map[string][]catalog.Entry{
		merged.Registries[0].CatalogKey: {
			{Kind: ir.KindSkill, Name: "skill-a"},
			{Kind: ir.KindCommand, Name: "command-a"},
			{Kind: ir.KindSubagent, Name: "subagent-a"},
		},
	}

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	// ExcludeAll sugar removes all resources from the entry's own selection.
	if len(got) != 0 {
		t.Fatalf("expected 0 resources after entry-level ExcludeAll sugar, got %d: %#v", len(got), got)
	}
}

func TestResolvePerRegistryExcludeAllSugarCrossLayer(t *testing.T) {
	repo := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:plugins-testing.git", Ref: "main", Default: true},
			{URL: "git@example.com:plugins-review.git", Ref: "main", Default: true},
		},
	}
	local := &config.LocalConfig{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:plugins-testing.git", Ref: "main", ExcludeAll: true},
		},
	}
	merged := config.MergeLayers([]config.Layer{
		{Source: config.RepoCommitted, Config: repo},
		{Source: config.RepoPersonal, Config: &config.Config{Registries: local.Registries}},
	})
	setCatalogKeys(merged)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:plugins-testing.git", "main"): {
			{Kind: ir.KindSkill, Name: "testing-skill"},
			{Kind: ir.KindCommand, Name: "testing-command"},
		},
		ck("git@example.com:plugins-review.git", "main"): {
			{Kind: ir.KindSkill, Name: "review-skill"},
		},
	}

	got, errs := Resolve(merged, catalogs, local)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	names := resolvedNames(got)
	// All resources from testing registry should be excluded (ExcludeAll sugar).
	if names["testing-skill"] || names["testing-command"] {
		t.Fatal("expected all testing resources excluded by ExcludeAll sugar")
	}
	// Review resources should remain.
	if !names["review-skill"] {
		t.Fatal("expected review-skill to survive (ExcludeAll scoped by URL)")
	}
}

func TestApplyCrossLayerExcludesUsesFilePrecedenceWithinLayer(t *testing.T) {
	for _, source := range []config.Source{config.RepoCommitted, config.UserPersonal} {
		t.Run(source.String(), func(t *testing.T) {
			artifact := ResolvedArtifact{
				Entry:          catalog.Entry{Kind: ir.KindSkill, Name: "included-skill"},
				RegistryURL:    "git@example.com:team.git",
				Source:         source,
				FilePrecedence: 1,
			}
			shadow := config.RegistryEntry{
				URL:            "git@example.com:team.git",
				Source:         source,
				FilePrecedence: 2,
				Skills:         config.KindSelector{Exclude: []string{"included-skill"}},
			}
			if got := applyCrossLayerExcludes([]ResolvedArtifact{artifact}, []config.RegistryEntry{shadow}); len(got) != 0 {
				t.Fatalf("later file did not suppress earlier artifact: %#v", got)
			}

			shadow.FilePrecedence = artifact.FilePrecedence
			if got := applyCrossLayerExcludes([]ResolvedArtifact{artifact}, []config.RegistryEntry{shadow}); len(got) != 1 {
				t.Fatalf("same file suppressed artifact: %#v", got)
			}

			shadow.FilePrecedence = 0
			artifact.FilePrecedence = 0
			if got := applyCrossLayerExcludes([]ResolvedArtifact{artifact}, []config.RegistryEntry{shadow}); len(got) != 1 {
				t.Fatalf("zero file precedence suppressed artifact: %#v", got)
			}
		})
	}
}

func TestResolveSkipsShadowedMissingSelectionsBeforeNotFound(t *testing.T) {
	for _, kind := range []ir.ResourceKind{ir.KindSkill, ir.KindCollection} {
		t.Run(kind.String(), func(t *testing.T) {
			cfg := &config.Config{Registries: []config.RegistryEntry{
				{URL: "registry", CatalogKey: "catalog", Source: config.RepoCommitted, FilePrecedence: 1},
				{URL: "registry", CatalogKey: "catalog", Source: config.RepoCommitted, FilePrecedence: 2},
			}}
			cfg.Registries[0].SelectorForKindPtr(kind).Include = []string{"missing"}
			cfg.Registries[1].SelectorForKindPtr(kind).Exclude = []string{"missing"}
			catalogs := map[string][]catalog.Entry{"catalog": {}}
			unshadowed := *cfg
			unshadowed.Registries = cfg.Registries[:1]
			if _, errs := Resolve(&unshadowed, catalogs, nil); len(errs) == 0 {
				t.Fatal("unshadowed missing selection did not report an error")
			}
			resolved, errs := Resolve(cfg, catalogs, nil)
			if len(errs) != 0 || len(resolved) != 0 {
				t.Fatalf("resolved=%#v errors=%v, want shadowed missing selection skipped", resolved, errs)
			}
		})
	}
}

func TestDeduplicateOccurrencesPrefersLaterFileMetadata(t *testing.T) {
	occurrences := []skillOccurrence{
		{resolved: ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: "registry", Source: config.RepoCommitted, FilePrecedence: 1, CatalogKey: "included"}, url: "registry", order: 0},
		{resolved: ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: "registry", Source: config.RepoCommitted, FilePrecedence: 2, CatalogKey: "root"}, url: "registry", order: 1},
	}
	got := deduplicateOccurrences(occurrences)
	if len(got) != 1 || got[0].resolved.CatalogKey != "root" {
		t.Fatalf("deduplicated = %#v, want later root metadata", got)
	}
}

func TestShadowedCollisionOccurrenceFallsBackWithoutWarning(t *testing.T) {
	selection := resourceSelection{kind: ir.KindSkill, name: "shared"}
	byName := map[resourceSelection][]skillOccurrence{
		selection: {
			{resolved: ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: "shadowed", Source: config.RepoCommitted, FilePrecedence: 1}, url: "shadowed", order: 0},
			{resolved: ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: "active", Source: config.RepoCommitted, FilePrecedence: 1}, url: "active", order: 1},
		},
	}
	shadow := config.RegistryEntry{URL: "shadowed", Source: config.RepoCommitted, FilePrecedence: 2, Skills: config.KindSelector{ExcludeAll: true}}
	filterShadowedOccurrences(byName, []config.RegistryEntry{shadow})
	winners, warnings := pickWinners(byName)
	if len(warnings) != 0 || len(winners) != 1 || winners[0].resolved.RegistryURL != "active" {
		t.Fatalf("winners=%#v warnings=%v, want active fallback without collision", winners, warnings)
	}
}

func TestCollectionCandidateAndMemberExclusionsRespectAuthority(t *testing.T) {
	included := config.RegistryEntry{URL: "registry", Source: config.RepoCommitted, FilePrecedence: 1}
	shadow := config.RegistryEntry{URL: "registry", Source: config.RepoCommitted, FilePrecedence: 2, Skills: config.KindSelector{ExcludeAll: true}}
	candidates := []memberEntryCandidate{{entry: included, catalogKey: "included"}}
	if got := candidatesNotScopedAway(candidates, ir.KindSkill, []config.RegistryEntry{included, shadow}); len(got) != 0 {
		t.Fatalf("shadowed collection candidate remained eligible: %#v", got)
	}

	rootMember := ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "member"}, RegistryURL: "registry", Source: config.RepoCommitted, FilePrecedence: 2}
	lowerExclude := config.RegistryEntry{URL: "registry", Source: config.RepoCommitted, FilePrecedence: 1, Skills: config.KindSelector{Exclude: []string{"member"}}}
	if collectionMemberShadowedByEntries(rootMember, []config.RegistryEntry{lowerExclude}) {
		t.Fatal("lower included fragment excluded a root-selected collection member")
	}
	higherExclude := lowerExclude
	higherExclude.FilePrecedence = 3
	if !collectionMemberShadowedByEntries(rootMember, []config.RegistryEntry{higherExclude}) {
		t.Fatal("higher-authority exclusion did not suppress collection member")
	}
	rootMember.Required = true
	if collectionMemberShadowedByEntries(rootMember, []config.RegistryEntry{higherExclude}) {
		t.Fatal("required collection member was excluded")
	}
}

func TestResolveExcludeAllAtRefDoesNotBleedAcrossRefs(t *testing.T) {
	main := ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: "git@example.com:shared.git", RegistryPath: "skills", Ref: "main", Source: config.RepoCommitted, FilePrecedence: 1}
	release := main
	release.Ref = "release"
	shadow := config.RegistryEntry{
		URL: "git@example.com:shared.git", Path: "skills", Ref: "release",
		Source: config.RepoCommitted, FilePrecedence: 2,
		Skills: config.KindSelector{ExcludeAll: true},
	}
	got := applyCrossLayerExcludes([]ResolvedArtifact{main, release}, []config.RegistryEntry{shadow})
	if len(got) != 1 || got[0].Ref != "main" {
		t.Fatalf("resolved = %#v, want main ref preserved and release excluded", got)
	}
}

func TestResolveExcludeAllDoesNotBleedAcrossRegistries(t *testing.T) {
	// Two registries with different skill names; ExcludeAll on one should not
	// affect the other. This verifies URL scoping in applyCrossLayerExcludes.
	repo := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:reg-a.git", Ref: "main", Default: true},
			{URL: "git@example.com:reg-b.git", Ref: "main", Default: true},
		},
	}
	local := &config.LocalConfig{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:reg-a.git", Ref: "main", Skills: config.KindSelector{ExcludeAll: true}},
		},
	}
	merged := config.MergeLayers([]config.Layer{
		{Source: config.RepoCommitted, Config: repo},
		{Source: config.RepoPersonal, Config: &config.Config{Registries: local.Registries}},
	})
	setCatalogKeys(merged)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:reg-a.git", "main"): {
			{Kind: ir.KindSkill, Name: "skill-from-a"},
		},
		ck("git@example.com:reg-b.git", "main"): {
			{Kind: ir.KindSkill, Name: "skill-from-b"},
		},
	}

	got, errs := Resolve(merged, catalogs, local)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	names := resolvedNames(got)
	// skill-from-a should be excluded (ExcludeAll on reg-a).
	if names["skill-from-a"] {
		t.Fatal("expected skill-from-a to be excluded by ExcludeAll on reg-a")
	}
	// skill-from-b should survive (ExcludeAll is scoped by URL, no bleed).
	if !names["skill-from-b"] {
		t.Fatal("expected skill-from-b to survive (ExcludeAll scoped by URL, no cross-registry bleed)")
	}
}

func TestResolveExcludeAllScopedByPathNotJustURL(t *testing.T) {
	// Regression: exclude_all on one path of a registry URL
	// must NOT suppress resources selected from a *different* path of the
	// same URL. Previously applyCrossLayerExcludes matched on URL only, so
	// an exclude_all intended for teams/example-team/plugins/oncall-engineering
	// silently nuked every repo-scoped resource of the shared awesome-ai.git
	// URL, including the unrelated pm-market-research bundle.
	const url = "git@gitlab.example.com:example-org/example-resources.git"
	repo := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: url, Ref: "main", Path: "teams/example-product/plugins/pm-market-research", Default: true},
		},
	}
	local := &config.LocalConfig{
		Registries: []config.RegistryEntry{
			{URL: url, Ref: "main", Path: "teams/example-team/plugins/oncall-engineering", ExcludeAll: true},
		},
	}
	merged := config.MergeLayers([]config.Layer{
		{Source: config.RepoCommitted, Config: repo},
		{Source: config.RepoPersonal, Config: &config.Config{Registries: local.Registries}},
	})
	setCatalogKeys(merged)

	catalogs := map[string][]catalog.Entry{
		ck(url, "main", "teams/example-product/plugins/pm-market-research"): {
			{Kind: ir.KindSkill, Name: "competitor-analysis"},
			{Kind: ir.KindSkill, Name: "market-sizing"},
			{Kind: ir.KindCommand, Name: "pm-report"},
		},
		ck(url, "main", "teams/example-team/plugins/oncall-engineering"): {
			{Kind: ir.KindSkill, Name: "oncall-runbook"},
		},
	}

	got, errs := Resolve(merged, catalogs, local)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	names := resolvedNames(got)
	if !names["competitor-analysis"] || !names["market-sizing"] || !names["pm-report"] {
		t.Fatalf("expected pm-market-research resources to survive exclude_all on a different path, got %v", names)
	}
}

func TestResolveExcludeAllRootPathCoversSubPaths(t *testing.T) {
	// A root-scoped (empty path) exclude_all suppresses resources from any
	// sub-path of the same URL, matching the "root discovers everything"
	// convention used elsewhere (isPathPrefix). This documents the chosen
	// semantics for the empty-path edge case.
	const url = "git@example.com:awesome-ai.git"
	repo := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: url, Ref: "main", Path: "teams/example-product", Default: true},
		},
	}
	local := &config.LocalConfig{
		Registries: []config.RegistryEntry{
			{URL: url, Ref: "main", ExcludeAll: true}, // root path (empty)
		},
	}
	merged := config.MergeLayers([]config.Layer{
		{Source: config.RepoCommitted, Config: repo},
		{Source: config.RepoPersonal, Config: &config.Config{Registries: local.Registries}},
	})
	setCatalogKeys(merged)

	catalogs := map[string][]catalog.Entry{
		ck(url, "main", "teams/example-product"): {
			{Kind: ir.KindSkill, Name: "pm-skill"},
		},
		// Root-path entry (the exclude_all one) still needs a catalog present.
		ck(url, "main", ""): {},
	}

	got, errs := Resolve(merged, catalogs, local)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if resolvedNames(got)["pm-skill"] {
		t.Fatal("expected root exclude_all to suppress resources from sub-paths of the same URL")
	}
}

func TestResolveExcludeAllNonRootParentCoversChild(t *testing.T) {
	// Exercises the non-root prefix branch of pathScopeCovers: an exclude_all
	// on a parent path (teams/example-team) must suppress resources discovered
	// at a deeper child path (teams/example-team/plugins/oncall-engineering)
	// while leaving a sibling path (teams/example-product/...) untouched.
	const url = "git@example.com:awesome-ai.git"
	repo := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: url, Ref: "main", Path: "teams/example-team/plugins/oncall-engineering", Default: true},
			{URL: url, Ref: "main", Path: "teams/example-product/plugins/pm-market-research", Default: true},
		},
	}
	local := &config.LocalConfig{
		Registries: []config.RegistryEntry{
			{URL: url, Ref: "main", Path: "teams/example-team", ExcludeAll: true}, // parent path
		},
	}
	merged := config.MergeLayers([]config.Layer{
		{Source: config.RepoCommitted, Config: repo},
		{Source: config.RepoPersonal, Config: &config.Config{Registries: local.Registries}},
	})
	setCatalogKeys(merged)

	catalogs := map[string][]catalog.Entry{
		ck(url, "main", "teams/example-team/plugins/oncall-engineering"): {
			{Kind: ir.KindSkill, Name: "oncall-runbook"},
		},
		ck(url, "main", "teams/example-product/plugins/pm-market-research"): {
			{Kind: ir.KindSkill, Name: "competitor-analysis"},
		},
		// Parent path entry (the exclude_all one) still needs a catalog present.
		ck(url, "main", "teams/example-team"): {},
	}

	got, errs := Resolve(merged, catalogs, local)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	names := resolvedNames(got)
	if names["oncall-runbook"] {
		t.Fatal("expected exclude_all on parent path teams/example-team to suppress the child oncall-runbook")
	}
	if !names["competitor-analysis"] {
		t.Fatal("expected the sibling pm-market-research path to survive the parent-scoped exclude_all")
	}
}

func TestResolveLocalConfigPerKindExcludeAllKeepsOtherKinds(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:repo.git", Ref: "main", Default: true},
		},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {
			{Kind: ir.KindSkill, Name: "skill-a"},
			{Kind: ir.KindSkill, Name: "skill-b"},
			{Kind: ir.KindCommand, Name: "command-a"},
			{Kind: ir.KindSubagent, Name: "subagent-a"},
		},
	}

	// Exclude all skills via local config top-level.
	localCfg := &config.LocalConfig{
		Skills: config.KindSelector{ExcludeAll: true},
	}

	got, errs := Resolve(cfg, catalogs, localCfg)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	names := resolvedNames(got)
	if names["skill-a"] || names["skill-b"] {
		t.Fatal("expected all skills excluded by local skills.exclude_all")
	}
	if !names["command-a"] {
		t.Fatal("expected command-a to survive (only skills excluded)")
	}
	if !names["subagent-a"] {
		t.Fatal("expected subagent-a to survive (only skills excluded)")
	}
}

func TestResolveOrganizationRequiredIgnoresRepoExclude(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:      "git@example.com:org.git",
				Ref:      "main",
				Required: true,
				Skills:   ks("required-skill"),
				Source:   config.Organization,
			},
			{
				URL:    "git@example.com:repo.git",
				Ref:    "main",
				Skills: kse(nil, []string{"required-skill"}),
				Source: config.RepoCommitted,
			},
		},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:org.git", "main"):  {{Name: "required-skill", Path: "/cache/org/required-skill"}},
		ck("git@example.com:repo.git", "main"): {},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected one ineffective repo exclude warning, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), `exclude "required-skill" in repo config is ineffective`) {
		t.Fatalf("expected ineffective repo exclude warning, got: %v", errs[0])
	}

	names := resolvedNames(got)
	if !names["required-skill"] {
		t.Fatalf("expected required-skill to survive repo exclude, got: %#v", got)
	}
	for _, r := range got {
		if r.Name == "required-skill" {
			if !r.Required {
				t.Fatalf("expected required-skill to remain required")
			}
			if r.Source != config.Organization {
				t.Fatalf("expected required-skill source %v, got %v", config.Organization, r.Source)
			}
		}
	}
}

func TestResolveOrganizationDefaultSuppressedByUserNotRepo(t *testing.T) {
	t.Run("user suppresses", func(t *testing.T) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{
				{
					URL:     "git@example.com:org.git",
					Ref:     "main",
					Default: true,
					Source:  config.Organization,
				},
				{
					URL:    "git@example.com:user.git",
					Ref:    "main",
					Skills: kse(nil, []string{"default-skill"}),
					Source: config.UserPersonal,
				},
			},
		}
		setCatalogKeys(cfg)

		catalogs := map[string][]catalog.Entry{
			ck("git@example.com:org.git", "main"):  {{Name: "default-skill", Path: "/cache/org/default-skill"}},
			ck("git@example.com:user.git", "main"): {},
		}

		got, errs := Resolve(cfg, catalogs, nil)
		if len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
		if resolvedNames(got)["default-skill"] {
			t.Fatalf("expected default-skill to be suppressed by user config")
		}
	})

	t.Run("repo cannot suppress", func(t *testing.T) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{
				{
					URL:     "git@example.com:org.git",
					Ref:     "main",
					Default: true,
					Source:  config.Organization,
				},
				{
					URL:    "git@example.com:repo.git",
					Ref:    "main",
					Skills: kse(nil, []string{"default-skill"}),
					Source: config.RepoCommitted,
				},
			},
		}
		setCatalogKeys(cfg)

		catalogs := map[string][]catalog.Entry{
			ck("git@example.com:org.git", "main"):  {{Name: "default-skill", Path: "/cache/org/default-skill"}},
			ck("git@example.com:repo.git", "main"): {},
		}

		got, errs := Resolve(cfg, catalogs, nil)
		if len(errs) != 1 {
			t.Fatalf("expected one ineffective repo exclude warning, got %v", errs)
		}
		if !strings.Contains(errs[0].Error(), `exclude "default-skill" in repo config is ineffective`) {
			t.Fatalf("expected ineffective repo exclude warning, got: %v", errs[0])
		}
		if !resolvedNames(got)["default-skill"] {
			t.Fatalf("expected default-skill to survive repo exclude, got: %#v", got)
		}
	})
}

func TestResolveOrganizationDefaultNotSuppressedByAnotherOrganizationEntry(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:     "git@example.com:org.git",
				Ref:     "main",
				Default: true,
				Source:  config.Organization,
			},
			{
				URL:    "git@example.com:team.git",
				Ref:    "main",
				Skills: kse(nil, []string{"default-skill"}),
				Source: config.Organization,
			},
		},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:org.git", "main"):  {{Name: "default-skill", Path: "/cache/org/default-skill"}},
		ck("git@example.com:team.git", "main"): {},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if !resolvedNames(got)["default-skill"] {
		t.Fatalf("expected default-skill to survive organization entry exclude, got: %#v", got)
	}
}

func TestResolveTargetHarnesses(t *testing.T) {
	t.Run("nil entry harnesses inherit target set", func(t *testing.T) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{{
				URL:    "git@example.com:infra.git",
				Ref:    "main",
				Skills: ks("vm-lifecycle"),
			}},
		}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			ck("git@example.com:infra.git", "main"): {{Name: "vm-lifecycle", Path: "/cache/infra/vm-lifecycle"}},
		}

		targets := []hcc.Target{hcc.TargetClaude, hcc.TargetFactory}
		got, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses(targets))
		if len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 artifact, got %d", len(got))
		}
		if !harnessesEqual(got[0].Harnesses, targets) {
			t.Fatalf("expected harnesses %v, got %v", targets, got[0].Harnesses)
		}
	})

	t.Run("empty target set preserves nil entry harnesses", func(t *testing.T) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{{
				URL:    "git@example.com:infra.git",
				Ref:    "main",
				Skills: ks("vm-lifecycle"),
			}},
		}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			ck("git@example.com:infra.git", "main"): {{Name: "vm-lifecycle", Path: "/cache/infra/vm-lifecycle"}},
		}

		got, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses(nil))
		if len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 artifact, got %d", len(got))
		}
		if got[0].Harnesses != nil {
			t.Fatalf("expected nil harnesses when no target set, got %v", got[0].Harnesses)
		}
	})

	t.Run("per-entry harnesses are intersected and not expanded", func(t *testing.T) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{{
				URL:       "git@example.com:infra.git",
				Ref:       "main",
				Skills:    ks("vm-lifecycle"),
				Harnesses: []string{string(hcc.TargetClaude), string(hcc.TargetCodex), string(hcc.TargetGemini)},
			}},
		}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			ck("git@example.com:infra.git", "main"): {{Name: "vm-lifecycle", Path: "/cache/infra/vm-lifecycle"}},
		}

		targets := []hcc.Target{hcc.TargetCodex, hcc.TargetFactory}
		got, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses(targets))
		if len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 artifact, got %d", len(got))
		}
		want := []hcc.Target{hcc.TargetCodex}
		if !harnessesEqual(got[0].Harnesses, want) {
			t.Fatalf("expected harnesses %v, got %v", want, got[0].Harnesses)
		}
	})

	t.Run("incompatible organization required entry returns hard error", func(t *testing.T) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{{
				URL:       "git@example.com:org.git",
				Ref:       "main",
				Required:  true,
				Source:    config.Organization,
				Skills:    ks("required-skill"),
				Harnesses: []string{string(hcc.TargetGemini)},
			}},
		}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			ck("git@example.com:org.git", "main"): {{Name: "required-skill", Path: "/cache/org/required-skill"}},
		}

		targets := []hcc.Target{hcc.TargetClaude, hcc.TargetFactory}
		got, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses(targets))
		if len(got) != 0 {
			t.Fatalf("expected no resolved artifacts, got %d", len(got))
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %v", errs)
		}
		var reqErr *IncompatibleOrganizationRequirement
		if !errors.As(errs[0], &reqErr) {
			t.Fatalf("expected *IncompatibleOrganizationRequirement, got %T", errs[0])
		}
		if IsWarning(errs[0]) {
			t.Fatalf("expected hard error, got warning")
		}
		if reqErr.Name != "required-skill" {
			t.Fatalf("expected error name %q, got %q", "required-skill", reqErr.Name)
		}
	})

	t.Run("incompatible organization default entry returns warning", func(t *testing.T) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{{
				URL:       "git@example.com:org.git",
				Ref:       "main",
				Default:   true,
				Source:    config.Organization,
				Harnesses: []string{string(hcc.TargetGemini)},
			}},
		}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			ck("git@example.com:org.git", "main"): {{Name: "default-skill", Path: "/cache/org/default-skill"}},
		}

		targets := []hcc.Target{hcc.TargetClaude, hcc.TargetFactory}
		got, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses(targets))
		if resolvedNames(got)["default-skill"] {
			t.Fatalf("expected default-skill to be skipped")
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %v", errs)
		}
		if !IsWarning(errs[0]) {
			t.Fatalf("expected warning, got %T", errs[0])
		}
		var warn *IncompatibleOrganizationDefault
		if !errors.As(errs[0], &warn) {
			t.Fatalf("expected *IncompatibleOrganizationDefault, got %T", errs[0])
		}
		if warn.Name != "default-skill" {
			t.Fatalf("expected warning name %q, got %q", "default-skill", warn.Name)
		}
	})

	t.Run("local resources intersect with target set", func(t *testing.T) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{{
				URL:    "git@example.com:repo.git",
				Ref:    "main",
				Source: config.RepoCommitted,
			}},
		}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			ck("git@example.com:repo.git", "main"): {
				{Name: "repo-skill", Path: "/cache/repo/repo-skill"},
				{Name: "local-skill", Path: "/cache/repo/local-skill"},
			},
		}
		localCfg := &config.LocalConfig{Skills: ks("local-skill")}

		targets := []hcc.Target{hcc.TargetFactory}
		got, errs := Resolve(cfg, catalogs, localCfg, WithTargetHarnesses(targets))
		if len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
		names := make(map[string]ResolvedArtifact)
		for _, r := range got {
			names[r.Name] = r
		}
		if !harnessesEqual(names["local-skill"].Harnesses, targets) {
			t.Fatalf("expected local-skill harnesses %v, got %v", targets, names["local-skill"].Harnesses)
		}
	})

	t.Run("collection members inherit and intersect with target set", func(t *testing.T) {
		col := ir.NewCollection("tools", "", "", []ir.CollectionMember{
			{Name: "member-skill", Kind: ir.KindSkill},
		}, nil, nil, ir.SourceRef{})
		cfg := &config.Config{
			Registries: []config.RegistryEntry{{
				URL:         "git@example.com:repo.git",
				Ref:         "main",
				Source:      config.RepoCommitted,
				Collections: ks("tools"),
			}},
		}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			ck("git@example.com:repo.git", "main"): {
				{Kind: ir.KindCollection, Name: "tools", Resource: col, Path: "/cache/repo/collections/tools"},
				{Name: "member-skill", Path: "/cache/repo/member-skill"},
			},
		}

		targets := []hcc.Target{hcc.TargetClaude, hcc.TargetFactory}
		got, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses(targets))
		if len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
		names := make(map[string]ResolvedArtifact)
		for _, r := range got {
			names[r.Name] = r
		}
		if names["tools"].Kind == ir.KindCollection {
			t.Fatalf("expected collection to be removed after expansion")
		}
		if _, ok := names["member-skill"]; !ok {
			t.Fatalf("expected member-skill to be resolved")
		}
		if !harnessesEqual(names["member-skill"].Harnesses, targets) {
			t.Fatalf("expected member harnesses %v, got %v", targets, names["member-skill"].Harnesses)
		}
	})

	t.Run("collection member per-entry harnesses are intersected with parent", func(t *testing.T) {
		col := ir.NewCollection("tools", "", "", []ir.CollectionMember{
			{Name: "member-skill", Kind: ir.KindSkill},
		}, nil, nil, ir.SourceRef{})
		cfg := &config.Config{
			Registries: []config.RegistryEntry{{
				URL:         "git@example.com:repo.git",
				Ref:         "main",
				Source:      config.RepoCommitted,
				Collections: ks("tools"),
				Harnesses:   []string{string(hcc.TargetClaude), string(hcc.TargetGemini)},
			}},
		}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			ck("git@example.com:repo.git", "main"): {
				{Kind: ir.KindCollection, Name: "tools", Resource: col, Path: "/cache/repo/collections/tools"},
				{Name: "member-skill", Path: "/cache/repo/member-skill"},
			},
		}

		targets := []hcc.Target{hcc.TargetClaude, hcc.TargetFactory}
		got, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses(targets))
		if len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
		want := []hcc.Target{hcc.TargetClaude}
		names := make(map[string]ResolvedArtifact)
		for _, r := range got {
			names[r.Name] = r
		}
		if !harnessesEqual(names["member-skill"].Harnesses, want) {
			t.Fatalf("expected member harnesses %v, got %v", want, names["member-skill"].Harnesses)
		}
	})
}

type contextCapturingMarketplaceResolver struct {
	got context.Context
}

func (r *contextCapturingMarketplaceResolver) Resolve(ctx context.Context, _ *config.Config, _ []hcc.Target, _ MarketplaceResolveMode) ([]ResolvedArtifact, []error) {
	r.got = ctx
	return nil, []error{ctx.Err()}
}

func TestResolvePassesCallerContextToMarketplaceResolver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mpResolver := &contextCapturingMarketplaceResolver{}
	digest := "sha256:" + strings.Repeat("a", 64)
	cfg := &config.Config{
		Marketplaces: []config.MarketplaceEntry{{
			URL:        "https://marketplace.example",
			InstanceID: "mp",
			Resources: []config.MarketplaceResourcePin{{
				RegistryID: "src",
				Kind:       "skill",
				Name:       "review",
				Revision:   digest,
				Digest:     digest,
			}},
		}},
	}

	_, errs := Resolve(cfg, nil, nil, WithContext(ctx), WithMarketplaceResolver(mpResolver))
	if mpResolver.got != ctx {
		t.Fatalf("Marketplace resolver context = %v, want caller context", mpResolver.got)
	}
	if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
		t.Fatalf("Resolve errors = %v, want context cancellation", errs)
	}
}

// harnessesEqual reports whether two harness target slices are equal, including
// nil-vs-empty distinction and order.
func harnessesEqual(a, b []hcc.Target) bool {
	return reflect.DeepEqual(a, b)
}

// resolvedNames returns a set of resource names from a resolved artifact list.
func resolvedNames(got []ResolvedArtifact) map[string]bool {
	names := make(map[string]bool)
	for _, r := range got {
		names[r.Name] = true
	}
	return names
}
