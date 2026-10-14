// Package opencodehook defines the fixed adapter and its data-only declarations.
package opencodehook

import (
	"path"
	"strings"

	_ "embed"
)

const Format = "agent-marketplace/opencode-hook-v1"

// AdapterPath is the native autoload location of the fixed adapter. Every
// contributing hook resource publishes the same reviewed bytes there, so a
// bundle must coalesce the copies into exactly one file.
const AdapterPath = "plugins/agent-marketplace.js"

// AdapterFragmentKey groups the identical adapter fragments emitted once per
// contributing hook resource. Fragments that share a path and this key coalesce
// into one published adapter.
const AdapterFragmentKey = "agent-marketplace/opencode-adapter-v1"

// DeclarationsDir holds the adapter's data-only JSON declarations. The fixed
// adapter reads that directory relative to its own compiled location.
const DeclarationsDir = "hooks/agent-marketplace"

// MaxDeclarationsPerToolEvent is the aggregate invocation budget the fixed
// adapter enforces at runtime for one tool event. Compilation refuses an
// unusable generation before publication instead of installing one the runtime
// would reject whole.
const MaxDeclarationsPerToolEvent = 64

// DefaultTimeout is the handler deadline, in seconds, written for a hook that
// declares none.
const DefaultTimeout = 60

// CanonicalTools mirrors the fixed adapter's runtime canonicalization table:
// the adapter rewrites these native tool IDs to the listed names before it
// compares a declaration matcher, so declarations must use the listed names.
var CanonicalTools = map[string]string{
	"bash": "Bash", "read": "Read", "write": "Write", "edit": "Edit", "glob": "Glob", "grep": "Grep",
	"webfetch": "WebFetch", "websearch": "WebSearch", "todowrite": "TodoWrite", "task": "Task", "skill": "Skill",
}

// IsDeclarationPath reports whether an artifact path is one adapter declaration.
func IsDeclarationPath(p string) bool {
	dir, base := path.Split(p)
	return path.Clean(dir) == DeclarationsDir && strings.HasSuffix(base, ".json") && len(base) > len(".json")
}

//go:embed adapter.js
var Adapter []byte

type Declaration struct {
	Format  string `json:"format"`
	Name    string `json:"name"`
	Event   string `json:"event"`
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}
