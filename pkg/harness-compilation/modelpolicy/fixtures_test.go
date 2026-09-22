package modelpolicy

import (
	"bytes"
	"os"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"gopkg.in/yaml.v3"
)

func TestCatalogConversions(t *testing.T) {
	raw, err := os.ReadFile("testdata/conversions.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name       string        `yaml:"name"`
		Source     target.Target `yaml:"source"`
		Target     target.Target `yaml:"target"`
		Model      string        `yaml:"model"`
		Effort     string        `yaml:"effort"`
		WantModel  string        `yaml:"want_model"`
		WantEffort string        `yaml:"want_effort"`
		Losses     []LossKind    `yaml:"losses"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("catalog conversion fixtures are empty")
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		if tc.Name == "" || seen[tc.Name] {
			t.Fatalf("invalid or duplicate fixture name %q", tc.Name)
		}
		seen[tc.Name] = true
		t.Run(tc.Name, func(t *testing.T) {
			result, err := Resolve(Request{Source: tc.Source, Target: tc.Target, Model: tc.Model, Effort: tc.Effort})
			if err != nil {
				t.Fatal(err)
			}
			var losses []LossKind
			for _, loss := range result.Losses {
				losses = append(losses, loss.Kind)
			}
			if result.Model != tc.WantModel || result.Effort != tc.WantEffort || !slices.Equal(losses, tc.Losses) {
				t.Fatalf("got model=%q effort=%q losses=%v; want model=%q effort=%q losses=%v", result.Model, result.Effort, losses, tc.WantModel, tc.WantEffort, tc.Losses)
			}
		})
	}
}
