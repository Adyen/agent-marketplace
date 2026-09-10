package resolver

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// --- Attack surface 3: cross-registry and cross-kind bleed ------------------

// TestResolveMalformedInOneRegistryDoesNotBlameAnotherRegistryAbsence is the
// core scoping guarantee: registry A has a malformed "foo", registry B simply
// does not have "foo". B's selection is genuinely stale and must keep the
// not-found diagnosis; A's must be the parse failure.
func TestResolveMalformedInOneRegistryDoesNotBlameAnotherRegistryAbsence(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:a.git", Ref: "main", Skills: ks("foo")},
			{URL: "git@example.com:b.git", Ref: "main", Skills: ks("foo")},
		},
	}
	keyA := ck("git@example.com:a.git", "main")
	keyB := ck("git@example.com:b.git", "main")
	catalogs := map[string][]catalog.Entry{keyA: {}, keyB: {}}
	malformed := map[string][]skill.MalformedSkill{
		keyA: {{DirName: "foo", Path: "/cache/a/skills/foo", Error: errors.New("yaml: boom")}},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 2 {
		t.Fatalf("expected one error per registry, got %d: %v", len(errs), errs)
	}
	var sawMalformedForA, sawNotFoundForB bool
	for _, err := range errs {
		var sme *SelectedMalformedError
		if errors.As(err, &sme) {
			if !strings.Contains(sme.URL, "a.git") {
				t.Fatalf("malformed diagnosis leaked to the wrong registry: %v", err)
			}
			sawMalformedForA = true
			continue
		}
		var snf *SelectedNotFoundError
		if errors.As(err, &snf) {
			if !strings.Contains(snf.URL, "b.git") {
				t.Fatalf("not-found diagnosis attached to the wrong registry: %v", err)
			}
			sawNotFoundForB = true
		}
	}
	if !sawMalformedForA || !sawNotFoundForB {
		t.Fatalf("expected a malformed diagnosis for A and a not-found for B, got: %v", errs)
	}
}

// TestResolveMalformedInBothRegistriesReportsBothAsParseFailures inverts the
// case above.
func TestResolveMalformedInBothRegistriesReportsBothAsParseFailures(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:a.git", Ref: "main", Skills: ks("foo")},
			{URL: "git@example.com:b.git", Ref: "main", Skills: ks("foo")},
		},
	}
	keyA := ck("git@example.com:a.git", "main")
	keyB := ck("git@example.com:b.git", "main")
	catalogs := map[string][]catalog.Entry{keyA: {}, keyB: {}}
	malformed := map[string][]skill.MalformedSkill{
		keyA: {{DirName: "foo", Path: "/cache/a/skills/foo", Error: errors.New("yaml: a")}},
		keyB: {{DirName: "foo", Path: "/cache/b/skills/foo", Error: errors.New("yaml: b")}},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 2 {
		t.Fatalf("expected one error per registry, got %d: %v", len(errs), errs)
	}
	for _, err := range errs {
		if !IsSelectedMalformed(err) {
			t.Fatalf("expected every registry to report a parse failure, got %T: %v", err, err)
		}
		if IsSelectedNotFound(err) {
			t.Fatalf("a malformed selection must never be a not-found error: %v", err)
		}
	}
}

// TestResolveMalformedDoesNotCorrelateAcrossKinds is the cross-kind
// misattribution attack. A malformed SKILL directory named "foo" says nothing
// about an MCP server or command named "foo": those selections are genuinely
// absent and must keep the not-found diagnosis (which is also what makes them
// prunable). Blaming skills/foo/SKILL.md for a missing MCP server sends the
// user to fix an unrelated file.
func TestResolveMalformedDoesNotCorrelateAcrossKinds(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:infra.git",
			Ref:      "main",
			Commands: ks("foo"),
			MCP:      ks("foo"),
		}},
	}
	key := ck("git@example.com:infra.git", "main")
	catalogs := map[string][]catalog.Entry{key: {}}
	malformed := map[string][]skill.MalformedSkill{
		key: {{
			Kind:    ir.KindSkill.String(),
			DirName: "foo",
			Path:    "/cache/infra/skills/foo",
			Error:   errors.New("yaml: boom"),
		}},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 2 {
		t.Fatalf("expected one error per selected kind, got %d: %v", len(errs), errs)
	}
	for _, err := range errs {
		var sme *SelectedMalformedError
		if errors.As(err, &sme) {
			t.Fatalf("a malformed %s must not be blamed for an absent %s: %v",
				ir.KindSkill, sme.Kind, err)
		}
		if !IsSelectedNotFound(err) {
			t.Fatalf("expected the unchanged not-found diagnosis, got %T: %v", err, err)
		}
	}
}

