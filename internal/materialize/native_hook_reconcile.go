package materialize

import (
	"context"
	"fmt"
	"os"
	"path"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type nativeHookAggregateDeclaration struct {
	partition targetmeta.NativePartition
	aggregate targetmeta.NativeAggregate
	file      targetmeta.NativeAggregateFile
}

func nativeHookAggregateForScope(target targetmeta.Target, machineScope bool) (nativeHookAggregateDeclaration, error) {
	layout, ok := targetmeta.LookupNativeLayout(target)
	if !ok {
		return nativeHookAggregateDeclaration{}, fmt.Errorf("target %s declares no native layout", target)
	}
	var declaration nativeHookAggregateDeclaration
	found := false
	for _, partition := range layout.Partitions {
		for _, aggregate := range partition.Aggregates {
			if aggregate.Kind != ir.KindHook {
				continue
			}
			if found {
				return nativeHookAggregateDeclaration{}, fmt.Errorf("target %s declares more than one hook aggregate", target)
			}
			file := aggregate.Project
			if machineScope {
				file = aggregate.User
			}
			declaration = nativeHookAggregateDeclaration{partition: partition, aggregate: aggregate, file: file}
			found = true
		}
	}
	if !found {
		return nativeHookAggregateDeclaration{}, fmt.Errorf("target %s declares no hook aggregate", target)
	}
	if declaration.file.Path == "" {
		return nativeHookAggregateDeclaration{}, fmt.Errorf("target %s declares an empty hook aggregate path", target)
	}
	return declaration, nil
}

func nativeHookJSONLayout(key outputKey, repoRoot string) (harness.Destination, ownedJSONFileSpec, error) {
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(key.target), key.machineScope)
	if err != nil {
		return harness.Destination{}, ownedJSONFileSpec{}, err
	}
	artifactPath := path.Join(declaration.partition.Prefix, declaration.file.Path)
	destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
	if err != nil {
		return harness.Destination{}, ownedJSONFileSpec{}, err
	}
	var destination harness.Destination
	var rel string
	matches := 0
	for _, candidate := range destinations {
		if len(candidate.AggregateRoutes) != 0 {
			continue
		}
		routed, routeErr := candidate.RouteArtifact(artifactPath)
		if routeErr != nil || routed != declaration.file.Path || candidate.Root == "" {
			continue
		}
		destination = candidate
		rel = routed
		matches++
	}
	if matches != 1 {
		return harness.Destination{}, ownedJSONFileSpec{}, fmt.Errorf("target %s has no unique native hook destination", key.target)
	}
	return destination, ownedJSONFileSpec{
		rel:       rel,
		container: declaration.file.Member,
		envelope:  declaration.file.Envelope,
		hookShape: declaration.aggregate.Shape,
	}, nil
}

func reconcileNativeHookJSON(ctx context.Context, report *Report, key outputKey, repoRoot string, work *ownedJSONAggregateWork, options ownedJSONReconcileOptions, dryRun bool) *Report {
	if report == nil {
		report = &Report{}
	}
	if report.PerTarget == nil {
		report.PerTarget = make(map[hcc.Target]TargetReport)
	}
	destination, file, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		return nativeHookDestinationError(report, key)
	}
	root, err := ownership.CanonicalRoot(destination.Root)
	if err != nil {
		return nativeHookDestinationError(report, key)
	}
	if info, statErr := os.Stat(root); statErr == nil && !info.IsDir() || statErr != nil && !os.IsNotExist(statErr) {
		return nativeHookDestinationError(report, key)
	}
	destination.Root = root
	options.rootID = destination.ID
	return reconcileOwnedJSONAggregates(ctx, report, key, destination.Root, []ownedJSONFileSpec{file}, work, options, dryRun)
}

func nativeHookDestinationError(report *Report, key outputKey) *Report {
	report.Diags = append(report.Diags, diag.Diagnostic{
		Code:     "materialize.destination.error",
		Severity: diag.SeverityError,
		Target:   string(key.target),
		Message:  "native hook destination could not be resolved safely; aggregate was left unchanged",
	})
	return report
}
