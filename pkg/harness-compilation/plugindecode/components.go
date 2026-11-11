package plugindecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
)

// componentKind maps a catalog component name to the leaf resource kind it
// decodes into. Catalog components without an entry (for example factory
// output-styles) carry no leaf kind and are reported as unsupported when the
// package ships them.
var componentKind = map[string]ir.ResourceKind{
	"skills":     ir.KindSkill,
	"commands":   ir.KindCommand,
	"agents":     ir.KindSubagent,
	"droids":     ir.KindSubagent,
	"hooks":      ir.KindHook,
	"mcp":        ir.KindMcpServer,
	"mcpServers": ir.KindMcpServer,
}

// discoverComponents runs each declared component's discovery rule in sorted
// component order, so member order is deterministic.
func (d *decoder) discoverComponents() {
	d.seenKeys = map[string]bool{}
	for _, component := range slices.Sorted(maps.Keys(d.info.Components)) {
		rule := d.info.Components[component]
		dir := d.layout.Components[component]
		kind, known := componentKind[component]
		if !known {
			d.reportUnsupportedComponent(component, dir)
			continue
		}
		switch rule {
		case pluginpolicy.ComponentImmediateChildren:
			d.discoverSkillChildren(dir)
		case pluginpolicy.ComponentFile:
			d.discoverComponentFile(component, kind, dir)
		case pluginpolicy.ComponentInlineOrFile:
			d.discoverInlineOrFile(component, kind, dir)
		}
	}
}

func (d *decoder) reportUnsupportedComponent(component, dir string) {
	if dir == "" {
		return
	}
	entries, err := fs.ReadDir(d.src, dir)
	if err != nil || len(entries) == 0 {
		return
	}
	d.warn(diag.CodePluginFormatComponentInvalid,
		fmt.Sprintf("component %q has no supported member kind; its contents are skipped", component), dir)
	d.record(dir, "component", "unsupported-component", diag.CodePluginFormatComponentInvalid)
}

// discoverSkillChildren loads skills as immediate children of dir that
// contain a SKILL.md regular file; deeper descendants are never searched.
func (d *decoder) discoverSkillChildren(dir string) {
	entries, err := fs.ReadDir(d.src, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		if info, statErr := fs.Stat(d.src, dir); statErr == nil && !info.IsDir() {
			d.warn(diag.CodePluginFormatComponentInvalid,
				"component location is not a directory; this component type is skipped", dir)
			d.record(dir, "component", "wrong-kind", diag.CodePluginFormatComponentInvalid)
			return
		}
		d.warn(diag.CodePluginFormatComponentInvalid,
			"component location cannot be read; this component type is skipped", dir)
		d.record(dir, "component", "unreadable", diag.CodePluginFormatComponentInvalid)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := path.Join(dir, entry.Name())
		info, err := fs.Stat(d.src, path.Join(skillPath, "SKILL.md"))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		skill, err := bundle.LoadSkill(d.src, skillPath)
		if err != nil {
			d.warn(diag.CodePluginMemberInvalid,
				fmt.Sprintf("skill %q failed validation and was skipped", entry.Name()), skillPath)
			d.record(skillPath, "member", "invalid-skill", diag.CodePluginMemberInvalid)
			continue
		}
		d.addMember(skill, skillPath)
	}
}

// discoverComponentFile implements the "file" discovery rule: markdown leaf
// kinds enumerate *.md inside the component directory; the JSON component
// kinds read their fixed file location.
func (d *decoder) discoverComponentFile(component string, kind ir.ResourceKind, dir string) {
	switch kind {
	case ir.KindCommand, ir.KindSubagent:
		d.discoverMarkdown(dir, kind)
	case ir.KindHook:
		d.loadHooksFile(path.Join(dir, "hooks.json"))
	case ir.KindMcpServer:
		d.loadMcpComponent(component)
	}
}

func (d *decoder) discoverMarkdown(dir string, kind ir.ResourceKind) {
	entries, err := fs.ReadDir(d.src, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		d.warn(diag.CodePluginFormatComponentInvalid,
			"component location cannot be read; this component type is skipped", dir)
		d.record(dir, "component", "unreadable", diag.CodePluginFormatComponentInvalid)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		file := path.Join(dir, entry.Name())
		var res ir.Resource
		var err error
		if kind == ir.KindSubagent {
			res, err = bundle.LoadSubagent(d.src, file)
		} else {
			res, err = bundle.LoadCommand(d.src, file)
		}
		if err != nil {
			d.warn(diag.CodePluginMemberInvalid,
				fmt.Sprintf("%s %q failed validation and was skipped", strings.ToLower(kind.String()), entry.Name()), file)
			d.record(file, "member", "invalid-member", diag.CodePluginMemberInvalid)
			continue
		}
		d.addMember(res, file)
	}
}

