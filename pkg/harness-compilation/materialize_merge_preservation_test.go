package harnesscompile

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/testutil"
)

// mergeArtifact builds the one merge artifact these tests materialize.
func mergeArtifact(path, desired string) *Result {
	return &Result{
		Artifacts: []Artifact{{
			Path:              path,
			Bytes:             []byte(desired),
			MergeWithExisting: true,
			ManagedByField:    "_managed_by",
			ManagedByValue:    "agent-marketplace",
		}},
	}
}

// assertNoStagingResidue fails when Materialize left a staging directory behind.
func assertNoStagingResidue(t *testing.T, outDir string) {
	t.Helper()
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read %s: %v", outDir, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatalf("staging directory %q left behind in %s", entry.Name(), outDir)
		}
	}
}

// TestMaterialize_MergePreservesUserBytesOnDisk is the end-to-end form of bead
// agent-marketplace-5exj.26: an ordinary sync that merges into an existing
// aggregate must leave the user's comments, key order, and handwritten spacing
// on DISK, for the real aggregate file of every one of the five marker-merge
// targets. Before the fix each of these files came back re-serialized by
// json.MarshalIndent: comments gone, keys sorted.
func TestMaterialize_MergePreservesUserBytesOnDisk(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		existing string
		desired  string
		// keep lists substrings of the user's own bytes that must survive
		// verbatim, including the comments and the deliberate odd spacing.
		keep []string
	}{
		{
			name: "claude .mcp.json",
			file: ".mcp.json",
			existing: "{\n" +
				"  // my servers\n" +
				"  \"mcpServers\": {\n" +
				"    \"zzz\": {\"command\":\"z\",   \"env\": {\"TOKEN\": \"" + testutil.FakeSecret + "\"}},\n" +
				"    \"aaa\": {\"command\": \"a\"}\n" +
				"  }\n" +
				"}\n",
			desired: `{"mcpServers":{"gen":{"command":"g"}}}`,
			keep: []string{
				"// my servers",
				"\"zzz\": {\"command\":\"z\",   \"env\": {\"TOKEN\": \"" + testutil.FakeSecret + "\"}}",
				"\"aaa\": {\"command\": \"a\"}",
			},
		},
		{
			name: "cursor mcp.json",
			file: "mcp.json",
			existing: "{\n" +
				"  \"mcpServers\": {\n" +
				"    /* keep me exactly */\n" +
				"    \"user\": {\"type\":\"stdio\",\"command\": \"u\"}\n" +
				"  }\n" +
				"}\n",
			desired: `{"mcpServers":{"gen":{"command":"g"}}}`,
			keep:    []string{"/* keep me exactly */", "\"user\": {\"type\":\"stdio\",\"command\": \"u\"}"},
		},
		{
			name: "windsurf mcp_config.json",
			file: "mcp_config.json",
			existing: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"docs\": {\"serverUrl\": \"https://example.invalid\"}\n" +
				"  },\n" +
				"  // a key windsurf does not know about, kept verbatim\n" +
				"  \"myOwnKey\": [1, 2, 3]\n" +
				"}\n",
			desired: `{"mcpServers":{"gen":{"command":"g"}}}`,
			keep: []string{
				"// a key windsurf does not know about, kept verbatim",
				"\"myOwnKey\": [1, 2, 3]",
			},
		},
		{
			name: "factory plugin.json array container",
			file: "plugin.json",
			existing: "{\n" +
				"  \"name\": \"mine\",\n" +
				"  \"skills\": [\n" +
				"    // handwritten\n" +
				"    {\"name\": \"user-skill\",  \"description\": \"keep\"}\n" +
				"  ]\n" +
				"}\n",
			desired: `{"skills":[{"name":"gen-skill"}]}`,
			keep: []string{
				"// handwritten",
				"{\"name\": \"user-skill\",  \"description\": \"keep\"}",
				"\"name\": \"mine\"",
			},
		},
		{
			name: "gemini gemini-extension.json",
			file: "gemini-extension.json",
			existing: "{\n" +
				"  \"name\": \"demo\",\n" +
				"  /* pinned on purpose */\n" +
				"  \"version\": \"0.1.0\"\n" +
				"}\n",
			desired: `{"mcpServers":{"gen":{"command":"g"}}}`,
			keep:    []string{"/* pinned on purpose */", "\"version\": \"0.1.0\"", "\"name\": \"demo\""},
		},
		{
			name: "factory settings.json hooks map of arrays",
			file: "settings.json",
			existing: "{\n" +
				"  \"hooks\": {\n" +
				"    \"PostToolUse\": [\n" +
				"      // mine, runs first\n" +
				"      {\"matcher\": \"Write\",   \"command\": \"user\"}\n" +
				"    ]\n" +
				"  },\n" +
				"  \"editor\": {\"theme\": \"dark\"}\n" +
				"}\n",
			desired: `{"hooks":{"PostToolUse":[{"matcher":"Edit","command":"gen"}]}}`,
			keep: []string{
				"// mine, runs first",
				"{\"matcher\": \"Write\",   \"command\": \"user\"}",
				"\"editor\": {\"theme\": \"dark\"}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			path := filepath.Join(outDir, tt.file)
			if err := os.WriteFile(path, []byte(tt.existing), 0o644); err != nil {
				t.Fatal(err)
			}

			report, err := Materialize(mergeArtifact(tt.file, tt.desired), outDir, WithWriteMode(WriteModeOverwrite))
			if err != nil {
				t.Fatalf("Materialize: %v", err)
			}
			if len(report.Written) != 1 {
				t.Fatalf("Written = %v, want the merged file", report.Written)
			}
			assertNoStagingResidue(t, outDir)

			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			for _, want := range tt.keep {
				if !strings.Contains(string(onDisk), want) {
					t.Fatalf("the user's bytes %q did not survive the merge:\n%s", want, onDisk)
				}
			}
			// Our entry really was merged in: the test is not vacuously passing
			// because nothing was written.
			if !strings.Contains(string(onDisk), "_managed_by") {
				t.Fatalf("no managed entry was merged in:\n%s", onDisk)
			}

			// A second identical sync must be a no-op: the merge result is a
			// fixed point, so Materialize skips the write entirely.
			second, err := Materialize(mergeArtifact(tt.file, tt.desired), outDir, WithWriteMode(WriteModeOverwrite))
			if err != nil {
				t.Fatalf("second Materialize: %v", err)
			}
			if len(second.Written) != 0 {
				t.Fatalf("second sync rewrote %v, want no write", second.Written)
			}
			again, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if !bytes.Equal(onDisk, again) {
				t.Fatalf("second sync mutated the file:\n%q\nvs\n%q", onDisk, again)
			}
			assertNoStagingResidue(t, outDir)
		})
	}
}

