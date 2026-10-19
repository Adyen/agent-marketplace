package devin

import (
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/toolpolicy"
)

// Frontmatter keys this package interprets. Each one is either mapped into a
// typed IR field or deliberately dropped; none of them may be left in Extra.
//
// Leaving any of them in Extra would be actively harmful rather than merely
// untidy: compile/devin reserves every one of these keys, so a recompile would
// emit compile.canonical-field-overrides-extra for a conflict the ingester
// invented, and the user would be told to delete a field they never wrote.
const (
	// kindExtensionKey is the stable command-provenance marker the Devin
	// compiler writes (compile/devin/skill.go). Plan: "The Devin ingester
	// interprets that value as command provenance. An ordinary Devin skill
	// remains a skill."
	kindExtensionKey = "x-agent-marketplace-kind"
	commandKindValue = "command"

	allowedToolsKey  = "allowed-tools"
	toolsKey         = "tools"
	triggersKey      = "triggers"
	argumentHintKey  = "argument-hint"
	modelKey         = "model"
	permissionsKey   = "permissions"
	subagentKey      = "subagent"
	agentKey         = "agent"
	maxNestingKey    = "max-nesting"
	altMaxNestingKey = "maxNesting"

	// triggerUser is the only trigger a converted command may carry. The Devin
	// command compiler writes exactly `triggers: [user]`.
	triggerUser = "user"
)

// loadSkills reads skills/<name>/SKILL.md and recovers each resource's kind.
//
// A missing skills directory is not an error: a Devin root may legitimately
// contain only hooks or only MCP servers.
func loadSkills(src fs.FS, r root) ([]ir.Resource, []diag.Diagnostic, error) {
	dirs, err := listSkillDirs(src, r)
	if err != nil {
		if isNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("ingest/devin: walk %s: %w", r.join(skillsDir), err)
	}

	var resources []ir.Resource
	var diags []diag.Diagnostic
	for _, dir := range dirs {
		file := path.Join(dir, skillFile)
		skill, err := bundle.LoadSkill(src, dir)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestDevinLoadSkillFailed, "skill", dir, file, err))
			continue
		}
		resource, resourceDiags, ok := canonicalizeSkill(skill, file)
		diags = append(diags, resourceDiags...)
		if !ok {
			continue
		}
		resources = append(resources, resource)
	}
	return resources, diags, nil
}

