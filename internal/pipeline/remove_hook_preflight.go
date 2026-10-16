package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/materialize"
)

func preflightHookRemoval(ctx context.Context, ws Workspace, edits []StagedConfigEdit, extraOverlays map[string][]byte) error {
	if len(edits) == 0 || !materialize.HasActiveNativeHooks(ws.MaterializeRoot()) {
		return nil
	}
	ec := LoadEffective(ctx, ConfigLoadOnline, config.LoadEffectiveOptions{RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir()}, nil)
	ctx, view := exactLockViewWithContext(ctx, ec, true)
	defer view.release()
	options := ResolveOptions{
		Workspace: ws, CatalogMode: catalog.LoadModeInspect,
		CatalogOption:           []catalog.LoadOption{view.catalogOption},
		IgnoreBlockedRegistries: true, SkipInventory: true,
		MarketplaceArtifactsFromCache: true,
	}
	before := Resolve(ctx, options)
	if len(before.Errors) != 0 {
		return errors.Join(before.Errors...)
	}
	overlays := make(map[string][]byte, len(edits)*2)
	maps.Copy(overlays, extraOverlays)
	for _, edit := range edits {
		overlays[edit.Path] = edit.Bytes
		overlays[edit.RequestedPath] = edit.Bytes
	}
	options.EffectiveOpts.FileOverlays = overlays
	after := Resolve(ctx, options)
	if len(after.Errors) != 0 {
		return errors.Join(after.Errors...)
	}
	for _, diagnostic := range materialize.CheckHookRemoval(ws.MaterializeRoot(), before.Resolved, after.Resolved) {
		slog.Warn("hook removal refused", "target", diagnostic.Target, "resource", diagnostic.Resource, "file", diagnostic.Source.File)
		return newDiagIssue(diagnostic)
	}
	return nil
}

// Direct pipeline callers without a dependency reconciler still stage edits so
// the removal preflight runs before their configuration is published.
func commitRemovalConfig(ctx context.Context, _, _ string, _ bool, edits []StagedConfigEdit) (DependencyLockView, error) {
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return DependencyLockView{}, err
	}
	files := make([]dependencytxn.DesiredFile, 0, len(edits))
	for _, edit := range edits {
		if err := os.MkdirAll(filepath.Dir(edit.Path), 0o755); err != nil {
			return DependencyLockView{}, err
		}
		file := dependencytxn.DesiredFile{Path: edit.Path, Expected: edit.Expected, Bytes: edit.Bytes, Mode: edit.Mode}
		if edit.Expected.Exists {
			file.ExpectedMode = edit.Mode
		}
		files = append(files, file)
	}
	_, err = dependencytxn.Commit(ctx, stateDir, files)
	return DependencyLockView{}, err
}
