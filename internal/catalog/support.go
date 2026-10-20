package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/harness"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var (
	ErrHookSupportMissing     = errors.New("marketplace.hook.support-missing")
	ErrHookSupportIntegrity   = errors.New("marketplace.hook.support-integrity")
	ErrHookSupportUnsupported = errors.New("marketplace.hook.support-unsupported")
)

// ValidateMirroredHookSupport checks the intrinsic inventory, not a mutable cache tree.
func ValidateMirroredHookSupport(hook *ir.Hook, support map[string]artifact.SupportFile) error {
	excluded := HookSupportExclusions(hook.Source.File, hook.HookSource)
	excluded["hooks.json"] = true
	for name, expected := range hook.SupportFiles {
		if err := artifact.ValidateSupportPath(name); err != nil || excluded[path.Base(name)] {
			return fmt.Errorf("%w: invalid or reserved support declaration", ErrHookSupportIntegrity)
		}
		file, exists := support[name]
		if !exists {
			return fmt.Errorf("%w: declared support is absent", ErrHookSupportMissing)
		}
		if artifact.SupportFileDigest(file) != expected {
			return fmt.Errorf("%w: support bytes or executable mode differ", ErrHookSupportIntegrity)
		}
	}
	for name := range support {
		if _, exists := hook.SupportFiles[name]; !exists {
			return fmt.Errorf("%w: undeclared support file", ErrHookSupportIntegrity)
		}
	}
	missing, unsupported, err := HookSupportReferenceStatus(hook.Command, hook.SupportFiles)
	if err != nil {
		return fmt.Errorf("read hook support policy: %w", err)
	}
	if unsupported {
		return fmt.Errorf("%w: assets fall outside the adjacent-file policy; keep the Git source", ErrHookSupportUnsupported)
	}
	if missing {
		return fmt.Errorf("%w: publish a revision containing the adjacent asset or keep the Git source", ErrHookSupportMissing)
	}
	return nil
}

func HookSupportFiles(sourcePath string) ([]string, error) {
	if sourcePath == "" {
		return nil, nil
	}
	return HookSupportFilesFS(os.DirFS(filepath.Dir(sourcePath)), filepath.Base(sourcePath))
}

