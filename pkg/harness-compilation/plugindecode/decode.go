// Package plugindecode decodes plugin package directories into ir.Plugin
// resources. One decoder covers every recognized package format; the manifest
// path and component directories come from target.PackageLayout, and the
// manifest field rules, recognized versions, and discovery rules come from the
// shipped pluginpolicy format catalogs. Agent Plugins 1.0.0 packages are
// interpreted against the bundled schemas under schemas/ — $schema is a
// version selector and is never fetched.
//
// A returned error, or an error-severity package diagnostic, means the package
// is rejected and Plugin is nil. Content that fails at a narrower boundary —
// one component location, one member, one MCP entry — is skipped with a
// warning diagnostic and recorded in Plugin.Invalid while valid siblings load.
package plugindecode

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// Read limits. JSON configuration reads (manifest, MCP, hooks files) are
// bounded at maxJSONBytes; contained-file digests at maxPluginFileBytes.
const (
	maxJSONBytes       = 1 << 20
	maxPluginFileBytes = 32 << 20
)

// Bundled Agent Plugins 1.0.0 schemas, fetched once at authoring time and
// committed; they are never fetched at runtime.
var (
	//go:embed schemas/plugin.schema.json
	pluginSchemaJSON []byte
	//go:embed schemas/mcp.schema.json
	mcpSchemaJSON []byte
)

const agentPluginSchemaID = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
const agentPluginMcpSchemaID = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"

// Decode reads the package rooted at rootPath as format and returns the
// plugin IR plus the diagnostics produced along the way.
func Decode(rootPath, format string) (*ir.Plugin, []*diag.Diagnostic, error) {
	return decodePackage(rootPath, format, nil)
}

// decodePackage is Decode with an optional catalog entry overlay: a
// non-nil overlay applies the catalog's declared entry-overlay policy, in
// which a missing package manifest means the entry itself is the manifest.
func decodePackage(rootPath, format string, overlay *EntryOverlay) (*ir.Plugin, []*diag.Diagnostic, error) {
	info, known, err := pluginpolicy.LookupPackageFormat(format)
	if err != nil {
		return nil, nil, err
	}
	layout, hasLayout := target.LookupPackageLayout(format)
	if !known || info.Role != pluginpolicy.RolePackage || !hasLayout || layout.CatalogRoot != "" {
		return nil, nil, diag.NewCodedError(diag.CodePluginFormatUnsupported, diag.SourceRef{},
			fmt.Errorf("format %q is not a recognized plugin package format", format))
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = root.Close() }()

	d := &decoder{root: root, src: rootFS{root: root}, format: format, info: info, layout: layout, overlay: overlay}
	files, err := d.containedFiles()
	if err != nil {
		return nil, d.diags, err
	}
	_, manifestErr := d.root.Stat(d.layout.ManifestPath)
	manifestPresent := manifestErr == nil
	if manifestPresent || overlay == nil {
		if err := d.decodeManifest(); err != nil {
			return nil, d.diags, err
		}
	} else {
		if err := d.decodeOverlayManifest(); err != nil {
			return nil, d.diags, err
		}
	}
	d.discoverComponents()
	if overlay != nil {
		if err := d.applyEntryOverlay(manifestPresent); err != nil {
			return nil, d.diags, err
		}
	}
	plugin := &ir.Plugin{
		Format:        format,
		FormatVersion: d.formatVersion,
		Manifest:      d.manifest,
		Members:       d.members,
		Files:         files,
		Extensions:    d.extensions,
		Invalid:       d.invalid,
	}
	return plugin, d.diags, nil
}

// rootFS adapts *os.Root to the fs.FS surface the leaf loaders need; every
// lookup stays inside the package root.
type rootFS struct {
	root *os.Root
}

func (f rootFS) Open(name string) (fs.File, error) { return f.root.Open(name) }

