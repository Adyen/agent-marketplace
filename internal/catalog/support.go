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
