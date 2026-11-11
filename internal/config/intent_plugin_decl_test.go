package config

import "testing"

// The lock path consumes SourceDecl selection policy: a required-only group is
// a valid empty selection in v4, so the declaration must not become select-all,
// matching the runtime adapter's Required normalization.
func TestSourceDeclCarriesNormalizedSelectionPolicy(t *testing.T) {
	parse := func(t *testing.T, group string) SourceDecl {
		v4, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
sources:
  cat: {git: https://example.com/cat.git, ref: main, format: claude-marketplace}
resources:
  cat: `+group+`
`), "config.yaml", true)
		if err != nil {
			t.Fatal(err)
		}
		decls := LayerIntent{Source: RepoCommitted, v4: v4}.Sources()
		if len(decls) != 1 {
			t.Fatalf("decls = %#v, want one", decls)
		}
		return decls[0]
	}

	if decl := parse(t, "{required: true}"); decl.Required || decl.All {
		t.Fatalf("required-only decl = %#v, want an empty selection", decl)
	}
	if decl := parse(t, "{required: true, include: [plugin/a]}"); !decl.Required || decl.All {
		t.Fatalf("required decl with an include = %#v, want Required without All", decl)
	}
	if decl := parse(t, "{all: true}"); !decl.All || decl.Required {
		t.Fatalf("all decl = %#v, want All without Required", decl)
	}
}
