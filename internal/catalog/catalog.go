// Package catalog provides skill discovery via the harness-compilation (HCC)
// IngestBundle pipeline.
//
// It replaces internal/skill.DiscoverSkills for list/TUI usage while keeping
// the existing internal/skill package as a types shim consumed by resolver
// and sync. The AsSkills method converts Catalog entries back to skill.Skill
// and skill.MalformedSkill for compatibility with the existing pipeline.
package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

// Harness name constants re-exported from HCC for convenience.
// These avoid callers needing to import the HCC package directly
// just for harness name strings.
const (
	HarnessClaude   = string(hcc.TargetClaude)
	HarnessFactory  = string(hcc.TargetFactory)
	HarnessCursor   = string(hcc.TargetCursor)
	HarnessWindsurf = string(hcc.TargetWindsurf)
	HarnessGemini   = string(hcc.TargetGemini)
	HarnessDevin    = string(hcc.TargetDevin)
)

// Entry represents a single cataloged resource discovered from a registry.
type Entry struct {
	Kind        ir.ResourceKind
	Name        string
	Description string
	Tags        []string
	Resource    ir.Resource // underlying IR resource for downstream compile/materialize
	Path        string      // source file path (from ir.SourceRef)
}

// Requirement identifies another resource that must be selected for a
// catalog entry to work. Registry and Path are optional provider constraints
// and installation hints; runtime satisfaction is checked by kind and name.
type Requirement struct {
	Kind     ir.ResourceKind
	Name     string
	Registry string
	Path     string
}

// Catalog holds the result of loading a registry via HCC.
type Catalog struct {
	Entries       []Entry
	Diagnostics   []diag.Diagnostic
	SourceHarness string // harness name used for ingestion (autodetected or explicit)
}

func Resources(c *Catalog) map[string]ir.Resource {
	resources := make(map[string]ir.Resource, len(c.Entries))
	for _, entry := range c.Entries {
		resources[skill.ResourceKey(entry.Kind, entry.Name)] = entry.Resource
	}
	return resources
}

// malformedFromDiagnostics converts diagnostics with severity Warning or Error
// into MalformedSkill entries for display in the TUI.
//
// For resource-attached diagnostics (d.Resource != ""), only SeverityError
// entries are surfaced as malformed. The rationale: a Warning on a specific
// resource means the resource was loaded but translated lossily (e.g. a field
// was dropped), which could be shown in a per-resource detail view; but an
// Error on a specific resource means the resource was never loaded at all
// (e.g. YAML frontmatter parse failure), so there is no per-resource view
// to attach it to — the only way to surface it is as a malformed entry.
func malformedFromDiagnostics(diagnostics []diag.Diagnostic) []skill.MalformedSkill {
	var malformed []skill.MalformedSkill
	for _, d := range diagnostics {
		if d.Severity > diag.SeverityWarning {
			continue
		}
		if d.Resource != "" && d.Severity != diag.SeverityError {
			continue
		}
		if d.Resource != "" {
			// Load-failed error: extract the skill name from the resource
			// path (e.g. "skills/my-skill" → "my-skill") so the TUI and
			// list/status output show a clean name, not a nested path.
			name := d.Resource
			if base := filepath.Base(d.Resource); base != "" && base != "." && base != "/" {
				name = base
			}
			path := d.Source.File
			if path == "" {
				path = d.Resource
			}
			malformed = append(malformed, skill.MalformedSkill{
				DirName: name,
				Path:    path,
				Kind:    kindFromDiagnosticCode(d.Code),
				Error:   fmt.Errorf("%s: %s", d.Code, d.Message),
			})
			continue
		}
		malformed = append(malformed, skill.MalformedSkill{
			DirName: d.Code,
			Path:    d.Source.File,
			Error:   fmt.Errorf("%s: %s", d.Code, d.Message),
		})
	}
	return malformed
}

// kindFromDiagnosticCode classifies a load-failure diagnostic by resource kind
// so a dropped resource can only be correlated with a selection of the SAME
// kind. Ingest load failures use a uniform "…load-<kind>-failed" code suffix
// (see diag/codes.go); anything else — including a coded error that replaced
// the ingester's own code — stays unclassified, which keeps matching on name
// alone rather than suppressing a correlation that used to work.
func kindFromDiagnosticCode(code string) string {
	switch {
	case strings.HasSuffix(code, ".load-skill-failed"):
		return ir.KindSkill.String()
	case strings.HasSuffix(code, ".load-command-failed"):
		return ir.KindCommand.String()
	case strings.HasSuffix(code, ".load-subagent-failed"):
		return ir.KindSubagent.String()
	case strings.HasSuffix(code, ".load-hook-failed"):
		return ir.KindHook.String()
	case strings.HasSuffix(code, ".load-mcp-servers-failed"):
		return ir.KindMcpServer.String()
	case strings.HasSuffix(code, ".load-rule-failed"):
		return ir.KindRule.String()
	default:
		return ""
	}
}

