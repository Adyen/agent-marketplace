package resourcediff

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func side(id Identity, revision string, resource ir.Resource, support map[string]SupportFile) Side {
	return Side{Identity: id, Revision: revision, Resource: resource, Support: support}
}

func gitIdentity(name, ref string) Identity {
	return Identity{Scope: "repo", Kind: "skill", Name: name, SourceURL: "https://git.example/reg.git", Ref: ref}
}

// spec: update-output/selection-and-collection-changes-are-distinguished
func TestCompareReportsAdditionsRemovalsAndIndependentSelections(t *testing.T) {
	deploy := ir.NewSkill("deploy", "Deploy", "Body", nil, nil, ir.SourceRef{})
	guard := ir.NewSkill("guard", "Guard", "Guard body", nil, nil, ir.SourceRef{})
	audit := ir.NewSkill("audit", "Audit", "Audit body", nil, nil, ir.SourceRef{})

	prior := []Side{
		side(gitIdentity("deploy", "main"), "sha256:aaaa", deploy, nil),
		// guard was selected only through a collection in the prior revision.
		side(Identity{Scope: "repo", Kind: "skill", Name: "guard", SourceURL: "https://git.example/reg.git", Ref: "main", Via: "collection:bundle@1.0"}, "sha256:bbbb", guard, nil),
	}
	candidate := []Side{
		side(gitIdentity("deploy", "main"), "sha256:cccc", deploy, nil),
		// guard is now directly selected; the collection no longer includes it,
		// but it must not be reported as an installation removal.
		side(gitIdentity("guard", "main"), "sha256:bbbb", guard, nil),
		// audit is newly selected by the changed collection.
		side(Identity{Scope: "repo", Kind: "skill", Name: "audit", SourceURL: "https://git.example/reg.git", Ref: "main", Via: "collection:bundle@2.0"}, "sha256:dddd", audit, nil),
	}

	changes, err := Compare(prior, candidate)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected exactly the addition and the revision change, got %d: %+v", len(changes), changes)
	}
	var sawDeployRevision, sawAuditAdded bool
	for _, change := range changes {
		switch change.Identity.Name {
		case "deploy":
			sawDeployRevision = true
			if change.Kind != ChangeRevisionOnly || change.OldRevision != "sha256:aaaa" || change.NewRevision != "sha256:cccc" {
				t.Fatalf("deploy change misclassified: %+v", change)
			}
			if change.DefinitionChanged {
				t.Fatalf("deploy definition is unchanged and must not report a content change: %+v", change)
			}
		case "audit":
			sawAuditAdded = true
			if change.Kind != ChangeAdded {
				t.Fatalf("audit must be reported as added: %+v", change)
			}
		case "guard":
			t.Fatalf("guard must not appear as a removal or addition: %+v", change)
		}
	}
	if !sawDeployRevision || !sawAuditAdded {
		t.Fatalf("expected deploy revision and audit addition changes: %+v", changes)
	}
}

// spec: update-output/changed-source-and-complementary-occurrences-remain-visible
func TestCompareDistinguishesCollisionWinnerChangeAndComplementaryOccurrences(t *testing.T) {
	shared := ir.NewSkill("shared", "Shared", "Same body", nil, nil, ir.SourceRef{})
	one := Identity{Scope: "repo", Kind: "skill", Name: "shared", SourceURL: "https://git.example/one.git", Ref: "main"}
	two := Identity{Scope: "repo", Kind: "skill", Name: "shared", SourceURL: "https://git.example/two.git", Ref: "main"}
	otherScope := Identity{Scope: "user", Kind: "skill", Name: "shared", SourceURL: "https://git.example/one.git", Ref: "main"}

	// Prior: one wins the repo-scope collision; the same name exists in user scope.
	prior := []Side{side(one, "r1", shared, nil), side(otherScope, "r1", shared, nil)}
	// Candidate: two wins the repo-scope collision with identical content; the
	// user-scope occurrence is untouched and must remain its own record.
	candidate := []Side{side(two, "r2", shared, nil), side(otherScope, "r1", shared, nil)}

	changes, err := Compare(prior, candidate)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected a single source-change record, got %d: %+v", len(changes), changes)
	}
	change := changes[0]
	if change.Kind != ChangeSource {
		t.Fatalf("collision winner change must be classified as a source change: %+v", change)
	}
	if change.Prior == nil || change.Candidate == nil || change.Prior.Identity.SourceURL != "https://git.example/one.git" || change.Candidate.Identity.SourceURL != "https://git.example/two.git" {
		t.Fatalf("source change must identify both old and new sources: %+v", change)
	}
	if change.DefinitionChanged {
		t.Fatalf("identical reviewed content must not report definition hunks: %+v", change)
	}
}

