package artifact

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// BenchmarkMergeFragments covers text and JSON fragment merging at multiple
// group sizes.
func BenchmarkMergeFragments(b *testing.B) {
	b.Run("json", func(b *testing.B) {
		for _, n := range []int{5, 20, 100} {
			b.Run(fmt.Sprintf("%d_fragments", n), func(b *testing.B) {
				artifacts := make([]Artifact, n)
				for i := 0; i < n; i++ {
					frag := map[string]any{
						fmt.Sprintf("key-%d", i): fmt.Sprintf("value-%d", i),
					}
					data, err := json.Marshal(frag)
					if err != nil {
						b.Fatal(err)
					}
					artifacts[i] = Artifact{
						Path:        "merged.json",
						Bytes:       data,
						Fragment:    true,
						FragmentKey: "mcp",
					}
				}
				// Add some non-fragment artifacts to cover the pass-through path.
				for i := 0; i < 20; i++ {
					artifacts = append(artifacts, Artifact{
						Path:  fmt.Sprintf("non-frag-%d.txt", i),
						Bytes: []byte(fmt.Sprintf("content-%d", i)),
					})
				}

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := MergeFragments(artifacts); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})

	b.Run("text", func(b *testing.B) {
		for _, n := range []int{5, 20, 100} {
			b.Run(fmt.Sprintf("%d_fragments", n), func(b *testing.B) {
				artifacts := make([]Artifact, n)
				for i := 0; i < n; i++ {
					artifacts[i] = Artifact{
						Path:        "merged.md",
						Bytes:       []byte(fmt.Sprintf("# Section %d\n\nContent %d\n", i, i)),
						Fragment:    true,
						FragmentKey: "doc",
					}
				}

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := MergeFragments(artifacts); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})

	b.Run("mixed_json_text_groups", func(b *testing.B) {
		b.Run("10_json_10_text", func(b *testing.B) {
			artifacts := make([]Artifact, 0, 20)
			for i := 0; i < 10; i++ {
				frag := map[string]any{fmt.Sprintf("key-%d", i): fmt.Sprintf("val-%d", i)}
				data, err := json.Marshal(frag)
				if err != nil {
					b.Fatal(err)
				}
				artifacts = append(artifacts, Artifact{
					Path: "merged.json", Bytes: data, Fragment: true, FragmentKey: "json",
				})
			}
			for i := 0; i < 10; i++ {
				artifacts = append(artifacts, Artifact{
					Path: "merged.md", Bytes: []byte(fmt.Sprintf("Section %d", i)),
					Fragment: true, FragmentKey: "text",
				})
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := MergeFragments(artifacts); err != nil {
					b.Fatal(err)
				}
			}
		})
	})
}

// BenchmarkMergeWithExistingJSON covers managed, user-owned, added, removed,
// and unchanged entries.
func BenchmarkMergeWithExistingJSON(b *testing.B) {
	existing := map[string]any{
		"mcpServers": map[string]any{
			"managed-1": map[string]any{
				"command":     "managed-1-cmd",
				"_managed_by": "agent-marketplace",
			},
			"managed-2": map[string]any{
				"command":     "managed-2-cmd",
				"_managed_by": "agent-marketplace",
			},
			"user-owned": map[string]any{
				"command": "user-cmd",
			},
		},
	}
	existingBytes, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		b.Fatal(err)
	}
	desired := map[string]any{
		"mcpServers": map[string]any{
			"managed-1": map[string]any{
				"command": "managed-1-cmd-updated",
			},
			"new-managed": map[string]any{
				"command": "new-cmd",
			},
		},
	}
	desiredBytes, err := json.MarshalIndent(desired, "", "  ")
	if err != nil {
		b.Fatal(err)
	}

	b.Run("small", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := MergeWithExistingJSON(existingBytes, desiredBytes, "_managed_by", "agent-marketplace"); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("large_existing", func(b *testing.B) {
		large := map[string]any{
			"mcpServers": makeLargeEntries(100),
		}
		largeBytes, err := json.MarshalIndent(large, "", "  ")
		if err != nil {
			b.Fatal(err)
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := MergeWithExistingJSON(largeBytes, desiredBytes, "_managed_by", "agent-marketplace"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkMergeWithExistingJSONArray covers the array-of-objects container
// path (plugin.json skills/droids/agents), which merges entries by "name".
func BenchmarkMergeWithExistingJSONArray(b *testing.B) {
	existing := map[string]any{
		"skills": []any{
			map[string]any{"name": "managed-1", "description": "v1", "_managed_by": "agent-marketplace"},
			map[string]any{"name": "managed-2", "description": "v1", "_managed_by": "agent-marketplace"},
			map[string]any{"name": "user-owned", "description": "user"},
		},
	}
	existingBytes, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		b.Fatal(err)
	}
	desired := map[string]any{
		"skills": []any{
			map[string]any{"name": "managed-1", "description": "v2"},
			map[string]any{"name": "new-managed", "description": "fresh"},
		},
	}
	desiredBytes, err := json.MarshalIndent(desired, "", "  ")
	if err != nil {
		b.Fatal(err)
	}

	b.Run("small", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := MergeWithExistingJSON(existingBytes, desiredBytes, "_managed_by", "agent-marketplace"); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("large_existing", func(b *testing.B) {
		large := map[string]any{"skills": makeLargeArrayEntries(100)}
		largeBytes, err := json.MarshalIndent(large, "", "  ")
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := MergeWithExistingJSON(largeBytes, desiredBytes, "_managed_by", "agent-marketplace"); err != nil {
				b.Fatal(err)
			}
		}
	})

	// large_existing above is the worst case on purpose: every one of the 100
	// existing entries is stale and the two desired entries are new, so nothing
	// can be matched and reused. large_existing_steady is what an ordinary
	// re-sync of the same file actually looks like — the destination already
	// says what we want — and is the case the concrete-syntax path is tuned
	// for: every element matches positionally, so the array is left untouched.
	b.Run("large_existing_steady", func(b *testing.B) {
		entries := makeLargeArrayEntries(100)
		largeBytes, err := json.MarshalIndent(map[string]any{"skills": entries}, "", "  ")
		if err != nil {
			b.Fatal(err)
		}
		steady := make([]any, 0, len(entries))
		for _, entry := range entries {
			desiredEntry := map[string]any{}
			for k, v := range entry.(map[string]any) {
				if k == "_managed_by" {
					continue
				}
				desiredEntry[k] = v
			}
			steady = append(steady, desiredEntry)
		}
		steadyBytes, err := json.MarshalIndent(map[string]any{"skills": steady}, "", "  ")
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := MergeWithExistingJSON(largeBytes, steadyBytes, "_managed_by", "agent-marketplace"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func makeLargeArrayEntries(n int) []any {
	entries := make([]any, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, map[string]any{
			"name":        fmt.Sprintf("entry-%04d", i),
			"description": fmt.Sprintf("desc-%d", i),
			"_managed_by": "agent-marketplace",
		})
	}
	return entries
}

func makeLargeEntries(n int) map[string]any {
	entries := make(map[string]any, n)
	for i := 0; i < n; i++ {
		entries[fmt.Sprintf("entry-%04d", i)] = map[string]any{
			"type":    "stdio",
			"command": fmt.Sprintf("cmd-%d", i),
			"args":    []string{fmt.Sprintf("arg-%d-1", i), fmt.Sprintf("arg-%d-2", i)},
		}
	}
	return entries
}

// BenchmarkMergeWithExistingJSONPreserving is the regression guard for the
// concrete-syntax write path added by bead agent-marketplace-5exj.26. Unlike
// the two benchmarks above, whose existing bytes come from json.MarshalIndent,
// these inputs look like a file a human maintains: comments, mixed indentation,
// and entries the merge must not touch. That is the case where the patcher has
// to preserve the most and therefore does the most work.
func BenchmarkMergeWithExistingJSONPreserving(b *testing.B) {
	desiredBytes := []byte(`{"mcpServers":{"managed-1":{"command":"c1"},"managed-2":{"command":"c2"}}}`)

	build := func(users int) []byte {
		var sb strings.Builder
		sb.WriteString("{\n  // my servers, hands off\n  \"mcpServers\": {\n")
		for i := 0; i < users; i++ {
			fmt.Fprintf(&sb, "    // server %d\n    \"user-%04d\": {\"command\":\"cmd-%d\",   \"args\": [\"a\", \"b\"], \"env\": {\"K\": \"V\"}},\n", i, i, i)
		}
		sb.WriteString("    \"managed-1\": {\"command\": \"c1\", \"_managed_by\": \"agent-marketplace\"}\n  }\n}\n")
		return []byte(sb.String())
	}

	for _, users := range []int{3, 100} {
		existingBytes := build(users)
		b.Run(fmt.Sprintf("%d_user_entries", users), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := MergeWithExistingJSON(existingBytes, desiredBytes, "_managed_by", "agent-marketplace"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// makeHookGroups builds n Factory rule groups for one event. managed stamps the
// ownership marker, which is what an earlier sync would have left behind.
func makeHookGroups(n int, managed bool, prefix string) []any {
	groups := make([]any, 0, n)
	for i := 0; i < n; i++ {
		group := map[string]any{
			"matcher": fmt.Sprintf("Tool%03d", i),
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": fmt.Sprintf("%s-%d.sh", prefix, i),
			}},
		}
		if managed {
			group["_managed_by"] = "agent-marketplace"
		}
		groups = append(groups, group)
	}
	return groups
}

// BenchmarkMergeWithExistingJSONHooks is the regression guard for the unnamed
// array-element path bead agent-marketplace-5exj.28 added. Factory's
// hooks/hooks.json holds rule groups with no "name", so identity is the group's
// behavior tuple: every existing managed group is marshalled once to key it,
// and every desired group is marshalled once to look it up.
//
// The three cases are the three things a sync can do to that array. steady is
// the one that runs on essentially every sync — the destination already says
// what we want — and is the case the identity exists to make free.
func BenchmarkMergeWithExistingJSONHooks(b *testing.B) {
	const groups = 50
	existing := map[string]any{"PreToolUse": append(makeHookGroups(groups, true, "gen"), makeHookGroups(5, false, "mine")...)}
	existingBytes, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		b.Fatal(err)
	}

	cases := map[string][]any{
		"steady":  makeHookGroups(groups, false, "gen"),
		"changed": makeHookGroups(groups, false, "gen-v2"),
		"grown":   makeHookGroups(groups+10, false, "gen"),
	}
	for name, desired := range cases {
		desiredBytes, err := json.MarshalIndent(map[string]any{"PreToolUse": desired}, "", "  ")
		if err != nil {
			b.Fatal(err)
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := MergeWithExistingJSON(existingBytes, desiredBytes, "_managed_by", "agent-marketplace"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