// EntriesResources builds an ir.Resource map from catalog entries that have
// nil Resource fields, using the same legacy heuristic as LegacyResources.
// Entries with non-nil Resource are included directly. Entries with nil
// Resource are only synthesized when their Kind is KindSkill; other kinds
// are skipped because the legacy heuristic can only produce Skill resources.
func EntriesResources(entries []Entry) map[string]ir.Resource {
	resources := make(map[string]ir.Resource, len(entries))
	for _, e := range entries {
		if e.Resource != nil {
			resources[skill.ResourceKey(e.Kind, e.Name)] = e.Resource
			continue
		}
		if e.Kind != ir.KindSkill {
			continue
		}
		body, sourceFile := legacySkillBody(e.Path)
		resources[skill.ResourceKey(e.Kind, e.Name)] = ir.NewSkill(e.Name, e.Description, body, nil, nil, ir.SourceRef{File: sourceFile, Line: 1})
	}
	return resources
}

// EntriesFromCollections creates catalog entries with KindCollection from
// discovered collections. Each collection becomes a catalog Entry with an
// ir.Collection resource attached, making it visible to the resolver,
// collision detector, and disambiguation logic.
func EntriesFromCollections(cols []collection.Collection) ([]Entry, error) {
	entries := make([]Entry, 0, len(cols))
	var memberErrs []error
	for i := range cols {
		col := &cols[i]
		members := make([]ir.CollectionMember, 0, len(col.Skills)+len(col.Resources))
		for _, cs := range col.Skills {
			members = append(members, ir.CollectionMember{
				Name:     cs.Name,
				Kind:     ir.KindSkill, // legacy skills: key defaults to skill
				Registry: cs.Registry,
				Ref:      cs.Ref,
				Path:     cs.Path,
			})
		}
		for _, ce := range col.Resources {
			kind, err := collectionParseKind(ce.Kind)
			if err != nil {
				memberErrs = append(memberErrs, fmt.Errorf("collection %q member %q: %w", col.Name, ce.Name, err))
				continue
			}
			members = append(members, ir.CollectionMember{
				Name:     ce.Name,
				Kind:     kind,
				Registry: ce.Registry,
				Ref:      ce.Ref,
				Path:     ce.Path,
			})
		}
		includes := make([]string, 0, len(col.Includes))
		for _, inc := range col.Includes {
			includes = append(includes, inc.Name)
		}
		irCol := ir.NewCollection(col.Name, col.Version, col.Description, members, includes, nil, ir.SourceRef{})

		entries = append(entries, Entry{
			Kind:        ir.KindCollection,
			Name:        col.Name,
			Description: col.Description,
			Resource:    irCol,
		})
	}
	return entries, errors.Join(memberErrs...)
}

// collectionParseKind maps a kind string from a CollectionEntry to an
// ir.ResourceKind. Empty string defaults to KindSkill.
// Returns an error for unknown or misspelled kinds so that config errors
// surface explicitly rather than silently defaulting to the wrong kind.
func collectionParseKind(s string) (ir.ResourceKind, error) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "", "skill":
		return ir.KindSkill, nil
	case "command":
		return ir.KindCommand, nil
	case "subagent":
		return ir.KindSubagent, nil
	case "hook":
		return ir.KindHook, nil
	case "mcp", "mcpserver":
		return ir.KindMcpServer, nil
	default:
		return ir.KindSkill, fmt.Errorf("unsupported collection member kind %q (must be one of skill, command, subagent, hook, mcp)", s)
	}
}

func legacySkillBody(path string) (string, string) {
	sourceFile := path
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		sourceFile = filepath.Join(path, "SKILL.md")
	}
	data, err := os.ReadFile(sourceFile)
	if err != nil {
		return "", sourceFile
	}
	return markdownBody(string(data)), sourceFile
}

func markdownBody(content string) string {
	lines := strings.Split(content, "\n")
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "\ufeff"))
		if trimmed == "" {
			continue
		}
		if trimmed != "---" {
			return content
		}
		start = i
		break
	}
	if start == -1 {
		return content
	}
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimLeft(strings.Join(lines[i+1:], "\n"), "\n")
		}
	}
	return content
}

