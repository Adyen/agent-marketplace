// Package opencodehook defines the fixed adapter and its data-only declarations.
package opencodehook

import _ "embed"

const Format = "agent-marketplace/opencode-hook-v1"

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
