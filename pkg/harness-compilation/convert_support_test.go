package harnesscompile

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// artifactByPath returns the artifact emitted for path, or nil.
func artifactByPath(artifacts []Artifact, path string) *Artifact {
	for i := range artifacts {
		if artifacts[i].Path == path {
			return &artifacts[i]
		}
	}
	return nil
}

func diagnosticCodes(result *Result) []string {
	var codes []string
	for _, d := range result.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}

// A declared skill support file tree must reach the compiled target resource
// root with exact bytes and executable mode when the source fs.FS is available.
func TestConvertAssemblesSkillSupportFiles(t *testing.T) {
	blob := []byte{0x00, 0x01, 0x02, 0xff, 0x7f}
	src := fstest.MapFS{
		"skills/demo/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: demo\ndescription: demo skill\n---\nBody"),
		},
		"skills/demo/scripts/run.sh": &fstest.MapFile{
			Data: []byte("#!/bin/sh\necho hi\n"),
			Mode: 0o755,
		},
		"skills/demo/assets/blob.bin": &fstest.MapFile{
			Data: blob,
			Mode: 0o644,
		},
	}

	result, err := Convert(src, HarnessClaude, HarnessClaude)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	script := artifactByPath(result.Artifacts, "skills/demo/scripts/run.sh")
	if script == nil {
		t.Fatalf("missing support artifact skills/demo/scripts/run.sh; artifacts: %v", result.Artifacts)
	}
	if !bytes.Equal(script.Bytes, []byte("#!/bin/sh\necho hi\n")) {
		t.Errorf("script bytes mismatch: %q", script.Bytes)
	}
	if script.Mode.Perm() != 0o755 {
		t.Errorf("script mode = %o, want 0755", script.Mode.Perm())
	}

	asset := artifactByPath(result.Artifacts, "skills/demo/assets/blob.bin")
	if asset == nil {
		t.Fatalf("missing support artifact skills/demo/assets/blob.bin")
	}
	if !bytes.Equal(asset.Bytes, blob) {
		t.Errorf("binary asset bytes mismatch")
	}

	for _, code := range diagnosticCodes(result) {
		if code == diag.CodeSkillSupportFilesDeferred {
			t.Errorf("support files were assembled but %q still reported", code)
		}
	}
}

// A hook command that references a bundled script through the canonical root
// token must carry that asset into the converted bundle, and token-equivalent
// targets must render their own spelling.
func TestConvertAssemblesReferencedHookAsset(t *testing.T) {
	src := fstest.MapFS{
		"hooks/hooks.json": &fstest.MapFile{
			Data: []byte(`{"hooks":{"Stop":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/check.sh"}]}}`),
		},
		"hooks/check.sh": &fstest.MapFile{
			Data: []byte("#!/bin/sh\nexit 0\n"),
			Mode: 0o755,
		},
	}

	result, err := Convert(src, HarnessClaude, HarnessClaude)
	if err != nil {
		t.Fatalf("Convert claude: %v", err)
	}
	asset := artifactByPath(result.Artifacts, "hooks/check.sh")
	if asset == nil {
		t.Fatalf("missing referenced hook asset hooks/check.sh")
	}
	if asset.Mode.Perm() != 0o755 {
		t.Errorf("hook asset mode = %o, want 0755", asset.Mode.Perm())
	}

	factory, err := Convert(src, HarnessClaude, HarnessFactory)
	if err != nil {
		t.Fatalf("Convert factory: %v", err)
	}
	if artifactByPath(factory.Artifacts, "hooks/check.sh") == nil {
		t.Fatalf("factory conversion dropped referenced asset hooks/check.sh")
	}
	hooksJSON := artifactByPath(factory.Artifacts, "hooks/hooks.json")
	if hooksJSON == nil {
		for _, a := range factory.Artifacts {
			if strings.Contains(a.Path, "hook") {
				hooksJSON = &a
			}
		}
	}
	if hooksJSON == nil {
		t.Fatalf("factory conversion emitted no hook artifact")
	}
	if !strings.Contains(string(hooksJSON.Bytes), "${DROID_PLUGIN_ROOT}/hooks/check.sh") {
		t.Errorf("factory hook command did not render native token: %s", hooksJSON.Bytes)
	}
}

