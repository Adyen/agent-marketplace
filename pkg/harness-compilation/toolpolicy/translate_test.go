package toolpolicy

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"gopkg.in/yaml.v3"
)

// matcherSpecs mirrors the matcher syntax each harness's hook catalog
// declares; toolpolicy cannot import hookpolicy (the dependency runs the
// other way), so the fixture supplies the spec the callers would pass.
var matcherSpecs = map[target.Target]MatcherSpec{
	target.TargetClaude:   {Syntax: MatcherExactOrRegex, ExactLists: true},
	target.TargetFactory:  {Syntax: MatcherExactOrRegex},
	target.TargetCursor:   {Syntax: MatcherRegex},
	target.TargetGemini:   {Syntax: MatcherRegex},
	target.TargetDevin:    {Syntax: MatcherRegex, Anchor: true},
	target.TargetCodex:    {Syntax: MatcherRegex, Anchor: true},
	target.TargetWindsurf: {Syntax: MatcherNone},
}

type matcherFixture struct {
	Name        string `yaml:"name"`
	Source      string `yaml:"source"`
	Target      string `yaml:"target"`
	Event       string `yaml:"event"`
	Matcher     string `yaml:"matcher"`
	Command     string `yaml:"command"`
	Timeout     *int   `yaml:"timeout"`
	WantEvent   string `yaml:"want_event"`
	WantMatcher string `yaml:"want_matcher"`
	WantCommand string `yaml:"want_command"`
	WantTimeout *int   `yaml:"want_timeout"`
	WantWarning string `yaml:"want_warning"`
	WantError   string `yaml:"want_error"`
}

type allowListFixture struct {
	Name        string   `yaml:"name"`
	Source      string   `yaml:"source"`
	Target      string   `yaml:"target"`
	Surface     string   `yaml:"surface"`
	Tools       []string `yaml:"tools"`
	WantTools   []string `yaml:"want_tools"`
	WantWarning string   `yaml:"want_warning"`
	WantError   string   `yaml:"want_error"`
}

type conversionFixtures struct {
	Matchers   []matcherFixture   `yaml:"matchers"`
	Events     []matcherFixture   `yaml:"events"`
	Env        []matcherFixture   `yaml:"env"`
	Timeouts   []matcherFixture   `yaml:"timeouts"`
	AllowLists []allowListFixture `yaml:"allow_lists"`
}

