package resolver

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// resolvedNames indexes resolved artifacts by "<kind>:<name>" so a test can
// assert both presence and provenance without scanning the slice twice.
func resolvedByKindName(artifacts []ResolvedArtifact) map[string]ResolvedArtifact {
	out := make(map[string]ResolvedArtifact, len(artifacts))
	for _, a := range artifacts {
		out[a.Kind.String()+":"+a.Name] = a
	}
	return out
}

func sourceLabels(refusal *ExecutableCollisionError) string {
	labels := make([]string, 0, len(refusal.Sources))
	for _, source := range refusal.Sources {
		labels = append(labels, source.Label())
	}
	return strings.Join(labels, " ")
}

func executableCollision(t *testing.T, errs []error) *ExecutableCollisionError {
	t.Helper()
	if len(errs) != 1 {
		t.Fatalf("expected exactly one executable collision refusal, got %d: %v", len(errs), errs)
	}
	var refusal *ExecutableCollisionError
	if !errors.As(errs[0], &refusal) {
		t.Fatalf("expected *ExecutableCollisionError, got %T: %v", errs[0], errs[0])
	}
	if !IsExecutableCollision(errs[0]) {
		t.Fatalf("IsExecutableCollision() = false for %v", errs[0])
	}
	if IsWarning(errs[0]) {
		t.Fatalf("executable collision must not be a soft warning: %v", errs[0])
	}
	if IsNameCollision(errs[0]) {
		t.Fatalf("executable collision must not be reported as a demoted name collision: %v", errs[0])
	}
	return refusal
}

// TestResolveRefusesHookCollisionAcrossRegistries pins the exploit: a second
// registry that publishes a hook name an already-configured registry provides
// must not be able to win by precedence. The hook is dropped entirely.
func TestResolveRefusesHookCollisionAcrossRegistries(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			// "aaa-evil" sorts before "trusted", so the alphabetical
			// tiebreaker in pickCollisionWinner used to hand this registry
			// the hook that actually runs on the developer's machine.
			{URL: "git@example.com:aaa-evil.git", Ref: "main", Hooks: ks("pre-commit-guard")},
			{URL: "git@example.com:trusted.git", Ref: "main", Hooks: ks("pre-commit-guard"), Skills: ks("safe-skill")},
		},
	}

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:aaa-evil.git", "main"): {
			{Kind: ir.KindHook, Name: "pre-commit-guard", Path: "/cache/evil/pre-commit-guard"},
		},
		ck("git@example.com:trusted.git", "main"): {
			{Kind: ir.KindHook, Name: "pre-commit-guard", Path: "/cache/trusted/pre-commit-guard"},
			{Name: "safe-skill", Path: "/cache/trusted/safe-skill"},
		},
	}

	setCatalogKeys(cfg)

	got, errs := Resolve(cfg, catalogs, nil)
	refusal := executableCollision(t, errs)

	if refusal.Kind != ir.KindHook || refusal.Name != "pre-commit-guard" {
		t.Fatalf("refusal identifies %v %q, want hook %q", refusal.Kind, refusal.Name, "pre-commit-guard")
	}
	if len(refusal.Sources) != 2 {
		t.Fatalf("refusal must name every source, got %v", refusal.Sources)
	}
	for _, want := range []string{"aaa-evil", "trusted"} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Fatalf("refusal message %q does not name source %q", errs[0].Error(), want)
		}
	}

	byKey := resolvedByKindName(got)
	if _, installed := byKey["hook:pre-commit-guard"]; installed {
		t.Fatalf("ambiguous hook must not be installed from any source, resolved: %#v", got)
	}
	if _, ok := byKey["skill:safe-skill"]; !ok {
		t.Fatalf("unambiguous resources must still resolve, resolved: %#v", got)
	}
}

// TestResolveRefusesMcpCollisionAcrossRegistries covers the MCP identity: an
// MCP server entry carries a command, args and env, so silently choosing one
// of two definitions hands execution to whichever source won a tiebreak.
func TestResolveRefusesMcpCollisionAcrossRegistries(t *testing.T) {
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
	refusal := executableCollision(t, errs)
	if refusal.Kind != ir.KindMcpServer {
		t.Fatalf("refusal kind = %v, want mcpserver", refusal.Kind)
	}
	if len(got) != 0 {
		t.Fatalf("ambiguous MCP server must not be installed, resolved: %#v", got)
	}
}

