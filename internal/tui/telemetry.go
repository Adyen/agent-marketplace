package tui

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func telemetryFromContext(ctx context.Context) *telemetry.Telemetry {
	return telemetry.TelemetryFromContext(ctxOrBackground(ctx))
}

func recordActionSpan(ctx context.Context, name string, attrs map[string]string) {
	tel := telemetryFromContext(ctx)
	if tel == nil {
		return
	}

	base := map[string]string{"action": name}
	for k, v := range attrs {
		base[k] = v
	}

	_, span := tel.StartSpan(ctxOrBackground(ctx), "agent_marketplace.tui.action", base)
	span.End()
}

func recordControlledErrorSpan(ctx context.Context, action string, err error) {
	tel := telemetryFromContext(ctx)
	if tel == nil || err == nil {
		return
	}

	attrs := map[string]string{"action": action}
	if coded, ok := diag.AsCodedError(err); ok && coded.Code != "" {
		attrs["error.code"] = coded.Code
	}

	_, span := tel.StartSpan(ctxOrBackground(ctx), "agent_marketplace.tui.controlled_error", attrs)
	span.End()
}

const configResourceChunkSize = 64

type configResourceGroupKey struct {
	kind     string
	location string
	path     string
	ref      string
	scope    string
	required bool
	remote   bool
}

func recordConfigSnapshotSpans(ctx context.Context, snapshot snapshotLoadedMsg) {
	tel := telemetryFromContext(ctx)
	if tel == nil || !tel.TracingEnabled() {
		return
	}
	result := snapshot.inventoryResult
	if result == nil {
		result = &pipeline.ResolveResult{
			MergedCfg:           &config.Config{Registries: snapshot.configuredRegistries},
			Resolved:            snapshot.resolved,
			TargetHarnesses:     snapshot.targetHarnesses,
			TargetHarnessSource: snapshot.targetHarnessSource,
		}
	}
	inventory, _, ok := pipeline.BuildInventorySnapshot(*result)
	if !ok {
		return
	}

	spanCtx, span := tel.StartSpan(ctxOrBackground(ctx), "agent_marketplace.tui.config", map[string]string{
		"harnesses":      inventory.Harnesses,
		"harness.scope":  inventory.HarnessScope,
		"registry.count": strconv.Itoa(len(inventory.Registries)),
		"resource.count": strconv.Itoa(len(inventory.Resources)),
	})
	defer span.End()

	for _, registry := range inventory.Registries {
		attrs := map[string]string{
			"registry.location": registry.Location,
			"config.scope":      registry.ConfigScope,
			"registry.required": strconv.FormatBool(registry.Required),
			"registry.default":  strconv.FormatBool(registry.Default),
		}
		if registry.ID != "local" {
			attrs["registry.path"] = registry.Path
			attrs["registry.ref"] = registry.Ref
		}
		_, registrySpan := tel.StartSpan(spanCtx, "agent_marketplace.tui.config_registry", attrs)
		registrySpan.End()
	}

	groups := make(map[configResourceGroupKey][]string)
	for _, resource := range inventory.Resources {
		key := configResourceGroupKey{
			kind:     resource.Kind,
			location: resource.RegistryLocation,
			path:     resource.RegistryPath,
			ref:      resource.RegistryRef,
			scope:    resource.ConfigScope,
			required: resource.Required,
			remote:   resource.RegistryID != "local",
		}
		groups[key] = append(groups[key], resource.Name)
	}

	keys := make([]configResourceGroupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].kind != keys[j].kind {
			return keys[i].kind < keys[j].kind
		}
		if keys[i].location != keys[j].location {
			return keys[i].location < keys[j].location
		}
		if keys[i].path != keys[j].path {
			return keys[i].path < keys[j].path
		}
		if keys[i].scope != keys[j].scope {
			return keys[i].scope < keys[j].scope
		}
		if keys[i].ref != keys[j].ref {
			return keys[i].ref < keys[j].ref
		}
		return !keys[i].required && keys[j].required
	})
	for _, key := range keys {
		names := groups[key]
		sort.Strings(names)
		chunks := (len(names) + configResourceChunkSize - 1) / configResourceChunkSize
		for chunk := 0; chunk < chunks; chunk++ {
			start := chunk * configResourceChunkSize
			end := min(start+configResourceChunkSize, len(names))
			encoded, _ := json.Marshal(names[start:end])
			attrs := map[string]string{
				"resource.kind":     key.kind,
				"resource.names":    string(encoded),
				"resource.count":    strconv.Itoa(len(names)),
				"registry.location": key.location,
				"config.scope":      key.scope,
				"resource.required": strconv.FormatBool(key.required),
				"chunk.index":       strconv.Itoa(chunk + 1),
				"chunk.count":       strconv.Itoa(chunks),
			}
			if key.remote {
				attrs["registry.ref"] = key.ref
				if key.path != "" {
					attrs["registry.path"] = key.path
				}
			}
			_, resourceSpan := tel.StartSpan(spanCtx, "agent_marketplace.tui.config_resources", attrs)
			resourceSpan.End()
		}
	}
}

func recordToggleActionSpan(ctx context.Context, act toggleAction, kind ir.ResourceKind) {
	name := "remove"
	if act.enable {
		name = "install"
	}
	recordActionSpan(ctx, name, map[string]string{
		"scope":         scopeName(act.scope),
		"resource.kind": kind.String(),
	})
}

func tabName(t tab) string {
	switch t {
	case tabScope:
		return "scope"
	case tabRegistry:
		return "registry"
	case tabStatus:
		return "status"
	}
	return "unknown"
}

func scopeName(s scope) string {
	switch s {
	case scopeGlobal:
		return "global"
	case scopeProject:
		return "project"
	case scopeLocal:
		return "local"
	case scopeLocalExclude:
		return "local_exclude"
	case scopeUserExclude:
		return "user_exclude"
	}
	return "unknown"
}
