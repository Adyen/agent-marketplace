package codex

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestDetectRequiresCodexMarkerAndLoadsSiblingSkills(t *testing.T) {
	sharedOnly := fstest.MapFS{".agents/skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nBody\n")}}
	if Detect(sharedOnly) {
		t.Fatal("shared .agents/skills selected Codex without .codex marker")
	}
	src := fstest.MapFS{
		".codex/agents/reviewer.toml":    {Data: []byte("name = \"reviewer\"\ndescription = \"Review\"\nmodel = \"gpt-5.6-terra\"\nmodel_reasoning_effort = \"high\"\ndeveloper_instructions = \"Be exact.\"\n")},
		".agents/skills/demo/SKILL.md":   {Data: []byte("---\nname: demo\ndescription: Demo\n---\nBody\n")},
		".agents/skills/deploy/SKILL.md": {Data: []byte("---\nname: deploy\ndescription: Deploy\nx-agent-marketplace-kind: command\n---\nDeploy.\n")},
	}
	if harness, ok := ingest.Autodetect(src); !ok || harness != ingest.HarnessCodex {
		t.Fatalf("Autodetect = %q, %v", harness, ok)
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 3 {
		t.Fatalf("resources = %#v", bundle.Resources)
	}
	want := []ir.ResourceKind{ir.KindSkill, ir.KindCommand, ir.KindSubagent}
	for i, resource := range bundle.Resources {
		if resource.Kind() != want[i] {
			t.Errorf("resource %d kind = %s, want %s", i, resource.Kind(), want[i])
		}
	}
}

func TestLoadHooksMCPAndDeprecatedPrompts(t *testing.T) {
	src := fstest.MapFS{
		".codex/hooks.json":        {Data: []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./check.sh"}]}]}}`)},
		".codex/config.toml":       {Data: []byte("[mcp_servers.docs]\ncommand = \"npx\"\nargs = [\"-y\", \"docs-mcp\"]\n[mcp_servers.docs.env]\nTOKEN = \"${TOKEN}\"\n")},
		".codex/prompts/legacy.md": {Data: []byte("Legacy command.\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []ir.ResourceKind{ir.KindHook, ir.KindMcpServer, ir.KindCommand}
	if len(bundle.Resources) != len(want) {
		t.Fatalf("resources = %#v", bundle.Resources)
	}
	for i, resource := range bundle.Resources {
		if resource.Kind() != want[i] {
			t.Errorf("resource %d kind = %s, want %s", i, resource.Kind(), want[i])
		}
	}
	if len(bundle.Diagnostics) == 0 {
		t.Fatal("deprecated prompt did not produce a diagnostic")
	}
}

func TestMalformedResourceDoesNotDiscardIndependentResources(t *testing.T) {
	src := fstest.MapFS{
		".codex/agents/bad.toml":       {Data: []byte("name = [")},
		".codex/agents/good.toml":      {Data: []byte("name = 'good'\ndeveloper_instructions = 'Good.'\n")},
		".agents/skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nBody\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 2 {
		t.Fatalf("resources = %#v, want valid skill and subagent", bundle.Resources)
	}
	if !diag.HasErrors(bundle.Diagnostics) {
		t.Fatalf("diagnostics = %#v, want malformed-resource error", bundle.Diagnostics)
	}
}

func TestDuplicateIdentityIsRejectedWithoutChoosingWinner(t *testing.T) {
	src := fstest.MapFS{
		".codex/agents/a.toml": {Data: []byte("name = 'same'\ndeveloper_instructions = 'A'\n")},
		".codex/agents/b.toml": {Data: []byte("name = 'same'\ndeveloper_instructions = 'B'\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 0 {
		t.Fatalf("resources = %#v, want both duplicates suppressed", bundle.Resources)
	}
	if !diag.HasErrors(bundle.Diagnostics) {
		t.Fatalf("diagnostics = %#v", bundle.Diagnostics)
	}
}
