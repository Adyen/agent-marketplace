package resolver

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// removalGuessPhrases are the fragments of the SelectedNotFoundError
// remediation that must never appear for a selection that is present
// upstream but failed ingest validation. Naming them here keeps the
// misdiagnosis this bead fixes from silently coming back.
var removalGuessPhrases = []string{
	"upstream removal",
	"stale ref pin",
	"cache issue",
	"to drop the selection",
}

func assertNoRemovalGuess(t *testing.T, text string) {
	t.Helper()
	for _, phrase := range removalGuessPhrases {
		if strings.Contains(text, phrase) {
			t.Fatalf("diagnosis must not guess at an upstream removal (%q), got:\n%s", phrase, text)
		}
	}
}

// malformedFixture builds a per-catalog malformed set for one registry key.
func malformedFixture(catalogKey string, entries ...skill.MalformedSkill) map[string][]skill.MalformedSkill {
	return map[string][]skill.MalformedSkill{catalogKey: entries}
}

// TestResolveSelectedButMalformedReportsParseErrorInsteadOfRemovalGuess covers
// the previously-untested intersection of "selected" and "malformed": the skill
// exists upstream, failed ingest validation, and was therefore dropped from
// Entries. It must be reported with its file and parse error, not as a deletion.
func TestResolveSelectedButMalformedReportsParseErrorInsteadOfRemovalGuess(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main", Skills: ks("broken-skill")},
		},
	}
	key := ck("git@example.com:infra.git", "main")
	catalogs := map[string][]catalog.Entry{
		key: {{Name: "valid-skill", Path: "/cache/infra/skills/valid-skill"}},
	}
	parseErr := errors.New("ingest.claude.load-skill-failed: yaml: did not find expected ',' or ']'")
	malformed := malformedFixture(key, skill.MalformedSkill{
		DirName: "broken-skill",
		Path:    "/cache/infra/skills/broken-skill",
		Error:   parseErr,
	})

	setCatalogKeys(cfg)

	resolved, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed), WithTargetHarnesses([]hcc.Target{hcc.TargetClaude, hcc.TargetFactory}))
	if len(resolved) != 0 {
		t.Fatalf("expected no resolved artifacts, got %#v", resolved)
	}
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %v", len(errs), errs)
	}

	if IsSelectedNotFound(errs[0]) {
		t.Fatalf("a malformed selection must NOT be reported as selected-but-not-found, got: %v", errs[0])
	}
	var sme *SelectedMalformedError
	if !errors.As(errs[0], &sme) {
		t.Fatalf("expected SelectedMalformedError, got %T: %v", errs[0], errs[0])
	}
	if sme.Name != "broken-skill" || sme.Kind != ir.KindSkill {
		t.Fatalf("unexpected error identity: kind=%v name=%q", sme.Kind, sme.Name)
	}
	if !IsSelectedMalformed(errs[0]) || !IsSoftSelectedMalformed(errs[0]) {
		t.Fatalf("expected a soft selected-malformed classification for a non-required registry, got %v", errs[0])
	}
	partitions := MalformedSelectionPartitions(errs[0])
	if len(partitions) != 2 || partitions[0].Target != hcc.TargetClaude || partitions[1].Target != hcc.TargetFactory {
		t.Fatalf("malformed selection partitions = %#v, want Claude and Factory repo partitions", partitions)
	}
	for _, partition := range partitions {
		if partition.MachineScope {
			t.Fatalf("direct repo selection unexpectedly fenced machine scope: %#v", partition)
		}
	}

	full := errs[0].Error() + "\n" + sme.Remediation()
	for _, needle := range []string{
		"broken-skill",
		"/cache/infra/skills/broken-skill/SKILL.md",
		"did not find expected ',' or ']'",
	} {
		if !strings.Contains(full, needle) {
			t.Fatalf("expected diagnosis to contain %q, got:\n%s", needle, full)
		}
	}
	assertNoRemovalGuess(t, full)
}

// TestResolveSelectedButMalformedMatchesFrontmatterName covers the case that
// most resembles a rename: the selection uses the frontmatter `name:` while the
// dropped resource is only known by its directory name.
func TestResolveSelectedButMalformedMatchesFrontmatterName(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main", Skills: ks("bubble-tea")},
		},
	}
	key := ck("git@example.com:infra.git", "main")
	catalogs := map[string][]catalog.Entry{key: {}}
	malformed := malformedFixture(key, skill.MalformedSkill{
		DirName:      "bubbletea-tui",
		DeclaredName: "bubble-tea",
		Path:         "/cache/infra/skills/bubbletea-tui",
		Error:        errors.New("yaml: mapping values are not allowed in this context"),
	})

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %v", len(errs), errs)
	}
	var sme *SelectedMalformedError
	if !errors.As(errs[0], &sme) {
		t.Fatalf("expected SelectedMalformedError for a frontmatter-name selection, got %T: %v", errs[0], errs[0])
	}
	if !strings.Contains(sme.Error(), "bubbletea-tui") {
		t.Fatalf("expected the failing directory to be named, got %q", sme.Error())
	}
	assertNoRemovalGuess(t, sme.Error()+"\n"+sme.Remediation())
}

