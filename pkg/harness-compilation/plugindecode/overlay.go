package plugindecode

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// entryOverlayPolicyID is the only entry overlay policy the decoder
// implements; the shipped format catalogs may reference only allowlisted
// policy identifiers, so reaching this package with any other value means a
// catalog was edited outside validation.
const entryOverlayPolicyID = "entry-overlay.strict"

// EntryOverlay is one catalog entry's overlay input under the catalog's
// declared entry-overlay policy.
type EntryOverlay struct {
	Name   string
	Strict bool
	// Fields carries the entry definition fields beyond name, source, and
	// strict.
	Fields map[string]any
	// Source labels diagnostics with the catalog manifest path.
	Source string
}

// entryComponentFields are the definition fields an overlay treats as
// component declarations: the leaf kinds that decode plus the member kinds
// with no decoder, which still count toward a non-strict manifest conflict.
var entryComponentFields = []string{"commands", "agents", "skills", "hooks", "outputStyles", "themes"}

// entryManifestOnlyFields are entry fields that apply only when the entry
// itself is the manifest; a present plugin.json wins over them.
var entryManifestOnlyFields = []string{"mcpServers", "lspServers", "userConfig", "channels"}

// DecodeEntry decodes the plugin root of one catalog entry under the entry
// overlay policy the catalog format declares. Catalogs without an overlay
// policy decode plainly; a policy identifier the decoder does not implement
// refuses rather than silently ignoring the overlay.
func DecodeEntry(rootPath, catalogFormat string, entry CatalogEntry) (*ir.Plugin, []*diag.Diagnostic, error) {
	pluginFormat := CatalogPluginFormat(catalogFormat)
	info, known, err := pluginpolicy.LookupPackageFormat(catalogFormat)
	if err != nil {
		return nil, nil, err
	}
	if !known || info.Role != pluginpolicy.RoleCatalog {
		return nil, nil, diag.NewCodedError(diag.CodePluginFormatUnsupported, diag.SourceRef{},
			fmt.Errorf("format %q is not a recognized plugin catalog format", catalogFormat))
	}
	switch info.EntryOverlay {
	case "":
		return Decode(rootPath, pluginFormat)
	case entryOverlayPolicyID:
		fields := maps.Clone(entry.Overlay)
		delete(fields, "strict")
		delete(fields, "name")
		delete(fields, "source")
		return decodePackage(rootPath, pluginFormat, &EntryOverlay{
			Name:   entry.Name,
			Strict: entry.Strict,
			Fields: fields,
			Source: entryOverlaySource(catalogFormat),
		})
	}
	return nil, nil, diag.NewCodedError(diag.CodePluginFormatUnsupported, diag.SourceRef{},
		fmt.Errorf("catalog format %q declares entry overlay policy %q, which this decoder does not implement", catalogFormat, info.EntryOverlay))
}

// entryOverlaySource labels overlay diagnostics with the catalog manifest
// path, so warnings point at the definition that declared the fields.
func entryOverlaySource(catalogFormat string) string {
	if layout, ok := target.LookupPackageLayout(catalogFormat); ok {
		return layout.ManifestPath
	}
	return "marketplace.json"
}

// decodeOverlayManifest synthesizes the manifest of an entry whose plugin
// root carries no plugin.json: under the strict policy the entry itself is
// the manifest, so its definition fields become the manifest object. A
// declared hooks field stays out of the manifest object so the overlay can
// reject file-path and array forms itself while suppressing the file
// fallback the manifest form would take.
func (d *decoder) decodeOverlayManifest() error {
	obj := maps.Clone(d.overlay.Fields)
	if _, declared := obj["hooks"]; declared {
		obj["hooks"] = map[string]any{}
	}
	obj["name"] = d.overlay.Name
	d.manifestObj = obj
	return d.decodeNativeManifest(obj)
}

// applyEntryOverlay folds the catalog entry's fields into the decoded
// package under entry-overlay.strict. With a plugin.json present and strict
// off, component fields conflict with the manifest and refuse the entry;
// otherwise leaf component paths append member locations, an inline hooks
// object replaces the manifest's hooks per event, unsupported member kinds
// are recorded, and manifest-only fields do not apply.
func (d *decoder) applyEntryOverlay(manifestPresent bool) error {
	fields := d.overlay.Fields
	componentDeclared := false
	for _, field := range entryComponentFields {
		if _, ok := fields[field]; ok {
			componentDeclared = true
		}
	}
	if manifestPresent && !d.overlay.Strict && componentDeclared {
		return codedErr(diag.CodePluginCatalogManifestConflict, d.overlay.Source,
			"entry declares strict: false with component fields, but the plugin root carries plugin.json; the entry is not installable")
	}
	for _, field := range []string{"outputStyles", "themes"} {
		if _, ok := fields[field]; ok {
			d.warn(diag.CodePluginFormatComponentInvalid,
				fmt.Sprintf("entry component %q has no supported member kind; it is skipped", field), d.overlay.Source)
			d.record(field, "component", "unsupported-overlay-kind", diag.CodePluginFormatComponentInvalid)
		}
	}
	for _, component := range []struct {
		name string
		kind ir.ResourceKind
	}{
		{"agents", ir.KindSubagent},
		{"commands", ir.KindCommand},
		{"skills", ir.KindSkill},
	} {
		raw, ok := fields[component.name]
		if !ok {
			continue
		}
		for _, location := range d.overlayComponentPaths(component.name, raw) {
			d.overlayComponentLocation(component.name, component.kind, location)
		}
	}
	if raw, ok := fields["hooks"]; ok {
		if obj, isObject := raw.(map[string]any); isObject {
			d.applyOverlayHooks(obj, manifestPresent)
		} else {
			d.warn(diag.CodePluginFormatComponentInvalid,
				"entry hooks must be an inline event-to-matchers object; the value is skipped", d.overlay.Source)
			d.record("hooks", "component", "invalid-overlay-hooks", diag.CodePluginFormatComponentInvalid)
		}
	}
	if manifestPresent {
		for _, field := range entryManifestOnlyFields {
			if _, ok := fields[field]; ok {
				d.warn(diag.CodePluginFormatComponentInvalid,
					fmt.Sprintf("entry field %q applies only when the entry is the manifest; it is skipped because plugin.json is present", field), d.overlay.Source)
				d.record(field, "component", "overlay-field-not-applied", diag.CodePluginFormatComponentInvalid)
			}
		}
	}
	return nil
}

