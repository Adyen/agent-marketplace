package devin

import (
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// maxNestingKey is Devin's documented flat-subagent nesting limit key
// (docs/devin-cli-harness-spec-3000.3.md, "Custom subagents": max-nesting).
// The plan makes it a typed Subagent IR field that "Devin round-trips".
const maxNestingKey = "max-nesting"

// builtinSubagentNames are the Devin built-in subagent identifiers. Evidence:
// "Built-in identifiers such as subagent_explore and subagent_general cannot be
// replaced" (docs/devin-cli-harness-spec-3000.3.md, "Custom subagents").
//
// Emitting agents/subagent_explore.md would produce a profile Devin ignores in
// favour of its built-in, so the user would believe a restricted profile is
// active while the unrestricted built-in actually runs. The plan therefore
// makes it an error with no artifact ("built-in name conflict: error; no
// artifact").
var builtinSubagentNames = map[string]struct{}{
	"subagent_explore": {},
	"subagent_general": {},
}

// subagentArtifactPath returns the canonical flat Devin subagent profile path
// for name, relative to the Devin target root.
//
// Plan, resource mapping: "Subagent | agents/<name>.md | Native flat
// custom-subagent profile". Devin also accepts agents/<name>/AGENT.md, but
// "Canonical compiler output is the flat form", and the flat file is the shape
// D10 materializes as a file symlink ("flat subagents use file symlinks").
func subagentArtifactPath(name string) string {
	return path.Join(subagentsDir, name+subagentSuffix)
}

// translateSubagent compiles a subagent to the canonical flat Devin profile
// agents/<name>.md.
//
// Fields that express a restriction Devin has NO FIELD FOR at all — isolation
// and disallowedTools — still fail the resource, as does a name that shadows a
// built-in. Fields that only express a preference are dropped with a warning.
//
// allowed-tools sits between those two and, since bead agent-marketplace-pu6b,
// is handled as the second case: an entry Devin cannot express omits the key
// with a warning instead of suppressing the profile, which does inherit Devin's
// default tool set. See resolveAllowedTools for the full rationale, including
// why Devin now diverges from Factory here.
func translateSubagent(s *ir.Subagent, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	source := compile.DiagSource(s.Source)
	name := s.Name()

	// Fail-closed checks first, in a fixed order so a resource with several
	// problems reports the same diagnostic sequence on every run. They are all
	// collected rather than short-circuited: a user fixing an unrepresentable
	// subagent should see every blocking reason in one compile.
	var errs []diag.Diagnostic
	errs = append(errs, nestedNameDiagnostics(name, source)...)
	errs = append(errs, builtinNameDiagnostics(name, source)...)
	errs = append(errs, securityRestrictionDiagnostics(s, source)...)

	// An unrepresentable allowed-tools restriction is NOT one of the fail-closed
	// checks any more (bead agent-marketplace-pu6b): it omits the key, warns,
	// and still publishes the profile. It is resolved here rather than below the
	// suppression gate so a subagent that is unrepresentable for several reasons
	// still reports the tool loss alongside the blocking ones — the user should
	// see every finding in one compile even when one of them costs the artifact.
	tools, toolsPresent, toolsFromExtra, toolDiags := resolveAllowedTools(name, source, s.Tools, s.Tools != nil, s.Extra, opts, diag.CodeSubagentDevinToolUnknown)

	if len(errs) > 0 {
		// Plan, subagent-only rules for Isolation, DisallowedTools, and
		// built-in name conflicts: no artifact is emitted for any of them.
		// Those three are genuinely unrepresentable restrictions or a shadowed
		// built-in, not an advisory tool list, so they still fail closed.
		return nil, append(errs, suppressedToolDiagnostics(toolDiags)...), nil
	}

	var diags []diag.Diagnostic
	diags = append(diags, toolDiags...)
	model, modelDiags := mapSubagentModel(name, source, s.Model, opts)
	diags = append(diags, modelDiags...)
	diags = append(diags, effortDiagnostics(s, source)...)

	fm := &compile.Frontmatter{}
	fm.Set("name", name)
	fm.Set("description", s.Description)
	if model != "" {
		fm.Set("model", model)
	}
	if toolsPresent {
		fm.Set(allowedToolsKey, tools)
	}
	if s.MaxNesting != nil {
		// Typed and round-tripped, including an explicit 0: the plan requires
		// "The Subagent IR gains a typed MaxNesting field. Devin round-trips
		// it". 0 is the strictest value, so treating it as absent would raise
		// the nesting limit to Devin's default.
		fm.Set(maxNestingKey, *s.MaxNesting)
	}

	// Unknown-field precedence, plan: "Canonical modeled fields win over
	// conflicting values in preserved extras. The compiler emits a stable
	// warning naming the conflicting field without including sensitive
	// content."
	canonical := compile.PresentFields(map[string]bool{
		"name":        true,
		"description": true,
		// model is reserved UNCONDITIONALLY, unlike the skill compiler. An
		// absent subagent model is not "no opinion": it selects Devin's default
		// subagent model and carries its own warning. Letting an extension
		// field supply a model would both make that warning a lie and emit a
		// model identifier that never passed the stable-alias check.
		"model": true,
		// allowed-tools is a canonical override only when the typed IR field is
		// set. When the restriction came from the extension field it was
		// consumed and mapped through the Devin vocabulary (see
		// resolveAllowedTools), so reporting it as "overridden" would be false
		// and would invite users to delete a restriction that was honored.
		allowedToolsKey: s.Tools != nil,
		// The remaining keys are reserved unconditionally because each one
		// would otherwise let an unverified extension value change a limit or a
		// restriction:
		//
		//   - max-nesting bounds recursive delegation, so an extension copy
		//     could raise a limit this compiler did not validate.
		//   - tools is the Claude/Gemini spelling of a tool restriction. Devin
		//     ignores it, so emitting it verbatim would show a restriction that
		//     is not enforced — effectively broadening privilege.
		//   - disallowedTools and isolation are the two restrictions that fail
		//     this resource outright. Re-emitting them from extras would fake a
		//     security control Devin does not implement.
		//   - effort is dropped with a warning; passing an extension copy
		//     through would contradict the diagnostic.
		maxNestingKey:     true,
		"tools":           true,
		"disallowedTools": true,
		"isolation":       true,
		"effort":          true,
	})
	extraKeys, extraDiags := compile.ExtraKeys(s.Extra, canonical, mapping.TargetDevin, name, source)
	diags = append(diags, extraDiags...)
	appendExtras(fm, s.Extra, extraKeys, toolsFromExtra)

	frontmatter, err := fm.Render()
	if err != nil {
		return nil, nil, fmt.Errorf("devin: render subagent frontmatter: %w", err)
	}
	return []artifact.Artifact{{
		Path:  subagentArtifactPath(name),
		Bytes: []byte(frontmatter + s.Body),
	}}, diags, nil
}

// suppressedToolDiagnostics restates an allowed-tools warning for a subagent
// that a fail-closed check already suppressed.
//
// resolveAllowedTools cannot know whether its caller will publish, so it words
// the loss for the normal case: "the allowed-tools key was omitted and Devin's
// default tool set applies". On this path no artifact exists at all, so that
// sentence describes a profile the user does not have and would send them
// looking on disk for an over-privileged install that was never written. The
// code and severity are unchanged — only the outcome clause is corrected — so a
// consumer UI keying off the code sees no difference.
func suppressedToolDiagnostics(diags []diag.Diagnostic) []diag.Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	restated := make([]diag.Diagnostic, 0, len(diags))
	for _, d := range diags {
		if cause, _, found := strings.Cut(d.Message, "; "+AllowedToolsDefaultSetClause); found {
			d.Message = cause + "; " + allowedToolsSuppressedClause
		}
		d.Hint = "Fix the blocking error reported alongside this warning; the allowed-tools loss is reported now so both can be fixed in one pass"
		restated = append(restated, d)
	}
	return restated
}

