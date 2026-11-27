package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

// SetDeployedPlaceTargetEnabled toggles a selected place target in the
// existing user config using deployed lock evidence and a journaled config edit.
func SetDeployedPlaceTargetEnabled(ctx context.Context, ws Workspace, targetID string, enable bool) (bool, error) {
	if err := ws.validate(); err != nil {
		return false, err
	}
	if err := validatePlaceTargetID(targetID); err != nil {
		return false, err
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return false, err
	}
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		return false, err
	}
	intentOptions := config.LoadEffectiveOptions{RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir()}
	intent := config.ReadIntent(ctx, intentOptions)
	if len(intent.Errors) > 0 {
		return false, errors.Join(intent.Errors...)
	}
	evidence, err := ReadDeployedLocks(ctx, intent)
	if err != nil {
		return false, err
	}
	effective := config.Bind(intent, evidence)
	if len(effective.Errors) > 0 {
		return false, errors.Join(effective.Errors...)
	}
	if effective.Merged == nil {
		return false, fmt.Errorf("target.config.user-root-required: an existing v4 user config is required")
	}
	userLayer := intent.Layers[config.UserPersonal]
	if !userLayer.Present() || userLayer.Path == "" {
		return false, fmt.Errorf("target.config.user-root-required: target enable and disable require an existing v4 user config")
	}
	if enable {
		if _, _, _, err := readSelectedDeployedPlaceProfile(ctx, intent, evidence, effective.Merged, targetID); err != nil {
			return false, err
		}
	}
	requestedPath := userLayer.Path
	canonicalPath, err := canonicalConfigDestination(requestedPath)
	if err != nil {
		return false, err
	}
	raw, generation, mode, err := dependencytxn.ReadSnapshot(canonicalPath)
	if err != nil {
		return false, err
	}
	if !generation.Exists {
		return false, fmt.Errorf("target.config.user-root-required: target enable and disable require an existing v4 user config")
	}
	userSettings := userLayer.Settings()
	baseline := userSettings.Harnesses
	if !userSettings.HasTargets && len(userSettings.Harnesses) == 0 {
		baseline = intent.Layers[config.Organization].Settings().Harnesses
	}
	rendered, changed, err := config.RenderPlaceTargetToggle(requestedPath, raw, baseline, targetID, enable)
	if err != nil || !changed {
		return false, err
	}
	candidateOptions := intentOptions
	candidateOptions.FileOverlays = map[string][]byte{
		requestedPath: rendered,
		canonicalPath: rendered,
	}
	candidate := config.ReadIntent(ctx, candidateOptions)
	if len(candidate.Errors) > 0 {
		return false, errors.Join(candidate.Errors...)
	}
	candidateEffective := config.Bind(candidate, evidence)
	if len(candidateEffective.Errors) > 0 {
		return false, errors.Join(candidateEffective.Errors...)
	}
	if enable {
		selected, err := candidateEffective.Merged.SelectedMarketplaceProfiles()
		if err != nil {
			return false, err
		}
		if _, ok := selected[targetID]; !ok {
			return false, fmt.Errorf("marketplace.profile.selection-invalid: target %q is not selected after enable", targetID)
		}
	}
	reads, err := deployedPolicyReadAssertions(intent, evidence)
	if err != nil {
		return false, err
	}
	result, err := dependencytxn.Commit(ctx, stateDir, []dependencytxn.DesiredFile{{
		Path: canonicalPath, Expected: generation, ExpectedMode: mode,
		Bytes: rendered, Mode: mode,
	}}, dependencytxn.WithReadAssertions(reads))
	if err != nil {
		return false, fmt.Errorf("commit target configuration: %w", err)
	}
	return len(result.Changed) > 0, nil
}