func (f rootFS) ReadDir(name string) ([]fs.DirEntry, error) {
	file, err := f.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	entries, err := file.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

func (f rootFS) Stat(name string) (fs.FileInfo, error) { return f.root.Stat(name) }

type decoder struct {
	root    *os.Root
	src     fs.FS
	format  string
	info    pluginpolicy.PackageFormat
	layout  target.PackageLayout
	overlay *EntryOverlay

	formatVersion string
	manifest      ir.Manifest
	extensions    map[string]any
	members       []ir.PluginMember
	invalid       []ir.PluginRecord
	diags         []*diag.Diagnostic
	seenKeys      map[string]bool
	manifestObj   map[string]any
}

func (d *decoder) warn(code, message, file string) {
	d.diags = append(d.diags, &diag.Diagnostic{
		Code:     code,
		Severity: diag.SeverityWarning,
		Resource: d.manifest.Name,
		Message:  message,
		Source:   diag.SourceRef{File: file},
	})
}

func (d *decoder) record(path, kind, reason, code string) {
	d.invalid = append(d.invalid, ir.PluginRecord{Path: path, Kind: kind, Reason: reason, Diagnostic: code})
}

// readBoundedJSON reads path from root, bounded, and decodes it into
// map[string]any through the JSONC parser that rejects duplicate keys.
func readBoundedJSON(root *os.Root, name string) (map[string]any, error) {
	f, err := root.Open(name)
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
	var obj map[string]any
	if err := jsonc.Unmarshal(name, data, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, codedErr(diag.CodePluginFormatManifestInvalid, name, "top-level value must be a JSON object")
	}
	return obj, nil
}

// readJSON reads path from the package root through readBoundedJSON.
func (d *decoder) readJSON(name string) (map[string]any, error) {
	return readBoundedJSON(d.root, name)
}

// containedFiles walks the package root once, collecting the regular-file
// set and rejecting escapes, cycles, special files, case-fold collisions,
// and oversized files before any component decoding. A symlink resolving to
// a regular file contributes a PluginFile at the link path with the resolved
// content's digest and executable bit; a symlink resolving to a directory is
// walked under the link path. A directory link is a cycle only when its
// resolved target is the package root or an ancestor of the link's own path
// (compared via os.SameFile, not error text); two links to one directory are
// allowed. A link that cannot be resolved at all (escape, dangling, or
// cyclic) reports the single path-escape code.
func (d *decoder) containedFiles() ([]ir.PluginFile, error) {
	var files []ir.PluginFile
	seenFold := map[string]string{}
	addFile := func(p string, info os.FileInfo) error {
		if info.Size() > maxPluginFileBytes {
			return codedErr(diag.CodePluginPackageOversized, p, "file exceeds the bounded read limit")
		}
		f, err := d.root.Open(p)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		files = append(files, ir.PluginFile{
			Path:       p,
			Digest:     fmt.Sprintf("%x", h.Sum(nil)),
			Executable: info.Mode()&0o111 != 0,
		})
		return nil
	}
	var walkFn fs.WalkDirFunc
	walkFn = func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == "." {
			return nil
		}
		folded := strings.ToLower(p)
		if first, clash := seenFold[folded]; clash && first != p {
			return codedErr(diag.CodePluginPackageCaseCollision, p,
				"path collides with %q under case folding", first)
		}
		seenFold[folded] = p
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			// A symlink that resolves inside the root is allowed (Agent
			// Plugins §4.1); os.Root refuses escapes itself, and an
			// unresolvable link — escape, dangling, or cyclic — reports
			// the single path-escape code.
			info, err := d.root.Stat(p)
			if err != nil {
				return codedErr(diag.CodePluginPackagePathEscape, p,
					"symlink does not resolve inside the package root")
			}
			if info.IsDir() {
				// A link to the root or to one of its own ancestors
				// would recurse forever; every other target is walked
				// under the link path.
				for ancestor := path.Dir(p); ; ancestor = path.Dir(ancestor) {
					ancestorInfo, err := d.root.Stat(ancestor)
					if err != nil {
						return err
					}
					if os.SameFile(ancestorInfo, info) {
						return codedErr(diag.CodePluginPackageSymlinkCycle, p,
							"symlinked directory resolves to the package root or an ancestor of the link")
					}
					if ancestor == "." {
						break
					}
				}
				return fs.WalkDir(d.src, p, walkFn)
			}
			if !info.Mode().IsRegular() {
				return codedErr(diag.CodePluginPackageSpecialFile, p,
					"package entry is not a regular file or directory")
			}
			return addFile(p, info)
		}
		if !entry.Type().IsRegular() {
			return codedErr(diag.CodePluginPackageSpecialFile, p, "package entry is not a regular file or directory")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return addFile(p, info)
	}
	if err := fs.WalkDir(d.src, ".", walkFn); err != nil {
		return nil, err
	}
	return files, nil
}