func HookSupportFilesFS(source fs.FS, sourcePath string) ([]string, error) {
	if sourcePath == "" {
		return nil, nil
	}
	entries, err := fs.ReadDir(source, path.Dir(sourcePath))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.Name() != "hooks.json" && !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// HookSupportExclusions excludes native definitions, not executable assets.
// Keep HookSupportFiles unchanged: historical acquisition locks hash its superset.
func HookSupportExclusions(sourcePath, sourceHarness string) map[string]bool {
	primary := filepath.Base(sourcePath)
	excluded := map[string]bool{primary: true}
	info, ok := harness.TargetInfoFor(hcc.Target(sourceHarness))
	if !ok {
		return excluded
	}
	groups := [...][]string{info.AggregateFiles.Project, info.AggregateFiles.User}
	for _, group := range groups {
		for _, source := range group {
			if filepath.Base(source) != primary {
				continue
			}
			for _, siblings := range groups {
				for _, sibling := range siblings {
					if filepath.Dir(source) == filepath.Dir(sibling) {
						excluded[filepath.Base(sibling)] = true
					}
				}
			}
		}
	}
	return excluded
}

type hookReferencePatterns struct {
	asset, root *regexp.Regexp
}

var hookSupportReferences = sync.OnceValues(func() (hookReferencePatterns, error) {
	names, err := hookpolicy.PluginRootNames()
	if err != nil {
		return hookReferencePatterns{}, err
	}
	for i, name := range names {
		names[i] = regexp.QuoteMeta(name)
	}
	alternatives := strings.Join(names, "|")
	root := `\$(?:\{(?:` + alternatives + `)\}|(?:` + alternatives + `)\b)`
	asset := root + `/hooks/([^\s"';&|<>()$` + "`" + `]+)`
	return hookReferencePatterns{asset: regexp.MustCompile(asset), root: regexp.MustCompile(root)}, nil
})

// HookSupportReferenceStatus distinguishes missing adjacent assets from
// plugin-root expressions outside the adjacent-file distribution policy.
func HookSupportReferenceStatus(command string, declared map[string]string) (missing, unsupported bool, err error) {
	references, err := hookSupportReferences()
	if err != nil {
		return false, false, err
	}
	for _, reference := range references.asset.FindAllStringSubmatch(command, -1) {
		if strings.Contains(reference[1], "/") {
			unsupported = true
		} else if _, exists := declared["hooks/"+reference[1]]; !exists {
			missing = true
		}
		command = strings.ReplaceAll(command, reference[0], "")
	}
	unsupported = unsupported || references.root.MatchString(command)
	return missing, unsupported, nil
}

// HookSupportReferenceResult reports the outcome of checking literal
// plugin-root path references in a hook command against the installed
// bundle-relative support paths.
type HookSupportReferenceResult struct {
	Command string // command with legacy references rewritten to their bundle-relative path
	Missing bool   // a literal reference resolved under neither meaning
	Legacy  bool   // a reference resolved only under the legacy folder meaning
}

// CheckHookSupportReferences checks each literal ${TOKEN}/<path> reference in
// command outside single quotes against the installed bundle-relative support
// paths. A reference that resolves only under the legacy folder meaning,
// prefixing the path with legacyPrefix, is rewritten to that path; an empty
// legacyPrefix disables the fallback. Bare tokens and references whose path
// carries a shell expansion or glob are not checked.
func CheckHookSupportReferences(command string, installed []string, legacyPrefix string) (HookSupportReferenceResult, error) {
	names, err := hookpolicy.PluginRootNames()
	if err != nil {
		return HookSupportReferenceResult{}, err
	}
	pluginNames := make(map[string]bool, len(names))
	for _, name := range names {
		pluginNames[name] = true
	}
	resolved := func(rel string) bool {
		for _, p := range installed {
			if p == rel || strings.HasPrefix(p, rel+"/") {
				return true
			}
		}
		return false
	}
	result := HookSupportReferenceResult{Command: command}
	var out strings.Builder
	pos := 0
	var edits bool
	var quote byte
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch quote {
		case '\'':
			if c == '\'' {
				quote = 0
			}
			continue
		case '"':
			switch {
			case c == '"':
				quote = 0
				continue
			case c == '\\' && i+1 < len(command) && strings.IndexByte("$`\"\\", command[i+1]) >= 0:
				i++
				continue
			case c != '$':
				continue
			}
		default:
			switch c {
			case '\\':
				i++
				continue
			case '\'', '"':
				quote = c
				continue
			case '$':
			default:
				continue
			}
		}
		if i+1 >= len(command) || command[i+1] == '(' {
			continue
		}
		end := hookPluginRootTokenEnd(command, i, pluginNames)
		if end <= i {
			continue
		}
		if end < len(command) && command[end] == '/' {
			pathStart := end + 1
			k := pathStart
			dynamic := false
			quoted := quote == '"'
			for k < len(command) {
				ch := command[k]
				if ch == '\\' {
					dynamic = true
					k = min(k+2, len(command))
					continue
				}
				if ch == '$' || ch == '`' {
					dynamic = true
					k++
					continue
				}
				if quoted {
					if ch == '"' {
						break
					}
				} else if strings.IndexByte(" \t\n\"';&|<>()", ch) >= 0 {
					break
				}
				k++
			}
			if !dynamic {
				if !quoted {
					// A quote inside an unquoted word means the shell word
					// continues past the scanned fragment.
					dynamic = k < len(command) && (command[k] == '"' || command[k] == '\'')
				} else if k < len(command) {
					// The scan stopped at the closing quote; anything but a
					// word delimiter after it concatenates onto the word.
					dynamic = k+1 < len(command) && strings.IndexByte(" \t\n;&|<>()", command[k+1]) < 0
				} else {
					// An unterminated quote leaves the word open.
					dynamic = true
				}
			}
			rel := strings.Trim(command[pathStart:k], "/")
			if rel != "" && !dynamic && !strings.ContainsAny(rel, "*?[]{}") {
				cleaned := path.Clean(rel)
				switch {
				case cleaned == ".":
				case cleaned == ".." || strings.HasPrefix(cleaned, "../"):
					result.Missing = true
				case resolved(cleaned):
				case legacyPrefix != "" && resolved(legacyPrefix+"/"+cleaned):
					out.WriteString(command[pos:pathStart])
					out.WriteString(legacyPrefix + "/")
					pos = pathStart
					edits = true
					result.Legacy = true
				default:
					result.Missing = true
				}
			}
			i = k - 1
			continue
		}
		i = end - 1
	}
	if edits {
		out.WriteString(command[pos:])
		result.Command = out.String()
	}
	return result, nil
}

// hookPluginRootTokenEnd parses a $NAME or ${NAME} token at command[start] and
// returns the index just past it when the name is a shipped plugin-root
// spelling; otherwise it returns start.
func hookPluginRootTokenEnd(command string, start int, pluginNames map[string]bool) int {
	if start+1 >= len(command) {
		return start
	}
	name := ""
	end := start + 1
	if command[end] == '{' {
		close := strings.IndexByte(command[end:], '}')
		if close < 0 {
			return start
		}
		name = command[end+1 : end+close]
		end = end + close + 1
	} else {
		for end < len(command) {
			c := command[end]
			if c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' {
				end++
				continue
			}
			break
		}
		if end == start+1 {
			return start
		}
		name = command[start+1 : end]
	}
	if !pluginNames[name] {
		return start
	}
	return end
}

// HookMaterializationSupportFiles is shared by publication and installation.
func HookMaterializationSupportFiles(sourcePath, sourceHarness string) ([]string, error) {
	names, err := HookSupportFiles(sourcePath)
	if err != nil {
		return nil, err
	}
	excluded := HookSupportExclusions(sourcePath, sourceHarness)
	result := names[:0]
	for _, name := range names {
		if excluded[name] || (sourceHarness == string(hcc.TargetWindsurf) && filepath.Base(filepath.Dir(sourcePath)) == "hooks" && filepath.Ext(name) == ".json") {
			continue
		}
		result = append(result, name)
	}
	return result, nil
}