// TestMaterialize_MergeIntoMissingDestinationIsConventional proves a first-ever
// write still produces conventionally formatted JSON, which a
// preservation-focused patcher can easily get wrong.
func TestMaterialize_MergeIntoMissingDestinationIsConventional(t *testing.T) {
	for _, tt := range []struct {
		name     string
		existing *string
		want     string
	}{
		{
			name: "no destination",
			want: "{\n  \"mcpServers\": {\n    \"gen\": {\n      \"_managed_by\": \"agent-marketplace\",\n      \"command\": \"g\"\n    }\n  }\n}\n",
		},
		{
			name:     "empty destination",
			existing: strPtr(""),
			want:     "{\n  \"mcpServers\": {\n    \"gen\": {\n      \"_managed_by\": \"agent-marketplace\",\n      \"command\": \"g\"\n    }\n  }\n}\n",
		},
		{
			name:     "destination with an empty root object",
			existing: strPtr("{}\n"),
			want:     "{\n  \"mcpServers\": {\n    \"gen\": {\n      \"_managed_by\": \"agent-marketplace\",\n      \"command\": \"g\"\n    }\n  }\n}\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			path := filepath.Join(outDir, "mcp.json")
			if tt.existing != nil {
				if err := os.WriteFile(path, []byte(*tt.existing), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Materialize(mergeArtifact("mcp.json", `{"mcpServers":{"gen":{"command":"g"}}}`), outDir,
				WithWriteMode(WriteModeOverwrite)); err != nil {
				t.Fatalf("Materialize: %v", err)
			}
			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(onDisk) != tt.want {
				t.Fatalf("on-disk bytes =\n%q\nwant\n%q", onDisk, tt.want)
			}
			assertNoStagingResidue(t, outDir)
		})
	}
}

func strPtr(s string) *string { return &s }

// TestMaterialize_MergeRefusalLeavesEveryTargetUntouched extends the
// single-file atomicity proof in materialize_atomicity_test.go to the real
// aggregate file of all five marker-merge targets: a duplicate key anywhere in
// the destination is refused before staging, the file is byte-identical
// afterwards, no staging directory survives, and the duplicated value is never
// echoed.
func TestMaterialize_MergeRefusalLeavesEveryTargetUntouched(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		existing string
		desired  string
		keyPath  string
		key      string
	}{
		{
			name:     "claude .mcp.json headers",
			file:     ".mcp.json",
			existing: "{\n  \"mcpServers\": {\n    \"u\": {\"headers\": {\"Authorization\": \"" + testutil.FakeSecret + "\", \"Authorization\": \"" + testutil.FakeSecret + "\"}}\n  }\n}\n",
			desired:  `{"mcpServers":{"gen":{"command":"g"}}}`,
			keyPath:  "$.mcpServers.u.headers",
			key:      "Authorization",
		},
		{
			name:     "cursor mcp.json env",
			file:     "mcp.json",
			existing: "{\n  \"mcpServers\": {\n    \"u\": {\"env\": {\"TOKEN\": \"" + testutil.FakeSecret + "\", \"TOKEN\": \"x\"}}\n  }\n}\n",
			desired:  `{"mcpServers":{"gen":{"command":"g"}}}`,
			keyPath:  "$.mcpServers.u.env",
			key:      "TOKEN",
		},
		{
			name:     "windsurf mcp_config.json server key",
			file:     "mcp_config.json",
			existing: "{\n  \"mcpServers\": {\n    \"docs\": {\"command\": \"safe\"},\n    \"docs\": {\"command\": \"rm -rf /\"}\n  }\n}\n",
			desired:  `{"mcpServers":{"gen":{"command":"g"}}}`,
			keyPath:  "$.mcpServers",
			key:      "docs",
		},
		{
			name:     "factory plugin.json array element",
			file:     "plugin.json",
			existing: "{\n  \"skills\": [\n    {\"name\": \"a\", \"name\": \"b\"}\n  ]\n}\n",
			desired:  `{"skills":[{"name":"gen"}]}`,
			keyPath:  "$.skills[0]",
			key:      "name",
		},
		{
			name:     "gemini gemini-extension.json top level",
			file:     "gemini-extension.json",
			existing: "{\n  \"version\": \"1\",\n  \"version\": \"2\"\n}\n",
			desired:  `{"mcpServers":{"gen":{"command":"g"}}}`,
			keyPath:  "$",
			key:      "version",
		},
		{
			name:     "factory settings.json hooks",
			file:     "settings.json",
			existing: "{\n  \"hooks\": {\"PostToolUse\": [{\"command\": \"a\"}]},\n  \"hooks\": {}\n}\n",
			desired:  `{"hooks":{"PostToolUse":[{"command":"gen"}]}}`,
			keyPath:  "$",
			key:      "hooks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			path := filepath.Join(outDir, tt.file)
			original := []byte(tt.existing)
			if err := os.WriteFile(path, original, 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := Materialize(mergeArtifact(tt.file, tt.desired), outDir, WithWriteMode(WriteModeOverwrite))
			testutil.AssertDuplicateKeyDiagnostic(t, err, "", tt.keyPath, tt.key)
			if err == nil {
				t.Fatalf("Materialize accepted a destination with a duplicate %q key", tt.key)
			}
			if !strings.Contains(err.Error(), tt.file) {
				t.Fatalf("error %q does not name the destination file", err)
			}

			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read back: %v", readErr)
			}
			if !bytes.Equal(after, original) {
				t.Fatalf("destination mutated:\nbefore: %q\nafter:  %q", original, after)
			}
			assertNoStagingResidue(t, outDir)
			entries, readErr := os.ReadDir(outDir)
			if readErr != nil {
				t.Fatalf("read outDir: %v", readErr)
			}
			if len(entries) != 1 {
				t.Fatalf("outDir entries = %d, want only the destination file", len(entries))
			}
		})
	}
}
