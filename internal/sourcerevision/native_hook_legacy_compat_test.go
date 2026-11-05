package sourcerevision

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// legacyCaptureFixtures are the exact parent-authored BASE fixture inputs the
// frozen hook/tool vectors were captured from. Do not edit: the vectors in
// testdata/frozen-hook-tool-*.json are byte/digest-exact oracles over these
// bytes.
var legacyCaptureFixtures = []struct {
	source string
	files  map[string]string
}{
	{"claude", map[string]string{
		".claude-plugin/plugin.json": `{"name":"legacy-hooks"}`,
		"hooks/hooks.json":           `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo legacy-claude"}]}]}}`,
		"hooks/check.sh":             "#!/bin/sh\necho legacy-support\n",
		"agents/review.md":           "---\nname: review\ndescription: Legacy review\ntools: [Read, Bash]\n---\nReview the change.\n",
	}},
	{"factory", map[string]string{
		".factory/hooks/hooks.json": `{"PreToolUse":[{"matcher":"Execute","commandRegex":"^git ","hooks":[{"type":"command","command":"echo legacy-factory"}]}]}`,
		".factory/hooks/check.sh":   "#!/bin/sh\necho legacy-support\n",
		".factory/droids/review.md": "---\nname: review\ndescription: Legacy review\ntools: [Read, Execute]\n---\nReview the change.\n",
	}},
	{"cursor", map[string]string{
		".cursor-plugin/plugin.json": `{"name":"legacy-hooks"}`,
		"hooks/hooks.json":           `{"hooks":{"PreToolUse":[{"matcher":"Bash","type":"command","command":"echo legacy-cursor"}]}}`,
		"hooks/check.sh":             "#!/bin/sh\necho legacy-support\n",
		"agents/review.md":           "---\nname: review\ndescription: Legacy review\ntools: [Read, Shell]\n---\nReview the change.\n",
	}},
	{"gemini", map[string]string{
		".gemini/settings.json":    `{"hooks":{"BeforeTool":[{"matcher":"run_shell_command","hooks":[{"type":"command","command":"echo legacy-gemini","timeout":2500}]}]}}`,
		".gemini/check.sh":         "#!/bin/sh\necho legacy-support\n",
		".gemini/agents/review.md": "---\nname: review\ndescription: Legacy review\nkind: local\ntools: [read_file, run_shell_command]\n---\nReview the change.\n",
	}},
	{"devin", map[string]string{
		".devin/hooks.v1.json":    `{"PostCompaction":[{"type":"command","command":"echo legacy-devin","timeout":3}]}`,
		".devin/check.sh":         "#!/bin/sh\necho legacy-support\n",
		".devin/agents/review.md": "---\nname: review\ndescription: Legacy review\nallowed-tools: [read, exec]\n---\nReview the change.\n",
	}},
	{"codex", map[string]string{
		".codex/hooks.json":         `{"hooks":{"PreToolUse":[{"matcher":"apply_patch","hooks":[{"type":"command","command":"echo legacy-codex","timeout":3}]}]}}`,
		".codex/check.sh":           "#!/bin/sh\necho legacy-support\n",
		".codex/agents/review.toml": "name = \"review\"\ndescription = \"Legacy review\"\ndeveloper_instructions = \"Review the change.\"\n",
	}},
	{"windsurf", map[string]string{
		".windsurf/hooks.json": `{"hooks":{"pre_run_command":[{"command":"echo legacy-windsurf"}]}}`,
		".windsurf/check.sh":   "#!/bin/sh\necho legacy-support\n",
	}},
}

func writeLegacyFixture(t *testing.T, fixture struct {
	source string
	files  map[string]string
}) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range fixture.files {
		writeFixture(t, filepath.Join(root, filepath.FromSlash(rel)), content)
	}
	return root
}

func loadFrozenVectors(t *testing.T, name string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var vectors map[string]json.RawMessage
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 28 {
		t.Fatalf("%s holds %d vectors, want 28", name, len(vectors))
	}
	return vectors
}

// TestFrozenLegacyHookVectors pins every shipped source-revision generator to
// the manifest bytes the BASE revision produced for identical historical
// sources (task 4.2). The vectors are parent-frozen; a mismatch is a digest
// compatibility regression, never a reason to regenerate the oracle.
func TestFrozenLegacyHookVectors(t *testing.T) {
	frozen := loadFrozenVectors(t, "frozen-hook-tool-source-revisions-v1-v4.json")
	for _, fixture := range legacyCaptureFixtures {
		for _, schema := range []string{"v1", "v2", "v3", "v4"} {
			key := fixture.source + "/" + schema
			t.Run(key, func(t *testing.T) {
				want, ok := frozen[key]
				if !ok {
					t.Fatalf("frozen vector %s missing", key)
				}
				var prior []byte
				var built Result
				for repeat := 0; repeat < 2; repeat++ {
					root := writeLegacyFixture(t, fixture)
					result, err := Build(Input{SnapshotRoot: root, SourceHarness: fixture.source, RevisionSchema: schema})
					if err != nil {
						t.Fatalf("%s: %v", key, err)
					}
					if len(result.Warnings) != 0 {
						t.Fatalf("%s unexpected omitted resources: %v", key, result.Warnings)
					}
					hookCount := 0
					for _, resource := range result.Manifest.Resources {
						if resource.Kind == "hook" {
							hookCount++
						}
					}
					if hookCount != 1 {
						t.Fatalf("%s hook count %d, want exactly one", key, hookCount)
					}
					if prior != nil && !bytes.Equal(prior, result.Payload) {
						t.Fatalf("%s payload depends on temporary root\nfirst: %s\nsecond: %s", key, prior, result.Payload)
					}
					prior = append([]byte(nil), result.Payload...)
					built = result
				}
				var frozenManifest sourceRevisionManifest
				if err := json.Unmarshal(want, &frozenManifest); err != nil {
					t.Fatalf("%s frozen manifest decode: %v", key, err)
				}
				if err := verifyManifest(built.Manifest, prior); err != nil {
					t.Fatalf("%s payload is not its own canonical manifest: %v", key, err)
				}
				if !reflect.DeepEqual(built.Manifest, frozenManifest) {
					t.Fatalf("%s manifest differs from the frozen BASE vector\ngot:  %s\nwant: %s", key, prior, want)
				}
			})
		}
	}
}

