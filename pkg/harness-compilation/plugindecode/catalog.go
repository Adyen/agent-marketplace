package plugindecode

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// CatalogSource is one resolved entry origin: a catalog-root-relative path
// or an exact Git requirement.
type CatalogSource struct {
	// Kind is the catalog's entry source type: "path" or "git".
	Kind string
	// Path is the resolved slash-relative path under the catalog root for
	// Kind "path" ("plugins/review-kit"); "." names the catalog root itself.
	Path string
	// URL, Ref, and Subdirectory describe the exact Git acquisition
	// requirement for Kind "git". Ref carries the entry's sha when set, else
	// its ref; an empty Ref resolves the repository's default branch.
	URL          string
	Ref          string
	Subdirectory string
}

// CatalogEntry is one outcome per catalog entry. A non-installable entry is
// still reported with its diagnostic; Installable is false exactly when the
// entry's source type is refused, its name or source is malformed, or its
// name duplicates another entry.
type CatalogEntry struct {
	Name   string
	Source CatalogSource
	// Strict is the entry's overlay flag under the catalog's
	// entry-overlay.strict policy (default true); meaningless for catalogs
	// without an overlay policy.
	Strict bool
	// Overlay carries the entry's definition fields beyond name and source
	// (description, component fields, strict), for overlay application.
	Overlay map[string]any
	// DefinitionDigest is the sha256 hex digest of the canonical entry
	// definition, committed to by plugin lock records.
	DefinitionDigest string
	Installable      bool
	// Diagnostic is the first refusal diagnostic when Installable is false.
	Diagnostic *diag.Diagnostic
}

// Catalog is the decoded form of a native plugin catalog (marketplace)
// manifest: the catalog name plus one outcome per declared entry.
type Catalog struct {
	Format  string
	Name    string
	Entries []CatalogEntry
}

// catalogSourceSpellings maps each format's native entry source spellings
// onto the format catalog's source types (path, github, git, npm, url,
// command). Claude and Factory share the Claude entry vocabulary; Codex
// uses its own.
var catalogSourceSpellings = map[string]map[string]string{
	"claude-marketplace": {
		"path": "path", "github": "github", "url": "git", "git-subdir": "git",
		"npm": "npm", "archive": "url", "command": "command",
	},
	"factory-marketplace": {
		"path": "path", "github": "github", "url": "git", "git-subdir": "git",
		"npm": "npm", "archive": "url", "command": "command",
	},
	"codex-marketplace": {
		"path": "path", "git": "git", "git-subdir": "git", "github": "github",
		"npm": "npm", "url": "url", "archive": "url", "command": "command",
	},
}

