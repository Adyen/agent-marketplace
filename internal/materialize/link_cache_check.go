package materialize

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// linkedCacheChanged plans the cache writes a real sync would perform, without
// preparing the cache or replacing the native link that exposes its content.
func linkedCacheChanged(link skillLink) (bool, error) {
	if len(link.artifacts) == 0 {
		return false, nil
	}
	cacheDir := link.cacheDir
	if cacheDir == "" {
		cacheDir = link.target
	}
	desired := append([]artifact.Artifact(nil), link.artifacts...)
	compiled := make(map[string]bool, len(desired))
	for _, a := range desired {
		compiled[filepath.ToSlash(filepath.Clean(a.Path))] = true
	}
	supportModes := make(map[string]os.FileMode)
	err := walkPortableSkillSupport(link, func(string) {}, func(root *fsboundary.Dir, sourceChild, relative string) error {
		if compiled[relative] {
			return nil
		}
		data, identity, err := readSupportSourceSnapshot(root, sourceChild)
		if err != nil {
			return err
		}
		desired = append(desired, artifact.Artifact{Path: relative, Bytes: data, Mode: identity.Mode.Perm()})
		supportModes[relative] = identity.Mode.Perm()
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("read desired support for linked cache %q: %w", cacheDir, err)
	}
	report, err := hcc.Materialize(&hcc.Result{Artifacts: desired}, cacheDir,
		hcc.WithWriteMode(hcc.WriteModeOverwrite), hcc.WithDryRun(), hcc.WithPruneStale(link.sourceDir != ""))
	if err != nil {
		return false, fmt.Errorf("compare linked cache %q: %w", cacheDir, err)
	}
	if len(report.Written) > 0 || len(report.Removed) > 0 {
		return true, nil
	}
	// SDK byte comparisons intentionally ignore modes; support copies restore
	// source permissions, unlike compiled files whose equal bytes are a no-op.
	for relative, mode := range supportModes {
		supportPath := filepath.Join(cacheDir, relative)
		info, err := os.Lstat(supportPath)
		if err != nil {
			return false, fmt.Errorf("inspect linked cache support %q: %w", supportPath, err)
		}
		if info.Mode().Perm() != mode {
			return true, nil
		}
	}
	return false, nil
}