// canonicalizeSkill turns a parsed Devin SKILL.md into the IR resource its
// provenance marker selects, translating Devin's vocabularies back to canonical
// ones on the way.
//
// bundle.LoadSkill does the YAML work and already lifts Devin's own field names
// (name, description, triggers, argument-hint, model, allowed-tools,
// permissions, subagent, agent) out of Extra into typed fields, because Devin's
// skill frontmatter is agentskills-compatible and uses the same spellings. What
// it cannot know is that two of those fields carry DEVIN vocabulary rather than
// canonical vocabulary, and that one extension key changes the resource's kind.
// That is exactly what this function fixes.
//
// ok is false when the resource must not be ingested at all.
func canonicalizeSkill(skill *ir.Skill, file string) (ir.Resource, []diag.Diagnostic, bool) {
	name := skill.Name()

	// allowed-tools is emitted in DEVIN vocabulary by compile/devin, so the
	// names stay verbatim and ToolSource records where they came from; the
	// tool catalog validates them against Devin's closed skill surface.
	// Skipping that validation would produce IR whose recompilation reports
	// skill.devin.tool-unknown on a list this very ingester produced, and since
	// bead agent-marketplace-pu6b that code drops the whole allowed-tools key
	// instead of failing, so the restriction would be silently lost rather than
	// reported as a hard error.
	tools, toolDiags := validateDevinTools(skill.AllowedTools, toolpolicy.SurfaceSkillAllowedTools, name, file)
	if len(toolDiags) > 0 {
		if kind, ok := skill.Extra[kindExtensionKey]; ok && kind == commandKindValue {
			if refusal, refused := commandControlRefusal(skill, file); refused {
				toolDiags = append(toolDiags, refusal)
			}
		}
		return nil, toolDiags, false
	}
	skill.AllowedTools = tools
	if tools != nil {
		skill.ToolSource = "devin"
	}

	skill.ModelSource = "devin"
	var diags []diag.Diagnostic

	// The provenance marker is ALWAYS consumed, whatever its value. Leaving it
	// in Extra would make every recompile emit
	// compile.canonical-field-overrides-extra, because compile/devin reserves
	// the key unconditionally on both skills and commands. An unrecognized
	// value therefore cannot be preserved either, so it is reported.
	kind, hasKind := skill.Extra[kindExtensionKey]
	if hasKind {
		delete(skill.Extra, kindExtensionKey)
		if len(skill.Extra) == 0 {
			skill.Extra = nil
		}
		if kind != commandKindValue {
			diags = append(diags, reservedFieldDropped(name, file, kindExtensionKey,
				"the only value this target interprets is \"command\"; an unrecognized provenance value cannot be preserved because the compiler owns this key"))
			hasKind = false
		}
	}

	if !hasKind {
		// An ordinary Devin skill. Every canonical key was already consumed by
		// bundle.LoadSkill, so Extra holds only genuine unknown extension
		// fields.
		//
		// A `triggers: []` list arrives here as an explicit EMPTY list and is
		// preserved as one. That is the honest reading: the compiler writes
		// `triggers: []` when a source trigger list mapped to nothing, and the
		// file now says "no activation trigger". Reconstructing the original
		// unmappable trigger names is impossible — they were dropped with a
		// warning at compile time — and widening the empty list back to Devin's
		// user+model default would grant activation the file denies.
		slog.Debug("devin skill ingested", "source", ingest.HarnessDevin, "resource", name, "file", file)
		return skill, diags, true
	}

	if refusal, refused := commandControlRefusal(skill, file); refused {
		return nil, []diag.Diagnostic{refusal}, false
	}
	command, commandDiags := commandFromSkill(skill, file)
	diags = append(diags, commandDiags...)
	slog.Debug("devin command provenance recovered", "source", ingest.HarnessDevin, "resource", name, "file", file)
	return command, diags, true
}

// commandFromSkill recovers an ir.Command from a Devin skill that carries the
// command-provenance marker.
//
// The marker itself is CONSUMED, never preserved: re-emitting it from Extra
// would collide with the canonical marker the command compiler writes.
//
// ir.Command has no typed argument-hint, model, permissions, subagent, or agent
// field, so each of those is handled explicitly:
//
//   - argument-hint is restored into Extra. It is the "command argument
//     metadata where available" the plan requires to round-trip, and the
//     command compiler emits it from Extra, so this is the exact inverse.
//   - model is DROPPED with a warning. It can only appear on a hand-written
//     file, because the Devin command compiler never writes it, and putting it
//     back into Extra would pass an unvalidated model straight into the next
//     artifact behind the compiler's back.
//   - permissions, subagent, agent, and triggers without `user` never reach
//     this function: commandControlRefusal refuses the resource, because
//     dropping them would change what it may do or who may invoke it.
//   - triggers is dropped. `[user]` is exactly what the compiler writes and is
//     restored from the kind, so it is dropped silently; any other list that
//     includes `user` only narrows activation to user-only and is reported.
func commandFromSkill(skill *ir.Skill, file string) (*ir.Command, []diag.Diagnostic) {
	name := skill.Name()

	extra := make(map[string]any, len(skill.Extra)+2)
	for key, value := range skill.Extra {
		extra[key] = value
	}
	if skill.ArgumentHint != "" {
		extra[argumentHintKey] = skill.ArgumentHint
	}
	if skill.AllowedTools != nil {
		// ir.Command has no ToolSource field, so the restriction is reversed
		// through the tool catalogs to canonical Claude IDs: the command
		// compiler reads its tool restriction from this extension key and maps
		// it forward through the Devin vocabulary. Validation above guarantees
		// every name is covered, so uncovered can only be empty.
		emitted, _, err := toolpolicy.TranslateToolList(mapping.TargetDevin, mapping.TargetClaude, toolpolicy.SurfaceSkillAllowedTools, skill.AllowedTools, nil)
		if err != nil {
			return nil, []diag.Diagnostic{ingest.LoadFailure(diag.CodeIngestDevinLoadSubagentFailed, "command", name, file, err)}
		}
		extra[allowedToolsKey] = emitted
	}
	if len(extra) == 0 {
		extra = nil
	}

	var diags []diag.Diagnostic
	dropped := func(field string) {
		slog.Warn("devin command field dropped", "source", ingest.HarnessDevin, "resource", name, "field", field)
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeIngestDevinCommandFieldDropped,
			Severity: diag.SeverityWarning,
			Resource: name,
			Message:  fmt.Sprintf("the command-provenance skill declares %q, which the canonical command IR cannot hold; the field was dropped", field),
			Hint:     "Remove the field, or remove x-agent-marketplace-kind: command so the resource is ingested as a skill",
			Source:   diag.SourceRef{File: file, Line: 1},
		})
	}

	if skill.Triggers != nil && (len(skill.Triggers) != 1 || skill.Triggers[0] != triggerUser) {
		dropped(triggersKey)
	}
	if skill.Model != "" {
		dropped(modelKey)
	}

	command := ir.NewCommand(name, skill.Description, skill.Body, extra, ir.SourceRef{File: file, Line: 1})
	if skill.AllowedTools != nil {
		command.ToolSource = "devin"
	}
	return command, diags
}

