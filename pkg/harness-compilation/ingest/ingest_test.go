package ingest_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"

	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/claude"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/cursor"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/devin"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/factory"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/gemini"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/windsurf"
)

func TestFor_ClaudeRegistered(t *testing.T) {
	_, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester should be registered")
	}
}

func TestFor_FactoryRegistered(t *testing.T) {
	_, ok := ingest.For(ingest.HarnessFactory)
	if !ok {
		t.Fatal("Factory ingester should be registered")
	}
}

func TestFor_CursorRegistered(t *testing.T) {
	_, ok := ingest.For(ingest.HarnessCursor)
	if !ok {
		t.Fatal("Cursor ingester should be registered")
	}
}

func TestFor_UnregisteredHarness(t *testing.T) {
	_, ok := ingest.For("nonexistent")
	if ok {
		t.Fatal("expected no ingester for nonexistent harness")
	}
}

func TestAutodetect_ClaudePluginForm(t *testing.T) {
	src := fstest.MapFS{
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\n---\nBody"),
		},
	}
	h, ok := ingest.Autodetect(src)
	if !ok {
		t.Fatal("Autodetect should detect Claude plugin form")
	}
	if h != ingest.HarnessClaude {
		t.Fatalf("Autodetect = %q, want %q", h, ingest.HarnessClaude)
	}
}

func TestAutodetect_ClaudeWorkspaceForm(t *testing.T) {
	src := fstest.MapFS{
		".claude/skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\n---\nBody"),
		},
	}
	h, ok := ingest.Autodetect(src)
	if !ok {
		t.Fatal("Autodetect should detect Claude workspace form")
	}
	if h != ingest.HarnessClaude {
		t.Fatalf("Autodetect = %q, want %q", h, ingest.HarnessClaude)
	}
}

func TestAutodetect_FactoryPluginForm(t *testing.T) {
	src := fstest.MapFS{
		".factory-plugin/plugin.json": &fstest.MapFile{
			Data: []byte(`{"skills":[{"name":"my-skill","description":""}]}`),
		},
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("Body"),
		},
	}
	h, ok := ingest.Autodetect(src)
	if !ok {
		t.Fatal("Autodetect should detect Factory plugin form")
	}
	if h != ingest.HarnessFactory {
		t.Fatalf("Autodetect = %q, want %q", h, ingest.HarnessFactory)
	}
}

func TestAutodetect_FactoryWorkspaceForm(t *testing.T) {
	src := fstest.MapFS{
		".factory/skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("Body"),
		},
	}
	h, ok := ingest.Autodetect(src)
	if !ok {
		t.Fatal("Autodetect should detect Factory workspace form")
	}
	if h != ingest.HarnessFactory {
		t.Fatalf("Autodetect = %q, want %q", h, ingest.HarnessFactory)
	}
}

func TestAutodetect_CursorPluginForm(t *testing.T) {
	src := fstest.MapFS{
		".cursor-plugin/plugin.json": &fstest.MapFile{
			Data: []byte(`{"name":"cursor-plugin"}`),
		},
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\n---\nBody"),
		},
	}
	h, ok := ingest.Autodetect(src)
	if !ok {
		t.Fatal("Autodetect should detect Cursor plugin form")
	}
	if h != ingest.HarnessCursor {
		t.Fatalf("Autodetect = %q, want %q", h, ingest.HarnessCursor)
	}
}

func TestAutodetect_CursorWorkspaceForm(t *testing.T) {
	src := fstest.MapFS{
		".cursor/skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\n---\nBody"),
		},
	}
	h, ok := ingest.Autodetect(src)
	if !ok {
		t.Fatal("Autodetect should detect Cursor workspace form")
	}
	if h != ingest.HarnessCursor {
		t.Fatalf("Autodetect = %q, want %q", h, ingest.HarnessCursor)
	}
}

func TestAutodetect_NoMatch(t *testing.T) {
	src := fstest.MapFS{}
	_, ok := ingest.Autodetect(src)
	if ok {
		t.Fatal("Autodetect should not detect anything in empty FS")
	}
}

func TestRegister_PanicOnDuplicate(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	ingest.Register(&duplicateIngester{})
}

// duplicateIngester is a test ingester that claims to handle Claude.
type duplicateIngester struct{}

func (d *duplicateIngester) Source() ingest.Harness                       { return ingest.HarnessClaude }
func (d *duplicateIngester) Detect(src fs.FS) bool                        { return false }
func (d *duplicateIngester) LoadBundle(src fs.FS) (*ingest.Bundle, error) { return nil, nil }