var githubRepoForm = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// DecodeCatalog reads the native plugin catalog rooted at rootPath as format
// and returns one outcome per declared entry. A returned error means the
// catalog itself was rejected; per-entry refusals live on the entries.
func DecodeCatalog(rootPath, format string) (*Catalog, []*diag.Diagnostic, error) {
	info, known, err := pluginpolicy.LookupPackageFormat(format)
	if err != nil {
		return nil, nil, err
	}
	layout, hasLayout := target.LookupPackageLayout(format)
	if !known || info.Role != pluginpolicy.RoleCatalog || !hasLayout || layout.CatalogRoot == "" {
		return nil, nil, diag.NewCodedError(diag.CodePluginFormatUnsupported, diag.SourceRef{},
			fmt.Errorf("format %q is not a recognized plugin catalog format", format))
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = root.Close() }()
	obj, err := readBoundedJSON(root, layout.ManifestPath)
	if err != nil {
		var ce *diag.CodedError
		if errors.As(err, &ce) {
			return nil, nil, err
		}
		return nil, nil, codedErr(diag.CodePluginFormatManifestInvalid, layout.ManifestPath, "catalog manifest cannot be read: %v", err)
	}
	var diags []*diag.Diagnostic
	warn := func(code, message string) {
		diags = append(diags, &diag.Diagnostic{
			Code:     code,
			Severity: diag.SeverityWarning,
			Message:  message,
			Source:   diag.SourceRef{File: layout.ManifestPath},
		})
	}
	for field, rule := range info.ManifestFields {
		raw, present := obj[field]
		if !present {
			if rule.Required {
				return nil, diags, codedErr(diag.CodePluginFormatManifestInvalid, layout.ManifestPath,
					"catalog manifest lacks required field %q", field)
			}
			continue
		}
		if jsonType(raw) != rule.Type {
			if rule.Invalid == "refuse-package" || rule.Required {
				return nil, diags, codedErr(diag.CodePluginFormatManifestInvalid, layout.ManifestPath,
					"catalog manifest field %q must be a %s", field, rule.Type)
			}
			warn(diag.CodePluginFormatFieldIgnored,
				fmt.Sprintf("catalog manifest field %q is not a %s; it is ignored", field, rule.Type))
		}
	}
	name, _ := obj["name"].(string)
	if name == "" {
		return nil, diags, codedErr(diag.CodePluginFormatManifestInvalid, layout.ManifestPath, "catalog manifest lacks a valid required name")
	}
	pluginRoot := ""
	if meta, ok := obj["metadata"].(map[string]any); ok {
		pluginRoot, _ = meta["pluginRoot"].(string)
	}
	rawPlugins, _ := obj["plugins"].([]any)
	catalog := &Catalog{Format: format, Name: name}
	pluginFormat := strings.TrimSuffix(format, "-marketplace") + "-plugin"
	for _, raw := range rawPlugins {
		entry, entryDiags := decodeCatalogEntry(raw, catalogSourceSpellings[format], info, pluginFormat, name, pluginRoot, layout.ManifestPath)
		catalog.Entries = append(catalog.Entries, entry)
		diags = append(diags, entryDiags...)
	}
	// Duplicate entry names mark every copy; none is installable.
	counts := map[string]int{}
	for _, entry := range catalog.Entries {
		if entry.Name != "" {
			counts[entry.Name]++
		}
	}
	for i := range catalog.Entries {
		entry := &catalog.Entries[i]
		if counts[entry.Name] < 2 {
			continue
		}
		d := &diag.Diagnostic{
			Code:     diag.CodePluginCatalogDuplicateEntry,
			Severity: diag.SeverityWarning,
			Resource: entry.Name,
			Message:  fmt.Sprintf("catalog entry name %q is declared more than once; no copy is installable", entry.Name),
			Source:   diag.SourceRef{File: layout.ManifestPath},
		}
		if entry.Diagnostic == nil {
			entry.Diagnostic = d
		}
		entry.Installable = false
		diags = append(diags, d)
	}
	return catalog, diags, nil
}

// decodeCatalogEntry interprets one plugins array element. Invalid entries
// and refused source types produce a diagnostic on the entry and leave it
// not installable while siblings load.
func decodeCatalogEntry(raw any, spellings map[string]string, info pluginpolicy.PackageFormat, pluginFormat, catalogName, pluginRoot, manifestPath string) (CatalogEntry, []*diag.Diagnostic) {
	var diags []*diag.Diagnostic
	entryDiag := func(code, message string) *diag.Diagnostic {
		d := &diag.Diagnostic{
			Code:     code,
			Severity: diag.SeverityWarning,
			Message:  message,
			Source:   diag.SourceRef{File: manifestPath},
		}
		diags = append(diags, d)
		return d
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return CatalogEntry{Diagnostic: entryDiag(diag.CodePluginMemberInvalid, "catalog entry is invalid: entry must be an object")}, diags
	}
	name, _ := obj["name"].(string)
	entry := CatalogEntry{Name: name}
	invalid := func(reason string) CatalogEntry {
		entry.Diagnostic = entryDiag(diag.CodePluginMemberInvalid, "catalog entry is invalid: "+reason)
		entry.Diagnostic.Resource = name
		return entry
	}
	if err := ir.ValidatePluginName(pluginFormat, name); err != nil {
		return invalid("entry lacks a valid required name: " + err.Error()), diags
	}
	rawSource, present := obj["source"]
	if !present {
		return invalid("entry lacks a required source"), diags
	}
	data, _ := json.Marshal(obj)
	entry.DefinitionDigest = fmt.Sprintf("%x", sha256.Sum256(data))
	entry.Strict = true
	if raw, has := obj["strict"]; has {
		b, isBool := raw.(bool)
		if !isBool {
			return invalid("entry strict must be a boolean"), diags
		}
		entry.Strict = b
	}
	entry.Overlay = make(map[string]any, len(obj))
	for k, v := range obj {
		if k != "name" && k != "source" {
			entry.Overlay[k] = v
		}
	}
	sourceType, sourceFields := catalogEntrySourceType(rawSource)
	mapped, recognized := spellings[sourceType]
	if !recognized || info.CatalogSources[mapped] != pluginpolicy.SourceAcquire {
		entry.Diagnostic = entryDiag(diag.CodePluginCatalogSourceUnsupported,
			fmt.Sprintf("catalog %q entry %q uses source type %q, which the format refuses", catalogName, name, sourceType))
		entry.Diagnostic.Resource = name
		return entry, diags
	}
	source, reason := catalogEntrySource(mapped, rawSource, sourceFields, pluginRoot)
	if reason != "" {
		return invalid(reason), diags
	}
	entry.Source = source
	entry.Installable = true
	return entry, diags
}

