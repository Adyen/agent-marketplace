package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Freeze the former parser, including refusal precedence and empty error
// outputs. Validators shared with it are unchanged by this allocation slice.
func legacyParseV4Locator(value, source string) (kind, registry, name string, err error) {
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\\x00\r\n") {
		return "", "", "", errors.New("must be a clean locator")
	}
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		kind, name = parts[0], parts[1]
	} else if len(parts) == 3 {
		registry, kind, name = parts[0], parts[1], parts[2]
	} else {
		return "", "", "", errors.New("must be kind/name or registry/kind/name")
	}
	if _, ok := v4Kinds[kind]; !ok || !v4NameRE.MatchString(name) || name == "." || name == ".." || strings.ContainsAny(name, "*?") {
		return "", "", "", errors.New("has invalid kind or resource name")
	}
	if err := validateMarketplaceSegment("name", name); err != nil {
		return "", "", "", err
	}
	if len(parts) == 3 && !v4SlugRE.MatchString(registry) {
		return "", "", "", errors.New("has invalid registry slug")
	}
	if source == "" {
		return "", "", "", errors.New("missing source")
	}
	return kind, registry, name, nil
}

func checkV4LocatorParsingReplay(t *testing.T, value, source string) {
	t.Helper()
	kind, registry, name, err := parseV4Locator(value, source)
	wantKind, wantRegistry, wantName, wantErr := legacyParseV4Locator(value, source)
	if kind != wantKind || registry != wantRegistry || name != wantName ||
		(err == nil) != (wantErr == nil) || err != nil && err.Error() != wantErr.Error() {
		t.Fatalf("parse(%q, %q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
			value, source, kind, registry, name, err, wantKind, wantRegistry, wantName, wantErr)
	}
}

func TestV4LocatorParsingReplay(t *testing.T) {
	values := []string{
		"", "*", "/", "//", "///", "skill", "skill/", "/review", "skill/review",
		"tools/skill/review", "/skill/review", "tools//review", "tools/skill/",
		"tools/skill/review/extra", "tools/skill/review/", "tools/skill//",
		"tools/not-kind/review", "tools/skill/*", "tools/skill/a?b",
		"tools/skill/.", "tools/skill/..", "tools/skill/a..b", "tools/skill/[a]",
		"tools/skill/review ", " tools/skill/review", "tools/skill/\x00",
		"tools/skill/\n", "tools/skill/\t", "tools/skill/\x7f", "tools/skill/\xff",
		"tools/skill/a\\b", "-tools/skill/review", "tools space/skill/review",
		"tools.v2_1/skill/review", "tools/skill/" + strings.Repeat("x", 4096),
	}
	for _, value := range values {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			for _, source := range []string{"", "g", " private/source "} {
				checkV4LocatorParsingReplay(t, value, source)
			}
		})
	}
}

func FuzzV4LocatorParsingReplay(f *testing.F) {
	for _, value := range []string{"", "//", "skill/review", "tools/skill/review", "tools/skill/..", "tools/skill/review/extra", "tools/skill/\xff"} {
		f.Add(value, "g")
		f.Add(value, "")
	}
	f.Fuzz(func(t *testing.T, value, source string) {
		checkV4LocatorParsingReplay(t, value, source)
	})
}

func TestV4LocatorParsingPublicKinds(t *testing.T) {
	for _, source := range []V4SourceKind{V4SourceGit, V4SourceDirectory, V4SourceMarketplace} {
		t.Run(string(source), func(t *testing.T) {
			for _, kind := range []string{"skill", "command", "subagent", "hook", "mcp", "collection"} {
				locator, wantRegistry := kind+"/review", ""
				binding := "git: https://example.test/tools.git, ref: main"
				switch source {
				case V4SourceDirectory:
					binding = fmt.Sprintf("directory: %q", filepath.ToSlash(t.TempDir()))
				case V4SourceMarketplace:
					binding, locator, wantRegistry = "marketplace: https://market.example", "tools/"+locator, "tools"
				}
				data := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {g: {%s}}\nresources: {g: {include: [%q]}}\n", binding, locator)
				cfg, err := ParseV4([]byte(data))
				if err != nil || cfg == nil || len(cfg.Requests) != 1 {
					t.Fatalf("%s fixture: %v", kind, err)
				}
				got := cfg.Requests[0]
				if cfg.Sources["g"].Kind != source || got.Kind != kind || got.Registry != wantRegistry ||
					got.Name != "review" || got.Locator != locator || got.SourceName != "g" ||
					got.Options.As != "review" || got.Reason != V4SelectionExplicit {
					t.Fatalf("public %s/%s request changed: %#v", source, kind, got)
				}
			}
		})
	}
}

func TestV4LocatorParsingPublicRefusals(t *testing.T) {
	for _, value := range []string{"skill", "skill/review/extra/parts", "tools/skill/a*b", "tools/skill/..", "-tools/skill/review", " tools/skill/review"} {
		t.Run(value, func(t *testing.T) {
			_, _, _, want := legacyParseV4Locator(value, "g")
			if want == nil {
				t.Fatal("refusal fixture must be rejected by the frozen parser")
			}
			for _, operation := range []string{"include", "exclude"} {
				data := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {g: {marketplace: https://market.example}}\nresources: {g: {%s: [%q]}}\n", operation, value)
				cfg, err := ParseV4([]byte(data))
				var refusal *V4ValidationError
				code, path := "v4.locator", "resources.g.include[0]"
				if operation == "exclude" {
					code, path = "v4.exclusion", value
				}
				if cfg != nil || !errors.As(err, &refusal) || refusal.Code != code ||
					refusal.Path != path || refusal.Msg != want.Error() {
					t.Errorf("%s refusal = %#v (%v), want %s at %s: %v", operation, refusal, err, code, path, want)
				}
			}
		})
	}
}

func TestV4LocatorParsingEditorPreservesOnRefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "apiVersion: agent-marketplace/v4\n# keep comment\nsources: {g: {git: https://example.test/tools.git, ref: main}}\nresources: {g: {include: [command/kept]}}\n"
	layerEditorWrite(t, path, original)
	editor := openTestEditor(t, RepoCommitted, path, path)
	binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/tools.git", Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []SelectionOperation{SelectionInclude, SelectionExclude, SelectionRemoveInclude, SelectionClearExclude} {
		err := editor.Apply(binding, SelectionChange{Operation: operation, Locators: []ResourceLocator{loc(ir.KindSkill, "review/extra/parts")}})
		var refusal *V4ValidationError
		if !errors.As(err, &refusal) || refusal.Code != "v4.locator" ||
			refusal.Path != "resources.g" || refusal.Msg != "must be kind/name or registry/kind/name" {
			t.Fatalf("editor refusal changed: %#v (%v)", refusal, err)
		}
		candidate, err := editor.Render()
		if err != nil || candidate.Changed || !bytes.Equal(candidate.Bytes, []byte(original)) {
			t.Fatalf("refusal changed editor state: %#v (%v)", candidate, err)
		}
	}
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "review")}}); err != nil {
		t.Fatal(err)
	}
	candidate, err := editor.Render()
	if err != nil || !candidate.Changed || !bytes.Contains(candidate.Bytes, []byte("skill/review")) {
		t.Fatalf("positive editor control did not change selection: %#v (%v)", candidate, err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != original {
		t.Fatalf("in-memory editor unexpectedly wrote user bytes: %v", err)
	}
}