// TestResolveRefusesExecutableCollisionEvenWhenOneSourceIsRequired pins that
// the required flag is not an escape hatch either: `required` exists to stop a
// consumer from opting out of an organization resource, not to let one source
// overwrite an executable another source already claims.
func TestResolveRefusesExecutableCollisionEvenWhenOneSourceIsRequired(t *testing.T) {
	// Both entries sit in the same layer, so both install into the same scope
	// and contend for the same aggregate entry.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:org.git", Ref: "main", Required: true, MCP: ks("audit-log")},
			{URL: "git@example.com:team.git", Ref: "main", MCP: ks("audit-log")},
		},
	}

	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:org.git", "main"): {
			{Kind: ir.KindMcpServer, Name: "audit-log", Path: "/cache/org/audit-log"},
		},
		ck("git@example.com:team.git", "main"): {
			{Kind: ir.KindMcpServer, Name: "audit-log", Path: "/cache/team/audit-log"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if refusal := executableCollision(t, errs); refusal.Name != "audit-log" {
		t.Fatalf("refusal names %q, want audit-log", refusal.Name)
	}
	if len(got) != 0 {
		t.Fatalf("ambiguous MCP server must not be installed, resolved: %#v", got)
	}
}

// TestResolveRefusesExecutableCollisionSameURLDifferentPaths covers the
// same-repository case: two configured sub-directories of one registry that
// both publish the hook contend for a single aggregate entry, and only one can
// materialize.
func TestResolveRefusesExecutableCollisionSameURLDifferentPaths(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:mono.git", Ref: "main", Path: "teams/dev", Hooks: ks("format-on-save")},
			{URL: "git@example.com:mono.git", Ref: "main", Path: "teams/pm", Hooks: ks("format-on-save")},
		},
	}

	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:mono.git", "main", "teams/dev"): {
			{Kind: ir.KindHook, Name: "format-on-save", Path: "/cache/mono/dev/format-on-save"},
		},
		ck("git@example.com:mono.git", "main", "teams/pm"): {
			{Kind: ir.KindHook, Name: "format-on-save", Path: "/cache/mono/pm/format-on-save"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	refusal := executableCollision(t, errs)
	for _, want := range []string{"teams/dev", "teams/pm"} {
		if !strings.Contains(sourceLabels(refusal), want) {
			t.Fatalf("refusal must distinguish sources by path, got %v", refusal.Sources)
		}
	}
	if len(got) != 0 {
		t.Fatalf("ambiguous hook must not be installed, resolved: %#v", got)
	}
}

// TestResolveRefusesExecutableCollisionSameURLDifferentRefs covers two pins of
// one repository: the refs identify different content, so the sources must be
// distinguishable in the message.
func TestResolveRefusesExecutableCollisionSameURLDifferentRefs(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:pinned.git", Ref: "v1.0", MCP: ks("deploy")},
			{URL: "git@example.com:pinned.git", Ref: "v2.0", MCP: ks("deploy")},
		},
	}

	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:pinned.git", "v1.0"): {
			{Kind: ir.KindMcpServer, Name: "deploy", Path: "/cache/v1/deploy"},
		},
		ck("git@example.com:pinned.git", "v2.0"): {
			{Kind: ir.KindMcpServer, Name: "deploy", Path: "/cache/v2/deploy"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	refusal := executableCollision(t, errs)
	joined := sourceLabels(refusal)
	for _, want := range []string{"v1.0", "v2.0"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("refusal must name both refs, got %v", refusal.Sources)
		}
	}
	if len(got) != 0 {
		t.Fatalf("ambiguous MCP server must not be installed, resolved: %#v", got)
	}
}

// TestResolveExecutableCollisionResolvedByExclude pins the documented way out:
// once the user names the source they do not want, the executable installs
// again from the remaining one.
func TestResolveExecutableCollisionResolvedByExclude(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:aaa-evil.git", Ref: "main", Hooks: kse(nil, []string{"pre-commit-guard"})},
			{URL: "git@example.com:trusted.git", Ref: "main", Hooks: ks("pre-commit-guard")},
		},
	}

	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:aaa-evil.git", "main"): {
			{Kind: ir.KindHook, Name: "pre-commit-guard", Path: "/cache/evil/pre-commit-guard"},
		},
		ck("git@example.com:trusted.git", "main"): {
			{Kind: ir.KindHook, Name: "pre-commit-guard", Path: "/cache/trusted/pre-commit-guard"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors once the ambiguity is resolved, got %v", errs)
	}
	byKey := resolvedByKindName(got)
	installed, ok := byKey["hook:pre-commit-guard"]
	if !ok {
		t.Fatalf("expected hook to install from the remaining source, resolved: %#v", got)
	}
	if installed.RegistryURL != "git@example.com:trusted.git" {
		t.Fatalf("hook installed from %q, want the non-excluded source", installed.RegistryURL)
	}
}

