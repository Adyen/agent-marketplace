// Package ext defines target-specific IR sidecars used to preserve
// harness-native detail without polluting the canonical IR core.
package ext

// Bag groups optional per-target sidecars. The zero value is safe to embed in
// IR resources; nil target pointers mean "derive from canonical fields".
type Bag struct {
	Factory  *Factory  `json:"factory,omitempty"`
	Cursor   *Cursor   `json:"cursor,omitempty"`
	Windsurf *Windsurf `json:"windsurf,omitempty"`
	Gemini   *Gemini   `json:"gemini,omitempty"`
	Claude   *Claude   `json:"claude,omitempty"`
	Codex    *Codex    `json:"codex,omitempty"`
	OpenCode *OpenCode `json:"opencode,omitempty"`
}

// OpenCode preserves native command and agent metadata. Permission rules use
// slices because OpenCode applies them in authored order (last match wins).
type OpenCode struct {
	Agent       string              `json:"agent,omitempty"`
	Model       string              `json:"model,omitempty"`
	Mode        string              `json:"mode,omitempty"`
	Subtask     *bool               `json:"subtask,omitempty"`
	Hidden      *bool               `json:"hidden,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
	Steps       *int                `json:"steps,omitempty"`
	ToolsSet    bool                `json:"toolsSet,omitempty"`
	Tools       map[string]bool     `json:"tools,omitempty"`
	Permission  *OpenCodePermission `json:"permission,omitempty"`
	MCP         *OpenCodeMCP        `json:"mcp,omitempty"`
}

type OpenCodePermission struct {
	Action string                   `json:"action,omitempty"`
	Rules  []OpenCodePermissionRule `json:"rules,omitempty"`
}

type OpenCodePermissionRule struct {
	Tool        string                      `json:"tool"`
	Action      string                      `json:"action,omitempty"`
	PatternsSet bool                        `json:"patternsSet,omitempty"`
	Patterns    []OpenCodePermissionPattern `json:"patterns,omitempty"`
}

type OpenCodePermissionPattern struct {
	Pattern string `json:"pattern"`
	Action  string `json:"action"`
}

type OpenCodeMCP struct {
	// NativeValues distinguishes native reference syntax from canonical IR
	// interpolation. Native values are never expanded by the compiler.
	NativeValues  bool   `json:"nativeValues,omitempty"`
	Timeout       *int   `json:"timeout,omitempty"`
	OAuthDisabled *bool  `json:"oauthDisabled,omitempty"`
	OAuthScope    string `json:"oauthScope,omitempty"`
}

// Codex preserves documented native fields without changing canonical semantics.
type Codex struct {
	Model                  string            `json:"model,omitempty"`
	ReasoningEffort        string            `json:"reasoningEffort,omitempty"`
	NativeHook             bool              `json:"nativeHook,omitempty"`
	HookAsync              *bool             `json:"hookAsync,omitempty"`
	HookStatusMessage      *string           `json:"hookStatusMessage,omitempty"`
	CommandWindows         *string           `json:"commandWindows,omitempty"`
	AdditionalContextLimit *int              `json:"additionalContextLimit,omitempty"`
	MCP                    *CodexMCP         `json:"mcp,omitempty"`
	SandboxMode            string            `json:"sandboxMode,omitempty"`
	BearerTokenEnvVar      string            `json:"bearerTokenEnvVar,omitempty"`
	EnvHTTPHeaders         map[string]string `json:"envHttpHeaders,omitempty"`
}

// CodexMCP preserves native literal-vs-forwarded MCP values. Strings that
// resemble another harness's interpolation syntax remain literal in Codex.
type CodexMCP struct {
	Command                 string            `json:"command,omitempty"`
	Args                    []string          `json:"args,omitempty"`
	CWD                     string            `json:"cwd,omitempty"`
	ExperimentalEnvironment string            `json:"experimentalEnvironment,omitempty"`
	Env                     map[string]string `json:"env,omitempty"`
	EnvVars                 []string          `json:"envVars,omitempty"`
	RemoteEnvVars           []string          `json:"remoteEnvVars,omitempty"`
	URL                     string            `json:"url,omitempty"`
	HTTPHeaders             map[string]string `json:"httpHeaders,omitempty"`
}

// Factory preserves Factory Droid-specific fields that do not belong in the
// canonical IR.
type Factory struct {
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	Isolation       string `json:"isolation,omitempty"`
}

// Cursor preserves Cursor-native values when they should be emitted verbatim
// instead of re-derived from canonical fields.
type Cursor struct {
	Model    string   `json:"model,omitempty"`
	Tools    []string `json:"tools,omitempty"`
	Effort   string   `json:"effort,omitempty"`
	HookType string   `json:"hookType,omitempty"`
}

// Windsurf preserves workflow and hook fields that are specific to Windsurf.
type Windsurf struct {
	AutoExecutionMode    string `json:"autoExecutionMode,omitempty"`
	AutoExecutionModeSet bool   `json:"autoExecutionModeSet,omitempty"`
	OriginalHookEvent    string `json:"originalHookEvent,omitempty"`
	HookPowerShell       string `json:"hookPowerShell,omitempty"`
	HookShowOutput       *bool  `json:"hookShowOutput,omitempty"`
	HookWorkingDir       string `json:"hookWorkingDir,omitempty"`
}

// Gemini preserves Gemini CLI-specific fields for commands, hooks, and
// subagents.
type Gemini struct {
	Temperature      *float64                   `json:"temperature,omitempty"`
	MaxTurns         *int                       `json:"maxTurns,omitempty"`
	TimeoutMinutes   *int                       `json:"timeoutMinutes,omitempty"`
	Kind             string                     `json:"kind,omitempty"`
	KindSet          bool                       `json:"kindSet,omitempty"`
	InlineMCPServers map[string]GeminiMCPServer `json:"inlineMcpServers,omitempty"`
	TOMLCommand      *GeminiTOMLCommand         `json:"tomlCommand,omitempty"`
	HookTimeout      *int                       `json:"hookTimeout,omitempty"`
	HookName         string                     `json:"hookName,omitempty"`
	HookGroupExtra   map[string]any             `json:"hookGroupExtra,omitempty"`
}

// GeminiMCPServer preserves inline Gemini subagent MCP definitions without
// introducing an import cycle back to the ir package.
type GeminiMCPServer struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
}

// GeminiTOMLCommand preserves Gemini command TOML metadata.
type GeminiTOMLCommand struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Prompt      string   `json:"prompt,omitempty"`
	Triggers    []string `json:"triggers,omitempty"`
	TriggersSet bool     `json:"triggersSet,omitempty"`
}

// Claude preserves Claude-native values when emitters should prefer the
// original target representation over canonical derivation.
type Claude struct {
	Model     string   `json:"model,omitempty"`
	Tools     []string `json:"tools,omitempty"`
	Effort    string   `json:"effort,omitempty"`
	Isolation string   `json:"isolation,omitempty"`
	HookType  string   `json:"hookType,omitempty"`
}
