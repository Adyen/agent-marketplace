package tui

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
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
	kind     ir.ResourceKind
	location string
	path     string
	ref      string
	scope    string
	required bool
}

func recordConfigSnapshotSpans(ctx context.Context, snapshot snapshotLoadedMsg) {
	tel := telemetryFromContext(ctx)
	if tel == nil || snapshot.mergedCfg == nil {
		return
	}

	spanCtx, span := tel.StartSpan(ctxOrBackground(ctx), "agent_marketplace.tui.config", map[string]string{
		"harnesses":      strings.Join(targetsToHarnesses(snapshot.targetHarnesses), ","),
		"harness.scope":  strings.ToLower(snapshot.targetHarnessSource.String()),
		"registry.count": strconv.Itoa(len(snapshot.configuredRegistries)),
		"resource.count": strconv.Itoa(len(snapshot.resolved)),
	})
	defer span.End()

	registries := append([]config.RegistryEntry(nil), snapshot.configuredRegistries...)
	sort.Slice(registries, func(i, j int) bool {
		left, right := registryTelemetryLocation(registries[i]), registryTelemetryLocation(registries[j])
		if left != right {
			return left < right
		}
		if registries[i].Path != registries[j].Path {
			return registries[i].Path < registries[j].Path
		}
		if registries[i].Source != registries[j].Source {
			return registries[i].Source < registries[j].Source
		}
		if registries[i].Ref != registries[j].Ref {
			return registries[i].Ref < registries[j].Ref
		}
		if registries[i].Required != registries[j].Required {
			return !registries[i].Required
		}
		return !registries[i].Default && registries[j].Default
	})
	for _, registry := range registries {
		location := registryTelemetryLocation(registry)
		attrs := map[string]string{
			"registry.location": location,
			"registry.ref":      registry.Ref,
			"config.scope":      strings.ToLower(registry.Source.String()),
			"registry.required": strconv.FormatBool(registry.Required),
			"registry.default":  strconv.FormatBool(registry.Default),
		}
		if location != "local" {
			attrs["registry.path"] = registry.Path
		}
		_, registrySpan := tel.StartSpan(spanCtx, "agent_marketplace.tui.config_registry", attrs)
		registrySpan.End()
	}

	groups := make(map[configResourceGroupKey][]string)
	for _, resource := range snapshot.resolved {
		registry := config.RegistryEntry{URL: resource.RegistryURL, Ref: resource.Ref}
		location := registryTelemetryLocation(registry)
		key := configResourceGroupKey{
			kind:     resource.Kind,
			location: location,
			ref:      resource.Ref,
			scope:    strings.ToLower(resource.Source.String()),
			required: resource.Required,
		}
		if location != "local" {
			key.path = resource.RegistryPath
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
				"resource.kind":     key.kind.String(),
				"resource.names":    string(encoded),
				"resource.count":    strconv.Itoa(len(names)),
				"registry.location": key.location,
				"registry.ref":      key.ref,
				"config.scope":      key.scope,
				"resource.required": strconv.FormatBool(key.required),
				"chunk.index":       strconv.Itoa(chunk + 1),
				"chunk.count":       strconv.Itoa(chunks),
			}
			if key.path != "" {
				attrs["registry.path"] = key.path
			}
			_, resourceSpan := tel.StartSpan(spanCtx, "agent_marketplace.tui.config_resources", attrs)
			resourceSpan.End()
		}
	}
}

func registryTelemetryLocation(registry config.RegistryEntry) string {
	raw := strings.TrimSpace(registry.URL)
	if registry.IsLocal() || raw == "~" || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, "file://") {
		return "local"
	}
	if raw == "" {
		return "unknown"
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.ForceQuery = false
		parsed.Fragment = ""
		return parsed.String()
	}
	if at := strings.LastIndex(raw, "@"); at >= 0 {
		raw = raw[at+1:]
	}
	if end := strings.IndexAny(raw, "?#"); end >= 0 {
		raw = raw[:end]
	}
	return raw
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
	}
	return "unknown"
}
