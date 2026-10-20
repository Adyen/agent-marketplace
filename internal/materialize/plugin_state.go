package materialize

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/packagegen"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func reconcilePluginState(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, report *Report, namespaceBlocked map[string]bool) {
	if report == nil {
		return
	}
	priorRefs, err := ownership.ReadPluginInstanceStates()
	if err != nil {
		report.Diags = append(report.Diags, diag.Diagnostic{
			Code:     diag.CodePluginGenerationPublish,
			Severity: diag.SeverityError,
			Message:  "plugin ownership state could not be read safely",
		})
		return
	}
	canonicalRoot, err := ownership.CanonicalRoot(repoRoot)
	if err != nil {
		report.Diags = append(report.Diags, diag.Diagnostic{
			Code:     diag.CodePluginGenerationPublish,
			Severity: diag.SeverityError,
			Message:  "plugin ownership root could not be resolved safely",
		})
		return
	}
	activeScopes := map[string]bool{"machine": true, "repo:" + ownership.RootKey(canonicalRoot): true}
	old := make(map[string]ownership.PluginGenerationRef)
	for _, ref := range priorRefs {
		if !activeScopes[ref.Scope] {
			continue
		}
		old[ref.InstanceKey] = ref
	}
	current := make(map[string]ownership.PluginGenerationRef)
	blocked := make(map[string]bool)
	for _, resource := range resolved {
		pkg := resource.PluginPackage
		if pkg == nil || len(resource.Harnesses) == 0 {
			continue
		}
		for _, target := range resource.Harnesses {
			if pkg.InstanceKey == "" {
				continue
			}
			if namespaceBlocked[pluginInstanceIdentity(resource, target)] || pkg.GenerationError != "" || len(pkg.DecodeFailures) > 0 || pkg.GenerationKey == "" {
				blocked[pkg.InstanceKey] = true
				continue
			}
			for _, item := range report.BlockedResources {
				if item.Target == target && item.MachineScope == resource.Source.IsMachineScope() && item.Name == resource.InstalledName {
					blocked[pkg.InstanceKey] = true
					break
				}
			}
			current[pkg.InstanceKey] = ownership.PluginGenerationRef{
				InstanceKey:   pkg.InstanceKey,
				Name:          pkg.Name,
				Target:        string(target),
				Scope:         pkg.GenerationScope,
				GenerationKey: pkg.GenerationKey,
			}
		}
	}
	for key := range blocked {
		delete(current, key)
		if priorRef, ok := old[key]; ok {
			current[key] = priorRef
		}
	}
	for key, ref := range old {
		if _, stillReferenced := current[key]; stillReferenced || blocked[key] {
			continue
		}
		stateRoot, stateErr := packagegen.StateRoot()
		if stateErr == nil {
			path := filepath.Join(stateRoot, "plugin-data", ref.InstanceKey)
			if info, err := os.Lstat(path); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				report.RetainedPluginData = append(report.RetainedPluginData, path)
			}
		}
	}
	if samePluginGenerationRefs(old, current) {
		return
	}
	keys := make([]string, 0, len(old)+len(current))
	seen := make(map[string]bool, len(old)+len(current))
	for key := range old {
		keys = append(keys, key)
		seen[key] = true
	}
	for key := range current {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var participants []dependencytxn.DesiredFile
	for _, key := range keys {
		previous, hadPrevious := old[key]
		next, hasNext := current[key]
		if hadPrevious && hasNext && previous == next {
			continue
		}
		state, statePath, prior, priorMode, err := ownership.ReadPluginState(key)
		if err != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     diag.CodePluginGenerationPublish,
				Severity: diag.SeverityError,
				Message:  "plugin instance ownership state could not be read safely",
			})
			return
		}
		participant := dependencytxn.DesiredFile{Path: statePath, Expected: prior, ExpectedMode: priorMode}
		if hasNext {
			state.Instance = next
			data, err := ownership.EncodePluginState(state)
			if err != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{
					Code:     diag.CodePluginGenerationPublish,
					Severity: diag.SeverityError,
					Message:  fmt.Sprintf("plugin ownership state could not be encoded: %v", err),
				})
				return
			}
			participant.Bytes = data
			participant.Mode = 0o600
		} else {
			participant.Delete = true
		}
		participants = append(participants, participant)
	}
	if len(participants) == 0 {
		return
	}
	for _, participant := range participants {
		if err := os.MkdirAll(filepath.Dir(participant.Path), 0o700); err != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     diag.CodePluginGenerationPublish,
				Severity: diag.SeverityError,
				Message:  "plugin ownership state directory could not be prepared",
			})
			return
		}
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		report.Diags = append(report.Diags, diag.Diagnostic{
			Code:     diag.CodePluginGenerationPublish,
			Severity: diag.SeverityError,
			Message:  "plugin ownership state transaction could not be prepared",
		})
		return
	}
	if _, err := dependencytxn.Commit(ctx, stateDir, participants); err != nil {
		report.Diags = append(report.Diags, diag.Diagnostic{
			Code:     diag.CodePluginGenerationPublish,
			Severity: diag.SeverityError,
			Message:  fmt.Sprintf("plugin ownership state transaction could not be committed: %v", err),
		})
		return
	}
	sort.Strings(report.RetainedPluginData)
	slog.Info("plugin ownership state reconciled", "instances", len(current), "participants", len(participants), "retained_data_paths", len(report.RetainedPluginData))
}

func samePluginGenerationRefs(old, current map[string]ownership.PluginGenerationRef) bool {
	if len(old) != len(current) {
		return false
	}
	for key, prior := range old {
		next, ok := current[key]
		if !ok || prior != next {
			return false
		}
	}
	return true
}
