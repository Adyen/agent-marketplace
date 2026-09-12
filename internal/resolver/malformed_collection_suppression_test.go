package resolver

import (
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// TestResolveCollectionMalformedInOneRegistryIsSuppressedWhenFoundInAnother
// pins the suppression that malformed correlation must not bypass.
// propagateTopLevelCollections copies a top-level collections.include onto
// EVERY registry entry, so a registry that does not provide the collection
// always produces a per-registry failure. That failure is deferred and dropped
// once another registry provides the collection. A malformed resource in the
// first registry that happens to share the collection's name must not turn
// that suppressed false positive into an error — and collection errors are
// hard (IsSoftSelectedMalformed is false for KindCollection), so it would fail
// a sync that resolves perfectly.
func TestResolveCollectionMalformedInOneRegistryIsSuppressedWhenFoundInAnother(t *testing.T) {
	cfg := &config.Config{
		Collections: config.KindSelector{Include: []string{"infra-tools"}},
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:a.git", Ref: "main"},
			{URL: "git@example.com:b.git", Ref: "main"},
		},
	}
	keyA := ck("git@example.com:a.git", "main")
	keyB := ck("git@example.com:b.git", "main")
	col := ir.NewCollection("infra-tools", "1.0.0", "Infra tools", nil, nil, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{
		keyA: {},
		keyB: {{Kind: ir.KindCollection, Name: "infra-tools", Resource: col}},
	}
	malformed := map[string][]skill.MalformedSkill{
		keyA: {{DirName: "infra-tools", Path: "/cache/a/skills/infra-tools", Error: errors.New("yaml: boom")}},
	}

	setCatalogKeys(cfg)

	if _, errs := Resolve(cfg, catalogs, nil); len(errs) != 0 {
		t.Fatalf("baseline (no malformed set) must resolve cleanly, got: %v", errs)
	}
	if _, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed)); len(errs) != 0 {
		t.Fatalf("a collection resolved from another registry must stay suppressed, got: %v", errs)
	}
}

// TestDeferredMalformedCollectionNarrowsToUnresolvedPartitions proves that a
// valid same-partition collection materializes its member while only the
// unresolved target remains fenced by the malformed sibling.
func TestDeferredMalformedCollectionNarrowsToUnresolvedPartitions(t *testing.T) {
	const (
		malformedURL = "git@example.com:malformed-collection.git"
		validURL     = "git@example.com:valid-collection.git"
	)
	cfg := &config.Config{
		Collections: config.KindSelector{Include: []string{"tools"}},
		Registries: []config.RegistryEntry{
			{
				URL:       malformedURL,
				Ref:       "main",
				Harnesses: []string{string(hcc.TargetClaude), string(hcc.TargetFactory)},
			},
			{
				URL:         validURL,
				Ref:         "main",
				Collections: ks("tools"),
				Harnesses:   []string{string(hcc.TargetClaude)},
			},
		},
	}
	setCatalogKeys(cfg)
	validCollection := ir.NewCollection("tools", "1.0.0", "Tools", []ir.CollectionMember{
		{Name: "healthy-skill", Kind: ir.KindSkill},
	}, nil, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {},
		cfg.Registries[1].CatalogKey: {
			{Kind: ir.KindCollection, Name: "tools", Resource: validCollection},
			{Kind: ir.KindSkill, Name: "healthy-skill", Resource: ir.NewSkill("healthy-skill", "Healthy", "", nil, nil, ir.SourceRef{})},
		},
	}
	malformed := malformedFixture(cfg.Registries[0].CatalogKey, skill.MalformedSkill{
		DirName: "tools",
		Path:    "/cache/malformed/collections/tools",
		Error:   errors.New("yaml: invalid collection"),
	})

	resolved, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed), WithTargetHarnesses([]hcc.Target{
		hcc.TargetClaude,
		hcc.TargetFactory,
	}))
	if len(resolved) != 1 || resolved[0].Name != "healthy-skill" {
		t.Fatalf("valid sibling must resolve in its Claude partition, got %#v", resolved)
	}
	if !harnessesEqual(resolved[0].Harnesses, []hcc.Target{hcc.TargetClaude}) {
		t.Fatalf("valid sibling harnesses = %v, want Claude only", resolved[0].Harnesses)
	}

	var malformedErr *SelectedMalformedError
	for _, err := range errs {
		if errors.As(err, &malformedErr) {
			break
		}
	}
	if malformedErr == nil {
		t.Fatalf("expected unresolved Factory partition to remain fenced, got %v", errs)
	}
	partitions := MalformedSelectionPartitions(malformedErr)
	if len(partitions) != 1 || partitions[0] != (SelectionPartition{Target: hcc.TargetFactory}) {
		t.Fatalf("malformed collection partitions = %#v, want unresolved Factory only", partitions)
	}
}

// TestResolveCollectionMalformedStillReportedWhenNoRegistryProvidesIt keeps the
// suppression above from swallowing a real diagnosis.
func TestResolveCollectionMalformedStillReportedWhenNoRegistryProvidesIt(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:a.git", Ref: "main", Collections: ks("infra-tools")},
		},
	}
	keyA := ck("git@example.com:a.git", "main")
	catalogs := map[string][]catalog.Entry{keyA: {}}
	malformed := map[string][]skill.MalformedSkill{
		keyA: {{DirName: "infra-tools", Path: "/cache/a/collections/infra-tools.yaml", Error: errors.New("yaml: boom")}},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %v", len(errs), errs)
	}
	var sme *SelectedMalformedError
	if !errors.As(errs[0], &sme) {
		t.Fatalf("expected SelectedMalformedError, got %T: %v", errs[0], errs[0])
	}
}

// TestMalformedDiagnosticCodeWithOneSegmentMatchesNothing pins that a
// bundle-level diagnostic cannot correlate a selection through the bundle path.
// diag.CodeJSONMalformed ("json.malformed") has a single dot, and its Path is
// the file the bundle failed on — whose stem would otherwise become a
// selection candidate and misattribute a genuinely-absent selection.
func TestMalformedDiagnosticCodeWithOneSegmentMatchesNothing(t *testing.T) {
	entries := []skill.MalformedSkill{
		{DirName: "json.malformed", Path: "/cache/reg/.claude/settings.json", Error: errors.New("bad json")},
	}
	for _, name := range []string{"settings", "json.malformed"} {
		if _, found := skill.MatchMalformed(entries, name); found {
			t.Fatalf("bundle-level diagnostic must not correlate with selection %q", name)
		}
	}
}
