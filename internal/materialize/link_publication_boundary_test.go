package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: materialization/cache-link-publication-refuses-a-replaced-parent
func TestRunCacheLinkPublicationRefusesReplacedParent(t *testing.T) {
	repo, root, resolved := rootBoundaryFixture(t, rootBoundaryCases[1], false)
	parent := filepath.Join(root, "skills")
	retained := filepath.Join(root, "captured-skills")
	hit := false
	report, err := Run(t.Context(), repo, resolved, func(cfg *runConfig) {
		cfg.linkPublicationHook = func(path string) {
			if path != filepath.Join(parent, "root-probe") {
				return
			}
			hit = true
			if err := os.Rename(parent, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(parent, "root-probe"), []byte("preserve replacement"), 0o640); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("consumer did not reach staged link publication")
	}
	if !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("replacement did not refuse publication: %+v", report.Diags)
	}
	if raw, err := os.ReadFile(filepath.Join(parent, "root-probe")); err != nil || string(raw) != "preserve replacement" {
		t.Fatalf("publication changed replacement: %q %v", raw, err)
	}
	for _, dir := range []string{parent, retained} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".agent-marketplace-link-") {
				t.Fatalf("attempt-owned staging survived in %s: %s", dir, entry.Name())
			}
		}
	}
	control, err := Run(t.Context(), t.TempDir(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, control.Diags)
	if control.TotalWritten() == 0 || control.PerTarget[hcc.TargetFactory].Target != hcc.TargetFactory {
		t.Fatal("valid control did not publish native output")
	}
}

// spec: materialization/cache-link-publication-refuses-a-replaced-leaf
func TestRunCacheLinkPublicationRefusesReplacedLeaf(t *testing.T) {
	repo, root, resolved := rootBoundaryFixture(t, rootBoundaryCases[1], false)
	leaf := filepath.Join(root, "skills", "root-probe")
	hit := false
	report, err := Run(t.Context(), repo, resolved, func(cfg *runConfig) {
		cfg.linkPublicationHook = func(path string) {
			if path != leaf {
				return
			}
			hit = true
			if err := os.WriteFile(leaf, []byte("preserve appeared leaf"), 0o640); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hit || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("appeared leaf was not refused at publication: %v %+v", hit, report.Diags)
	}
	if raw, err := os.ReadFile(leaf); err != nil || string(raw) != "preserve appeared leaf" {
		t.Fatalf("publication overwrote appeared leaf: %q %v", raw, err)
	}
	entries, err := os.ReadDir(filepath.Dir(leaf))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".agent-marketplace-link-") {
			t.Fatalf("staging residue: %s", entry.Name())
		}
	}
}

// spec: materialization/managed-cache-links-update-and-repeat-without-mutation
func TestRunManagedCacheLinksUpdateAndRepeat(t *testing.T) {
	for _, tc := range rootBoundaryCases {
		t.Run(tc.name, func(t *testing.T) {
			repo, root, resolved := rootBoundaryFixture(t, tc, false)
			initial, err := Run(t.Context(), repo, resolved)
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, initial.Diags)
			leaf := filepath.Join(root, "skills", "root-probe")
			if tc.subagent {
				leaf = filepath.Join(root, "agents", "root-probe.toml")
			}
			oldTarget, err := os.Readlink(leaf)
			if err != nil {
				t.Fatal(err)
			}
			oldTarget += "-previous-version"
			if err := os.Remove(leaf); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(oldTarget, leaf); err != nil {
				t.Fatal(err)
			}
			updated, err := Run(t.Context(), repo, resolved)
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, updated.Diags)
			newTarget, err := os.Readlink(leaf)
			if err != nil || oldTarget == newTarget {
				t.Fatalf("managed update did not publish new target: %q -> %q %v", oldTarget, newTarget, err)
			}
			payload := leaf
			if !tc.subagent {
				payload = filepath.Join(leaf, "SKILL.md")
			}
			if raw, err := os.ReadFile(payload); err != nil || !strings.Contains(string(raw), "inert body") {
				t.Fatalf("new native payload missing: %q %v", raw, err)
			}
			before, err := os.Lstat(leaf)
			if err != nil {
				t.Fatal(err)
			}
			repeat, err := Run(t.Context(), repo, resolved)
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, repeat.Diags)
			if after, err := os.Lstat(leaf); err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("repeat rewrote managed link: %v", err)
			}
		})
	}
}
