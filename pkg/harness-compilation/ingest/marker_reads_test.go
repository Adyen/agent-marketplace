package ingest_test

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
)

// readCountingFS counts the content bytes read through it, so tests can tell
// a presence check from a read.
type readCountingFS struct {
	fs.FS
	bytes *int64
}

type readCountingFile struct {
	fs.File
	bytes *int64
}

func (c readCountingFile) Read(p []byte) (int, error) {
	n, err := c.File.Read(p)
	*c.bytes += int64(n)
	return n, err
}

func (c readCountingFile) ReadDir(n int) ([]fs.DirEntry, error) {
	return c.File.(fs.ReadDirFile).ReadDir(n)
}

func (c readCountingFS) Open(name string) (fs.File, error) {
	file, err := c.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return readCountingFile{File: file, bytes: c.bytes}, nil
}

func (c readCountingFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(c.FS, name) }

func TestAutodetectChecksMarkersWithoutReadingThem(t *testing.T) {
	skill := []byte("---\nname: demo\ndescription: d\n---\nbody\n")
	for _, tc := range []struct {
		name string
		src  fstest.MapFS
		want ingest.Harness
	}{
		{"claude plugin manifest", fstest.MapFS{".claude-plugin/plugin.json": {Data: []byte(`{"name":"p"}`)}, "skills/demo/SKILL.md": {Data: skill}}, ingest.HarnessClaude},
		{"claude bare mcp", fstest.MapFS{".mcp.json": {Data: []byte(`{"mcpServers":{}}`)}}, ingest.HarnessClaude},
		{"claude single skill", fstest.MapFS{"SKILL.md": {Data: skill}}, ingest.HarnessClaude},
		{"claude skills directory", fstest.MapFS{"demo/SKILL.md": {Data: skill}}, ingest.HarnessClaude},
		{"claude workspace mcp", fstest.MapFS{".claude/.mcp.json": {Data: []byte(`{"mcpServers":{}}`)}}, ingest.HarnessClaude},
		{"factory plugin manifest", fstest.MapFS{".factory-plugin/plugin.json": {Data: []byte(`{"name":"p"}`)}, "skills/demo/SKILL.md": {Data: skill}}, ingest.HarnessFactory},
		{"factory workspace hooks", fstest.MapFS{".factory/hooks.json": {Data: []byte(`{"hooks":{}}`)}}, ingest.HarnessFactory},
		{"factory workspace mcp", fstest.MapFS{".factory/mcp.json": {Data: []byte(`{"mcpServers":{}}`)}}, ingest.HarnessFactory},
		{"factory bare mcp", fstest.MapFS{"mcp.json": {Data: []byte(`{"mcpServers":{}}`)}}, ingest.HarnessFactory},
		{"cursor plugin manifest", fstest.MapFS{".cursor-plugin/plugin.json": {Data: []byte(`{"name":"p"}`)}, "skills/demo/SKILL.md": {Data: skill}}, ingest.HarnessCursor},
		{"cursor workspace mcp", fstest.MapFS{".cursor/mcp.json": {Data: []byte(`{"mcpServers":{}}`)}}, ingest.HarnessCursor},
		{"cursor workspace hooks", fstest.MapFS{".cursor/hooks.json": {Data: []byte(`{"version":1,"hooks":{}}`)}}, ingest.HarnessCursor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var read int64
			got, ok := ingest.Autodetect(readCountingFS{FS: tc.src, bytes: &read})
			if !ok || got != tc.want {
				t.Fatalf("Autodetect = %q, %t; want %q", got, ok, tc.want)
			}
			if read != 0 {
				t.Fatalf("autodetection read %d content bytes; marker checks must not read content", read)
			}
		})
	}
}

func TestAutodetectIgnoresMarkerPathsThatAreNotRegularFiles(t *testing.T) {
	control := fstest.MapFS{
		".claude-plugin/plugin.json":  {Data: []byte(`{"name":"p"}`)},
		"skills/demo/SKILL.md":        {Data: []byte("---\nname: demo\ndescription: d\n---\nbody\n")},
		".factory-plugin/plugin.json": {Data: []byte(`{"name":"p"}`)},
	}
	if got, ok := ingest.Autodetect(control); !ok || got != ingest.HarnessClaude {
		t.Fatalf("Autodetect = %q, %t; want a regular Claude manifest to win over the Factory manifest", got, ok)
	}
	src := fstest.MapFS{
		".claude-plugin/plugin.json/nested": {Data: []byte("x")},
		"skills/demo/SKILL.md":              {Data: []byte("---\nname: demo\ndescription: d\n---\nbody\n")},
		".factory-plugin/plugin.json":       {Data: []byte(`{"name":"p"}`)},
	}
	if got, ok := ingest.Autodetect(src); !ok || got != ingest.HarnessFactory {
		t.Fatalf("Autodetect = %q, %t; want the factory manifest, not a directory named like the claude manifest", got, ok)
	}
}

func TestClaudeBareSkillsDirectoryReadsEachSkillOnce(t *testing.T) {
	src := fstest.MapFS{}
	var total int64
	for i := range 20 {
		data := []byte(fmt.Sprintf("---\nname: skill-%02d\ndescription: d\n---\n%s\n", i, strings.Repeat("x", 4096)))
		src[fmt.Sprintf("skill-%02d/SKILL.md", i)] = &fstest.MapFile{Data: data}
		total += int64(len(data))
	}
	var read int64
	counting := readCountingFS{FS: src, bytes: &read}
	harness, ok := ingest.Autodetect(counting)
	if !ok || harness != ingest.HarnessClaude {
		t.Fatalf("Autodetect = %q, %t; want claude", harness, ok)
	}
	ingester, _ := ingest.For(harness)
	bundle, err := ingester.LoadBundle(counting)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 20 {
		t.Fatalf("resources = %d, want 20", len(bundle.Resources))
	}
	if read != total {
		t.Fatalf("detection and loading read %d bytes, want each SKILL.md read once (%d bytes)", read, total)
	}
}
