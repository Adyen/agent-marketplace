package harnesscompile

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: mcp-publishing-metadata-native-fence/canonical-restoration-retains-content-while-native-output-excludes-metadata
func TestMcpPublishingCanonicalRestorationRetainsNamespace(t *testing.T) {
	server := ir.NewMcpServer("docs", "npx", []string{"-y", "docs-mcp"}, nil, map[string]any{
		mcpFenceNamespace: mcpFenceValidMetadata(),
	}, ir.SourceRef{})

	built, err := artifact.FromIR(server)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	if !bytes.Contains(built.Payload, []byte(mcpFenceNamespace)) || !bytes.Contains(built.Payload, []byte(mcpFenceDocMarker)) {
		t.Fatalf("canonical payload dropped the publishing namespace: %s", built.Payload)
	}
	raw, digest, err := artifact.Encode(built)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, decodedRaw, err := artifact.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(decoded.Payload, built.Payload) || decoded.Manifest.ContentDigest != built.Manifest.ContentDigest {
		t.Fatal("decoded canonical payload or content digest changed")
	}
	if err := decoded.VerifyContentDigest(); err != nil {
		t.Fatalf("content digest: %v", err)
	}
	if err := decoded.VerifyArchiveDigest(decodedRaw); err != nil {
		t.Fatalf("archive digest: %v", err)
	}
	reencoded, reencodedDigest, err := artifact.Encode(decoded)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if reencodedDigest != digest || !bytes.Equal(reencoded, raw) {
		t.Fatal("re-encoding a decoded namespace-bearing artifact changed payload bytes or digest")
	}

	resource, err := artifact.DecodeResource(decoded.Manifest.Kind, decoded.Payload)
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	restored, ok := resource.(*ir.McpServer)
	if !ok {
		t.Fatalf("decoded resource kind = %T, want *ir.McpServer", resource)
	}
	metadata, ok := restored.Extra[mcpFenceNamespace].(map[string]any)
	if !ok || metadata["readme_content"] != "# Docs\n"+mcpFenceDocMarker+"\n" {
		t.Fatalf("decoded resource lost the publishing namespace: %#v", restored.Extra)
	}

	for _, target := range []Target{TargetClaude, TargetDevin} {
		for _, scope := range []Scope{ScopeProject, ScopeUser} {
			result, err := Compile(restored, target, WithScope(scope))
			if err != nil || len(result.Artifacts) == 0 {
				t.Fatalf("Compile restored resource for %s/%v: %v %+v", target, scope, err, result)
			}
			for _, art := range result.Artifacts {
				lower := bytes.ToLower(art.Bytes)
				if bytes.Contains(lower, []byte(mcpFenceNamespace)) || bytes.Contains(art.Bytes, []byte(mcpFenceDocMarker)) {
					t.Fatalf("restored publishing metadata leaked into %s artifact %q:\n%s", target, art.Path, art.Bytes)
				}
				if !strings.Contains(string(art.Bytes), "npx") {
					t.Fatalf("restored connection dropped for %s artifact %q:\n%s", target, art.Path, art.Bytes)
				}
			}
		}
	}
	if metadata["readme_content"] != "# Docs\n"+mcpFenceDocMarker+"\n" {
		t.Fatalf("native compilation mutated the decoded publishing metadata: %#v", restored.Extra)
	}
}

// spec: mcp-publishing-metadata-native-fence/historical-identity-and-writer-defaults-remain-unchanged
func TestMcpPublishingMetadataFreeCanonicalPayloadFrozen(t *testing.T) {
	const (
		wantPayload = `{"Args":["-y","docs-mcp"],"Command":"npx","Disabled":false,"DisabledSet":false,"Env":null,"Ext":{},"Extra":null,"Headers":null,"OAuthClientID":"","OAuthClientSecret":"","OAuthResource":"","Source":{"File":"","Line":0},"Transport":"","URL":"","name":"docs"}`
		wantDigest  = "sha256:dfc3e731c23dd023f940856e8dca2e7babd70f9f0fd6568c3033ec6e75bba03f"
	)
	free, err := artifact.FromIR(ir.NewMcpServer("docs", "npx", []string{"-y", "docs-mcp"}, nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR metadata-free server: %v", err)
	}
	if string(free.Payload) != wantPayload {
		t.Fatalf("metadata-free canonical payload changed:\ngot:  %s\nwant: %s", free.Payload, wantPayload)
	}
	if free.Manifest.ContentDigest != wantDigest {
		t.Fatalf("metadata-free content digest changed: got %s, want %s", free.Manifest.ContentDigest, wantDigest)
	}
	if bytes.Contains(free.Payload, []byte(mcpFenceNamespace)) {
		t.Fatalf("metadata-free payload gained the publishing namespace: %s", free.Payload)
	}
}
