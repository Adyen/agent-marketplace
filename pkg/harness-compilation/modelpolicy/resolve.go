package modelpolicy

import (
	"slices"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type Request struct {
	Source    target.Target
	Target    target.Target
	Surface   string
	Model     string
	Effort    string
	Overrides map[string]string
}

type LossKind string

const (
	ModelOmitted     LossKind = "model.unresolved"
	EffortOmitted    LossKind = "model.reasoning-omitted"
	SelectionChanged LossKind = "model.inheritance-mismatch"
	EffortConflict   LossKind = "model.reasoning-conflict"
	SelectorPinned   LossKind = "model.selector-pinned"
)

type Loss struct {
	Kind  LossKind
	Value string
}

type Result struct {
	Model       string
	Effort      string
	EffortField string
	Losses      []Loss
}

func Resolve(req Request) (Result, error) {
	r, err := shipped()
	if err != nil {
		return Result{}, err
	}
	return r.resolve(req), nil
}

func (r *registry) resolve(req Request) Result {
	if req.Surface == "" {
		req.Surface = "subagent"
	}
	c := r.catalogs[req.Target]
	surf, supported := c.Surfaces[req.Surface]
	result := Result{EffortField: surf.EffortField}
	if !supported {
		if req.Model != "" {
			result.lose(ModelOmitted, req.Model)
		}
		if req.Effort != "" {
			result.lose(EffortOmitted, req.Effort)
		}
		return result
	}
	if override, ok := req.Overrides[req.Model]; ok {
		req.Model, req.Source, req.Overrides = override, req.Target, nil
		result = r.resolve(req)
		if override != "" {
			if _, known := c.native[Encoding{Model: override}]; !known && req.Effort != "" {
				result.Effort = ""
				result.lose(EffortOmitted, req.Effort)
			}
		}
		return result
	}
	source := req.Source
	if source == "" {
		source = target.TargetClaude
	}
	sourceCatalog := r.catalogs[source]
	sourceSurface := sourceCatalog.Surfaces[req.Surface]
	if req.Model == "" || req.Model == "inherit" {
		selectionMode := sourceSurface.OmittedModel
		if selectionMode == "" || req.Model == "inherit" {
			selectionMode = "inherit"
		}
		if selectionMode == "inherit" && surf.InheritModel != nil {
			if req.Model != "" {
				result.Model = *surf.InheritModel
			}
		} else if selectionMode != surf.OmittedModel {
			result.lose(SelectionChanged, req.Model)
		}
		if req.Source == req.Target && surf.EffortField != "" && len(surf.InheritedEfforts) > 0 {
			result.Effort = req.Effort
		} else {
			result.inheritedEffort(surf, req.Effort)
		}
		return result
	}
	selected, known := sourceCatalog.native[Encoding{Model: req.Model, Effort: req.Effort}]
	nativeEncodingKnown := known
	if !known {
		selected, known = sourceCatalog.native[Encoding{Model: req.Model}]
		if known && req.Effort != "" {
			if selected.Effort != "" && selected.Effort != req.Effort {
				result.lose(EffortConflict, req.Effort)
			}
			selected.Effort = req.Effort
		}
	}
	if !known && req.Source == req.Target && surf.EffortField != "" {
		if _, available := sourceCatalog.Families[req.Model]; available {
			selected, known = selection{Identity: req.Model, Effort: req.Effort}, true
		}
	}
	if !known {
		id := r.portable[req.Model]
		if id == "" {
			for _, entry := range r.catalogs {
				if _, ok := entry.Models[req.Model]; ok {
					id = req.Model
					break
				}
			}
		}
		if id != "" {
			selected, known = selection{Identity: id, Effort: req.Effort}, true
		}
	}
	if req.Source == req.Target && len(result.Losses) == 0 {
		result.Model = req.Model
		if req.Effort != "" {
			if surf.EffortField != "" {
				result.Effort = req.Effort
				if known && !nativeEncodingKnown {
					entry, available := c.lookup(selected.Identity)
					if entry.Pin != "" {
						entry = c.Models[entry.Pin]
					}
					if _, supported := c.native[Encoding{Model: entry.Model, Effort: req.Effort}]; available && !supported {
						result.Effort = ""
						result.lose(EffortOmitted, req.Effort)
					}
				}
			} else if known {
				entry, _ := c.lookup(selected.Identity)
				if encoding, ok := entry.Reasoning[selected.Effort]; ok && req.Surface == "subagent" {
					if encoding.Model != "" {
						result.Model = encoding.Model
					}
				} else {
					result.lose(EffortOmitted, req.Effort)
				}
			} else {
				result.lose(EffortOmitted, req.Effort)
			}
		}
		return result
	}
	entry, available := c.lookup(selected.Identity)
	if known && available && entry.Pin != "" {
		entry = c.Models[entry.Pin]
		result.lose(SelectorPinned, selected.Identity)
	}
	if !known || !available {
		result.lose(ModelOmitted, req.Model)
		result.inheritedEffort(surf, selected.Effort)
		if !known && req.Effort != "" {
			result.inheritedEffort(surf, req.Effort)
		}
		return result
	}
	result.Model = entry.Model
	if selected.Effort != "" {
		if _, ok := entry.Reasoning[selected.Effort]; ok && req.Surface == "subagent" {
			encoding := entry.encoding(selected.Effort)
			result.Model, result.Effort = encoding.Model, encoding.Effort
		} else {
			result.lose(EffortOmitted, selected.Effort)
		}
	}
	if result.Model == "" {
		result.lose(ModelOmitted, req.Model)
	}
	return result
}

func (r *Result) inheritedEffort(s surface, effort string) {
	if effort == "" {
		return
	}
	if s.EffortField != "" && slices.Contains(s.InheritedEfforts, effort) {
		r.Effort = effort
	} else {
		r.lose(EffortOmitted, effort)
	}
}

func (r *Result) lose(kind LossKind, value string) {
	loss := Loss{Kind: kind, Value: value}
	if !slices.Contains(r.Losses, loss) {
		r.Losses = append(r.Losses, loss)
	}
}
