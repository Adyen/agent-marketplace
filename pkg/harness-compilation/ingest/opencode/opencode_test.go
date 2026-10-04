package opencode

import (
	"strings"
	"testing"
	"testing/fstest"

	compiler "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/opencode"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/opencodehook"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: harness-opencode/opencode-marker-distinguishes-a-shared-skills-layout
func TestMarkerDetection(t *testing.T) {
	if Detect(fstest.MapFS{".agents/skills/review/SKILL.md": {Data: []byte("---\nname: review\n---\nBody")}}) {
		t.Fatal("shared skills layout claimed as OpenCode")
	}
	if !Detect(fstest.MapFS{".opencode/commands/check.md": {Data: []byte("Check")}}) {
		t.Fatal("OpenCode marker not detected")
	}
}

// spec: harness-opencode/native-aliases-do-not-create-duplicate-resources
func TestNativeAliases(t *testing.T) {
	b, err := LoadBundle(fstest.MapFS{
		".opencode/commands/check.md": {Data: []byte("Native")},
		".opencode/command/check.md":  {Data: []byte("Legacy")},
		".opencode/agent/reviewer.md": {Data: []byte("---\ndescription: Review\nmode: subagent\n---\nReview")},
	})
	if err != nil || len(b.Resources) != 2 {
		t.Fatalf("load: %v %#v", err, b)
	}
	if b.Resources[0].(*ir.Command).Body != "Native" {
		t.Fatal("plural native path did not win")
	}
}

func TestDistinctFilesWithDuplicateIdentity(t *testing.T) {
	b, err := LoadBundle(fstest.MapFS{
		"skills/first/SKILL.md":  {Data: []byte("---\nname: shared\ndescription: First\n---\nFirst")},
		"skills/second/SKILL.md": {Data: []byte("---\nname: shared\ndescription: Second\n---\nSecond")},
		"commands/control.md":    {Data: []byte("Control")},
	})
	if err != nil || len(b.Resources) != 1 || b.Resources[0].Name() != "control" || len(b.Diagnostics) != 2 {
		t.Fatalf("ambiguous identity: err=%v bundle=%#v", err, b)
	}
	for _, diagnostic := range b.Diagnostics {
		if diagnostic.Code != "ingest.opencode.duplicate-identity" {
			t.Fatalf("diagnostic: %#v", diagnostic)
		}
	}
}

// spec: harness-opencode/native-fields-survive-a-supported-round-trip
// spec: harness-opencode/ordered-and-empty-agent-policies-retain-their-meaning
func TestNativeRoundTrip(t *testing.T) {
	for _, permission := range []string{"{}", "\n  bash:\n    '*': deny\n    'git *': allow\n  edit: ask"} {
		src := fstest.MapFS{
			"commands/check.md":  {Data: []byte("---\ndescription: Check\nagent: reviewer\nmodel: provider/model\nsubtask: true\n---\nCheck $ARGUMENTS and $1.\n")},
			"agents/reviewer.md": {Data: []byte("---\ndescription: Review\nmode: subagent\nmodel: provider/model\npermission: " + permission + "\n---\nReview\n")},
			"opencode.jsonc": {Data: []byte(`{ // native settings
 "mcp": {"remote":{"type":"remote","url":"https://example.invalid","headers":{"Authorization":"Bearer {env:TOKEN}"},"enabled":false,"oauth":false,"timeout":5000}}
}`)},
		}
		b, err := LoadBundle(src)
		if err != nil || len(b.Resources) != 3 || len(b.Diagnostics) != 0 {
			t.Fatalf("load: err=%v bundle=%#v", err, b)
		}
		out := fstest.MapFS{}
		for _, r := range b.Resources {
			a, d, err := (compiler.Compiler{}).Translate(r)
			if err != nil || len(d) != 0 || len(a) != 1 {
				t.Fatalf("compile %s: %v %v", r.Name(), d, err)
			}
			out[a[0].Path] = &fstest.MapFile{Data: a[0].Bytes}
		}
		agent := string(out["agents/reviewer.md"].Data)
		if permission == "{}" && !strings.Contains(agent, "permission: {}") {
			t.Fatalf("empty policy lost: %s", agent)
		}
		if permission != "{}" && strings.Index(agent, "'*': deny") > strings.Index(agent, "git *: allow") {
			t.Fatalf("permission precedence lost: %s", agent)
		}
		if !strings.Contains(string(out["commands/check.md"].Data), "$ARGUMENTS and $1") {
			t.Fatal("command placeholders changed")
		}
		if !strings.Contains(string(out["opencode.json"].Data), "Bearer {env:TOKEN}") {
			t.Fatal("native reference changed")
		}
		second, err := LoadBundle(out)
		if err != nil || len(second.Resources) != 3 || len(second.Diagnostics) != 0 {
			t.Fatalf("round-trip reload: %v %#v", err, second)
		}
	}
}

// spec: harness-opencode/ambiguous-execution-fields-are-rejected-in-both-orders
func TestAmbiguousFields(t *testing.T) {
	for _, data := range []string{
		"---\nmodel: provider/a\nModel: provider/b\n---\nBody",
		"---\nModel: provider/b\nmodel: provider/a\n---\nBody",
		"---\npermission: {}\npermissions: {bash: allow}\n---\nBody",
		"---\ntools: {bash: true, Bash: false}\n---\nBody", // dictionaries are case sensitive; unknown tools are refused
	} {
		b, err := LoadBundle(fstest.MapFS{"agents/reviewer.md": {Data: []byte(data)}, "commands/control.md": {Data: []byte("Control")}})
		if err != nil || len(b.Resources) != 1 || b.Resources[0].Name() != "control" || len(b.Diagnostics) != 1 || b.Diagnostics[0].Code != "ingest.opencode.invalid-resource" {
			t.Fatalf("ambiguous fields: %v %#v", err, b)
		}
	}
}

// spec: harness-opencode/read-limits-apply-at-the-actual-consumer-boundary
func TestNativeReadLimit(t *testing.T) {
	b, err := LoadBundle(fstest.MapFS{"commands/large.md": {Data: []byte(strings.Repeat("x", maxResourceBytes+1))}, "commands/control.md": {Data: []byte("Control")}})
	if err != nil || len(b.Resources) != 1 || len(b.Diagnostics) != 1 || b.Diagnostics[0].Code != "ingest.opencode.read-refused" {
		t.Fatalf("read refusal: %v %#v", err, b)
	}
}

func TestCommentOnlyFrontmatter(t *testing.T) {
	bundle, err := LoadBundle(fstest.MapFS{"agents/reviewer.md": {Data: []byte("---\n# comment only\n---\nReview")}})
	if err != nil || len(bundle.Resources) != 1 || len(bundle.Diagnostics) != 0 {
		t.Fatalf("comment header: %v %#v", err, bundle)
	}
}

func TestMergedPermissionIsRefused(t *testing.T) {
	bundle, err := LoadBundle(fstest.MapFS{
		"agents/reviewer.md":  {Data: []byte("---\npermission: &policy {bash: deny}\n<<: {permission: *policy}\n---\nReview")},
		"commands/control.md": {Data: []byte("Control")},
	})
	if err != nil || len(bundle.Resources) != 1 || bundle.Resources[0].Name() != "control" || len(bundle.Diagnostics) != 1 {
		t.Fatalf("merged policy: %v %#v", err, bundle)
	}
}

func TestInlineResourcesDoNotLoseOverrides(t *testing.T) {
	bundle, err := LoadBundle(fstest.MapFS{
		"opencode.json":       {Data: []byte(`{"agent":{"reviewer":{"permission":{"bash":"deny"}},"inline-only":{}},"command":{"check":{"template":"Override"}}}`)},
		"agents/reviewer.md":  {Data: []byte("Review")},
		"commands/check.md":   {Data: []byte("Check")},
		"commands/control.md": {Data: []byte("Control")},
	})
	if err != nil || len(bundle.Resources) != 1 || bundle.Resources[0].Name() != "control" || len(bundle.Diagnostics) != 2 {
		t.Fatalf("inline source: %v %#v", err, bundle)
	}
}

func TestHookDeclarationNativeJSONAndMatcherTypes(t *testing.T) {
	valid := `{"format":"agent-marketplace/opencode-hook-v1","name":"check","event":"PreToolUse","matcher":"Bash","command":"true","timeout":1}`
	for _, tc := range []struct {
		data string
		want int
	}{
		{valid, 1},
		{strings.Replace(valid, `"matcher":"Bash"`, `"matcher":null`, 1), 0},
		{strings.Replace(valid, `{`, `{ /* comment */ `, 1), 0},
		{strings.TrimSuffix(valid, "}") + ",}", 0},
	} {
		bundle, err := LoadBundle(fstest.MapFS{
			"plugins/agent-marketplace.js":       {Data: opencodehook.Adapter},
			"hooks/agent-marketplace/check.json": {Data: []byte(tc.data)},
			"commands/control.md":                {Data: []byte("Control")},
		})
		if err != nil || len(bundle.Resources) != tc.want+1 || len(bundle.Diagnostics) != 1-tc.want {
			t.Fatalf("declaration: %v %#v", err, bundle)
		}
	}
}