// TestResolveSelectedButMalformedMatchesPathDirectoryComponent covers ingesters
// that attach the SKILL.md file (not the directory) to the diagnostic.
func TestResolveSelectedButMalformedMatchesPathDirectoryComponent(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main", Skills: ks("broken-skill")},
		},
	}
	key := ck("git@example.com:infra.git", "main")
	catalogs := map[string][]catalog.Entry{key: {}}
	malformed := malformedFixture(key, skill.MalformedSkill{
		DirName: "",
		Path:    "/cache/infra/skills/broken-skill/SKILL.md",
		Error:   errors.New("yaml: line 3: could not find expected ':'"),
	})

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %v", len(errs), errs)
	}
	var sme *SelectedMalformedError
	if !errors.As(errs[0], &sme) {
		t.Fatalf("expected SelectedMalformedError matched via the path directory component, got %T: %v", errs[0], errs[0])
	}
	if !strings.Contains(sme.Error(), "/cache/infra/skills/broken-skill/SKILL.md") {
		t.Fatalf("expected the failing file to be named exactly once, got %q", sme.Error())
	}
}

// TestResolveCollectionMemberMalformedReportsParseError covers the second
// trigger site: a malformed member reached through a collection.
func TestResolveCollectionMemberMalformedReportsParseError(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main", Collections: ks("infra-tools")},
		},
	}
	key := ck("git@example.com:infra.git", "main")
	col := ir.NewCollection("infra-tools", "1.0.0", "Infra tools",
		[]ir.CollectionMember{{Name: "broken-skill", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{
		key: {{Kind: ir.KindCollection, Name: "infra-tools", Resource: col}},
	}
	malformed := malformedFixture(key, skill.MalformedSkill{
		DirName: "broken-skill",
		Path:    "/cache/infra/skills/broken-skill",
		Error:   errors.New("yaml: did not find expected ',' or ']'"),
	})

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	var sme *SelectedMalformedError
	for _, err := range errs {
		if errors.As(err, &sme) {
			break
		}
	}
	if sme == nil {
		t.Fatalf("expected SelectedMalformedError for a collection member, got: %v", errs)
	}
	if sme.Via == "" || !strings.Contains(sme.Via, "infra-tools") {
		t.Fatalf("expected Via to name the collection, got %q", sme.Via)
	}
	full := sme.Error() + "\n" + sme.Remediation()
	if !strings.Contains(full, "did not find expected ',' or ']'") {
		t.Fatalf("expected the parse error in the collection diagnosis, got:\n%s", full)
	}
	assertNoRemovalGuess(t, full)
	for _, err := range errs {
		if IsSelectedNotFound(err) {
			t.Fatalf("collection member present-but-malformed must not produce a not-found error, got: %v", err)
		}
	}
}

// TestResolveGenuinelyAbsentKeepsNotFoundEvenWithMalformedPresent pins the
// regression guard from the reported incident: resources that really are gone
// upstream must keep the existing warning and remediation.
func TestResolveGenuinelyAbsentKeepsNotFoundEvenWithMalformedPresent(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main", Skills: ks("beads")},
		},
	}
	key := ck("git@example.com:infra.git", "main")
	catalogs := map[string][]catalog.Entry{
		key: {{Name: "valid-skill", Path: "/cache/infra/skills/valid-skill"}},
	}
	malformed := malformedFixture(key, skill.MalformedSkill{
		DirName: "broken-skill",
		Path:    "/cache/infra/skills/broken-skill",
		Error:   errors.New("yaml: did not find expected ',' or ']'"),
	})

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %v", len(errs), errs)
	}
	var snf *SelectedNotFoundError
	if !errors.As(errs[0], &snf) {
		t.Fatalf("expected a genuinely-absent skill to keep SelectedNotFoundError, got %T: %v", errs[0], errs[0])
	}
	if !strings.Contains(snf.Remediation(), "upstream removal") ||
		!strings.Contains(snf.Remediation(), "to drop the selection") {
		t.Fatalf("expected the existing not-found remediation to be unchanged, got %q", snf.Remediation())
	}
}
