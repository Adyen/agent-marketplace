package devin

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

// Devin ingestion is a user-visible latency path: `convert --from devin`,
// `sync`, `status`, and every catalog load run it once per configured registry,
// and Detect runs on every autodetection probe. Both earn a benchmark under the
// repository's performance policy the first time they are written.
//
// The sub-benchmarks are split by SHAPE and by KIND so a regression can be
// attributed. Detect is separated from LoadBundle because it is called far more
// often (autodetection probes every registered ingester) and must stay cheap:
// it reads a fixed list of marker paths and never parses a resource.

// benchWorkspace is the workspace-shaped root: one skill, one converted command,
// one flat subagent, three hooks across two files, and two MCP servers.
func benchWorkspace() fstest.MapFS { return benchRoot(".devin/") }

// benchDirect is the same content in the direct `~/.config/devin` shape, so the
// two root forms can be compared: they must cost the same, since the only
// difference is a path prefix.
func benchDirect() fstest.MapFS { return benchRoot("") }

func benchRoot(prefix string) fstest.MapFS {
	return fstest.MapFS{
		prefix + "skills/bench-skill/SKILL.md":       file("---\nname: bench-skill\ndescription: A benchmark skill\ntriggers:\n  - user\n  - model\nargument-hint: \"<path>\"\nmodel: sonnet\nallowed-tools:\n  - read\n  - edit\n  - grep\n  - glob\n  - exec\npermissions:\n  allow:\n    - Read\n  deny: []\nx-team: payments\n---\nBody of the benchmark skill.\n"),
		prefix + "skills/bench-skill/scripts/run.sh": file("#!/bin/sh\necho run\n"),
		prefix + "skills/bench-cmd/SKILL.md":         file("---\nname: bench-cmd\ndescription: A benchmark command\nx-agent-marketplace-kind: command\ntriggers:\n  - user\nargument-hint: \"<issue>\"\nallowed-tools:\n  - read\n---\nBody of the benchmark command.\n"),
		prefix + "agents/bench-agent.md":             file("---\nname: bench-agent\ndescription: A benchmark subagent\nmodel: opus\nallowed-tools:\n  - read\n  - exec\nmax-nesting: 4\n---\nBody of the benchmark agent.\n"),
		prefix + "hooks.v1.json": file(`{
  "PreToolUse": [{"type":"command","command":"/plugins/demo/pre.sh","matcher":"Read","timeout":30}],
  "PostToolUse": [{"matcher":"Edit","hooks":[{"type":"command","command":"/plugins/demo/post.sh"}]}]
}`),
		prefix + "config.json": file(`{"model":"sonnet","hooks":{"SessionEnd":[{"type":"prompt","prompt":"Summarize."}]}}`),
		prefix + "mcp_config.json": file(`{"mcpServers":{
  "remote":{"url":"https://example.invalid/mcp","transport":"http","headers":{"X-Client":"${env:CLIENT_ID}"},"oauthClientId":"id","oauthClientSecret":"${env:SECRET}","oauthResource":"https://example.invalid","disabled":false},
  "stdio":{"command":"npx","args":["-y","bench"],"env":{"TOKEN":"${env:TOKEN}"},"disabled":true}
}}`),
	}
}

func BenchmarkDetect(b *testing.B) {
	cases := []struct {
		name string
		src  fs.FS
		want bool
	}{
		{"workspace-hit", benchWorkspace(), true},
		// The miss paths are what autodetection pays on every non-Devin
		// registry, so they are measured separately. A miss is necessarily
		// dearer than a hit — a hit returns at the first marker that exists,
		// while a miss must prove all five are absent — so the number to watch
		// for regressions is the miss, not the ratio.
		{"direct-miss", benchDirect(), false},
		{"unrelated-miss", fstest.MapFS{".claude/skills/x/SKILL.md": file("---\nname: x\n---\nB\n")}, false},
	}
	for _, tt := range cases {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if Detect(tt.src) != tt.want {
					b.Fatalf("Detect = %t, want %t", !tt.want, tt.want)
				}
			}
		})
	}
}

func BenchmarkLoadBundle(b *testing.B) {
	cases := []struct {
		name string
		src  fs.FS
	}{
		{"workspace-root", benchWorkspace()},
		{"direct-root", benchDirect()},
	}
	for _, tt := range cases {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				bundle, err := LoadBundle(tt.src)
				if err != nil {
					b.Fatal(err)
				}
				if len(bundle.Resources) != 8 {
					b.Fatalf("resources = %d, want 8", len(bundle.Resources))
				}
			}
		})
	}
}

// BenchmarkLoadBundleByKind isolates each kind so a regression in one loader
// cannot hide behind the others.
func BenchmarkLoadBundleByKind(b *testing.B) {
	full := benchWorkspace()
	cases := []struct {
		name  string
		paths []string
		want  int
	}{
		{"skill", []string{".devin/skills/bench-skill/SKILL.md", ".devin/skills/bench-skill/scripts/run.sh"}, 1},
		{"command", []string{".devin/skills/bench-cmd/SKILL.md"}, 1},
		{"subagent", []string{".devin/agents/bench-agent.md"}, 1},
		{"hook", []string{".devin/hooks.v1.json", ".devin/config.json"}, 3},
		{"mcpserver", []string{".devin/mcp_config.json"}, 2},
	}
	for _, tt := range cases {
		src := fstest.MapFS{}
		for _, path := range tt.paths {
			src[path] = full[path]
		}
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				bundle, err := LoadBundle(src)
				if err != nil {
					b.Fatal(err)
				}
				if len(bundle.Resources) != tt.want {
					b.Fatalf("resources = %d, want %d", len(bundle.Resources), tt.want)
				}
			}
		})
	}
}