// overlayComponentPaths normalizes an entry component field to contained
// plugin-root-relative paths. A single string or an array of strings is
// accepted; anything else, and any path escaping the package root, is
// recorded and skipped.
func (d *decoder) overlayComponentPaths(component string, raw any) []string {
	var values []any
	switch v := raw.(type) {
	case string:
		values = []any{v}
	case []any:
		values = v
	default:
		d.warn(diag.CodePluginMemberInvalid,
			fmt.Sprintf("entry component %q must be a path or an array of paths; it is skipped", component), d.overlay.Source)
		d.record(component, "component", "invalid-overlay-paths", diag.CodePluginMemberInvalid)
		return nil
	}
	var out []string
	for _, value := range values {
		text, isString := value.(string)
		cleaned := strings.TrimPrefix(text, "./")
		if !isString || !validPackageRelPath(cleaned) {
			d.warn(diag.CodePluginMemberInvalid,
				fmt.Sprintf("entry component %q path %q does not resolve inside the plugin root; it is skipped", component, text), d.overlay.Source)
			d.record(text, "component", "overlay-path-escape", diag.CodePluginMemberInvalid)
			continue
		}
		out = append(out, cleaned)
	}
	return out
}

// overlayComponentLocation decodes members from one appended component
// location: a directory whose immediate children carry the component, a
// skill directory, or a single markdown leaf file.
func (d *decoder) overlayComponentLocation(component string, kind ir.ResourceKind, location string) {
	info, err := fs.Stat(d.src, location)
	if err != nil {
		d.warn(diag.CodePluginMemberInvalid,
			fmt.Sprintf("entry component %q path %q cannot be read; it is skipped", component, location), d.overlay.Source)
		d.record(location, "component", "unreadable", diag.CodePluginMemberInvalid)
		return
	}
	if info.IsDir() {
		switch component {
		case "skills":
			if stat, statErr := fs.Stat(d.src, path.Join(location, "SKILL.md")); statErr == nil && stat.Mode().IsRegular() {
				d.loadOverlaySkill(location)
				return
			}
			d.discoverSkillChildren(location)
		default:
			d.discoverMarkdown(location, kind)
		}
		return
	}
	if strings.HasSuffix(location, ".md") && (kind == ir.KindCommand || kind == ir.KindSubagent) {
		d.loadOverlayMarkdown(location, kind)
		return
	}
	d.warn(diag.CodePluginMemberInvalid,
		fmt.Sprintf("entry component %q path %q is not a usable member location; it is skipped", component, location), d.overlay.Source)
	d.record(location, "component", "wrong-kind", diag.CodePluginMemberInvalid)
}

func (d *decoder) loadOverlaySkill(location string) {
	skill, err := bundle.LoadSkill(d.src, location)
	if err != nil {
		d.warn(diag.CodePluginMemberInvalid,
			fmt.Sprintf("entry skill %q failed validation and was skipped", path.Base(location)), location)
		d.record(location, "member", "invalid-skill", diag.CodePluginMemberInvalid)
		return
	}
	d.addMember(skill, location)
}

func (d *decoder) loadOverlayMarkdown(file string, kind ir.ResourceKind) {
	var res ir.Resource
	var err error
	if kind == ir.KindSubagent {
		res, err = bundle.LoadSubagent(d.src, file)
	} else {
		res, err = bundle.LoadCommand(d.src, file)
	}
	if err != nil {
		d.warn(diag.CodePluginMemberInvalid,
			fmt.Sprintf("%s %q failed validation and was skipped", strings.ToLower(kind.String()), file), file)
		d.record(file, "member", "invalid-member", diag.CodePluginMemberInvalid)
		return
	}
	d.addMember(res, file)
}

// applyOverlayHooks decodes an inline entry hooks object through the leaf
// loader. When plugin.json is the manifest (strict), each event the entry
// declares replaces the manifest's hooks for that event; events the entry
// does not touch survive. Without a manifest the entry's hooks simply apply.
func (d *decoder) applyOverlayHooks(obj map[string]any, manifestPresent bool) {
	if manifestPresent {
		var kept []ir.PluginMember
		for _, member := range d.members {
			if hook, isHook := member.Resource.(*ir.Hook); isHook {
				if _, replaced := obj[hook.Event]; replaced {
					delete(d.seenKeys, member.Key())
					continue
				}
			}
			kept = append(kept, member)
		}
		d.members = kept
	}
	data, _ := json.Marshal(obj)
	d.loadComponentData(ir.KindHook, "entry-hooks.json", data, d.overlay.Source)
}
