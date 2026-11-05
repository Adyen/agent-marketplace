package artifact_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"pgregory.net/rapid"
)

func dedupeFragment(body string, dedupe bool) artifact.Artifact {
	return artifact.Artifact{Path: "hooks.json", Bytes: []byte(body), Fragment: true, FragmentKey: "hooks", DedupeFragmentEntries: dedupe, Mode: 0o640}
}

func TestFragmentDedupePreservesReplacementSemantics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bodies []string
		want   string
		dedupe bool
	}{
		{"key-order-and-entry-order", []string{`{"E":[{"type":"command","command":"one"}]}`, `{"E":[{"command":"one","type":"command"},{"command":"two","type":"command"}]}`}, `{"E":[{"command":"one","type":"command"},{"command":"two","type":"command"}]}`, true},
		{"initial-duplicates", []string{`{"E":[{"v":1},{"v":1}]}`, `{"E":[{"v":1},{"v":2},{"v":2}]}`}, `{"E":[{"v":1},{"v":1},{"v":2}]}`, true},
		{"empty-array-reset", []string{`{"E":[{"v":1}]}`, `{"E":[{"v":2}]}`, `{"E":[]}`, `{"E":[{"v":1}]}`, `{"E":[{"v":2}]}`}, `{"E":[{"v":1},{"v":2}]}`, true},
		{"scalar-array-reset", []string{`{"E":[{"v":1}]}`, `{"E":[{"v":2}]}`, `{"E":[0]}`, `{"E":[{"v":1}]}`, `{"E":[{"v":2}]}`}, `{"E":[{"v":1},{"v":2}]}`, true},
		{"object-reset", []string{`{"E":[{"v":1}]}`, `{"E":[{"v":2}]}`, `{"E":{"nested":[{"v":3}]}}`, `{"E":[{"v":1}]}`, `{"E":[{"v":2}]}`}, `{"E":[{"v":1},{"v":2}]}`, true},
		{"ancestor-reset", []string{`{"root":{"E":[{"v":1}]}}`, `{"root":{"E":[{"v":2}]}}`, `{"root":null}`, `{"root":{"E":[{"v":1}]}}`, `{"root":{"E":[{"v":2}]}}`}, `{"root":{"E":[{"v":1},{"v":2}]}}`, true},
		{"distinct-paths", []string{`{"root/E":[{"v":1}],"root":{"E":[{"v":2}]}}`, `{"root/E":[{"v":2}],"root":{"E":[{"v":1}]}}`}, `{"root":{"E":[{"v":2},{"v":1}]},"root/E":[{"v":1},{"v":2}]}`, true},
		{"nested-array-order", []string{`{"E":[{"args":[1,2]}]}`, `{"E":[{"args":[2,1]},{"args":[1,2]}]}`}, `{"E":[{"args":[1,2]},{"args":[2,1]}]}`, true},
		{"numeric-encoding", []string{`{"E":[{"v":1},{"v":-0}]}`, `{"E":[{"v":1.0},{"v":0}]}`}, `{"E":[{"v":1},{"v":-0},{"v":0}]}`, true},
		{"opt-in-only", []string{`{"E":[{"v":1}]}`, `{"E":[{"v":1}]}`}, `{"E":[{"v":1},{"v":1}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input []artifact.Artifact
			for _, body := range tc.bodies {
				input = append(input, dedupeFragment(body, false))
			}
			input[len(input)-1].DedupeFragmentEntries = tc.dedupe
			var want bytes.Buffer
			if err := json.Indent(&want, []byte(tc.want), "", "  "); err != nil {
				t.Fatal(err)
			}
			expected := []artifact.Artifact{{Path: "hooks.json", Bytes: want.Bytes(), Mode: 0o640, DedupeFragmentEntries: tc.dedupe}}
			for range 2 {
				got, err := artifact.MergeFragments(input)
				if err != nil || !reflect.DeepEqual(got, expected) {
					t.Fatalf("merged = %+v, error=%v; want bytes=%s", got, err, want.Bytes())
				}
			}
		})
	}
}

// Retain the original pairwise JSON equality and merge contract independently
// of the candidate's per-group indexing.
func restartMergeOracle(dst, src map[string]any, dedupe bool) {
	objects := func(values []any) bool {
		if len(values) == 0 {
			return false
		}
		for _, value := range values {
			if _, ok := value.(map[string]any); !ok {
				return false
			}
		}
		return true
	}
	for key, value := range src {
		dm, dmOK := dst[key].(map[string]any)
		sm, smOK := value.(map[string]any)
		if dmOK && smOK {
			restartMergeOracle(dm, sm, dedupe)
			continue
		}
		da, daOK := dst[key].([]any)
		sa, saOK := value.([]any)
		if daOK && saOK && objects(da) && objects(sa) {
			for _, entry := range sa {
				duplicate := false
				if dedupe {
					for _, existing := range da {
						left, leftErr := json.Marshal(existing)
						right, rightErr := json.Marshal(entry)
						if leftErr == nil && rightErr == nil && bytes.Equal(left, right) {
							duplicate = true
							break
						}
					}
				}
				if !duplicate {
					da = append(da, entry)
				}
			}
			dst[key] = da
			continue
		}
		dst[key] = value
	}
}

func TestFragmentDedupeMatchesRestartOracle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dedupe := rapid.Bool().Draw(t, "dedupe")
		var input []artifact.Artifact
		merged := map[string]any{}
		steps := rapid.IntRange(4, 24).Draw(t, "steps")
		for step := 0; step < steps; step++ {
			value := any([]any{map[string]any{"v": 1}, map[string]any{"v": 1}, map[string]any{"v": 2}})
			switch rapid.IntRange(0, 5).Draw(t, "shape") {
			case 1:
				value = []any{}
			case 2:
				value = []any{float64(1), "x"}
			case 3:
				value = nil
			case 4:
				value = map[string]any{"E": []any{map[string]any{"v": rapid.IntRange(1, 3).Draw(t, "nested")}}}
			case 5:
				value = []any{map[string]any{"v": rapid.IntRange(1, 3).Draw(t, "entry")}}
			}
			key := rapid.SampledFrom([]string{"E", "root/E", "root"}).Draw(t, "key")
			doc := map[string]any{key: value}
			if rapid.Bool().Draw(t, "nested-path") {
				doc = map[string]any{"root": doc}
			}
			// Force an accumulation before generated replacements/other paths.
			if step < 2 {
				doc = map[string]any{"root": map[string]any{"E": []any{map[string]any{"v": step + 1}}}}
			}
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			var normalized map[string]any
			if err := json.Unmarshal(raw, &normalized); err != nil {
				t.Fatal(err)
			}
			restartMergeOracle(merged, normalized, dedupe)
			input = append(input, dedupeFragment(string(raw), false))
		}
		input[len(input)-1].DedupeFragmentEntries = dedupe
		want, err := json.MarshalIndent(merged, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		got, err := artifact.MergeFragments(input)
		expected := []artifact.Artifact{{Path: "hooks.json", Bytes: want, Mode: 0o640, DedupeFragmentEntries: dedupe}}
		if err != nil || !reflect.DeepEqual(got, expected) {
			t.Fatalf("merged = %+v, error=%v; want bytes=%s", got, err, want)
		}
	})
}

func TestFragmentDedupeAllocationGrowth(t *testing.T) {
	const count = 256
	fragments := compiledHookFragments(t, count, false)
	var last []byte
	allocations := testing.AllocsPerRun(2, func() {
		result, err := artifact.MergeFragments(fragments)
		if err != nil || len(result) != 1 {
			t.Fatalf("merge fragments: %v, result=%+v", err, result)
		}
		last = result[0].Bytes
	})
	var doc map[string][]any
	if err := json.Unmarshal(last, &doc); err != nil || len(doc["PreToolUse"]) != count {
		t.Fatalf("result lost hooks: %v", err)
	}
	if budget := float64(256 * count); allocations > budget {
		t.Fatalf("allocations = %v, want <=%v without repeated entry serialization", allocations, budget)
	}
}