func commandControlRefusal(skill *ir.Skill, file string) (diag.Diagnostic, bool) {
	// Fixed field order so a file with several unrepresentable fields reports
	// the same diagnostic sequence on every run.
	var fields []string
	if skill.Triggers != nil && !slices.Contains(skill.Triggers, triggerUser) {
		fields = append(fields, triggersKey)
	}
	if skill.PermissionsSet {
		fields = append(fields, permissionsKey)
	}
	if skill.Subagent != "" {
		fields = append(fields, subagentKey)
	}
	if skill.Agent != "" {
		fields = append(fields, agentKey)
	}
	if len(fields) == 0 {
		return diag.Diagnostic{}, false
	}
	slog.Warn("devin command control unrepresentable", "source", ingest.HarnessDevin, "resource", skill.Name(), "fields", fields)
	return diag.Diagnostic{
		Code:     diag.CodeIngestDevinCommandSecurityUnsupported,
		Severity: diag.SeverityError,
		Resource: skill.Name(),
		Message:  fmt.Sprintf("the command-provenance skill declares %s, which a converted command cannot hold; the resource was not ingested", strings.Join(fields, ", ")),
		Hint:     "Remove x-agent-marketplace-kind: command so the resource is ingested as a skill with its controls, or remove the control",
		Source:   diag.SourceRef{File: file, Line: 1},
	}, true
}

// loadSubagents reads the flat agents/<name>.md profiles.
//
// Plan: "Subagent | agents/<name>.md | Native flat custom-subagent profile",
// and the evidence baseline adds "Canonical compiler output is the flat form".
// Devin also accepts agents/<name>/AGENT.md; that form is reported rather than
// ingested, because this target neither writes nor materializes it and silently
// skipping a profile would make a converted bundle look complete.
func loadSubagents(src fs.FS, r root) ([]ir.Resource, []diag.Diagnostic, error) {
	files, directoryForms, err := listSubagentFiles(src, r)
	if err != nil {
		if isNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("ingest/devin: walk %s: %w", r.join(agentsDir), err)
	}

	var diags []diag.Diagnostic
	for _, file := range directoryForms {
		slog.Warn("devin subagent directory form skipped", "source", ingest.HarnessDevin, "file", file)
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeIngestDevinSubagentDirectoryFormSkipped,
			Severity: diag.SeverityWarning,
			Resource: path.Base(path.Dir(file)),
			Message:  fmt.Sprintf("%q uses the Devin subagent directory form, which this target does not ingest; the profile was skipped", file),
			Hint:     "Canonical Devin output is the flat agents/<name>.md form; convert the profile to a flat file so it is ingested",
			Source:   diag.SourceRef{File: file, Line: 1},
		})
	}

	var resources []ir.Resource
	for _, file := range files {
		subagent, err := bundle.LoadSubagent(src, file)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestDevinLoadSubagentFailed, "subagent", file, file, err))
			continue
		}
		subagentDiags, ok := canonicalizeSubagent(subagent, file)
		diags = append(diags, subagentDiags...)
		if !ok {
			continue
		}
		resources = append(resources, subagent)
	}
	return resources, diags, nil
}