func loadConversionFixtures(t *testing.T) conversionFixtures {
	t.Helper()
	raw, err := os.ReadFile("../hookpolicy/testdata/conversions.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures conversionFixtures
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

// TestMatcherFixtures runs the independently authored matcher fixtures
// against DecodeMatcher/RenderMatcher. Windsurf expectations are split by
// event in hookpolicy, so for a Windsurf target only the decode half is
// proven here.
func TestMatcherFixtures(t *testing.T) {
	for _, c := range loadConversionFixtures(t).Matchers {
		t.Run(c.Name, func(t *testing.T) {
			source := target.Target(c.Source)
			dst := target.Target(c.Target)
			decoded, err := DecodeMatcher(source, c.Matcher, matcherSpecs[source])
			if c.WantError != "" {
				if err == nil {
					if _, rerr := RenderMatcher(dst, decoded, matcherSpecs[dst]); rerr == nil {
						t.Fatalf("want error %q, got decoded %#v", c.WantError, decoded)
					}
					return
				}
				if !errors.Is(err, ErrUntranslatable) {
					t.Fatalf("err = %v, want %v", err, ErrUntranslatable)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeMatcher: %v", err)
			}
			if dst == target.TargetWindsurf {
				if decoded.Concepts == nil && !decoded.All {
					t.Fatalf("windsurf matcher decoded to nothing: %#v", decoded)
				}
				return
			}
			got, err := RenderMatcher(dst, decoded, matcherSpecs[dst])
			if err != nil {
				t.Fatalf("RenderMatcher: %v", err)
			}
			if got != c.WantMatcher {
				t.Fatalf("matcher = %q, want %q", got, c.WantMatcher)
			}
		})
	}
}

func TestMatchAllDecode(t *testing.T) {
	for _, tc := range []struct {
		matcher string
		wantAll bool
		wantErr bool
	}{
		{matcher: "*", wantAll: true},
		{matcher: ".*", wantAll: true},
		{matcher: " .* ", wantAll: true},
		{matcher: "Bash.*", wantErr: true},
		{matcher: ".+", wantErr: true},
		{matcher: "^.*$", wantErr: true},
		{matcher: "mcp__.*"},
	} {
		t.Run(tc.matcher, func(t *testing.T) {
			got, err := DecodeMatcher(target.TargetClaude, tc.matcher, matcherSpecs[target.TargetClaude])
			if tc.wantErr {
				if !errors.Is(err, ErrUntranslatable) {
					t.Fatalf("err = %v, want %v", err, ErrUntranslatable)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeMatcher: %v", err)
			}
			if got.All != tc.wantAll {
				t.Fatalf("All = %v, want %v (decoded %#v)", got.All, tc.wantAll, got)
			}
		})
	}
}

func TestMatchAllDecodeRefusesRegexlessSyntax(t *testing.T) {
	none := MatcherSpec{Syntax: MatcherNone}
	if got, err := DecodeMatcher(target.TargetClaude, "*", none); err != nil || !got.All {
		t.Fatalf("star with no regex syntax = %#v, %v; want All", got, err)
	}
	if got, err := DecodeMatcher(target.TargetClaude, ".*", none); !errors.Is(err, ErrUntranslatable) {
		t.Fatalf("regex .* with no regex syntax = %#v, %v; want %v", got, err, ErrUntranslatable)
	}
}

// TestAllowListFixtures runs the allow-list fixtures against
// TranslateToolList: subset-only coverage, uncovered names reported for the
// caller's diagnostic, and ErrNoSurface where the target cannot restrict.
func TestAllowListFixtures(t *testing.T) {
	for _, c := range loadConversionFixtures(t).AllowLists {
		t.Run(c.Name, func(t *testing.T) {
			emitted, uncovered, err := TranslateToolList(target.Target(c.Source), target.Target(c.Target), c.Surface, c.Tools, nil)
			if c.WantError != "" {
				if err == nil {
					t.Fatalf("want error %q, emitted = %v", c.WantError, emitted)
				}
				return
			}
			if err != nil {
				t.Fatalf("TranslateToolList: %v", err)
			}
			if !slices.Equal(emitted, c.WantTools) {
				t.Fatalf("emitted = %v, want %v", emitted, c.WantTools)
			}
			if c.WantTools == nil && len(uncovered) == 0 {
				t.Fatal("uncovered source tool was not reported")
			}
		})
	}
}

func TestTranslateToolListOverride(t *testing.T) {
	emitted, uncovered, err := TranslateToolList(target.TargetClaude, target.TargetFactory, SurfaceSubagentTools, []string{"Bash"}, map[string]string{"Bash": "Shell"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(emitted, []string{"Shell"}) || len(uncovered) != 0 {
		t.Fatalf("emitted=%v uncovered=%v", emitted, uncovered)
	}
}

func TestDecodeMatcherUnknownToolError(t *testing.T) {
	spec := MatcherSpec{Syntax: MatcherExactOrRegex, ExactLists: true}
	for _, tc := range []struct {
		name, matcher string
		source        target.Target
		wantKnownIn   string
		wantSource    string
	}{
		{"catalog name of another harness", "Edit|MultiEdit", target.TargetFactory, string(target.TargetClaude), "factory"},
		{"name in no catalog", "zqxNoSuchTool", target.TargetFactory, "", "factory"},
		{"no provenance reads as claude and skips it", "Execute", "", string(target.TargetFactory), "claude"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeMatcher(tc.source, tc.matcher, spec)
			var unknown *UnknownToolError
			if !errors.As(err, &unknown) || !errors.Is(err, ErrUntranslatable) {
				t.Fatalf("err = %v; want an *UnknownToolError wrapping ErrUntranslatable", err)
			}
			if unknown.KnownIn != tc.wantKnownIn || unknown.Source != tc.wantSource {
				t.Fatalf("KnownIn/Source = %q/%q, want %q/%q", unknown.KnownIn, unknown.Source, tc.wantKnownIn, tc.wantSource)
			}
		})
	}
}
