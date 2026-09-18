package analyze

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// builtin is an in-process Marketplace analyzer that owns a focused set of
// scanners and/or capability checks.
type builtin struct {
	id      string
	version string
	kinds   []ir.ResourceKind
	scan    func(samples []textSample, files FileSet, analyzer string) []Finding
	caps    func(r ir.Resource, analyzer string) []Finding
}

func (b *builtin) ID() string               { return b.id }
func (b *builtin) Version() string          { return b.version }
func (b *builtin) Kinds() []ir.ResourceKind { return b.kinds }

func (b *builtin) Analyze(ctx context.Context, s Subject, _ AnalyzerConfig) (Audit, error) {
	if ctx.Err() != nil {
		return Audit{}, ctx.Err()
	}
	if err := validateSubject(&s); err != nil {
		return Audit{}, err
	}

	start := time.Now()

	var findings []Finding
	if b.scan != nil {
		findings = b.scan(s.samples, s.Files, b.id)
	}
	if b.caps != nil {
		findings = append(findings, b.caps(s.Resource, b.id)...)
	}

	for i := range findings {
		findings[i].Analyzer = b.id
		findings[i].AnalyzerVersion = b.version
	}

	duration := time.Since(start)
	slog.Debug("builtin audit raw",
		"analyzer", b.id,
		"findings", len(findings),
		"duration", duration)

	return Audit{
		AnalyzerID:      b.id,
		AnalyzerVersion: b.version,
		Findings:        findings,
		Duration:        duration,
	}, nil
}

// builtinDefinition binds the public metadata for a built-in analyzer with the
// constructor that produces it. The table is the single source of truth for
// both Builtins() and DefaultRegistrations().
type builtinDefinition struct {
	metadata    BuiltinMetadata
	constructor func() Analyzer
}

var builtinDefinitions = []builtinDefinition{
	{
		metadata: BuiltinMetadata{
			ID:          AnalyzerHiddenContent,
			Version:     AnalyzerHiddenContentVersionV1,
			DefaultRole: RoleGating,
			FindingCodes: []string{
				FindingHTMLComment,
				FindingTerminalControl,
				FindingUnsafeBidi,
				FindingZeroWidth,
			},
		},
		constructor: newHiddenContentAnalyzer,
	},
	{
		metadata: BuiltinMetadata{
			ID:          AnalyzerObfuscation,
			Version:     AnalyzerObfuscationVersionV1,
			DefaultRole: RoleGating,
			FindingCodes: []string{
				FindingDynamicExecution,
				FindingObfuscatedInstruction,
			},
		},
		constructor: newObfuscationAnalyzer,
	},
	{
		metadata: BuiltinMetadata{
			ID:          AnalyzerAssets,
			Version:     AnalyzerAssetsVersionV1,
			DefaultRole: RoleGating,
			FindingCodes: []string{
				FindingAssetPathEscape,
				FindingExecutableAsset,
				FindingExecutableScript,
			},
		},
		constructor: newAssetsAnalyzer,
	},
	{
		metadata: BuiltinMetadata{
			ID:          AnalyzerReferences,
			Version:     AnalyzerReferencesVersionV1,
			DefaultRole: RoleAdvisory,
			FindingCodes: []string{
				FindingDangerousURL,
				FindingRemoteReference,
				FindingSuspiciousPath,
			},
		},
		constructor: newReferencesAnalyzer,
	},
	{
		metadata: BuiltinMetadata{
			ID:          AnalyzerCapabilities,
			Version:     AnalyzerCapabilitiesVersionV1,
			DefaultRole: RoleAdvisory,
			FindingCodes: []string{
				FindingHookCapability,
				FindingMcpCapability,
				FindingToolGrant,
			},
		},
		constructor: newCapabilitiesAnalyzer,
	},
}

// Builtins returns the canonical ordered list of built-in analyzer metadata.
// The returned metadata is a deep clone so callers cannot mutate the canonical
// definitions.
func Builtins() []BuiltinMetadata {
	out := make([]BuiltinMetadata, len(builtinDefinitions))
	for i, def := range builtinDefinitions {
		codes := append([]string(nil), def.metadata.FindingCodes...)
		sort.Strings(codes)
		out[i] = BuiltinMetadata{
			ID:           def.metadata.ID,
			Version:      def.metadata.Version,
			DefaultRole:  def.metadata.DefaultRole,
			FindingCodes: codes,
		}
	}
	return out
}