// Load discovers resources from a directory on disk using HCC IngestBundle,
// filtered to KindSkill. The sourceHarness parameter controls the ingest
// format; empty string triggers autodetection via ingest.Autodetect.
//
// On autodetect failure, Load falls back to HarnessClaude and records
// a diagnostic.
func Load(dir string, sourceHarness string) (*Catalog, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve load root %q: %w", dir, err)
	}
	srcFS := os.DirFS(root)
	cat, err := LoadFS(srcFS, sourceHarness)
	if err != nil {
		return nil, err
	}
	for i := range cat.Entries {
		if cat.Entries[i].Path != "" && !filepath.IsAbs(cat.Entries[i].Path) {
			cat.Entries[i].Path = filepath.Join(root, cat.Entries[i].Path)
		}
	}
	return cat, nil
}

// LoadFS discovers resources from an fs.FS using HCC IngestBundle.
func LoadFS(srcFS fs.FS, sourceHarness string) (*Catalog, error) {
	from := ingest.Harness(sourceHarness)
	var autodetectDiag diag.Diagnostic

	if from == "" {
		detected, ok := ingest.Autodetect(srcFS)
		if ok {
			from = detected
		} else {
			// Fall back to claude; record a diagnostic so callers
			// can report the fallback if desired.
			from = ingest.HarnessClaude
			autodetectDiag = diag.Diagnostic{
				Code:     "catalog.autodetect.fallback",
				Severity: diag.SeverityInfo,
				Message:  "could not autodetect source harness; defaulting to claude",
			}
		}
	}

	bundle, err := hcc.IngestBundle(srcFS, hcc.Harness(from))
	if err != nil {
		return nil, fmt.Errorf("hcc ingest (%s): %w", from, err)
	}

	entries := make([]Entry, 0, len(bundle.Resources))
	for _, r := range bundle.Resources {
		entry := Entry{
			Kind:     r.Kind(),
			Name:     r.Name(),
			Resource: r,
		}

		entry.Description = descriptionFromResource(r)
		entry.Tags = tagsFromResource(r)

		// Path from SourceRef
		if src, ok := sourceRefFromResource(r); ok {
			entry.Path = src
		}

		entries = append(entries, entry)
	}

	allDiags := make([]diag.Diagnostic, 0, len(bundle.Diagnostics)+1)
	allDiags = append(allDiags, bundle.Diagnostics...)
	if autodetectDiag.Code != "" {
		allDiags = append(allDiags, autodetectDiag)
	}

	return &Catalog{
		Entries:       entries,
		Diagnostics:   allDiags,
		SourceHarness: string(from),
	}, nil
}

// tagsFromExtra extracts the tags slice from a resource's Extra map,
// following the agentskills.io convention of metadata.tags.
func tagsFromExtra(extra map[string]any) []string {
	if extra == nil {
		return nil
	}
	meta, ok := extra["metadata"].(map[string]any)
	if !ok {
		return nil
	}
	tagsVal, ok := meta["tags"]
	if !ok {
		return nil
	}
	switch v := tagsVal.(type) {
	case []string:
		return v
	case []any:
		tags := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				tags = append(tags, s)
			}
		}
		return tags
	default:
		return nil
	}
}

