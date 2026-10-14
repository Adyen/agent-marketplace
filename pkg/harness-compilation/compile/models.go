package compile

import (
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/modelpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func SubagentModel(s *ir.Subagent, destination Target, opts Options) (modelpolicy.Result, []diag.Diagnostic, error) {
	req := modelpolicy.Request{Source: target.Target(s.ModelSource), Target: destination, Surface: "subagent", Model: s.Model, Effort: s.Effort, Overrides: opts.ModelOverride()}
	if s.ModelSource == "" {
		type native struct {
			target        Target
			model, effort string
		}
		var values []native
		if e := s.Ext.Claude; e != nil && (e.Model != "" || e.Effort != "") {
			values = append(values, native{TargetClaude, e.Model, e.Effort})
		}
		if e := s.Ext.Cursor; e != nil && (e.Model != "" || e.Effort != "") {
			values = append(values, native{TargetCursor, e.Model, e.Effort})
		}
		if e := s.Ext.Codex; e != nil && (e.Model != "" || e.ReasoningEffort != "") {
			values = append(values, native{TargetCodex, e.Model, e.ReasoningEffort})
		}
		if e := s.Ext.Factory; e != nil && e.ReasoningEffort != "" {
			values = append(values, native{target: TargetFactory, effort: e.ReasoningEffort})
		}
		var chosen *native
		for i := range values {
			if values[i].target == destination {
				chosen = &values[i]
				break
			}
		}
		if chosen == nil && len(values) == 1 {
			chosen = &values[0]
		}
		if chosen != nil {
			if chosen.model != "" {
				req.Model, req.Source = chosen.model, chosen.target
			}
			if chosen.effort != "" {
				if chosen.model == "" && chosen.target == destination {
					req.Effort = ""
					base, prior, err := resolveModel(s, s.Source, req)
					if err != nil {
						return base, prior, err
					}
					req.Model, req.Source, req.Effort, req.Overrides = base.Model, chosen.target, chosen.effort, nil
					resolved, diagnostics, err := resolveModel(s, s.Source, req)
					return resolved, append(prior, diagnostics...), err
				}
				req.Effort = chosen.effort
				if req.Model == "" || req.Model == "inherit" {
					req.Source = chosen.target
				}
			}
		}
		if chosen == nil && len(values) > 1 {
			result, diagnostics, err := resolveModel(s, s.Source, req)
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeModelProvenanceConflict, Severity: diag.SeverityWarning, Target: string(destination), Resource: s.Name(), Source: DiagSource(s.Source), Message: "Conflicting native model sidecars were ignored; canonical model fields were used"})
			return result, diagnostics, err
		}
	}
	return resolveModel(s, s.Source, req)
}

// CommandModel resolves a canonical command model selection. Harness catalogs
// declare one model vocabulary per harness, so commands use the subagent
// selection surface.
func CommandModel(c *ir.Command, model string, destination Target, opts Options) (modelpolicy.Result, []diag.Diagnostic, error) {
	return resolveModel(c, c.Source, modelpolicy.Request{Target: destination, Surface: "subagent", Model: model, Overrides: opts.ModelOverride()})
}

func SkillModel(s *ir.Skill, destination Target, opts Options) (modelpolicy.Result, []diag.Diagnostic, error) {
	return resolveModel(s, s.Source, modelpolicy.Request{Source: target.Target(s.ModelSource), Target: destination, Surface: "skill", Model: s.Model, Overrides: opts.ModelOverride()})
}

func resolveModel(resource ir.Resource, source ir.SourceRef, req modelpolicy.Request) (modelpolicy.Result, []diag.Diagnostic, error) {
	result, err := modelpolicy.Resolve(req)
	if err != nil {
		return result, nil, fmt.Errorf("resolve model catalog: %w", err)
	}
	var diagnostics []diag.Diagnostic
	for _, loss := range result.Losses {
		code := string(loss.Kind)
		severity := diag.SeverityWarning
		message := ""
		switch loss.Kind {
		case modelpolicy.ModelOmitted:
			message = fmt.Sprintf("model %q could not be resolved for %s; the model field was omitted and the harness default applies", loss.Value, req.Target)
			if req.Source == TargetCodex {
				code = diag.CodeSubagentCodexModelDropped
			} else if req.Target == TargetDevin {
				code = diag.CodeSubagentDevinModelUnmappable
				if req.Surface == "skill" {
					code = diag.CodeSkillDevinModelUnmappable
				}
			}
		case modelpolicy.EffortOmitted:
			message = fmt.Sprintf("reasoning preference %q could not be represented for %s; it was omitted", loss.Value, req.Target)
			if req.Source == TargetCodex {
				code = diag.CodeSubagentCodexEffortDropped
			} else if req.Target == TargetDevin {
				code = diag.CodeSubagentDevinEffortDropped
			}
		case modelpolicy.SelectionChanged:
			message = fmt.Sprintf("model inheritance/default selection cannot be preserved for %s; the model field was omitted", req.Target)
			if req.Target == TargetDevin && req.Surface == "subagent" {
				code = diag.CodeSubagentDevinModelDefault
				message = "Devin uses its default subagent model rather than inheriting the parent model"
			}
		case modelpolicy.EffortConflict:
			message = "explicit reasoning preference overrides a conflicting preference embedded in the model identifier"
		case modelpolicy.SelectorPinned:
			severity = diag.SeverityInfo
			message = fmt.Sprintf("floating model family %q was pinned to %q by the target catalog", loss.Value, result.Model)
		}
		diagnostics = append(diagnostics, diag.Diagnostic{Code: code, Severity: severity, Target: string(req.Target), Resource: resource.Name(), Message: message, Hint: "Update the shipped harness model catalog or use an explicit model override", Source: DiagSource(source)})
		slog.Debug("model selection resolved with loss", "target", req.Target, "resource", resource.Name(), "code", code)
	}
	return result, diagnostics, nil
}
