package devin

import (
	"fmt"
	"log/slog"
	"path"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const (
	// kindExtensionKey is the stable YAML extension that records which
	// marketplace resource kind a Devin skill directory was compiled from.
	// Plan: "A command compiled as a skill carries the stable YAML extension:
	// x-agent-marketplace-kind: command". It is emitted only for converted
	// commands; an ordinary Devin skill remains a skill and carries no marker.
	kindExtensionKey = "x-agent-marketplace-kind"
	// commandKindValue is the only value this compiler writes for
	// kindExtensionKey. The Devin ingester (D8) reads it as command provenance.
	commandKindValue = "command"

	// triggerUser and triggerModel are Devin's documented activation triggers
	// (docs/devin-cli-harness-spec-3000.3.md, "Skills": triggers (`user` and
	// `model`)). The vocabulary is closed.
	triggerUser  = "user"
	triggerModel = "model"
)

// skillArtifactPath returns the canonical Devin skill-directory path for name,
// relative to the Devin target root. Skills and converted commands share it,
// which is exactly why bundle compilation needs a cross-kind collision guard.
func skillArtifactPath(name string) string {
	return path.Join(skillsDir, name, skillFile)
}

// translateSkill compiles a native Devin skill to skills/<name>/SKILL.md.
//
// Plan, resource mapping: "Skill | skills/<name>/SKILL.md | Native Devin skill".
func translateSkill(s *ir.Skill, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	source := compile.DiagSource(s.Source)
	if nameDiags := nestedNameDiagnostics(s.Name(), source); len(nameDiags) > 0 {
		return nil, nameDiags, nil
	}

	// An unrepresentable allowed-tools restriction no longer suppresses the
	// skill (bead agent-marketplace-pu6b): resolveAllowedTools reports
	// present=false with warnings, the key is omitted, and the skill is still
	// published. The diagnostics come first so the loss the user most needs to
	// see is the first one reported.
	tools, toolsPresent, toolsFromExtra, toolDiags := resolveAllowedTools(s.Name(), source, s.AllowedTools, s.AllowedTools != nil, s.Extra, opts, diag.CodeSkillDevinToolUnknown)

	var diags []diag.Diagnostic
	diags = append(diags, toolDiags...)
	triggers, triggersPresent, triggerDiags := mapTriggers(s.Name(), source, s.Triggers)
	diags = append(diags, triggerDiags...)

	model, modelDiags := mapModel(s.Name(), source, s.Model, opts, diag.CodeSkillDevinModelUnmappable)
	diags = append(diags, modelDiags...)

	permissions := compile.SkillPermissionsValueWithExtra(s.Permissions.Allow, s.Permissions.Ask, s.Permissions.Deny, s.Permissions.Extra, s.PermissionsSet)

	fm := &compile.Frontmatter{}
	fm.Set("name", s.Name())
	fm.Set("description", s.Description)
	if triggersPresent {
		fm.Set("triggers", triggers)
	}
	if s.ArgumentHint != "" {
		fm.Set("argument-hint", s.ArgumentHint)
	}
	if model != "" {
		fm.Set("model", model)
	}
	if toolsPresent {
		fm.Set(allowedToolsKey, tools)
	}
	if permissions != nil {
		// Devin represents permissions.allow/ask/deny natively, so the policy is
		// emitted verbatim: it is never widened, narrowed, or reordered here.
		fm.Set("permissions", permissions)
	}
	if s.Subagent != "" {
		fm.Set("subagent", s.Subagent)
	}
	if s.Agent != "" {
		fm.Set("agent", s.Agent)
	}

	// Unknown-field precedence, plan: "Canonical modeled fields win over
	// conflicting values in preserved extras." Presence is keyed on the IR
	// field, not on what survived mapping, so a dropped canonical model or
	// trigger can never be resurrected from an extension field.
	canonical := compile.PresentFields(map[string]bool{
		"name":        true,
		"description": true,
		"triggers":    s.Triggers != nil,
		// A dropped-to-empty trigger list still counts as canonical: the IR
		// asked for a restriction, so an extension field must not reinstate it.
		"argument-hint": s.ArgumentHint != "",
		"model":         s.Model != "",
		// allowed-tools is a canonical override only when the typed IR field is
		// set. When the restriction came from the extension field it was
		// consumed and mapped through the Devin vocabulary (see
		// resolveAllowedTools), so reporting it as "overridden" would be false
		// and would invite users to delete a restriction that was honored.
		allowedToolsKey: s.AllowedTools != nil,
		// permissions is reserved unconditionally: a permissions block must
		// never reach the artifact verbatim from an extension field, because
		// this compiler cannot verify that it is representable on Devin.
		"permissions":    true,
		"subagent":       s.Subagent != "",
		"agent":          s.Agent != "",
		kindExtensionKey: true, // reserved: a skill never carries command provenance
	})
	extraKeys, extraDiags := compile.ExtraKeys(s.Extra, canonical, mapping.TargetDevin, s.Name(), source)
	diags = append(diags, extraDiags...)
	diags = append(diags, compile.SupportFileDiagnostics(s, mapping.TargetDevin)...)
	appendExtras(fm, s.Extra, extraKeys, toolsFromExtra)

	return renderSkillArtifact("skill", s.Name(), fm, s.Body, diags)
}

// translateCommand compiles a command into the Devin skill-directory shape.
//
// Plan, resource mapping: "Command | skills/<name>/SKILL.md | User-triggered
// skill with x-agent-marketplace-kind: command", and IR evolution: "A converted
// command skill is emitted with triggers: [user], no model trigger, command
// argument metadata where available, and the stable command-origin extension."
func translateCommand(c *ir.Command, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	source := compile.DiagSource(c.Source)
	if nameDiags := nestedNameDiagnostics(c.Name(), source); len(nameDiags) > 0 {
		return nil, nameDiags, nil
	}

	// As for skills, an unrepresentable restriction omits the key and warns
	// instead of suppressing the command (bead agent-marketplace-pu6b). This is
	// the path the openspec commands and skills take: `Bash(openspec:*)` is a
	// scoped specifier Devin cannot express.
	tools, toolsPresent, toolsFromExtra, toolDiags := resolveAllowedTools(c.Name(), source, nil, false, c.Extra, opts, diag.CodeSkillDevinToolUnknown)

	fm := &compile.Frontmatter{}
	fm.Set("name", c.Name())
	fm.Set("description", c.Description)
	fm.Set(kindExtensionKey, commandKindValue)
	// User-only activation: Devin has no commands directory, so a command is a
	// user-invoked skill and must never be model-triggered.
	fm.Set("triggers", []string{triggerUser})
	if toolsPresent {
		fm.Set(allowedToolsKey, tools)
	}

	canonical := compile.PresentFields(map[string]bool{
		"name":           true,
		"description":    true,
		kindExtensionKey: true,
		"triggers":       true,
		// permissions is reserved unconditionally (see translateSkill): a
		// command has no typed permissions field, so an extension field must
		// never pass a permissions block through verbatim.
		"permissions": true,
		// allowed-tools is deliberately NOT reserved here. A command has no
		// typed allowed-tools field, so any restriction necessarily comes from
		// the extension field, is consumed by resolveAllowedTools, and is
		// emitted in mapped form. Nothing was overridden, so no conflict
		// warning is honest.
	})
	extraKeys, extraDiags := compile.ExtraKeys(c.Extra, canonical, mapping.TargetDevin, c.Name(), source)
	var diags []diag.Diagnostic
	diags = append(diags, toolDiags...)
	diags = append(diags, extraDiags...)
	// Command argument metadata (argument-hint) and any other preserved
	// extension field ride along unchanged so D8 can recover them.
	appendExtras(fm, c.Extra, extraKeys, toolsFromExtra)

	return renderSkillArtifact("command", c.Name(), fm, c.Body, diags)
}

// appendExtras writes the preserved extension fields in the order ExtraKeys
// returned them. When the allowed-tools restriction was consumed from the
// extension field, its raw source value is skipped: the canonical section
// already emitted the strictly mapped Devin equivalent, and re-emitting the
// unmapped value would write tool identifiers Devin does not accept.
func appendExtras(fm *compile.Frontmatter, extra map[string]any, keys []string, toolsFromExtra bool) {
	for _, key := range keys {
		if toolsFromExtra && key == allowedToolsKey {
			continue
		}
		fm.Set(key, extra[key])
	}
}

// renderSkillArtifact renders one Devin skill directory artifact. kind is used
// for structured logging only and never appears in the artifact.
func renderSkillArtifact(kind, name string, fm *compile.Frontmatter, body string, diags []diag.Diagnostic) ([]artifact.Artifact, []diag.Diagnostic, error) {
	frontmatter, err := fm.Render()
	if err != nil {
		return nil, nil, fmt.Errorf("devin: render %s frontmatter: %w", kind, err)
	}
	return []artifact.Artifact{{
		Path:  skillArtifactPath(name),
		Bytes: []byte(frontmatter + body),
	}}, diags, nil
}

// mapTriggers restricts activation to Devin's closed user/model vocabulary.
// An unrepresentable trigger is dropped with a warning, and the three branches
// below keep activation from ever ending up BROADER than the source asked for.
//
// This is deliberately NOT the same shape as the allowed-tools decision, and the
// difference is the reason `allowed-tools: []` was rejected as the fix for an
// unrepresentable tool restriction (bead agent-marketplace-pu6b): `triggers: []`
// only stops auto-activation and leaves the resource usable, while
// `allowed-tools: []` means it may use no tools at all. Emitting the narrowest
// representable value is safe here and inert there.
//
// Devin treats an absent `triggers` key as its default of
// user+model ("Skills may be invoked by users or the model",
// docs/devin-cli-harness-spec-3000.3.md), so omitting the key is only correct
// when the source expressed no opinion:
//
//   - nil triggers: omit the key and inherit Devin's default.
//   - some triggers map: emit the mapped subset (narrowing is safe) and warn
//     for each dropped trigger.
//   - a non-empty source list where nothing maps: emit `triggers: []`, the
//     narrowest representable value, and warn. Omitting the key here would
//     silently widen activation to Devin's default.
//
// Whether Devin 3000.3 accepts `triggers: []` is a runtime probe item for D12.
func mapTriggers(resource string, source diag.SourceRef, triggers []string) (mapped []string, present bool, diags []diag.Diagnostic) {
	if triggers == nil {
		return nil, false, nil
	}
	mapped = make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		switch trigger {
		case triggerUser, triggerModel:
			mapped = append(mapped, trigger)
		default:
			slog.Warn("devin trigger unmappable", "target", mapping.TargetDevin, "resource", resource, "trigger", trigger)
			diags = append(diags, diag.Diagnostic{
				Code:     diag.CodeSkillDevinTriggerUnmappable,
				Severity: diag.SeverityWarning,
				Target:   string(mapping.TargetDevin),
				Resource: resource,
				Message:  fmt.Sprintf("Devin has no %q activation trigger; the trigger was dropped", trigger),
				Hint:     "Devin activates skills on the user or model trigger only",
				Source:   source,
			})
		}
	}
	// mapped is always non-nil here, so an all-dropped non-empty source list
	// renders as `triggers: []` rather than omitting the key.
	return mapped, true, diags
}