// DefaultRegistrations returns the five built-in analyzers in canonical order
// with their default roles and public policy. Each call constructs fresh
// analyzer instances from the same definitions.
func DefaultRegistrations() []Registration {
	regs := make([]Registration, len(builtinDefinitions))
	for i, def := range builtinDefinitions {
		regs[i] = Registration{
			Analyzer: def.constructor(),
			Role:     def.metadata.DefaultRole,
			Policy:   PublicPolicy(),
		}
	}
	return regs
}

func newHiddenContentAnalyzer() Analyzer {
	return &builtin{
		id:      AnalyzerHiddenContent,
		version: AnalyzerHiddenContentVersionV1,
		scan: func(samples []textSample, _ FileSet, analyzer string) []Finding {
			var findings []Finding
			for _, s := range samples {
				findings = append(findings, ScanHTMLComments(s.content, s.path, analyzer)...)
				findings = append(findings, ScanZeroWidth(s.content, s.path, analyzer)...)
				findings = append(findings, ScanUnsafeBidi(s.content, s.path, analyzer)...)
				findings = append(findings, ScanTerminalControl(s.content, s.path, analyzer)...)
			}
			return findings
		},
	}
}

func newObfuscationAnalyzer() Analyzer {
	return &builtin{
		id:      AnalyzerObfuscation,
		version: AnalyzerObfuscationVersionV1,
		scan: func(samples []textSample, _ FileSet, analyzer string) []Finding {
			var findings []Finding
			for _, s := range samples {
				findings = append(findings, ScanObfuscatedInstructions(s.content, s.path, analyzer)...)
				findings = append(findings, ScanDynamicExecution(s.content, s.path, analyzer)...)
			}
			return findings
		},
	}
}

func newAssetsAnalyzer() Analyzer {
	return &builtin{
		id:      AnalyzerAssets,
		version: AnalyzerAssetsVersionV1,
		scan: func(_ []textSample, files FileSet, analyzer string) []Finding {
			return ScanAssets(files, analyzer)
		},
	}
}

func newReferencesAnalyzer() Analyzer {
	return &builtin{
		id:      AnalyzerReferences,
		version: AnalyzerReferencesVersionV1,
		scan: func(samples []textSample, _ FileSet, analyzer string) []Finding {
			var findings []Finding
			for _, s := range samples {
				findings = append(findings, ScanDangerousURLs(s.content, s.path, analyzer)...)
				findings = append(findings, ScanRemoteReferences(s.content, s.path, analyzer)...)
				findings = append(findings, ScanSuspiciousPaths(s.content, s.path, analyzer)...)
			}
			return findings
		},
	}
}

func newCapabilitiesAnalyzer() Analyzer {
	return &builtin{
		id:      AnalyzerCapabilities,
		version: AnalyzerCapabilitiesVersionV1,
		caps: func(r ir.Resource, analyzer string) []Finding {
			switch res := r.(type) {
			case *ir.Skill:
				if len(res.AllowedTools) > 0 || res.Subagent != "" || res.Agent != "" {
					msg := "resource declares tool or execution grants"
					if len(res.AllowedTools) > 0 {
						msg = fmt.Sprintf("resource declares allowed tools: %v", res.AllowedTools)
					}
					return []Finding{NewFinding(FindingToolGrant, msg, analyzer, &SourceRange{File: "skill/" + res.Name()})}
				}
			case *ir.Subagent:
				if len(res.Tools) > 0 {
					msg := fmt.Sprintf("subagent declares tools: %v", res.Tools)
					return []Finding{NewFinding(FindingToolGrant, msg, analyzer, &SourceRange{File: "subagent/" + res.Name()})}
				}
			case *ir.Hook:
				msg := fmt.Sprintf("resource declares hook capability: event=%s type=%s", res.Event, res.Type)
				return []Finding{NewFinding(FindingHookCapability, msg, analyzer, &SourceRange{File: "hook/" + res.Name()})}
			case *ir.McpServer:
				return []Finding{NewFinding(FindingMcpCapability, "resource declares MCP server capability", analyzer, &SourceRange{File: "mcpserver/" + res.Name()})}
			}
			return nil
		},
	}
}
