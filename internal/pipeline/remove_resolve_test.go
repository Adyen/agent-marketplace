package pipeline

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func artifact(kind ir.ResourceKind, name, url, path string, source config.Source) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{
		Entry:        catalog.Entry{Kind: kind, Name: name},
		RegistryURL:  url,
		RegistryPath: path,
		Source:       source,
	}
}

// A single match resolves cleanly regardless of kind filtering.
func TestResolvedResourceByName_SingleMatch(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "sql-queries", "reg", "plugins", config.UserPersonal),
	}
	got, err := resolvedResourceByName(resolved, "sql-queries", ir.KindSkill, true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Name != "sql-queries" {
		t.Fatalf("expected sql-queries match, got %#v", got)
	}
}

// Same-kind matches from multiple config layers must NOT report a cross-kind
// ambiguity — that message is nonsensical (it repeats the same kind and tells
// the user to pass --kind, which they already did). It should instead give a
// ready-to-run invocation per layer, keyed by --scope.
func TestResolvedResourceByName_SameKindMultipleLayers(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.UserPersonal),
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.RepoPersonal),
	}
	_, err := resolvedResourceByName(resolved, "sql-queries", ir.KindSkill, true, nil)
	if err == nil {
		t.Fatal("expected an ambiguity error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "ambiguous across kinds") {
		t.Fatalf("must not report cross-kind ambiguity for same-kind matches: %q", msg)
	}
	// One line per layer, each keyed by the --scope that makes it effective.
	for _, want := range []string{"--scope user", "--scope local", "--from", "--path", "teams/pm/plugins", "USER", "REPO*"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected message to contain %q, got: %q", want, msg)
		}
	}
}

// The suggested --path must be the configured (raw) entry path, not the deeper
// discovered sub-bundle path recorded on the artifact — otherwise the user is
// sent to a --path that matches no config entry ("not found in any config").
func TestResolvedResourceByName_SuggestsRawEntryPath(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "awesome-ai", Ref: "main", Path: "teams/pm/plugins", Source: config.UserPersonal},
		},
	}
	resolved := []resolver.ResolvedArtifact{
		// Artifact resolved from a discovered sub-bundle beneath the entry.
		{
			Entry:        catalog.Entry{Kind: ir.KindSkill, Name: "sql-queries"},
			RegistryURL:  "awesome-ai",
			RegistryPath: "teams/pm/plugins/pm-data-analytics",
			Ref:          "main",
			Source:       config.UserPersonal,
		},
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.RepoPersonal),
	}
	_, err := resolvedResourceByName(resolved, "sql-queries", ir.KindSkill, true, cfg)
	if err == nil {
		t.Fatal("expected an ambiguity error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "pm-data-analytics") {
		t.Fatalf("must suggest the configured parent path, not the discovered sub-bundle path: %q", msg)
	}
	if !strings.Contains(msg, `--path "teams/pm/plugins"`) {
		t.Fatalf("expected the raw entry path in the suggestion, got: %q", msg)
	}
}

// Genuinely different kinds sharing a name is the one case where --kind helps.
func TestResolvedResourceByName_DifferentKinds(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "deploy", "reg", "", config.UserPersonal),
		artifact(ir.KindCommand, "deploy", "reg", "", config.UserPersonal),
	}
	_, err := resolvedResourceByName(resolved, "deploy", 0, false, nil)
	if err == nil {
		t.Fatal("expected an ambiguity error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ambiguous across kinds") || !strings.Contains(msg, "rerun with --kind") {
		t.Fatalf("expected cross-kind ambiguity message, got: %q", msg)
	}
	if !strings.Contains(msg, "command") || !strings.Contains(msg, "skill") {
		t.Fatalf("expected both kind labels in message, got: %q", msg)
	}
}

