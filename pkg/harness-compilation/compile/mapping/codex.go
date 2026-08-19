package mapping

var codexSandboxModes = map[string]struct{}{
	"":                   {},
	"read-only":          {},
	"workspace-write":    {},
	"danger-full-access": {},
}

// CodexReasoningEffort classifies low, medium, and high as portable canonical
// values. Codex accepts every other non-empty string, so those values remain
// native provenance for lossless same-target round trips.
func CodexReasoningEffort(effort string) (canonical, native string, valid bool) {
	switch effort {
	case "":
		return "", "", true
	case "low", "medium", "high":
		return effort, "", true
	default:
		return "", effort, true
	}
}

// CodexSandboxModeSupported reports whether mode belongs to Codex's closed
// sandbox_mode enum. The empty value means no explicit override.
func CodexSandboxModeSupported(mode string) bool {
	_, ok := codexSandboxModes[mode]
	return ok
}
