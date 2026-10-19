package pipeline

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// leakedValues are the value classes that must never appear in a diagnostic,
// hint or log attribute (pkg/harness-compilation/diag/codes.go). The fixture
// below deliberately hides all of them inside one scoped tool specifier.
var leakedValues = []string{"Bash(", "git push", "--force", "credentials.json"}

// writeScopedAllowedToolsSkill writes a skill whose explicit allowed-tools
// restriction carries a scoped Claude specifier, which the Devin compiler must
// report (skill.devin.tool-unknown). Bead agent-marketplace-pu6b made that a
// WARNING that omits the key instead of an error that suppressed the artifact;
// the promotion under test here is severity-agnostic (internal/pipeline's
// diagIssue renders and dedups warnings and errors through the same code), so
// every property below still holds. The error-severity half of the contract is
// covered at the cmd level and by the MCP transport fixture in
// agent-marketplace-tbjf, which uses a code pu6b did NOT downgrade.
func writeScopedAllowedToolsSkill(t *testing.T, root, name string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "skills", name, "SKILL.md"),
		"---\nname: "+name+"\ndescription: "+name+" skill\nallowed-tools:\n  - Bash(git push --force --config credentials.json:*)\n---\n\n# "+name+"\n")
}

func devinScopedToolSyncResult(t *testing.T, names ...string) SyncResult {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	registry := filepath.Join(t.TempDir(), "registry")
	for _, name := range names {
		writeScopedAllowedToolsSkill(t, registry, name)
	}

	repoCfg := &config.Config{
		APIVersion: "agent-marketplace/v4",
		Registries: []config.RegistryEntry{{
			URL:           registry,
			SourceHarness: "claude",
			Skills:        config.KindSelector{Include: names},
		}},
	}

	return Sync(context.Background(), ResolveOptions{
		Workspace: mustWorkspace(t, t.TempDir()),
		NoBackup:  true,
		Loaders: Loaders{
			Org:    func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) { return &config.Config{Harnesses: []string{"devin"}}, nil },
			Repo:   func() (*config.Config, error) { return repoCfg, nil },
			Local:  func() (*config.LocalConfig, error) { return nil, nil },
		},
	})
}

// TestSyncPromotedMaterializeErrorNamesResourceAndSource is the pipeline-level
// half of the beta-tester report in bead agent-marketplace-oc2r: three failures
// in three different skills must be three distinguishable diagnostics, each
// naming its resource and its source file, each carrying the compiler's hint.
func TestSyncPromotedMaterializeErrorNamesResourceAndSource(t *testing.T) {
	names := []string{"alpha", "beta", "gamma"}
	result := devinScopedToolSyncResult(t, names...)

	promoted := make([]error, 0, len(names))
	for _, w := range result.Warnings {
		if strings.Contains(w.Error(), "skill.devin.tool-unknown") {
			promoted = append(promoted, w)
		}
	}
	if len(promoted) != len(names) {
		t.Fatalf("promoted = %d, want one per failing resource (%d): %v", len(promoted), len(names), result.Warnings)
	}

	keys := make(map[string]string, len(names))
	for _, name := range names {
		var matched error
		for _, err := range promoted {
			if strings.Contains(err.Error(), `resource "`+name+`"`) {
				matched = err
				break
			}
		}
		if matched == nil {
			t.Fatalf("no promoted diagnostic names resource %q; got: %v", name, promoted)
		}
		if !strings.Contains(matched.Error(), "skill.devin.tool-unknown") {
			t.Errorf("error for %q lost its diagnostic code: %v", name, matched)
		}
		wantSource := filepath.ToSlash(filepath.Join("skills", name, "SKILL.md"))
		if !strings.Contains(matched.Error(), wantSource) {
			t.Errorf("error for %q does not name its source file %q: %v", name, wantSource, matched)
		}
		var r resolver.Remediable
		if !errors.As(matched, &r) || !strings.Contains(r.Remediation(), "cannot be scoped to a command") {
			t.Errorf("error for %q does not carry the compiler hint: %v", name, matched)
		}

		var keyer interface{ DedupKey() string }
		if !errors.As(matched, &keyer) {
			t.Fatalf("error for %q is not dedup-keyed, so identical failures would print repeatedly: %v", name, matched)
		}
		if prior, dup := keys[keyer.DedupKey()]; dup {
			t.Fatalf("error for %q shares a dedup key with %q; distinct resources would be collapsed", name, prior)
		}
		keys[keyer.DedupKey()] = name
	}

	for _, err := range promoted {
		for _, forbidden := range leakedValues {
			if strings.Contains(err.Error(), forbidden) {
				t.Errorf("promoted diagnostic leaks %q: %v", forbidden, err)
			}
		}
	}
}

// TestSyncPromotedMaterializeErrorsCollapsePerResource covers the other dedup
// direction: two byte-identical diagnostics from ONE resource (a skill with two
// scoped specifiers) share one identity, so the CLI prints one line.
func TestSyncPromotedMaterializeErrorsCollapsePerResource(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	registry := filepath.Join(t.TempDir(), "registry")
	writeFile(t, filepath.Join(registry, "skills", "alpha", "SKILL.md"),
		"---\nname: alpha\ndescription: alpha skill\nallowed-tools:\n  - Bash(git push --force:*)\n  - Bash(git commit:*)\n---\n\n# alpha\n")

	repoCfg := &config.Config{
		APIVersion: "agent-marketplace/v4",
		Registries: []config.RegistryEntry{{
			URL:           registry,
			SourceHarness: "claude",
			Skills:        config.KindSelector{Include: []string{"alpha"}},
		}},
	}

	result := Sync(context.Background(), ResolveOptions{
		Workspace: mustWorkspace(t, t.TempDir()),
		NoBackup:  true,
		Loaders: Loaders{
			Org:    func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) { return &config.Config{Harnesses: []string{"devin"}}, nil },
			Repo:   func() (*config.Config, error) { return repoCfg, nil },
			Local:  func() (*config.LocalConfig, error) { return nil, nil },
		},
	})

	promoted := make([]error, 0, 2)
	for _, w := range result.Warnings {
		if strings.Contains(w.Error(), "skill.devin.tool-unknown") {
			promoted = append(promoted, w)
		}
	}
	if len(promoted) < 2 {
		t.Fatalf("expected the compiler to report both scoped specifiers, got: %v", result.Warnings)
	}
	keys := make(map[string]struct{})
	for _, err := range promoted {
		var keyer interface{ DedupKey() string }
		if !errors.As(err, &keyer) {
			t.Fatalf("promoted diagnostic is not dedup-keyed: %v", err)
		}
		keys[keyer.DedupKey()] = struct{}{}
	}
	if len(keys) != 1 {
		t.Fatalf("identical diagnostics from one resource produced %d dedup keys, want 1: %v", len(keys), promoted)
	}
}
