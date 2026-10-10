package compile

import (
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func NativeHookArtifact(target Target, scope Scope, event string, rule any) (artifact.Artifact, error) {
	if event == "" {
		return artifact.Artifact{}, fmt.Errorf("native hook event is empty")
	}
	machineScope := false
	switch scope {
	case "", ScopeProject:
	case ScopeUser:
		machineScope = true
	default:
		return artifact.Artifact{}, fmt.Errorf("invalid native hook scope %q", scope)
	}
	projection, ok := targetmeta.LookupHookProjection(targetmeta.Target(target))
	if !ok {
		return artifact.Artifact{}, fmt.Errorf("target %q has no native hook projection", target)
	}
	return nativeProjectionArtifact(&projection, machineScope, event, rule)
}