func requirementsFromExtra(extra map[string]any) []Requirement {
	if extra == nil {
		return nil
	}
	meta, ok := extra["metadata"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := meta["requires"]
	if !ok {
		return nil
	}

	items, ok := raw.([]any)
	if !ok {
		slog.Warn("ignoring malformed metadata.requires", "reason", "requires must be a list")
		return nil
	}
	requirements := make([]Requirement, 0, len(items))
	for i, item := range items {
		switch value := item.(type) {
		case string:
			kind, name, ok := parseRequirementShorthand(value)
			if ok {
				requirements = append(requirements, Requirement{Kind: kind, Name: name})
			} else {
				slog.Warn("ignoring malformed resource requirement", "index", i, "reason", "invalid shorthand")
			}
		case map[string]any:
			name, valid := requirementMapString(value, "name", true)
			name = strings.TrimSpace(name)
			if !valid || name == "" {
				slog.Warn("ignoring malformed resource requirement", "index", i, "reason", "name must be a non-empty string")
				continue
			}
			kindText, valid := requirementMapString(value, "kind", false)
			if !valid {
				slog.Warn("ignoring malformed resource requirement", "index", i, "reason", "kind must be a string")
				continue
			}
			kind, ok := parseRequirementKind(kindText)
			if !ok {
				slog.Warn("ignoring malformed resource requirement", "index", i, "reason", "unknown or unsupported kind")
				continue
			}
			registry, registryValid := requirementMapString(value, "registry", false)
			path, pathValid := requirementMapString(value, "path", false)
			if !registryValid || !pathValid {
				slog.Warn("ignoring malformed resource requirement", "index", i, "reason", "registry and path must be strings")
				continue
			}
			requirements = append(requirements, Requirement{
				Kind:     kind,
				Name:     name,
				Registry: strings.TrimSpace(registry),
				Path:     strings.TrimSpace(path),
			})
		default:
			slog.Warn("ignoring malformed resource requirement", "index", i, "reason", "requirement must be a string or map")
		}
	}
	return requirements
}

func parseRequirementShorthand(value string) (ir.ResourceKind, string, bool) {
	kindText, name, found := strings.Cut(strings.TrimSpace(value), ":")
	if !found {
		name = kindText
		kindText = "skill"
	}
	kind, ok := parseRequirementKind(kindText)
	name = strings.TrimSpace(name)
	return kind, name, ok && name != ""
}

func parseRequirementKind(value string) (ir.ResourceKind, bool) {
	if strings.TrimSpace(value) == "" {
		return ir.KindSkill, true
	}
	kind, ok := skill.ParseKind(value)
	if !ok || kind == ir.KindCollection {
		return ir.KindSkill, false
	}
	return kind, true
}

func requirementMapString(value map[string]any, key string, required bool) (string, bool) {
	raw, exists := value[key]
	if !exists {
		return "", !required
	}
	text, ok := raw.(string)
	return text, ok
}

func descriptionFromResource(r ir.Resource) string {
	switch v := r.(type) {
	case *ir.Skill:
		return v.Description
	case *ir.Command:
		return v.Description
	case *ir.Subagent:
		return v.Description
	case *ir.Hook:
		return v.Event + " " + v.Type
	case *ir.McpServer:
		return v.Command
	case *ir.Collection:
		return v.Description
	default:
		return ""
	}
}

func tagsFromResource(r ir.Resource) []string {
	return tagsFromExtra(extraFromResource(r))
}

func RequirementsFromResource(r ir.Resource) []Requirement {
	return requirementsFromExtra(extraFromResource(r))
}

func extraFromResource(r ir.Resource) map[string]any {
	switch v := r.(type) {
	case *ir.Skill:
		return v.Extra
	case *ir.Command:
		return v.Extra
	case *ir.Subagent:
		return v.Extra
	case *ir.Hook:
		return v.Extra
	case *ir.McpServer:
		return v.Extra
	case *ir.Collection:
		return v.Extra
	default:
		return nil
	}
}

func preserveResourceSourcePath(r ir.Resource, sourcePath string) {
	if r == nil || !filepath.IsAbs(sourcePath) {
		return
	}
	var source *ir.SourceRef
	switch v := r.(type) {
	case *ir.Skill:
		source = &v.Source
	case *ir.Command:
		source = &v.Source
	case *ir.Subagent:
		source = &v.Source
	case *ir.Hook:
		source = &v.Source
	case *ir.McpServer:
		source = &v.Source
	case *ir.Rule:
		source = &v.Source
	case *ir.Collection:
		source = &v.Source
	}
	if source != nil && !filepath.IsAbs(source.File) {
		source.File = sourcePath
	}
}

// SourcePath returns the authoritative source path for detail inspection. Exact
// catalogs may keep Entry.Path rebased to a logical legacy-cache location for
// display compatibility, while the IR SourceRef remains pinned to the immutable
// snapshot that was actually ingested.
func SourcePath(entry Entry) string {
	if entry.Resource != nil {
		if source, ok := sourceRefFromResource(entry.Resource); ok {
			return source
		}
	}
	return entry.Path
}

// sourceRefFromResource extracts the source file path from an ir.Resource.
// The ir.Resource interface only exposes Kind() and Name(); this helper checks
// concrete resource types for their exported SourceRef fields.
func sourceRefFromResource(r ir.Resource) (string, bool) {
	// The Path field is used by list/TUI detail views and by materialize
	// when a target can link native skill directories directly. Prefer HCC's
	// SourceRef and fall back to the legacy heuristic if it is absent.
	switch r.Kind() {
	case ir.KindSkill:
		if sk, ok := r.(*ir.Skill); ok && sk.Source.File != "" {
			return sk.Source.File, true
		}
		return r.Name() + "/SKILL.md", true
	case ir.KindCommand:
		if cmd, ok := r.(*ir.Command); ok && cmd.Source.File != "" {
			return cmd.Source.File, true
		}
	case ir.KindSubagent:
		if subagent, ok := r.(*ir.Subagent); ok && subagent.Source.File != "" {
			return subagent.Source.File, true
		}
	case ir.KindHook:
		if hook, ok := r.(*ir.Hook); ok && hook.Source.File != "" {
			return hook.Source.File, true
		}
	case ir.KindMcpServer:
		if mcp, ok := r.(*ir.McpServer); ok && mcp.Source.File != "" {
			return mcp.Source.File, true
		}
	case ir.KindCollection:
		if col, ok := r.(*ir.Collection); ok && col.Source.File != "" {
			return col.Source.File, true
		}
	default:
		return "", false
	}
	return "", false
}
