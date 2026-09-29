package harnesscompile

import (
	"bytes"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

// spec: harness-model-catalogs/authoritative-fields-override-stale-legacy-sidecars
func TestModelPolicyArchivePreservesAuthoritativeNativeSelection(t *testing.T) {
	r := ir.NewSubagent("review", "Review", "Keep this body.\n", "gpt-5.6-luna", nil, "medium", nil, "", nil, ir.SourceRef{})
	r.ModelSource = "codex"
	r.Ext.Codex = &ext.Codex{Model: "stale-model", ReasoningEffort: "stale-effort"}
	archive, err := artifact.FromIR(r)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := artifact.DecodeResource("subagent", archive.Payload)
	if err != nil {
		t.Fatal(err)
	}
	s := decoded.(*ir.Subagent)
	if s.ModelSource != "codex" || s.Model != "gpt-5.6-luna" || s.Effort != "medium" {
		t.Fatalf("archive lost selection: %+v", s)
	}
	s.Model, s.Effort = "gpt-5.6-terra", "high"
	result, err := Compile(s, TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	fields := modelPolicyFields(t, result.Artifacts)
	if fields["model"] != "gpt-5.6-terra" || fields["model_reasoning_effort"] != "high" {
		t.Fatalf("stale sidecar overrode edited selection: %v", fields)
	}
}

// spec: harness-model-catalogs/legacy-archives-remain-byte-stable-and-resolvable
func TestModelPolicyLegacyArchiveRemainsUnchanged(t *testing.T) {
	r := ir.NewSubagent("review", "Review", "Keep this body.\n", "sonnet", nil, "", nil, "", nil, ir.SourceRef{})
	before, err := artifact.FromIR(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(before.Payload, []byte("ModelSource")) {
		t.Fatal("legacy payload gained provenance")
	}
	decoded, err := artifact.DecodeResource("subagent", before.Payload)
	if err != nil {
		t.Fatal(err)
	}
	after, err := artifact.FromIR(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Payload, after.Payload) {
		t.Fatal("legacy archive changed")
	}
	result, err := Compile(decoded, TargetDevin)
	if err != nil {
		t.Fatal(err)
	}
	if modelPolicyFields(t, result.Artifacts)["model"] != "sonnet" {
		t.Fatal("legacy alias no longer resolves")
	}
}

// spec: harness-model-catalogs/extras-cannot-forge-provenance-or-resurrect-dropped-fields
func TestModelPolicyArchiveExtrasCannotForgeProvenance(t *testing.T) {
	for _, source := range []string{"", "factory"} {
		for _, key := range []string{"ModelSource", "modelsource", "MODELSOURCE"} {
			t.Run(source+"/"+key, func(t *testing.T) {
				for _, kind := range []string{"skill", "subagent"} {
					extra := map[string]any{key: "devin", "x-kept": true}
					var resource ir.Resource
					if kind == "skill" {
						skill := ir.NewSkill("review", "Review", "Body", nil, extra, ir.SourceRef{})
						skill.ModelSource = source
						resource = skill
					} else {
						agent := ir.NewSubagent("review", "Review", "Body", "", nil, "", nil, "", extra, ir.SourceRef{})
						agent.ModelSource = source
						resource = agent
					}
					encoded, err := artifact.FromIR(resource)
					if err != nil {
						t.Fatal(err)
					}
					decoded, err := artifact.DecodeResource(kind, encoded.Payload)
					if err != nil {
						t.Fatalf("producer emitted ambiguous %s provenance: %v", kind, err)
					}
					var got string
					switch value := decoded.(type) {
					case *ir.Skill:
						got = value.ModelSource
					case *ir.Subagent:
						got = value.ModelSource
					}
					if got != source || !bytes.Contains(encoded.Payload, []byte(`"x-kept":true`)) || extra[key] != "devin" {
						t.Fatalf("provenance forged or unrelated input changed: %s", encoded.Payload)
					}
				}
			})
		}
	}
}

// spec: harness-model-catalogs/ambiguous-or-malformed-provenance-is-rejected
func TestModelPolicyArchiveRejectsAmbiguousProvenance(t *testing.T) {
	for _, kind := range []string{"skill", "subagent"} {
		for _, model := range []string{"", "sonnet"} {
			t.Run(kind+"/null/"+model, func(t *testing.T) {
				var payloads [][]byte
				for _, provenance := range []string{"", `,"ModelSource":null`} {
					resource, err := artifact.DecodeResource(kind, []byte(`{"name":"review","Model":"`+model+`"`+provenance+`}`))
					if err != nil {
						t.Fatal(err)
					}
					encoded, err := artifact.FromIR(resource)
					if err != nil {
						t.Fatal(err)
					}
					payloads = append(payloads, encoded.Payload)
					result, err := Compile(resource, TargetDevin)
					if err != nil {
						t.Fatal(err)
					}
					if len(result.Artifacts) == 0 {
						t.Fatal("resource was suppressed")
					}
					foundModel := false
					for _, a := range result.Artifacts {
						foundModel = foundModel || bytes.Contains(a.Bytes, []byte("model: sonnet\n"))
						if model == "" && bytes.Contains(a.Bytes, []byte("model:")) {
							t.Fatalf("omitted model gained a selection: %s", a.Bytes)
						}
					}
					if model != "" && !foundModel {
						t.Fatal("explicit model selection was lost")
					}
				}
				if !bytes.Equal(payloads[0], payloads[1]) {
					t.Fatal("null provenance differs from absent provenance")
				}
			})
		}
	}
	for _, payload := range []string{
		`{"name":"review","ModelSource":"codex","modelsource":"devin"}`,
		`{"name":"review","ModelSource":true}`,
	} {
		if _, err := artifact.DecodeResource("subagent", []byte(payload)); err == nil {
			t.Fatalf("accepted malformed provenance: %s", payload)
		}
	}
}
