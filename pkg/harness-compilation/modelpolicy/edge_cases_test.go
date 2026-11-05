package modelpolicy

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestCatalogVariantOnlyAndAmbiguousPortableNames(t *testing.T) {
	const data = `schema: 1
harness: devin
evidence:
  checked: "2026-09-21"
  refs: [synthetic-test-fixture]
surfaces:
  subagent:
    model_field: model
    omitted_model: default
models:
  example/variant:
    reasoning:
      medium: {model: "native:variant@medium"}
  first/duplicate:
    model: native-first
  second/duplicate:
    model: native-second
`
	r, err := loadCatalogs(fstest.MapFS{"devin.yaml": {Data: []byte(data)}}, "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		model, effort, want string
		losses              []LossKind
	}{
		{"variant", "medium", "native:variant@medium", nil},
		{"variant", "", "", []LossKind{ModelOmitted}},
		{"variant", "max", "", []LossKind{EffortOmitted, ModelOmitted}},
		{"duplicate", "", "", []LossKind{ModelOmitted}},
		{"first/duplicate", "", "native-first", nil},
		{"second/duplicate", "", "native-second", nil},
	} {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			result := r.resolve(Request{Target: target.TargetDevin, Model: tc.model, Effort: tc.effort})
			var losses []LossKind
			for _, loss := range result.Losses {
				losses = append(losses, loss.Kind)
			}
			if result.Model != tc.want || result.Effort != "" || !slices.Equal(losses, tc.losses) {
				t.Fatalf("result=%+v want model=%q losses=%v", result, tc.want, tc.losses)
			}
		})
	}
}