// A referenced asset that does not exist in the source must fail closed: the
// owning resource loses all of its output and a stable error is reported.
func TestConvertMissingReferencedAssetFailsClosed(t *testing.T) {
	src := fstest.MapFS{
		"hooks/hooks.json": &fstest.MapFile{
			Data: []byte(`{"hooks":{"Stop":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/absent.sh"}]}}`),
		},
	}

	result, err := Convert(src, HarnessClaude, HarnessClaude)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !sliceContains(diagnosticCodes(result), diag.CodeBundleSupportFileMissing) {
		t.Errorf("missing %q diagnostic; codes: %v", diag.CodeBundleSupportFileMissing, diagnosticCodes(result))
	}
	if artifactByPath(result.Artifacts, "hooks/absent.sh") != nil {
		t.Errorf("absent referenced asset was materialized")
	}
	if !result.HasErrors() {
		t.Errorf("expected error-bearing result for missing referenced asset")
	}
}

// Root references that escape the bundle root must fail closed instead of
// reading outside the source.
func TestConvertUnsafeReferencedAssetFailsClosed(t *testing.T) {
	src := fstest.MapFS{
		"hooks/hooks.json": &fstest.MapFile{
			Data: []byte(`{"hooks":{"Stop":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/../outside.sh"}]}}`),
		},
		"hooks/inner.sh": &fstest.MapFile{Data: []byte("x")},
	}

	result, err := Convert(src, HarnessClaude, HarnessClaude)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !sliceContains(diagnosticCodes(result), diag.CodeBundleSupportFileUnsafe) {
		t.Errorf("missing %q diagnostic; codes: %v", diag.CodeBundleSupportFileUnsafe, diagnosticCodes(result))
	}
	for _, a := range result.Artifacts {
		if strings.Contains(a.Path, "..") {
			t.Errorf("artifact escaped target root: %q", a.Path)
		}
	}

	// A symlinked support path component is refused even when the reference
	// itself is bundle-relative.
	linkSrc := fstest.MapFS{
		"hooks/hooks.json": &fstest.MapFile{
			Data: []byte(`{"hooks":{"Stop":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/evil.sh"}]}}`),
		},
		"hooks/evil.sh": &fstest.MapFile{
			Data: []byte("/outside/target.sh"),
			Mode: fs.ModeSymlink,
		},
	}
	linkResult, err := Convert(linkSrc, HarnessClaude, HarnessClaude)
	if err != nil {
		t.Fatalf("Convert symlink: %v", err)
	}
	if !sliceContains(diagnosticCodes(linkResult), diag.CodeBundleSupportFileUnsafe) {
		t.Errorf("symlinked support file was not refused; codes: %v", diagnosticCodes(linkResult))
	}
}

// An IR skill that declares an unclean support-file name must fail closed
// rather than producing a target path outside the compiled resource root.
func TestAssembleSkillSupportRejectsUncleanRel(t *testing.T) {
	skill := ir.NewSkill("demo", "d", "body", nil, nil, ir.SourceRef{File: "skills/demo/SKILL.md"})
	skill.SupportFiles = []string{"../escape.sh"}
	src := fstest.MapFS{
		"skills/demo/SKILL.md":     &fstest.MapFile{Data: []byte("x")},
		"skills/demo/scripts/s.sh": &fstest.MapFile{Data: []byte("y")},
	}
	compiled := []Artifact{{Path: "skills/demo/SKILL.md", Bytes: []byte("x")}}
	support, diags := assembleResourceSupportArtifacts(src, skill, compiled, TargetClaude)
	if len(support) != 0 {
		t.Fatalf("unclean support rel produced artifacts: %v", support)
	}
	if len(diags) == 0 || diags[0].Code != diag.CodeBundleSupportFileUnsafe {
		t.Fatalf("expected %q diagnostic, got %v", diag.CodeBundleSupportFileUnsafe, diags)
	}
}