// The --from path narrows the lookup to a single registry entry so the same
// resource resolving at multiple scopes no longer trips the ambiguity guard.
func TestResolvedResourceInRegistry_FiltersByURLAndPath(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.UserPersonal),
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.RepoCommitted),
		artifact(ir.KindSkill, "sql-queries", "other-reg", "elsewhere", config.RepoCommitted),
	}
	got, err := resolvedResourceInRegistry(resolved, "sql-queries", ir.KindSkill, true, "awesome-ai", "teams/pm/plugins")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected a match for the named registry entry")
	}
	if got.RegistryURL != "awesome-ai" || got.RegistryPath != "teams/pm/plugins" {
		t.Fatalf("matched the wrong entry: %#v", got)
	}
}

// With --kind omitted, a single registry entry that contains a skill and a
// command sharing the same name is ambiguous: removeFromAdHoc must be told
// which kind to disable rather than silently picking the first match.
func TestResolvedResourceInRegistry_SameRegistryCrossKindRequiresKind(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "deploy", "awesome-ai", "teams/pm/plugins", config.RepoCommitted),
		artifact(ir.KindCommand, "deploy", "awesome-ai", "teams/pm/plugins", config.RepoCommitted),
	}
	// hasKind == false → the two kinds cannot be disambiguated; expect an error.
	got, err := resolvedResourceInRegistry(resolved, "deploy", 0, false, "awesome-ai", "teams/pm/plugins")
	if err == nil {
		t.Fatalf("expected a cross-kind ambiguity error, got match %#v", got)
	}
	if got != nil {
		t.Fatalf("expected no match on ambiguity, got %#v", got)
	}
	if msg := err.Error(); !strings.Contains(msg, "ambiguous across kinds") || !strings.Contains(msg, "--kind") {
		t.Fatalf("expected cross-kind ambiguity message mentioning --kind, got: %q", msg)
	}
	// hasKind == true → the caller named the kind, so the matching entry from
	// the requested registry is returned instead of erroring.
	got, err = resolvedResourceInRegistry(resolved, "deploy", ir.KindCommand, true, "awesome-ai", "teams/pm/plugins")
	if err != nil {
		t.Fatalf("unexpected error once --kind narrows the lookup: %v", err)
	}
	if got == nil || got.Kind != ir.KindCommand {
		t.Fatalf("expected the command from the requested registry, got %#v", got)
	}
}

func TestResolvedResourceInRegistry_NoMatch(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.UserPersonal),
	}
	got, err := resolvedResourceInRegistry(resolved, "sql-queries", ir.KindSkill, true, "awesome-ai", "different/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for a non-matching path, got %#v", got)
	}
}

// The resolver records the discovered sub-bundle path on an artifact, which can
// be deeper than the configured entry path. A --path naming the entry must
// still own artifacts materialized from beneath it.
func TestResolvedResourceInRegistry_EntryPathOwnsDiscoveredSubPath(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins/pm-data-analytics", config.UserPersonal),
	}
	got, err := resolvedResourceInRegistry(resolved, "sql-queries", ir.KindSkill, true, "awesome-ai", "teams/pm/plugins")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("entry path should own the deeper discovered sub-bundle path")
	}
}

func TestRegistryPathOwns(t *testing.T) {
	cases := []struct {
		entry, artifact string
		want            bool
	}{
		{"", "anything/at/all", true},                          // URL-only targeting
		{"teams/pm/plugins", "teams/pm/plugins", true},         // exact
		{"teams/pm/plugins", "teams/pm/plugins/pm-data", true}, // artifact beneath entry
		{"teams/pm/plugins", "teams/pm/plugins-other", false},  // not a component boundary
		{"teams/pm/plugins", "teams/pm", false},                // entry deeper than artifact
		{"teams/pm/plugins", "other/tree", false},              // unrelated
	}
	for _, c := range cases {
		if got := registryPathOwns(c.entry, c.artifact); got != c.want {
			t.Errorf("registryPathOwns(%q, %q) = %v, want %v", c.entry, c.artifact, got, c.want)
		}
	}
}