// discoverInlineOrFile implements the "inline-or-file" rule: the manifest
// field may name a file or carry the component inline; an absent field falls
// back to the fixed file location.
func (d *decoder) discoverInlineOrFile(component string, kind ir.ResourceKind, dir string) {
	raw, present := d.manifestObj[component]
	if present {
		switch value := raw.(type) {
		case string:
			if !validPackageRelPath(value) {
				d.warn(diag.CodePluginMemberInvalid,
					fmt.Sprintf("manifest field %q does not name a package-relative file; it is ignored", component), d.layout.ManifestPath)
				d.record(component, "component", "invalid-inline-path", diag.CodePluginMemberInvalid)
				return
			}
			d.loadComponentFile(kind, value)
		case map[string]any:
			d.loadInlineComponent(kind, component, value)
		default:
			d.warn(diag.CodePluginMemberInvalid,
				fmt.Sprintf("manifest field %q must name a file or carry the component inline; it is ignored", component), d.layout.ManifestPath)
			d.record(component, "component", "invalid-inline-value", diag.CodePluginMemberInvalid)
		}
		return
	}
	switch kind {
	case ir.KindHook:
		d.loadHooksFile(path.Join(dir, "hooks.json"))
	case ir.KindMcpServer:
		d.loadMcpComponent(component)
	}
}

// loadComponentFile decodes a JSON component file through the existing leaf
// loaders over a bounded read.
func (d *decoder) loadComponentFile(kind ir.ResourceKind, file string) {
	switch kind {
	case ir.KindHook:
		d.loadHooksFile(file)
	case ir.KindMcpServer:
		d.loadMcpFile(file)
	}
}

// loadInlineComponent decodes a manifest-inline hooks or mcpServers object
// through the same leaf loaders the file form uses.
func (d *decoder) loadInlineComponent(kind ir.ResourceKind, component string, value map[string]any) {
	var fileName string
	var payload any
	switch kind {
	case ir.KindHook:
		fileName = "hooks.json"
		payload = value
	case ir.KindMcpServer:
		fileName = ".mcp.json"
		payload = map[string]any{"mcpServers": value}
	default:
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	d.loadComponentData(kind, fileName, data, d.layout.ManifestPath)
}

func (d *decoder) loadHooksFile(file string) {
	data, err := d.readBounded(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		d.componentLoadFailed("hooks", file, err)
		return
	}
	d.loadComponentData(ir.KindHook, file, data, file)
}

func (d *decoder) loadMcpComponent(component string) {
	if d.format == "agent-plugin" {
		d.loadAgentMcp("mcp.json")
		return
	}
	candidates := []string{"mcp.json", ".mcp.json"}
	if d.format == "claude-plugin" {
		candidates = []string{".mcp.json", "mcp.json"}
	}
	for _, file := range candidates {
		if _, err := fs.Stat(d.src, file); err == nil {
			d.loadMcpFile(file)
			return
		}
	}
}

func (d *decoder) loadMcpFile(file string) {
	data, err := d.readBounded(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		d.componentLoadFailed("mcp", file, err)
		return
	}
	d.loadComponentData(ir.KindMcpServer, file, data, file)
}

// loadComponentData feeds already-read bytes through the leaf loaders via an
// in-memory FS, keeping every JSON read bounded. The bytes go through the
// same jsonc parse readJSON applies first, so a duplicate key fails this
// component while siblings load.
func (d *decoder) loadComponentData(kind ir.ResourceKind, file string, data []byte, reportFile string) {
	var probe map[string]any
	if err := jsonc.Unmarshal(file, data, &probe); err != nil {
		d.componentLoadFailed(componentName(kind), reportFile, err)
		return
	}
	mem := fstest.MapFS{file: &fstest.MapFile{Data: data}}
	switch kind {
	case ir.KindHook:
		hooks, hookDiags, err := bundle.LoadHooks(mem, file)
		if err != nil {
			d.componentLoadFailed("hooks", reportFile, err)
			return
		}
		d.diags = append(d.diags, hookDiags...)
		for _, h := range hooks {
			d.addMember(h, file)
		}
	case ir.KindMcpServer:
		servers, err := bundle.LoadMcpServers(mem, file)
		if err != nil {
			d.componentLoadFailed("mcp", reportFile, err)
			return
		}
		for _, s := range servers {
			d.addMember(s, file)
		}
	}
}

// componentName is the diagnostic label for a leaf kind's component file.
func componentName(kind ir.ResourceKind) string {
	if kind == ir.KindHook {
		return "hooks"
	}
	return "mcp"
}

func (d *decoder) componentLoadFailed(component, file string, err error) {
	d.warn(diag.CodePluginFormatComponentInvalid,
		fmt.Sprintf("component %s at %s could not be loaded and is skipped", component, file), file)
	d.record(file, "component", "load-failed", diag.CodePluginFormatComponentInvalid)
}

func (d *decoder) addMember(res ir.Resource, file string) {
	member := ir.PluginMember{Resource: res}
	key := member.Key()
	if d.seenKeys[key] {
		d.warn(diag.CodePluginMemberInvalid,
			fmt.Sprintf("member %q duplicates an earlier member and is skipped", key), file)
		d.record(file, "member", "duplicate-key", diag.CodePluginMemberInvalid)
		return
	}
	d.seenKeys[key] = true
	d.members = append(d.members, member)
}

// readBounded reads one file from the package root under the JSON read
// limit.
func (d *decoder) readBounded(name string) ([]byte, error) {
	f, err := d.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxJSONBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxJSONBytes {
		return nil, codedErr(diag.CodePluginPackageOversized, name, "file exceeds the bounded read limit")
	}
	return data, nil
}

// validPackageRelPath reports whether a manifest-declared component path is a
// clean relative path that cannot leave the package root.
func validPackageRelPath(value string) bool {
	if value == "" || path.IsAbs(value) || strings.Contains(value, "\\") {
		return false
	}
	clean := path.Clean(value)
	return clean == value && !strings.HasPrefix(clean, "..") && clean != "." && fs.ValidPath(clean)
}
