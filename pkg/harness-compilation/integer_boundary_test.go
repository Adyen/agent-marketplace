package harnesscompile_test

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/factory"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/gemini"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: harness-compilation/integer-metadata-does-not-wrap
func TestSubagentIntegerBoundaries(t *testing.T) {
	loaders := map[string]func(fstest.MapFS) (*ir.Subagent, error){
		"canonical": func(src fstest.MapFS) (*ir.Subagent, error) { return bundle.LoadSubagent(src, "agent.md") },
		"factory": func(src fstest.MapFS) (*ir.Subagent, error) {
			agent, _, err := factory.LoadSubagent(src, "agent.md")
			return agent, err
		},
		"gemini": func(src fstest.MapFS) (*ir.Subagent, error) {
			agent, _, err := gemini.LoadSubagent(src, "agent.md")
			return agent, err
		},
	}
	for name, load := range loaders {
		t.Run(name, func(t *testing.T) {
			for _, value := range []string{"9223372036854775808.0", "9.223372036854776e18", "\"9223372036854775808\"", ".inf", ".nan", "1.5"} {
				t.Run(value, func(t *testing.T) {
					src := fstest.MapFS{"agent.md": {Data: []byte("---\nname: agent\nmax-nesting: " + value + "\n---\nbody\n")}}
					if agent, err := load(src); err == nil || agent != nil {
						t.Fatalf("out-of-range integer accepted: %#v, %v", agent, err)
					}
				})
			}
			src := fstest.MapFS{"agent.md": {Data: []byte("---\nname: agent\nmax-nesting: 2.0\n---\nbody\n")}}
			agent, err := load(src)
			if err != nil || agent == nil || agent.MaxNesting == nil || *agent.MaxNesting != 2 {
				t.Fatalf("valid integer control: %#v, %v", agent, err)
			}
		})
	}
}