// canonicalizeSubagent translates a parsed Devin flat profile into canonical IR.
//
// The one shape difference from skills is the tool key. Devin spells a subagent
// tool restriction `allowed-tools` (evidence baseline, "Custom subagents":
// supported fields are name, description, model, allowed-tools, max-nesting),
// while the canonical IR field is Subagent.Tools, which bundle.LoadSubagent
// fills from the CLAUDE spelling `tools`. So `allowed-tools` arrives in Extra
// and has to be lifted out, reversed, and installed as Tools.
//
// `tools` is dropped when it appears. Devin does not read it, so it enforces
// nothing; preserving it as an extra would make the Devin compiler re-emit a
// restriction that looks enforced and is not, which is the "never silently
// broaden" rule in its most misleading form. compile/devin reserves the key
// unconditionally for exactly this reason.
//
// effort, isolation, and disallowedTools are NOT dropped. bundle.LoadSubagent
// lifts them into typed IR fields, and each has an exact canonical meaning;
// keeping them typed is lossless and makes a later Devin compile fail closed
// (isolation and disallowedTools are hard errors there) instead of quietly
// re-emitting an unenforced field.
//
// ok is false when the resource must not be ingested at all.
func canonicalizeSubagent(subagent *ir.Subagent, file string) ([]diag.Diagnostic, bool) {
	name := subagent.Name()
	var diags []diag.Diagnostic

	raw, hasAllowedTools := subagent.Extra[allowedToolsKey]
	if hasAllowedTools {
		delete(subagent.Extra, allowedToolsKey)
		declared, err := toStringSlice(raw)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestDevinLoadSubagentFailed, "subagent", name, file,
				fmt.Errorf("invalid %s: %w", allowedToolsKey, err)))
			return diags, false
		}
		if subagent.Tools != nil {
			diags = append(diags, reservedFieldDropped(name, file, toolsKey,
				"Devin reads allowed-tools, not tools; the tools value was dropped in favour of the allowed-tools restriction"))
		}
		tools, toolDiags := validateDevinTools(declared, toolpolicy.SurfaceSubagentTools, name, file)
		if len(toolDiags) > 0 {
			return append(diags, toolDiags...), false
		}
		subagent.Tools = tools
		subagent.ToolSource = "devin"
	} else if subagent.Tools != nil {
		diags = append(diags, reservedFieldDropped(name, file, toolsKey,
			"Devin does not read the tools key, so the restriction it declares is not enforced and was not carried into canonical IR"))
		subagent.Tools = nil
	}

	subagent.ModelSource = "devin"

	if len(subagent.Extra) == 0 {
		subagent.Extra = nil
	}
	slog.Debug("devin subagent ingested", "source", ingest.HarnessDevin, "resource", name, "file", file)
	return diags, true
}

// reservedFieldDropped reports a frontmatter key that was neither mapped into
// typed IR nor preserved as an extra, because compile/devin reserves it and
// would refuse to re-emit it anyway. Only the field NAME is reported.
func reservedFieldDropped(resource, file, field, message string) diag.Diagnostic {
	slog.Warn("devin reserved field dropped", "source", ingest.HarnessDevin, "resource", resource, "field", field)
	return diag.Diagnostic{
		Code:     diag.CodeIngestDevinReservedFieldDropped,
		Severity: diag.SeverityWarning,
		Resource: resource,
		Message:  fmt.Sprintf("%q: %s", field, message),
		Hint:     "This frontmatter key is owned by the Devin compiler and cannot be carried through as an extension field",
		Source:   diag.SourceRef{File: file, Line: 1},
	}
}

// toStringSlice accepts the YAML shapes a tool list can take. It is
// deliberately strict: a shape it does not recognize is an error rather than a
// guess, because guessing rewrites an explicit tool restriction.
func toStringSlice(value any) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		result := make([]string, 0, len(typed))
		for i, element := range typed {
			text, ok := element.(string)
			if !ok {
				return nil, fmt.Errorf("list item %d has unsupported type %T", i, element)
			}
			result = append(result, text)
		}
		return result, nil
	case string:
		return []string{typed}, nil
	default:
		return nil, fmt.Errorf("value must be a string list, got %T", value)
	}
}