// catalogEntrySourceType reports the entry's declared source spelling: the
// string itself for relative-path sources, or the source object's source key.
func catalogEntrySourceType(rawSource any) (string, map[string]any) {
	if _, isString := rawSource.(string); isString {
		return "path", nil
	}
	obj, ok := rawSource.(map[string]any)
	if !ok {
		return "", nil
	}
	typ, _ := obj["source"].(string)
	return typ, obj
}

// catalogEntrySource resolves one acquired entry source into a catalog-root
// path or an exact Git requirement.
func catalogEntrySource(mapped string, rawSource any, fields map[string]any, pluginRoot string) (CatalogSource, string) {
	switch mapped {
	case "path":
		value, _ := rawSource.(string)
		if fields != nil {
			value, _ = fields["path"].(string)
		}
		resolved, ok := catalogRelPath(value, pluginRoot)
		if !ok {
			return CatalogSource{}, "entry source does not resolve to a path inside the catalog root"
		}
		return CatalogSource{Kind: "path", Path: resolved}, ""
	case "github":
		repo, _ := fields["repo"].(string)
		if !githubRepoForm.MatchString(repo) {
			return CatalogSource{}, "github entry requires an owner/repo"
		}
		return CatalogSource{Kind: "git", URL: "https://github.com/" + repo + ".git", Ref: gitRequirementRef(fields)}, ""
	case "git":
		rawURL, _ := fields["url"].(string)
		if rawURL == "" {
			rawURL, _ = fields["repo"].(string)
			if githubRepoForm.MatchString(rawURL) {
				rawURL = "https://github.com/" + rawURL + ".git"
			}
		}
		if rawURL == "" || strings.ContainsAny(rawURL, "\x00\r\n") {
			return CatalogSource{}, "git entry requires a repository url"
		}
		subdir, _ := fields["path"].(string)
		if subdir != "" && !validPackageRelPath(subdir) {
			return CatalogSource{}, "git entry subdirectory must be a clean relative path"
		}
		return CatalogSource{Kind: "git", URL: rawURL, Ref: gitRequirementRef(fields), Subdirectory: subdir}, ""
	}
	return CatalogSource{}, "entry source type is unsupported"
}

// gitRequirementRef applies the shared ref/sha rule: an explicit sha wins
// over ref; both empty means the repository's default branch.
func gitRequirementRef(fields map[string]any) string {
	if sha, _ := fields["sha"].(string); sha != "" {
		return sha
	}
	ref, _ := fields["ref"].(string)
	return ref
}

// catalogRelPath resolves an authored path source against the catalog root:
// "." names the root, "./x" a subdirectory, and a bare name resolves under
// the catalog's metadata.pluginRoot.
func catalogRelPath(value, pluginRoot string) (string, bool) {
	if value == "." {
		return ".", true
	}
	if strings.ContainsAny(value, "\\\x00") || path.IsAbs(value) {
		return "", false
	}
	if strings.HasPrefix(value, "./") {
		value = value[2:]
	} else if !strings.Contains(value, "/") && pluginRoot != "" {
		base := strings.TrimPrefix(pluginRoot, "./")
		value = path.Join(base, value)
	} else {
		return "", false
	}
	if value == "" || !fs.ValidPath(value) {
		return "", false
	}
	return value, true
}
