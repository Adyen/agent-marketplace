package tui

import (
	"context"

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
