package harnesscompile

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func payloadRoundTrip(t *testing.T, resource ir.Resource) ([]byte, *Result) {
	t.Helper()
	canonical, err := artifact.FromIR(resource)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := artifact.DecodeResource(resource.Kind().String(), canonical.Payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Compile(decoded, TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	return canonical.Payload, result
}

// spec: harness-compilation/decoded-resources-compile-without-a-spurious-extra-field
func TestDecodedResourcesCompileWithoutSpuriousExtraField(t *testing.T) {
	for _, resource := range []ir.Resource{
		ir.NewSkill("plain", "Plain", "Body.\n", nil, nil, ir.SourceRef{}),
		ir.NewCommand("plain", "Plain", "Body.\n", nil, ir.SourceRef{}),
		ir.NewSubagent("plain", "Plain", "Body.\n", "", nil, "", nil, "", nil, ir.SourceRef{}),
		ir.NewHook("plain", "PostToolUse", "inert-hook", "command", nil, ir.SourceRef{}),
	} {
		t.Run(resource.Kind().String(), func(t *testing.T) {
			payload, result := payloadRoundTrip(t, resource)
			if bytes.Contains(payload, []byte(`"Extra"`)) {
				t.Fatalf("payload carries the zeroed Extra member: %s", payload)
			}
			if len(result.Artifacts) == 0 {
				t.Fatalf("nothing compiled: %+v", result.Diagnostics)
			}
			for _, a := range result.Artifacts {
				if bytes.Contains(a.Bytes, []byte("Extra")) {
					t.Fatalf("%s carries a spurious Extra field:\n%s", a.Path, a.Bytes)
				}
			}
		})
	}
}

// spec: harness-compilation/re-encoding-an-earlier-payload-drops-the-null-extra-member
func TestReEncodingEarlierPayloadDropsNullExtra(t *testing.T) {
	hook := ir.NewHook("guard", "PreToolUse", "inert-hook", "command", nil, ir.SourceRef{})
	direct, err := artifact.FromIR(hook)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(direct.Payload, &fields); err != nil {
		t.Fatal(err)
	}
	fields["Extra"] = nil
	earlier, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := artifact.DecodeResource("hook", earlier)
	if err != nil {
		t.Fatal(err)
	}
	if value, kept := decoded.(*ir.Hook).Extra["Extra"]; !kept || value != nil {
		t.Fatalf("decoding an earlier payload changed: %#v", decoded.(*ir.Hook).Extra)
	}
	reencoded, err := artifact.FromIR(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded.Payload, direct.Payload) || bytes.Contains(reencoded.Payload, []byte(`"Extra"`)) {
		t.Fatalf("re-encoded earlier payload differs:\n got %s\nwant %s", reencoded.Payload, direct.Payload)
	}

	authored := ir.NewSkill("authored", "Authored", "Body.\n", nil, map[string]any{"Extra": "kept"}, ir.SourceRef{})
	_, kept := payloadRoundTrip(t, authored)
	var out strings.Builder
	for _, a := range kept.Artifacts {
		out.Write(a.Bytes)
	}
	if !strings.Contains(out.String(), "Extra: kept") {
		t.Fatalf("authored Extra extension field was lost:\n%s", out.String())
	}
}