// TestResolveExecutableCrossScopeIsNotACollision guards against over-refusal:
// the same executable from the same source configured at machine and repo
// scope is complementary (two different install roots), not ambiguous.
func TestResolveExecutableCrossScopeIsNotACollision(t *testing.T) {
	global := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:shared.git", Ref: "main", MCP: ks("youtrack")},
		},
	}
	repo := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:shared.git", Ref: "main", MCP: ks("youtrack")},
		},
	}

	merged := config.Merge(global, repo)
	setCatalogKeys(merged)

	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:shared.git", "main"): {
			{Kind: ir.KindMcpServer, Name: "youtrack", Path: "/cache/shared/youtrack"},
		},
	}

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("same source at two scopes must not be refused, got %v", errs)
	}
	if len(got) == 0 {
		t.Fatalf("expected the MCP server to resolve, resolved: %#v", got)
	}
}

// TestResolveNonExecutableCollisionStillPicksWinner guards the blast radius:
// documentation-only kinds keep the demoted-warning behavior, because a skill
// or subagent that loses a tiebreak cannot execute anything by itself.
func TestResolveNonExecutableCollisionStillPicksWinner(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  ir.ResourceKind
		apply func(*config.RegistryEntry)
	}{
		{"skill", ir.KindSkill, func(e *config.RegistryEntry) { e.Skills = ks("shared") }},
		{"command", ir.KindCommand, func(e *config.RegistryEntry) { e.Commands = ks("shared") }},
		{"subagent", ir.KindSubagent, func(e *config.RegistryEntry) { e.Subagents = ks("shared") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entryA := config.RegistryEntry{URL: "git@example.com:registry-a.git", Ref: "main"}
			entryB := config.RegistryEntry{URL: "git@example.com:registry-b.git", Ref: "main"}
			tc.apply(&entryA)
			tc.apply(&entryB)
			cfg := &config.Config{Registries: []config.RegistryEntry{entryA, entryB}}
			setCatalogKeys(cfg)

			catalogs := map[string][]catalog.Entry{
				ck("git@example.com:registry-a.git", "main"): {
					{Kind: tc.kind, Name: "shared", Path: "/cache/a/shared"},
				},
				ck("git@example.com:registry-b.git", "main"): {
					{Kind: tc.kind, Name: "shared", Path: "/cache/b/shared"},
				},
			}

			got, errs := Resolve(cfg, catalogs, nil)
			if len(errs) != 1 || !IsWarning(errs[0]) {
				t.Fatalf("expected one soft collision warning, got %v", errs)
			}
			if IsExecutableCollision(errs[0]) {
				t.Fatalf("%s must not be treated as executable, got %v", tc.name, errs[0])
			}
			if len(got) == 0 {
				t.Fatalf("expected a winner to be picked for %s, resolved: %#v", tc.name, got)
			}
		})
	}
}

// TestExecutableCollisionRefusalRedactsCredentials keeps registry credentials
// out of a message that names every source of the ambiguity.
func TestExecutableCollisionRefusalRedactsCredentials(t *testing.T) {
	secret := "https://oauth2:s3cr3t-tok" + "en@example.com/registry.git"
	refusal := &ExecutableCollisionError{
		Kind:    ir.KindMcpServer,
		Name:    "deploy",
		Sources: []ExecutableSource{{URL: secret, Ref: "main"}},
	}

	for name, text := range map[string]string{
		"error":       refusal.Error(),
		"remediation": refusal.Remediation(),
	} {
		if strings.Contains(text, "s3cr3t") {
			t.Fatalf("%s leaked credentials: %s", name, text)
		}
	}
	if !strings.Contains(refusal.Error(), "https://***@example.com/registry") {
		t.Fatalf("error dropped the redacted URL: %s", refusal.Error())
	}
}

// TestExecutableCollisionRemediationNamesSourceQualifiedSelection pins that the
// refusal tells the user how to resolve it, since a refusal without a way out
// is a dead end for anyone who legitimately configured both registries.
func TestExecutableCollisionRemediationNamesSourceQualifiedSelection(t *testing.T) {
	refusal := &ExecutableCollisionError{
		Kind: ir.KindHook,
		Name: "pre-commit-guard",
		Sources: []ExecutableSource{
			{URL: "git@example.com:aaa-evil.git", Ref: "main"},
			{URL: "git@example.com:trusted.git", Ref: "main"},
		},
	}
	hint := refusal.Remediation()
	for _, want := range []string{"agent-marketplace remove", "--from", "pre-commit-guard"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("remediation %q does not mention %q", hint, want)
		}
	}
}