// TestResolveMalformedCorrelatesWithinTheSameKind is the positive counterpart:
// kind scoping must not break the fix it protects.
func TestResolveMalformedCorrelatesWithinTheSameKind(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:infra.git",
			Ref:      "main",
			Commands: ks("deploy"),
		}},
	}
	key := ck("git@example.com:infra.git", "main")
	catalogs := map[string][]catalog.Entry{key: {}}
	malformed := map[string][]skill.MalformedSkill{
		key: {{
			Kind:    ir.KindCommand.String(),
			DirName: "deploy.md",
			Path:    "/cache/infra/commands/deploy.md",
			Error:   errors.New("yaml: boom"),
		}},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 1 || !IsSelectedMalformed(errs[0]) {
		t.Fatalf("expected a parse failure for a malformed command, got: %v", errs)
	}
}

// TestResolveMalformedWithUnknownKindStillCorrelates keeps producers that
// cannot classify a failure (skill.DiscoverSkills, legacy paths) working: an
// unset Kind means "unknown", not "no kind".
func TestResolveMalformedWithUnknownKindStillCorrelates(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main", Skills: ks("broken")},
		},
	}
	key := ck("git@example.com:infra.git", "main")
	catalogs := map[string][]catalog.Entry{key: {}}
	malformed := map[string][]skill.MalformedSkill{
		key: {{DirName: "broken", Path: "/cache/infra/skills/broken", Error: errors.New("yaml: boom")}},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 1 || !IsSelectedMalformed(errs[0]) {
		t.Fatalf("expected an unclassified malformed entry to still correlate, got: %v", errs)
	}
}

// --- Attack surface 5: collection deferral ----------------------------------

// TestResolveCollectionMalformedInEveryRegistryIsStillReported makes sure the
// suppression fix did not turn "malformed everywhere" into silence.
func TestResolveCollectionMalformedInEveryRegistryIsStillReported(t *testing.T) {
	cfg := &config.Config{
		Collections: config.KindSelector{Include: []string{"infra-tools"}},
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:a.git", Ref: "main"},
			{URL: "git@example.com:b.git", Ref: "main"},
		},
	}
	keyA := ck("git@example.com:a.git", "main")
	keyB := ck("git@example.com:b.git", "main")
	catalogs := map[string][]catalog.Entry{keyA: {}, keyB: {}}
	malformed := map[string][]skill.MalformedSkill{
		keyA: {{Kind: ir.KindCollection.String(), DirName: "infra-tools", Path: "/cache/a/collections/infra-tools.yaml", Error: errors.New("yaml: a")}},
		keyB: {{Kind: ir.KindCollection.String(), DirName: "infra-tools", Path: "/cache/b/collections/infra-tools.yaml", Error: errors.New("yaml: b")}},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) == 0 {
		t.Fatalf("a collection that is malformed in every registry must still be reported")
	}
	for _, err := range errs {
		if !IsSelectedMalformed(err) {
			t.Fatalf("expected parse failures, got %T: %v", err, err)
		}
		if IsSoftSelectedMalformed(err) {
			t.Fatalf("collection failures stay hard errors, got a soft one: %v", err)
		}
	}
}

// TestResolveCollectionMalformedDoesNotBleedIntoASkillOfTheSameName pins kind
// separation for the deferral path: a malformed COLLECTION named "shared" must
// not be blamed for an absent SKILL named "shared", and vice versa.
func TestResolveCollectionMalformedDoesNotBleedIntoASkillOfTheSameName(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:a.git", Ref: "main", Skills: ks("shared")},
		},
	}
	keyA := ck("git@example.com:a.git", "main")
	catalogs := map[string][]catalog.Entry{keyA: {}}
	malformed := map[string][]skill.MalformedSkill{
		keyA: {{Kind: ir.KindCollection.String(), DirName: "shared", Path: "/cache/a/collections/shared.yaml", Error: errors.New("yaml: boom")}},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %v", len(errs), errs)
	}
	if !IsSelectedNotFound(errs[0]) {
		t.Fatalf("a malformed collection must not be blamed for an absent skill, got %T: %v", errs[0], errs[0])
	}
}

