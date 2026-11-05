package toolpolicy

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const validConcepts = `schema: 1
tools:
  shell: Run a shell command.
  file.read: Read file contents.
events:
  PreToolUse: Before a tool call runs.
`

const validCatalog = `schema: 1
harness: devin
evidence:
  checked: "2026-09-29"
  refs: [synthetic-test-fixture]
mcp:
  pattern: "mcp__{server}__{tool}"
  all: "mcp__.*"
  server_scoped: true
surfaces: [hook_matcher, subagent_tools]
tools:
  exec: {concepts: [shell]}
  read: {concepts: [file.read], surfaces: [hook_matcher]}
`

func load(t *testing.T, catalog string) error {
	t.Helper()
	_, err := loadCatalogs(fstest.MapFS{
		"concepts.yaml":       {Data: []byte(validConcepts)},
		"catalogs/devin.yaml": {Data: []byte(catalog)},
	}, "concepts.yaml", "catalogs/*.yaml")
	return err
}

func TestShippedCatalogsValidate(t *testing.T) {
	if err := ValidateCatalogs(); err != nil {
		t.Fatal(err)
	}
}

func TestEveryTargetShipsACatalog(t *testing.T) {
	r, err := shipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range target.Targets() {
		if _, ok := r.catalogs[tg]; !ok {
			t.Fatalf("no tool catalog for target %s", tg)
		}
	}
}

func TestMalformedCatalogsRejected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		catalog string
		want    string
	}{
		{
			name: "unknown key",
			catalog: strings.Replace(validCatalog,
				"surfaces: [hook_matcher, subagent_tools]",
				"bogus_field: true\nsurfaces: [hook_matcher, subagent_tools]", 1),
			want: "bogus_field",
		},
		{
			name: "duplicate key",
			catalog: strings.Replace(validCatalog,
				"schema: 1", "schema: 1\nschema: 1", 1),
			want: "duplicate",
		},
		{
			name: "unknown concept",
			catalog: strings.Replace(validCatalog,
				"concepts: [shell]", "concepts: [shell.nope]", 1),
			want: "shell.nope",
		},
		{
			name: "surface narrowing to undeclared surface",
			catalog: strings.Replace(validCatalog,
				"surfaces: [hook_matcher]", "surfaces: [skill_allowed_tools]", 1),
			want: "skill_allowed_tools",
		},
		{
			name: "invalid surface name",
			catalog: strings.Replace(validCatalog,
				"surfaces: [hook_matcher, subagent_tools]",
				"surfaces: [hook_matcher, prompt_hooks]", 1),
			want: "prompt_hooks",
		},
		{
			name: "missing evidence",
			catalog: strings.Replace(validCatalog,
				"evidence:\n  checked: \"2026-09-29\"\n  refs: [synthetic-test-fixture]\n", "", 1),
			want: "evidence",
		},
		{
			name: "evidence without refs",
			catalog: strings.Replace(validCatalog,
				"refs: [synthetic-test-fixture]", "refs: []", 1),
			want: "evidence",
		},
		{
			name: "mcp pattern without placeholders",
			catalog: strings.Replace(validCatalog,
				`pattern: "mcp__{server}__{tool}"`, `pattern: "mcp__fixed"`, 1),
			want: "pattern",
		},
		{
			name: "empty tool name",
			catalog: strings.Replace(validCatalog,
				"exec: {concepts: [shell]}", `"": {concepts: [shell]}`, 1),
			want: "tool",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := load(t, tc.catalog)
			if err == nil {
				t.Fatalf("invalid catalog accepted; want failure containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want mention of %q", err, tc.want)
			}
		})
	}
}

// spec: harness-tool-catalogs/colliding-alias-fails-the-check
func TestCollidingAliasRejected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		catalog string
	}{
		{
			name: "alias collides with a tool key",
			catalog: strings.Replace(validCatalog,
				"exec: {concepts: [shell]}",
				"exec: {concepts: [shell], aliases: [read]}", 1),
		},
		{
			name: "alias collides with another alias",
			catalog: strings.Replace(validCatalog,
				"read: {concepts: [file.read], surfaces: [hook_matcher]}",
				"read: {concepts: [file.read], surfaces: [hook_matcher], aliases: [exec2]}\n  write: {concepts: [file.read], aliases: [exec2]}", 1),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := load(t, tc.catalog); err == nil {
				t.Fatal("colliding alias accepted")
			}
		})
	}
}

func TestAliasAcceptedOnDistinctName(t *testing.T) {
	catalog := strings.Replace(validCatalog,
		"exec: {concepts: [shell]}", "exec: {concepts: [shell], aliases: [shell_exec]}", 1)
	if err := load(t, catalog); err != nil {
		t.Fatal(err)
	}
}

// TestDevinVocabularyIsClosed carries forward the retired mapping-table
// invariant: the tools Devin's allow-list surfaces expose are exactly the five
// documented names, so a catalog edit cannot widen Devin's privileges
// silently.
func TestDevinVocabularyIsClosed(t *testing.T) {
	for _, surface := range []string{SurfaceSubagentTools, SurfaceSkillAllowedTools} {
		c, err := catalogFor(target.TargetDevin)
		if err != nil {
			t.Fatal(err)
		}
		got := c.surfaceTools(surface)
		want := []string{"exec", "read", "edit", "grep", "glob"}
		if !slices.Equal(got, want) {
			t.Errorf("devin %s tools = %v, want %v", surface, got, want)
		}
	}
}

// TestCodexHasNoToolListSurface carries forward the invariant that Codex
// custom agents have no per-agent tool allow-list: a non-empty restriction
// must fail with ErrNoSurface rather than emit an ignored field.
func TestCodexHasNoToolListSurface(t *testing.T) {
	for _, surface := range []string{SurfaceSubagentTools, SurfaceSkillAllowedTools} {
		if _, _, err := TranslateToolList(target.TargetClaude, target.TargetCodex, surface, []string{"Read"}, nil); !errors.Is(err, ErrNoSurface) {
			t.Errorf("TranslateToolList(claude→codex, %s) err = %v, want ErrNoSurface", surface, err)
		}
	}
}