// TestFrozenLegacyHookFallbacks pins the IR-serialization fallback digests:
// when a catalog entry has no readable source file the digest must still be
// computed over the historical (BASE) projection of the resource, not the
// runtime provenance fields.
func TestFrozenLegacyHookFallbacks(t *testing.T) {
	type frozenFallback struct {
		Kind    string          `json:"kind"`
		Name    string          `json:"name"`
		Payload json.RawMessage `json:"payload"`
		Digest  string          `json:"digest"`
	}
	data, err := os.ReadFile(filepath.Join("testdata", "frozen-hook-tool-fallbacks-v1-v4.json"))
	if err != nil {
		t.Fatal(err)
	}
	var frozen map[string][]frozenFallback
	if err := json.Unmarshal(data, &frozen); err != nil {
		t.Fatal(err)
	}
	if len(frozen) != 28 {
		t.Fatalf("fallback vectors hold %d keys, want 28", len(frozen))
	}
	for _, fixture := range legacyCaptureFixtures {
		root := writeLegacyFixture(t, fixture)
		sourceFS, err := fsboundary.OpenSourceFS(root)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := sourceFS.Close(); err != nil {
				t.Errorf("close legacy source snapshot: %v", err)
			}
		}()
		for _, schema := range []string{"v1", "v2", "v3", "v4"} {
			contract, err := lockfile.RevisionContract(schema)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := catalog.LoadWithOptions(root, fixture.source, catalog.IngestOptions{LegacyWindsurfHooks: !contract.AdditiveWindsurfHooks, LegacyBareMCP: !contract.BareBundleMCP})
			if err != nil {
				t.Fatalf("%s/%s load: %v", fixture.source, schema, err)
			}
			key := fixture.source + "/" + schema
			t.Run(key, func(t *testing.T) {
				want, ok := frozen[key]
				if !ok {
					t.Fatalf("frozen fallback %s missing", key)
				}
				var got []frozenFallback
				for _, entry := range loaded.Entries {
					entry.Path = ""
					payload, err := resourceSourceBytes(root, sourceFS, entry, contract)
					if err != nil {
						t.Fatalf("%s: %v", key, err)
					}
					if bytes.Contains(payload, []byte(root)) {
						t.Fatalf("%s fallback payload contains temporary root", key)
					}
					got = append(got, frozenFallback{Kind: entry.Kind.String(), Name: entry.Name, Payload: payload, Digest: contentDigest(payload)})
				}
				if len(got) != len(want) {
					t.Fatalf("%s fallback count %d, want %d\ngot:  %v\nwant: %v", key, len(got), len(want), got, want)
				}
				for i := range want {
					if got[i].Kind != want[i].Kind || got[i].Name != want[i].Name || got[i].Digest != want[i].Digest {
						t.Errorf("%s fallback[%d] differs\ngot:  %s/%s %s\nwant: %s/%s %s", key, i, got[i].Kind, got[i].Name, got[i].Digest, want[i].Kind, want[i].Name, want[i].Digest)
						continue
					}
					var gotPayload, wantPayload any
					if err := json.Unmarshal(got[i].Payload, &gotPayload); err != nil {
						t.Fatalf("%s payload[%d] decode: %v", key, i, err)
					}
					if err := json.Unmarshal(want[i].Payload, &wantPayload); err != nil {
						t.Fatalf("%s frozen payload[%d] decode: %v", key, i, err)
					}
					if !reflect.DeepEqual(gotPayload, wantPayload) {
						t.Errorf("%s fallback[%d] payload differs\ngot:  %s\nwant: %s", key, i, got[i].Payload, want[i].Payload)
					}
				}
			})
			// The projection is digest-only: catalog entries keep their
			// runtime provenance after the fallback is computed.
			for _, entry := range loaded.Entries {
				hook, ok := entry.Resource.(*ir.Hook)
				if !ok {
					continue
				}
				switch fixture.source {
				case "gemini":
					if hook.HookSource != "gemini" || hook.Event != "BeforeTool" {
						t.Fatalf("%s runtime hook mutated by digest projection: %+v", key, hook)
					}
				case "windsurf":
					if hook.HookSource != "windsurf" || hook.Event != "pre_run_command" {
						t.Fatalf("%s runtime hook mutated by digest projection: %+v", key, hook)
					}
				case "codex":
					if hook.Ext.Codex == nil || !hook.Ext.Codex.NativeHook || hook.HookSource != "codex" {
						t.Fatalf("%s runtime hook mutated by digest projection: %+v", key, hook)
					}
				case "devin":
					if hook.HookSource != "devin" || hook.Event != "PostCompaction" {
						t.Fatalf("%s runtime hook mutated by digest projection: %+v", key, hook)
					}
				}
			}
		}
	}
}