func TestCompareSameNamedIndependentHooksRemainDistinct(t *testing.T) {
	hookA := ir.NewHook("formatter", "PreToolUse", "fmt-a", "command", nil, ir.SourceRef{})
	hookB := ir.NewHook("formatter", "PreToolUse", "fmt-b", "command", nil, ir.SourceRef{})
	a := Identity{Scope: "repo", Kind: "hook", Name: "formatter", SourceURL: "https://git.example/one.git", Ref: "main"}
	b := Identity{Scope: "repo", Kind: "hook", Name: "formatter", SourceURL: "https://git.example/two.git", Ref: "main"}

	prior := []Side{side(a, "r1", hookA, nil), side(b, "r1", hookB, nil)}
	hookB2 := ir.NewHook("formatter", "PreToolUse", "fmt-b-v2", "command", nil, ir.SourceRef{})
	candidate := []Side{side(a, "r1", hookA, nil), side(b, "r2", hookB2, nil)}

	changes, err := Compare(prior, candidate)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("only the changed hook occurrence must be reported, got %d: %+v", len(changes), changes)
	}
	if changes[0].Identity.SourceURL != "https://git.example/two.git" || !changes[0].DefinitionChanged {
		t.Fatalf("the changed independent hook must be identified by its source: %+v", changes[0])
	}
}

// spec: update-output/supporting-file-changes-are-not-hidden-by-an-unchanged-definition
func TestCompareSurfacesSupportOnlyChanges(t *testing.T) {
	defn := ir.NewSkill("runner", "Runs", "Run the tool.", []string{"runner"}, nil, ir.SourceRef{})
	id := gitIdentity("runner", "main")
	prior := []Side{side(id, "r1", defn, map[string]SupportFile{
		"run.sh":   {Data: []byte("#!/bin/sh\necho v1\n"), Executable: true},
		"data.bin": {Data: []byte{0x00, 0x01}},
	})}
	candidate := []Side{side(id, "r2", defn, map[string]SupportFile{
		"run.sh":   {Data: []byte("#!/bin/sh\necho v2\n"), Executable: true},
		"data.bin": {Data: []byte{0x00, 0x02}},
		"new.txt":  {Data: []byte("added\n")},
	})}

	changes, err := Compare(prior, candidate)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected one change record, got %d", len(changes))
	}
	change := changes[0]
	if change.Kind != ChangeModified || change.DefinitionChanged {
		t.Fatalf("definition is unchanged; the record must reflect support-only modification: %+v", change)
	}
	if len(change.Support) != 3 {
		t.Fatalf("expected all three support changes, got %+v", change.Support)
	}
	byPath := map[string]SupportChange{}
	for _, support := range change.Support {
		byPath[support.Path] = support
	}
	if byPath["run.sh"].Kind != SupportText {
		t.Fatalf("changed text support must be a text change: %+v", byPath["run.sh"])
	}
	if byPath["data.bin"].Kind != SupportBinary {
		t.Fatalf("changed binary support must be a binary change: %+v", byPath["data.bin"])
	}
	if byPath["new.txt"].Kind != SupportAdded {
		t.Fatalf("new support file must be an addition: %+v", byPath["new.txt"])
	}
}

