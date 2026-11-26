package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/materialize"
)

func preflightHookRemoval(ctx context.Context, ws Workspace, edits []StagedConfigEdit, extraOverlays map[string][]byte, removed *missingMarketplaceSelection) error {
	if len(edits) == 0 || !materialize.HasActiveNativeHooks(ws.MaterializeRoot()) {
		return nil
	}
	ctx = WithTransientOrganizationMarketplaceLockCache(ctx)
	beforeOpts := config.LoadEffectiveOptions{RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir()}
	if removed != nil && removed.request.Reason == config.V4SelectionExplicit && !removed.request.Required && removed.request.Options.Revision == "" {
		// Only these direct leaves cannot contribute hooks. An unrecorded
		// collection may have installed hooks, so its before-state is required.
		switch removed.request.Kind {
		case "skill", "command", "subagent", "mcp":
			beforeOpts.SkipMarketplaceSelections = []config.MarketplaceSelectionToSkip{removed.MarketplaceSelectionToSkip}
		}
	}
	ec := LoadEffective(ctx, ConfigLoadOnline, beforeOpts, nil)
	ctx, view := exactLockViewWithContext(ctx, ec, true)
	defer view.release()
	options := ResolveOptions{
		Workspace: ws, CatalogMode: catalog.LoadModeInspect, EffectiveOpts: beforeOpts,
		CatalogOption:           []catalog.LoadOption{view.catalogOption},
		IgnoreBlockedRegistries: true, SkipInventory: true,
		MarketplaceArtifactsFromCache: true,
	}
	before := Resolve(ctx, options)
	if len(before.Errors) != 0 {
		return errors.Join(before.Errors...)
	}
	options.EffectiveOpts = config.LoadEffectiveOptions{FileOverlays: stagedConfigOverlays(edits, extraOverlays)}
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
