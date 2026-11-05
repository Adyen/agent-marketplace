package codex

import (
	"testing"
	"testing/fstest"
)

func codexBenchBundle() fstest.MapFS {
	return fstest.MapFS{
		".agents/skills/bench-skill/SKILL.md":   {Data: []byte("---\nname: bench-skill\ndescription: A benchmark skill\n---\nBenchmark skill body.\n")},
		".agents/skills/bench-skill/run.sh":     {Data: []byte("#!/bin/sh\necho bench\n")},
		".agents/skills/.git/HEAD":              {Data: []byte("ref: refs/heads/main\n")},
		".agents/skills/.git/hooks/pre-commit":  {Data: []byte("#!/bin/sh\n")},
		".agents/skills/bench-command/SKILL.md": {Data: []byte("---\nname: bench-command\ndescription: A benchmark command\nx-agent-marketplace-kind: command\n---\nBenchmark command body.\n")},
		".codex/agents/bench-agent.toml": {Data: []byte(`name = "bench-agent"
description = "A benchmark subagent"
developer_instructions = "Review benchmark input."
model = "gpt-5"
model_reasoning_effort = "high"
sandbox_mode = "read-only"
`)},
		".codex/hooks.json": {Data: []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./check.sh","timeout":30}]}]}}`)},
		".codex/config.toml": {Data: []byte(`[mcp_servers.stdio]
command = "npx"
args = ["-y", "bench"]
env = { TOKEN = "literal" }

[mcp_servers.remote]
url = "https://example.invalid/mcp"
http_headers = { X-Client = "literal" }
oauth_resource = "https://example.invalid/resource"
`)},
	}
}

func BenchmarkLoadBundle(b *testing.B) {
	src := codexBenchBundle()
	b.ReportAllocs()
	for b.Loop() {
		bundle, err := LoadBundle(src)
		if err != nil {
			b.Fatal(err)
		}
		if len(bundle.Resources) != 6 {
			b.Fatalf("resources = %d, want 6", len(bundle.Resources))
		}
	}
}

func BenchmarkLoadBundleByKind(b *testing.B) {
	full := codexBenchBundle()
	cases := []struct {
		name  string
		paths []string
		want  int
	}{
		{"skill", []string{".agents/skills/bench-skill/SKILL.md", ".agents/skills/bench-skill/run.sh", ".agents/skills/.git/HEAD", ".agents/skills/.git/hooks/pre-commit"}, 1},
		{"command", []string{".agents/skills/bench-command/SKILL.md"}, 1},
		{"subagent", []string{".codex/agents/bench-agent.toml"}, 1},
		{"hook", []string{".codex/hooks.json"}, 1},
		{"mcpserver", []string{".codex/config.toml"}, 2},
	}
	for _, tt := range cases {
		src := fstest.MapFS{}
		for _, path := range tt.paths {
			src[path] = full[path]
		}
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
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