func TestCompareSupportModeOnlyChange(t *testing.T) {
	defn := ir.NewSkill("runner", "Runs", "Run.", nil, nil, ir.SourceRef{})
	id := gitIdentity("runner", "main")
	prior := []Side{side(id, "r1", defn, map[string]SupportFile{"run.sh": {Data: []byte("same\n"), Executable: false}})}
	candidate := []Side{side(id, "r2", defn, map[string]SupportFile{"run.sh": {Data: []byte("same\n"), Executable: true}})}
	changes, err := Compare(prior, candidate)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(changes) != 1 || len(changes[0].Support) != 1 {
		t.Fatalf("expected one support change: %+v", changes)
	}
	if changes[0].Support[0].Kind != SupportModeOnly {
		t.Fatalf("identical bytes with a changed executable mode must be a mode-only change: %+v", changes[0].Support[0])
	}
	if !changes[0].Support[0].New.Executable || changes[0].Support[0].Old.Executable {
		t.Fatalf("mode change must carry old and new executable state: %+v", changes[0].Support[0])
	}
}

// A summary comparison detects support changes by digest without retaining
// bodies, and never mislabels a digest change as text or binary.
func TestCompareDetectsDigestOnlySupportChangesWithoutBodies(t *testing.T) {
	defn := ir.NewSkill("runner", "Runs", "Run.", nil, nil, ir.SourceRef{})
	id := gitIdentity("runner", "main")
	prior := []Side{side(id, "r1", defn, map[string]SupportFile{
		"run.sh": {Digest: "sha256:" + strings.Repeat("a", 64), Executable: true},
		"same":   {Digest: "sha256:" + strings.Repeat("c", 64)},
	})}
	candidate := []Side{side(id, "r2", defn, map[string]SupportFile{
		"run.sh": {Digest: "sha256:" + strings.Repeat("b", 64), Executable: true},
		"same":   {Digest: "sha256:" + strings.Repeat("c", 64)},
	})}
	changes, err := Compare(prior, candidate)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected one change record, got %d", len(changes))
	}
	change := changes[0]
	if change.Kind != ChangeModified || change.DefinitionChanged {
		t.Fatalf("digest-only support change must be a support-only modification: %+v", change)
	}
	if len(change.Support) != 1 {
		t.Fatalf("unchanged digest must produce no record: %+v", change.Support)
	}
	if change.Support[0].Path != "run.sh" || change.Support[0].Kind != SupportChanged {
		t.Fatalf("digest-only change must be labeled changed: %+v", change.Support[0])
	}
}

// A digest-only mode-only change is detected without bodies.
func TestCompareDigestOnlyModeChange(t *testing.T) {
	defn := ir.NewSkill("runner", "Runs", "Run.", nil, nil, ir.SourceRef{})
	id := gitIdentity("runner", "main")
	prior := []Side{side(id, "r1", defn, map[string]SupportFile{"run.sh": {Digest: "sha256:" + strings.Repeat("a", 64), Executable: false}})}
	candidate := []Side{side(id, "r2", defn, map[string]SupportFile{"run.sh": {Digest: "sha256:" + strings.Repeat("a", 64), Executable: true}})}
	changes, err := Compare(prior, candidate)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(changes) != 1 || len(changes[0].Support) != 1 || changes[0].Support[0].Kind != SupportModeOnly {
		t.Fatalf("expected a mode-only change: %+v", changes)
	}
}

// spec: update-output/unselected-shared-manifest-entries-are-excluded
func TestCompareUsesOnlyTheSelectedResourceDefinition(t *testing.T) {
	// Two MCP servers share one source manifest. Only the selected one is
	// reviewed; the unrelated entry's data never enters the comparison.
	selected := ir.NewMcpServer("selected-server", "sh", []string{"-c", "run-selected"}, nil, nil, ir.SourceRef{})
	id := Identity{Scope: "repo", Kind: "mcpserver", Name: "selected-server", SourceURL: "https://git.example/reg.git", Ref: "main"}
	changed := ir.NewMcpServer("selected-server", "sh", []string{"-c", "run-selected-v2"}, nil, nil, ir.SourceRef{})
	prior := []Side{side(id, "r1", selected, nil)}
	candidate := []Side{side(id, "r2", changed, nil)}
	result, err := Compare(prior, candidate)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(result) != 1 || !result[0].DefinitionChanged {
		t.Fatalf("selected definition change must be reported: %+v", result)
	}
	if result[0].CandidateDoc == "" || !contains(result[0].CandidateDoc, "run-selected-v2") {
		t.Fatalf("candidate definition must carry only the selected resource's data: %+v", result[0])
	}
}

func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
