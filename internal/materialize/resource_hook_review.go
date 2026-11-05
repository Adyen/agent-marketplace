package materialize

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"golang.org/x/sys/unix"
)

// maxResourceHookDeclarationBytes bounds one declaration read for review,
// matching the fixed OpenCode adapter's own declaration limit.
const maxResourceHookDeclarationBytes = 1 << 20

var errResourceHookDeclarationTooLarge = errors.New("hook declaration exceeds the review read limit")

// resourceHookDeclaration is one hook declaration a resource-file hook target
// installs as its own file, in the canonical form the review lists.
type resourceHookDeclaration struct {
	event   string
	matcher string
	raw     []byte
}

// parseResourceHookDeclaration canonicalizes declaration bytes for review.
// Anything that is not a declaration object is skipped: the adapter never
// executes it as a hook either.
func parseResourceHookDeclaration(data []byte) (resourceHookDeclaration, bool) {
	var fields struct {
		Format  string `json:"format"`
		Event   string `json:"event"`
		Matcher string `json:"matcher"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(data, &fields); err != nil || fields.Format == "" || fields.Event == "" || fields.Command == "" {
		return resourceHookDeclaration{}, false
	}
	canonical, err := canonicalOwnedJSON(data)
	if err != nil {
		return resourceHookDeclaration{}, false
	}
	return resourceHookDeclaration{event: fields.Event, matcher: fields.Matcher, raw: canonical}, true
}

// installedResourceHookDeclarations reads the owned declarations under a
// resource-file hook target's declaration directory. A regular file is owned
// when the portable manifest records it; a link is owned when it resolves into
// the target's compiled cache. Unowned, unreadable, and oversized entries are
// skipped, so the review lists only owned hook entries.
func installedResourceHookDeclarations(key outputKey, outDir, repoRoot string) map[string]resourceHookDeclaration {
	dirRel := resourceInstallDirRel(key.target, ir.KindHook)
	if dirRel == "" {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(outDir, filepath.FromSlash(dirRel)))
	if err != nil {
		return nil
	}
	state, stateErr := loadPortableState(repoRoot, key.machineScope)
	cacheRoot := filepath.Join(cache.CacheDir(), "compiled", string(key.target)) + string(filepath.Separator)
	declarations := make(map[string]resourceHookDeclaration)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		rel := path.Join(dirRel, entry.Name())
		file := filepath.Join(outDir, filepath.FromSlash(rel))
		info, err := os.Lstat(file)
		if err != nil {
			continue
		}
		read := file
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(file)
			if err != nil || !filepath.IsAbs(target) || !strings.HasPrefix(filepath.Clean(target), cacheRoot) {
				continue
			}
			read = filepath.Clean(target)
		case info.Mode().IsRegular():
			if stateErr != nil {
				continue
			}
			if _, owned := state.entry(key.target, rel); !owned {
				continue
			}
		default:
			continue
		}
		data, err := readResourceHookDeclaration(read)
		if err != nil {
			continue
		}
		if declaration, ok := parseResourceHookDeclaration(data); ok {
			declarations[entry.Name()] = declaration
		}
	}
	return declarations
}

// readResourceHookDeclaration reads one regular file without following a
// final link, so a file swapped after classification is not followed.
func readResourceHookDeclaration(file string) ([]byte, error) {
	f, err := os.OpenFile(file, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxResourceHookDeclarationBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResourceHookDeclarationBytes {
		return nil, errResourceHookDeclarationTooLarge
	}
	return data, nil
}

// desiredResourceHookDeclarations returns the declarations a check would
// publish. While the hook component is blocked, previously installed
// declarations are retained rather than pruned, so they stay in the result.
func desiredResourceHookDeclarations(key outputKey, artifacts []artifact.Artifact, links map[string]*skillLinkGroup, installed map[string]resourceHookDeclaration, retainInstalled bool) map[string]resourceHookDeclaration {
	dirRel := resourceInstallDirRel(key.target, ir.KindHook)
	declarations := make(map[string]resourceHookDeclaration)
	add := func(rel string, data []byte) {
		name, ok := strings.CutPrefix(rel, dirRel+"/")
		if !ok || strings.Contains(name, "/") || !strings.HasSuffix(name, ".json") {
			return
		}
		if declaration, ok := parseResourceHookDeclaration(data); ok {
			declarations[name] = declaration
		}
	}
	for _, a := range artifacts {
		add(a.Path, a.Bytes)
	}
	for _, group := range links {
		for _, link := range group.links {
			if link.kind != ir.KindHook {
				continue
			}
			for _, a := range link.artifacts {
				add(path.Join(group.installDirRel, a.Path), a.Bytes)
			}
		}
	}
	if retainInstalled {
		for name, declaration := range installed {
			if _, ok := declarations[name]; !ok {
				declarations[name] = declaration
			}
		}
	}
	return declarations
}

// resourceHookChanges lists added, replaced, and removed declarations by
// declaration file, in the same shape as aggregate hook review rows.
func resourceHookChanges(key outputKey, outDir string, before, after map[string]resourceHookDeclaration) []HookChange {
	dirRel := resourceInstallDirRel(key.target, ir.KindHook)
	names := make(map[string]bool, len(before)+len(after))
	for name := range before {
		names[name] = true
	}
	for name := range after {
		names[name] = true
	}
	var changes []HookChange
	for name := range names {
		old, hadOld := before[name]
		current, hasCurrent := after[name]
		if hadOld && hasCurrent && bytes.Equal(old.raw, current.raw) {
			continue
		}
		change := HookChange{Target: key.target, MachineScope: key.machineScope, File: filepath.Join(outDir, filepath.FromSlash(dirRel), name)}
		if hadOld {
			change.Event, change.Matcher, change.Before = old.event, old.matcher, json.RawMessage(old.raw)
		}
		if hasCurrent {
			change.Event, change.Matcher, change.After = current.event, current.matcher, json.RawMessage(current.raw)
		}
		changes = append(changes, change)
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].File < changes[j].File })
	return changes
}

// markPendingResourceHookOutputs records pending declaration changes as
// pending outputs, so a check fails on them even when the installed file is a
// link whose target path does not change. Outputs the dry run already counted
// are not counted twice.
func markPendingResourceHookOutputs(report *Report, key outputKey, outDir string, changes []HookChange) {
	counted := make(map[string]bool)
	for _, tr := range report.TargetReports() {
		if tr.Target != key.target || tr.MachineScope != key.machineScope {
			continue
		}
		for _, rel := range append(slices.Clone(tr.Written), tr.Removed...) {
			counted[filepath.Join(tr.OutDir, filepath.FromSlash(rel))] = true
		}
	}
	pending := TargetReport{Target: key.target, MachineScope: key.machineScope, OutDir: outDir}
	for _, change := range changes {
		rel, err := filepath.Rel(outDir, change.File)
		if err != nil || counted[change.File] {
			continue
		}
		rel = filepath.ToSlash(rel)
		if len(change.After) == 0 {
			pending.Removed = append(pending.Removed, rel)
		} else {
			pending.Written = append(pending.Written, rel)
		}
	}
	if len(pending.Written) == 0 && len(pending.Removed) == 0 {
		return
	}
	if len(report.PerRoot) > 0 {
		report.PerRoot = append(report.PerRoot, pending)
	}
	report.PerTarget[key.target] = mergeTargetReports(report.PerTarget[key.target], pending)
}
