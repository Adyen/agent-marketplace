package mapping

import targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"

// Target represents a compilation target harness. It is owned by the shared
// target registry so mapping tables and public compilation APIs use the same
// identity type without importing the root harnesscompile package.
type Target = targetmeta.Target

const (
	TargetClaude   = targetmeta.TargetClaude
	TargetFactory  = targetmeta.TargetFactory
	TargetCursor   = targetmeta.TargetCursor
	TargetWindsurf = targetmeta.TargetWindsurf
	TargetGemini   = targetmeta.TargetGemini
	TargetDevin    = targetmeta.TargetDevin
	TargetCodex    = targetmeta.TargetCodex
	TargetOpenCode = targetmeta.TargetOpenCode
)
