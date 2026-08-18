package codex

import (
	"testing"
	"testing/fstest"

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
}