// TestResolveNestedCollectionMemberMalformedKeepsCollectionRemediation walks a
// collection that includes another collection whose member is malformed.
func TestResolveNestedCollectionMemberMalformedKeepsCollectionRemediation(t *testing.T) {
	inner := ir.NewCollection("inner", "1.0.0", "Inner",
		[]ir.CollectionMember{{Name: "broken", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
	outer := ir.NewCollection("outer", "1.0.0", "Outer", nil, []string{"inner"}, nil, ir.SourceRef{})

	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:a.git", Ref: "main", Collections: ks("outer")},
		},
	}
	keyA := ck("git@example.com:a.git", "main")
	catalogs := map[string][]catalog.Entry{keyA: {
		{Kind: ir.KindCollection, Name: "outer", Resource: outer},
		{Kind: ir.KindCollection, Name: "inner", Resource: inner},
	}}
	malformed := map[string][]skill.MalformedSkill{
		keyA: {{Kind: ir.KindSkill.String(), DirName: "broken", Path: "/cache/a/skills/broken", Error: errors.New("yaml: boom")}},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	var sme *SelectedMalformedError
	for _, err := range errs {
		if errors.As(err, &sme) {
			break
		}
	}
	if sme == nil {
		t.Fatalf("expected a parse failure for the nested collection member, got: %v", errs)
	}
	if sme.Via == "" {
		t.Fatalf("expected the collection attribution to survive nesting, got %#v", sme)
	}
	// A member reached through a collection is not a user config error, so it
	// must not fail the run any harder than a stale member does.
	if !IsSoftSelectedMalformed(sme) {
		t.Fatalf("a malformed skill contributed by a collection must stay a warning, got a hard error: %v", sme)
	}
}

// --- Attack surface 6: severity ---------------------------------------------

// TestResolveMalformedRequiredRegistryStaysHard keeps policy enforcement.
func TestResolveMalformedRequiredRegistryStaysHard(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:a.git", Ref: "main", Required: true, Skills: ks("broken")},
		},
	}
	keyA := ck("git@example.com:a.git", "main")
	catalogs := map[string][]catalog.Entry{keyA: {}}
	malformed := map[string][]skill.MalformedSkill{
		keyA: {{Kind: ir.KindSkill.String(), DirName: "broken", Path: "/cache/a/skills/broken", Error: errors.New("yaml: boom")}},
	}

	setCatalogKeys(cfg)

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %v", len(errs), errs)
	}
	if IsSoftSelectedMalformed(errs[0]) {
		t.Fatalf("a required registry must keep a hard failure, got a soft one: %v", errs[0])
	}
	if !IsSelectedMalformed(errs[0]) {
		t.Fatalf("expected the parse-failure diagnosis, got %T: %v", errs[0], errs[0])
	}
}

// TestResolveMalformedResourceNobodySelectedIsSilent pins the blast radius: a
// broken resource in a shared registry must not affect a user who never
// selected it.
func TestResolveMalformedResourceNobodySelectedIsSilent(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:a.git", Ref: "main", Skills: ks("valid")},
		},
	}
	keyA := ck("git@example.com:a.git", "main")
	catalogs := map[string][]catalog.Entry{keyA: {{Name: "valid", Path: "/cache/a/skills/valid"}}}
	malformed := map[string][]skill.MalformedSkill{
		keyA: {{Kind: ir.KindSkill.String(), DirName: "broken", Path: "/cache/a/skills/broken", Error: errors.New("yaml: boom")}},
	}

	setCatalogKeys(cfg)

	resolved, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 0 {
		t.Fatalf("an unselected malformed resource must not produce errors, got: %v", errs)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected the healthy selection to resolve, got %#v", resolved)
	}
}
