package harnesscompile

import (
	"encoding/json"
	"fmt"
	"testing"
	"testing/fstest"
)

// BenchmarkCompileBundleLarge measures the cost of compiling large synthetic
// bundles of various resource kinds.
func BenchmarkCompileBundleLarge(b *testing.B) {
	cases := []struct {
		name      string
		numSkills int
		numCmd    int
		numSub    int
		numHooks  int
		numMcp    int
		target    Target
	}{
		{"100_skills_claude", 100, 0, 0, 0, 0, TargetClaude},
		{"50_mixed_factory", 30, 5, 5, 5, 5, TargetFactory},
		{"50_mixed_cursor", 30, 5, 5, 5, 5, TargetCursor},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			fsys := benchmarkBundleFS(b, c.numSkills, c.numCmd, c.numSub, c.numHooks, c.numMcp)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := CompileBundle(fsys, c.target); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCompileBundleViaHCC measures the end-to-end compile path from
// an in-memory filesystem through CompileBundle.
func BenchmarkCompileBundleViaHCC(b *testing.B) {
	cases := []struct {
		name      string
		numSkills int
		target    Target
	}{
		{"20_skills_factory", 20, TargetFactory},
		{"50_skills_claude", 50, TargetClaude},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			fsys := make(fstest.MapFS)
			fsys[".claude-plugin/plugin.json"] = &fstest.MapFile{
				Data: []byte(`{"name": "bench-plugin", "version": "1.0.0"}`),
			}
			for i := 0; i < c.numSkills; i++ {
				name := fmt.Sprintf("skill-%04d", i)
				fsys[fmt.Sprintf("skills/%s/SKILL.md", name)] = &fstest.MapFile{
					Data: []byte(fmt.Sprintf("---\nname: %s\ndescription: skill %d\n---\n\n# %s\n\nBody\n", name, i, name)),
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := CompileBundle(fsys, c.target); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCompileBundleRichFields(b *testing.B) {
	fsys := make(fstest.MapFS)
	for i := 0; i < 25; i++ {
		name := fmt.Sprintf("rich-skill-%04d", i)
		fsys[fmt.Sprintf("skills/%s/SKILL.md", name)] = &fstest.MapFile{Data: []byte(fmt.Sprintf("---\nname: %s\ndescription: rich skill\nallowed-tools: [Read, Edit]\npermissions:\n  allow: [Read]\n  deny: [Bash]\nmodel: sonnet\ncustom: true\n---\nBody\n", name))}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CompileBundle(fsys, TargetFactory); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompileBundlePartialSkips measures the bundle-compile path when part
// of the bundle is suppressed by error diagnostics.
//
// Before bead agent-marketplace-5exj.17 this shape returned early with zero
// artifacts, so it had no meaningful baseline: the work it now does (compile
// everything, publish the clean resources, merge their fragments) simply did not
// happen. It is here as a regression guard for the path a Devin sync hits
// whenever one skill carries an unmappable tool restriction.
func BenchmarkCompileBundlePartialSkips(b *testing.B) {
	cases := []struct {
		name       string
		numSkills  int
		numBadSubs int
		target     Target
	}{
		{"50_skills_1_bad_subagent_cursor", 50, 1, TargetCursor},
		{"50_skills_10_bad_subagents_cursor", 50, 10, TargetCursor},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			fsys := benchmarkBundleFS(b, c.numSkills, 0, 0, 0, 0)
			for i := 0; i < c.numBadSubs; i++ {
				name := fmt.Sprintf("bad-agent-%04d", i)
				// disallowedTools is an error with no artifact on Cursor.
				fsys[fmt.Sprintf("agents/%s.md", name)] = &fstest.MapFile{
					Data: []byte(fmt.Sprintf("---\nname: %s\ndescription: agent %d\nmodel: sonnet\ndisallowedTools:\n  - Bash\n---\n\nYou are agent %d.\n", name, i, i)),
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := CompileBundle(fsys, c.target)
				if err != nil {
					b.Fatal(err)
				}
				if len(result.Skipped) != c.numBadSubs {
					b.Fatalf("skipped = %d, want %d", len(result.Skipped), c.numBadSubs)
				}
				if len(result.Artifacts) != c.numSkills {
					b.Fatalf("artifacts = %d, want %d", len(result.Artifacts), c.numSkills)
				}
			}
		})
	}
}

func benchmarkBundleFS(b *testing.B, numSkills, numCommands, numSubagents, numHooks, numMCP int) fstest.MapFS {
	b.Helper()

	fsys := make(fstest.MapFS)
	for i := 0; i < numSkills; i++ {
		name := fmt.Sprintf("skill-%04d", i)
		fsys[fmt.Sprintf("skills/%s/SKILL.md", name)] = &fstest.MapFile{
			Data: []byte(fmt.Sprintf("---\nname: %s\ndescription: skill %d\n---\n\n# %s\n\nBody\n", name, i, name)),
		}
	}
	for i := 0; i < numCommands; i++ {
		name := fmt.Sprintf("cmd-%04d", i)
		fsys[fmt.Sprintf("commands/%s.md", name)] = &fstest.MapFile{
			Data: []byte(fmt.Sprintf("---\nname: %s\ndescription: command %d\n---\n\nRun command %d\n", name, i, i)),
		}
	}
	for i := 0; i < numSubagents; i++ {
		name := fmt.Sprintf("agent-%04d", i)
		fsys[fmt.Sprintf("agents/%s.md", name)] = &fstest.MapFile{
			Data: []byte(fmt.Sprintf("---\nname: %s\ndescription: agent %d\nmodel: sonnet\ntools:\n  - Read\n---\n\nYou are agent %d.\n", name, i, i)),
		}
	}
	if numHooks > 0 {
		commands := make([]map[string]string, numHooks)
		for i := range commands {
			commands[i] = map[string]string{"type": "command", "command": fmt.Sprintf("echo hook-%d", i)}
		}
		data, err := json.Marshal(map[string]any{"hooks": map[string]any{"postToolUse": commands}})
		if err != nil {
			b.Fatal(err)
		}
		fsys["hooks/hooks.json"] = &fstest.MapFile{Data: data}
	}
	if numMCP > 0 {
		servers := make(map[string]any, numMCP)
		for i := 0; i < numMCP; i++ {
			servers[fmt.Sprintf("mcp-%04d", i)] = map[string]any{"command": "node", "args": []string{fmt.Sprintf("server-%d.js", i)}}
		}
		data, err := json.Marshal(map[string]any{"mcpServers": servers})
		if err != nil {
			b.Fatal(err)
		}
		fsys[".mcp.json"] = &fstest.MapFile{Data: data}
	}
	return fsys
}