// nestedNameDiagnostics rejects a name the flat Devin layout cannot express.
//
// Plan: "Subagent | agents/<name>.md | Native flat custom-subagent profile",
// and the evidence baseline adds "Canonical compiler output is the flat form".
// A name such as `team/reviewer` would either write agents/team/reviewer.md,
// which Devin's flat discovery does not read, or be flattened to
// agents/team-reviewer.md, which can silently collide with a real
// `team-reviewer` subagent and replace its profile. Neither is acceptable, so
// the resource fails and the collision is reported instead of invented.
//
// ir.ValidateName is the outer gate: today it restricts resource names to
// letters, digits, hyphens, and underscores, so compile.Compile rejects a
// nesting name before this translator runs. This check is the layer-local
// invariant, not a duplicate of that gate — it makes the flat-layout
// requirement explicit at the only place that turns a name into a path, so a
// future relaxation of the name grammar (or any direct caller inside this
// package) cannot silently start flattening collisions.
func nestedNameDiagnostics(name string, source diag.SourceRef) []diag.Diagnostic {
	// A separator in either flavour expresses a directory, and a bare `.` or
	// `..` resolves to the agents directory itself rather than to a profile
	// inside it. path.Join would quietly absorb both.
	if !strings.ContainsAny(name, `/\`) && name != "." && name != ".." {
		return nil
	}
	slog.Error("devin subagent name expresses nesting", "target", mapping.TargetDevin, "resource", name)
	return []diag.Diagnostic{{
		Code:     diag.CodeSubagentDevinNestedNameUnsupported,
		Severity: diag.SeverityError,
		Target:   string(mapping.TargetDevin),
		Resource: name,
		Message:  "subagent name expresses nesting, which Devin's flat agents directory cannot represent; artifact suppressed",
		Hint:     "Rename the subagent to a flat name: Devin discovers only agents/<name>.md, and flattening the name could silently collide with another subagent",
		Source:   source,
	}}
}

// builtinNameDiagnostics rejects a name that shadows a Devin built-in subagent.
//
// Plan, subagent-only fields: "built-in name conflict: error; no artifact".
// The comparison is case-insensitive: the artifact path is a filename, and on
// the case-insensitive filesystems Devin runs on, agents/Subagent_Explore.md
// and agents/subagent_explore.md are the same file. Matching exactly would let
// a differently-cased name produce the very profile this rule exists to
// prevent.
func builtinNameDiagnostics(name string, source diag.SourceRef) []diag.Diagnostic {
	if _, builtin := builtinSubagentNames[strings.ToLower(name)]; !builtin {
		return nil
	}
	slog.Error("devin subagent name collides with a built-in", "target", mapping.TargetDevin, "resource", name)
	return []diag.Diagnostic{{
		Code:     diag.CodeSubagentDevinBuiltinNameConflict,
		Severity: diag.SeverityError,
		Target:   string(mapping.TargetDevin),
		Resource: name,
		Message:  "subagent name collides with a Devin built-in subagent, which cannot be replaced; artifact suppressed",
		Hint:     "Rename the subagent so it does not shadow a Devin built-in such as subagent_explore or subagent_general",
		Source:   source,
	}}
}

// securityRestrictionDiagnostics rejects the two subagent restrictions Devin
// has no field for.
//
// Plan, subagent-only fields: "Isolation: error; no artifact" and
// "DisallowedTools: error; no artifact", under the general rule that
// "Security-affecting restrictions [...] fail a resource when a target cannot
// represent them; they are never silently narrowed, broadened, or dropped."
//
// That general rule was amended for allowed-tools ONLY, by bead
// agent-marketplace-pu6b: an allowed-tools entry Devin cannot express now omits
// the key with a warning. These two fields are unaffected and keep the original
// contract. The distinction is that Devin has no isolation or disallowedTools
// field at all, so emitting a profile without them advertises a sandbox and a
// denylist that do not exist, whereas allowed-tools IS a Devin field whose
// absence has a documented, honest meaning: Devin's default tool set. Absence is
// therefore a truthful statement about the emitted profile, while an omitted
// isolation mode is not.
//
// Neither diagnostic nor log quotes the values: DisallowedTools carries the
// same scoped permission strings as allowed-tools, and an isolation mode names
// a sandbox strategy that the plan's observability section keeps out of logs.
// Resource and Source localize the failure instead.
func securityRestrictionDiagnostics(s *ir.Subagent, source diag.SourceRef) []diag.Diagnostic {
	var diags []diag.Diagnostic
	if len(s.DisallowedTools) > 0 {
		slog.Error("devin subagent restriction unrepresentable", "target", mapping.TargetDevin, "resource", s.Name(), "field", "disallowedTools")
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeSubagentSecurityRestrictionUnsupported,
			Severity: diag.SeverityError,
			Target:   string(mapping.TargetDevin),
			Resource: s.Name(),
			Message:  "Devin cannot represent disallowedTools; subagent artifact suppressed",
			Hint:     "Express the restriction as an allowed-tools list Devin can enforce, or use a target that supports disallowedTools",
			Source:   source,
		})
	}
	if s.Isolation != "" {
		slog.Error("devin subagent restriction unrepresentable", "target", mapping.TargetDevin, "resource", s.Name(), "field", "isolation")
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeSubagentSecurityRestrictionUnsupported,
			Severity: diag.SeverityError,
			Target:   string(mapping.TargetDevin),
			Resource: s.Name(),
			Message:  "Devin cannot represent isolation; subagent artifact suppressed",
			Hint:     "Remove the isolation mode explicitly or use a target that supports it",
			Source:   source,
		})
	}
	return diags
}

// effortDiagnostics reports the dropped reasoning-effort preference.
//
// Plan, subagent-only fields: "Effort: warning and drop". Effort selects how
// much reasoning a subagent spends; it grants no capability, so dropping it
// cannot widen the privilege or activation surface and does not fail the
// resource. The value is included because it is a documented effort level, not
// a command string or permission value.
func effortDiagnostics(s *ir.Subagent, source diag.SourceRef) []diag.Diagnostic {
	if s.Effort == "" {
		return nil
	}
	slog.Warn("devin subagent effort dropped", "target", mapping.TargetDevin, "resource", s.Name(), "effort", s.Effort)
	return []diag.Diagnostic{{
		Code:     diag.CodeSubagentDevinEffortDropped,
		Severity: diag.SeverityWarning,
		Target:   string(mapping.TargetDevin),
		Resource: s.Name(),
		Message:  fmt.Sprintf("Devin has no subagent effort field; the %q effort level was dropped", s.Effort),
		Hint:     "Remove effort or use a target that supports it",
		Source:   source,
	}}
}

// mapSubagentModel resolves the subagent model and reports the model losses the
// plan requires.
//
// Plan, models: "An unmappable explicit model is omitted and emits a warning.
// An empty subagent model stays empty and emits a warning because Devin
// interprets absence as its default subagent model, not parent-model
// inheritance."
//
// Droid spells parent-model inheritance as the explicit model value `inherit`.
// Devin has no equivalent concept, so `inherit` is treated as unset and Devin's
// default subagent model is used. No warning is emitted because the source
// explicitly asked for inheritance, not a concrete model alias.
//
// These are deliberately separate codes for the remaining two cases.
// subagent.devin.model-unmappable says "you asked for a model this target
// cannot name"; subagent.devin.model-default says "you asked for nothing, and
// on Devin that is a concrete choice rather than inheritance". Collapsing them
// would hide the second case, which is the one a user migrating from an
// inheriting harness gets wrong.
//
// An unmappable model degrades to the same default, so the unmappable case does
// not additionally emit the default warning: the model-unmappable diagnostic
// already tells the user no model was written.
func mapSubagentModel(resource string, source diag.SourceRef, model string, opts compile.Options) (string, []diag.Diagnostic) {
	if model == "" {
		slog.Warn("devin subagent model absent", "target", mapping.TargetDevin, "resource", resource)
		return "", []diag.Diagnostic{{
			Code:     diag.CodeSubagentDevinModelDefault,
			Severity: diag.SeverityWarning,
			Target:   string(mapping.TargetDevin),
			Resource: resource,
			Message:  "no subagent model was set, so Devin uses its default subagent model rather than inheriting the parent model",
			Hint:     "Set an explicit stable Devin model alias (sonnet, opus, or haiku) when a fixed model is required",
			Source:   source,
		}}
	}
	if strings.EqualFold(model, "inherit") {
		// `model: inherit` means "use the invoking agent's model" in Droid, but
		// Devin has no parent-model inheritance. Treat it as unset so Devin uses
		// its default subagent model. Honor an explicit model override if the
		// caller provided one keyed by the original `inherit` value.
		if override, ok := opts.ModelOverride()[model]; ok && override != "" {
			return override, nil
		}
		slog.Debug("devin subagent model is inherit; using default subagent model", "target", mapping.TargetDevin, "resource", resource)
		return "", nil
	}
	return mapModel(resource, source, model, opts, diag.CodeSubagentDevinModelUnmappable)
}
