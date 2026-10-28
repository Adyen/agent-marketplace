package telemetry

import "context"

func RecordInteractiveAction(ctx context.Context, surface, action string, attrs map[string]string) {
	switch surface {
	case "gui", "tui":
	default:
		return
	}
	runtime := TelemetryFromContext(ctx)
	if runtime == nil || !runtime.TracingEnabled() {
		return
	}

	attributes := make(map[string]string, len(attrs)+2)
	for key, value := range attrs {
		attributes[key] = value
	}
	attributes["action"] = action
	attributes["command.surface"] = surface
	_, span := runtime.StartSpan(ctx, "agent_marketplace."+surface+".action", attributes)
	span.End()
}
