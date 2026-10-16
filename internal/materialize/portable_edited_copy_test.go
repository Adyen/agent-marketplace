package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func editedCopyFixture(t *testing.T) (source, skillsDir, destination string, state *portableState, link skillLink) {
	t.Helper()
	source = portableModeSource(t, 0o755, 0o755)
	parent := t.TempDir()
	skillsDir = filepath.Join(parent, "skills")
	destination = filepath.Join(skillsDir, "probe")
	resetPortableModeTreeOnCleanup(t, destination)
	link = skillLink{name: "probe", target: source, resourceName: "probe", kind: ir.KindSkill}
	state = &portableState{entries: make(map[string]portableManifestEntry)}
	if _, err := syncPortableSkillTrees(skillsDir, []skillLink{link}, false, state, hcc.TargetFactory, "skills", make(map[string]bool)); err != nil {
		t.Fatal(err)
	}
	changePortableModeSourceBody(t, destination, "local edit")
	return source, skillsDir, destination, state, link
}

func assertEditedCopyDiag(t *testing.T, diags []diag.Diagnostic, wantPath, wantResource string) {
	t.Helper()
	if len(diags) != 1 {
		t.Fatalf("want exactly one diagnostic, got %+v", diags)
	}
	d := diags[0]
	if d.Code != "materialize.collision.user-owned" || d.Severity != diag.SeverityWarning {
		t.Fatalf("unexpected diagnostic code/severity: %+v", d)
	}
	for _, want := range []string{"locally edited copy", wantPath, wantResource, "delete or move", "agent-marketplace sync"} {
		if !strings.Contains(d.Message, want) {
			t.Fatalf("edited-copy message %q missing %q", d.Message, want)
		}
	}
	if strings.Contains(d.Message, "not managed") {
		t.Fatalf("edited-copy message still calls the output unmanaged: %q", d.Message)
	}
}

// spec: materialization/edited-file-mode-skill-copy-is-reported-as-a-local-edit
func TestEditedPortableSkillTreeReportsLocalEdit(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "check"}[dryRun], func(t *testing.T) {
			_, skillsDir, destination, state, link := editedCopyFixture(t)
			entryBefore, ok := state.entry(hcc.TargetFactory, "skills/probe")
			if !ok || entryBefore.Digest == "" {
				t.Fatalf("fixture manifest entry missing digest: %+v", entryBefore)
			}
			report, err := syncPortableSkillTrees(skillsDir, []skillLink{link}, dryRun, state, hcc.TargetFactory, "skills", make(map[string]bool))
			if err != nil {
				t.Fatal(err)
			}
			assertEditedCopyDiag(t, report.Diags, destination, "probe")
			assertCleanupBytes(t, filepath.Join(destination, "SKILL.md"), "local edit")
			entryAfter, ok := state.entry(hcc.TargetFactory, "skills/probe")
			if !ok || entryAfter.Digest != entryBefore.Digest {
				t.Fatalf("manifest entry changed: before=%+v after=%+v", entryBefore, entryAfter)
			}
		})
	}
}

// spec: materialization/edited-file-mode-single-file-copy-is-reported-as-a-local-edit
func TestEditedPortableArtifactFileReportsLocalEdit(t *testing.T) {
	outDir := t.TempDir()
	cacheDir := t.TempDir()
	compiledArtifact := artifact.Artifact{Path: "command.md", Bytes: []byte("command body")}
	if err := os.WriteFile(filepath.Join(cacheDir, compiledArtifact.Path), compiledArtifact.Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	target := hcc.TargetClaude
	state := &portableState{entries: make(map[string]portableManifestEntry)}
	collisionMeta := map[string]skillLink{"command.md": {resourceName: "probe", kind: ir.KindCommand}}
	if _, _, err := syncPortableArtifactFiles(outDir, []artifact.Artifact{compiledArtifact}, cacheDir, false, false, collisionMeta, state, target, "commands", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(outDir, "command.md")
	if err := os.WriteFile(dst, []byte("local edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, diags, err := syncPortableArtifactFiles(outDir, []artifact.Artifact{compiledArtifact}, cacheDir, false, false, collisionMeta, state, target, "commands", map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	assertEditedCopyDiag(t, diags, dst, "probe")
	assertCleanupBytes(t, dst, "local edit")
}

// spec: materialization/edited-copy-blocking-a-return-to-symlinks-is-reported-as-a-local-edit
func TestEditedCopyBlockingSymlinkReturnReportsLocalEdit(t *testing.T) {
	_, skillsDir, destination, state, link := editedCopyFixture(t)
	report, err := syncSkillLinks(skillsDir, []skillLink{link}, false, false, portableSyncContext{
		state: state, target: hcc.TargetFactory, reportPrefix: "skills", desired: make(map[string]bool),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEditedCopyDiag(t, report.Diags, destination, "probe")
	info, err := os.Lstat(destination)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("edited copy was replaced by a link: %v %v", info, err)
	}
	assertCleanupBytes(t, filepath.Join(destination, "SKILL.md"), "local edit")
}

// spec: materialization/unrecorded-existing-output-keeps-the-unmanaged-collision-warning
func TestUnrecordedOutputKeepsUnmanagedCollisionWarning(t *testing.T) {
	source := portableModeSource(t, 0o755, 0o755)
	parent := t.TempDir()
	skillsDir := filepath.Join(parent, "skills")
	destination := filepath.Join(skillsDir, "probe")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "SKILL.md"), []byte("user file"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := &portableState{entries: make(map[string]portableManifestEntry)}
	report, err := syncPortableSkillTrees(skillsDir, []skillLink{{name: "probe", target: source, resourceName: "probe", kind: ir.KindSkill}}, false, state, hcc.TargetFactory, "skills", make(map[string]bool))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Diags) != 1 || report.Diags[0].Code != "materialize.collision.user-owned" {
		t.Fatalf("want one user-owned collision, got %+v", report.Diags)
	}
	message := report.Diags[0].Message
	if !strings.Contains(message, "(not managed by agent-marketplace)") || !strings.Contains(message, "to resolve, exclude") {
		t.Fatalf("unmanaged collision message changed: %q", message)
	}
	assertCleanupBytes(t, filepath.Join(destination, "SKILL.md"), "user file")
}

// spec: materialization/removing-an-edited-copy-restores-the-registry-version
func TestRemovingEditedCopyRestoresRegistryVersion(t *testing.T) {
	_, skillsDir, destination, state, link := editedCopyFixture(t)
	for _, name := range []string{".", "nested", "nested/deeper"} {
		if err := os.Chmod(filepath.Join(destination, filepath.FromSlash(name)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(destination); err != nil {
		t.Fatal(err)
	}
	report, err := syncPortableSkillTrees(skillsDir, []skillLink{link}, false, state, hcc.TargetFactory, "skills", make(map[string]bool))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("restore reported diagnostics: %+v", report.Diags)
	}
	if len(report.Written) != 1 {
		t.Fatalf("restore did not write the copy: %+v", report)
	}
	assertPortableModeTree(t, destination, 0o755, 0o755, "original payload")
	if _, ok := state.entry(hcc.TargetFactory, "skills/probe"); !ok {
		t.Fatal("restored copy is not recorded in the manifest")
	}
}