// A support-file artifact that collides with a compiled artifact path must
// fail closed through the existing duplicate-path contract rather than
// overwriting compiled output.
func TestConvertSupportFileCollisionFailsClosed(t *testing.T) {
	src := fstest.MapFS{
		"skills/demo/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: demo\ndescription: demo skill\n---\nBody"),
		},
		"hooks/hooks.json": &fstest.MapFile{
			Data: []byte(`{"hooks":{"Stop":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/skills/demo/SKILL.md"}]}}`),
		},
	}

	result, err := Convert(src, HarnessClaude, HarnessClaude)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !result.HasErrors() {
		t.Fatalf("expected collision to fail closed; artifacts: %v", result.Artifacts)
	}
	codes := diagnosticCodes(result)
	if !sliceContains(codes, diag.CodeBundleConflictDuplicateArtifactPath) &&
		!sliceContains(codes, diag.CodeBundleConflictCrossKindArtifactPath) {
		t.Errorf("missing artifact path conflict diagnostic; codes: %v", codes)
	}
}

// Factory and Cursor MCP entries written with their native plugin-root token
// must ingest to the canonical IR spelling so referenced assets ride along
// and token-equivalent destinations render the canonical reference.
func TestConvertNormalizesNativeMcpRootTokens(t *testing.T) {
	cases := []struct {
		name    string
		source  Harness
		token   string
		mcpPath string
	}{
		{"factory", HarnessFactory, "${DROID_PLUGIN_ROOT}", ".factory/mcp.json"},
		{"cursor", HarnessCursor, "${CURSOR_PLUGIN_ROOT}", ".cursor/mcp.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := `{"mcpServers":{` +
				`"demo":{"command":"` + tc.token + `/bin/run.sh",` +
				`"args":["--env","` + tc.token + `/config/.env"],` +
				`"env":{"CFG":"` + tc.token + `/config/.env"}},` +
				`"remote":{"url":"https://example.com/` + tc.token + `/sse",` +
				`"headers":{"X-Root":"` + tc.token + `/tok"}}` +
				`}}`
			src := fstest.MapFS{
				tc.mcpPath:    &fstest.MapFile{Data: []byte(entry)},
				"bin/run.sh":  &fstest.MapFile{Data: []byte("#!/bin/sh\nexit 0\n"), Mode: 0o755},
				"config/.env": &fstest.MapFile{Data: []byte("TOKEN=1\n")},
			}

			result, err := Convert(src, tc.source, HarnessClaude)
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			for _, rel := range []string{"bin/run.sh", "config/.env"} {
				if artifactByPath(result.Artifacts, rel) == nil {
					t.Errorf("missing referenced MCP asset %s; artifacts: %v", rel, result.Artifacts)
				}
			}
			mcpJSON := artifactByPath(result.Artifacts, ".mcp.json")
			if mcpJSON == nil {
				t.Fatalf("conversion emitted no .mcp.json artifact")
			}
			if !strings.Contains(string(mcpJSON.Bytes), "${CLAUDE_PLUGIN_ROOT}/config/.env") {
				t.Errorf("MCP env did not carry canonical root token: %s", mcpJSON.Bytes)
			}
			if strings.Contains(string(mcpJSON.Bytes), tc.token) {
				t.Errorf("native token %s leaked into converted output: %s", tc.token, mcpJSON.Bytes)
			}
		})
	}
}

// Literal-only destinations must keep a stable diagnostic and must never
// embed a caller-supplied author-machine path in emitted bytes.
func TestConvertLiteralTargetDoesNotEmbedSourcePath(t *testing.T) {
	src := fstest.MapFS{
		"hooks/hooks.json": &fstest.MapFile{
			Data: []byte(`{"hooks":{"Stop":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh"}]}}`),
		},
		"hooks/run.sh": &fstest.MapFile{
			Data: []byte("#!/bin/sh\nexit 0\n"),
			Mode: 0o755,
		},
	}

	result, err := Convert(src, HarnessClaude, HarnessWindsurf)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	for _, a := range result.Artifacts {
		if strings.Contains(string(a.Bytes), "/home/author") || strings.Contains(string(a.Bytes), "/tmp/") {
			t.Errorf("artifact %q embeds an author-machine path: %s", a.Path, a.Bytes)
		}
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == diag.CodeHookWindsurfEnvTokenExpanded {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %q diagnostic for literal-only target; codes: %v",
			diag.CodeHookWindsurfEnvTokenExpanded, diagnosticCodes(result))
	}
}
