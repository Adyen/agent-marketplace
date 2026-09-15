package harness

import (
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestInstallationAdapterRegistryCoverage(t *testing.T) {
	registry, err := NewInstallationAdapterRegistry()
	if err != nil {
		t.Fatal(err)
	}
	want := []hcc.Target{hcc.TargetCodex, hcc.TargetDevin}
	got := registry.Targets()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("adapter targets = %v, want %v", got, want)
	}
	for _, target := range want {
		adapter, ok := registry.Lookup(target)
		if !ok {
			t.Fatalf("adapter %q is missing", target)
		}
		layout, ok := targetmeta.LookupNativeLayout(targetmeta.Target(target))
		if !ok {
			t.Fatalf("descriptor %q is missing", target)
		}
		if !reflect.DeepEqual(adapter.Layout(), layout) {
			t.Errorf("adapter %q does not retain the validated descriptor", target)
		}
		if adapter.AggregateHandlerKey() == "" || adapter.RepairKey() == "" {
			t.Errorf("adapter %q has incomplete materialization binding", target)
		}
	}
}

func TestInstallationAdapterInventoryMatchesDescriptor(t *testing.T) {
	registry, err := NewInstallationAdapterRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []hcc.Target{hcc.TargetCodex, hcc.TargetDevin} {
		adapter, ok := registry.Lookup(target)
		if !ok {
			t.Fatalf("adapter %q is missing", target)
		}
		layout := adapter.Layout()
		for _, machineScope := range []bool{false, true} {
			var want []string
			for _, partition := range layout.Partitions {
				for _, aggregate := range partition.Aggregates {
					file := aggregate.Project
					if machineScope {
						file = aggregate.User
					}
					want = append(want, joinDescriptorPath(partition.Prefix, file.Path))
				}
			}
			sort.Strings(want)
			got := append([]string(nil), adapter.Info().AggregateFilesForScope(machineScope)...)
			sort.Strings(got)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s machine_scope=%v inventory = %v, descriptor = %v", target, machineScope, got, want)
			}
		}
	}
}

func joinDescriptorPath(prefix, rel string) string {
	if prefix == "." {
		return rel
	}
	return prefix + "/" + rel
}

func TestRegistryFailsClosedOnDescriptorValidationError(t *testing.T) {
	sentinel := errors.New("invalid native descriptor")
	_, err := newInstallationAdapterRegistry(func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("registry error = %v, want descriptor validation cause", err)
	}
}

func TestMigratedTargetsHaveNoLegacyMetadataFallback(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetDevin, hcc.TargetCodex} {
		if _, ok := TargetInfoMap[target]; ok {
			t.Fatalf("migrated target %q is duplicated in TargetInfoMap", target)
		}
		if _, ok := TargetInfoFor(target); !ok {
			t.Fatalf("migrated target %q has no validated adapter metadata", target)
		}
	}
}

func TestAdapterBackedMigratedRootRouting(t *testing.T) {
	repoRoot := filepath.Join("/tmp", "adapter-routing-repo")
	devin, err := ResolveDestinations(hcc.TargetDevin, false, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(devin) != 1 || devin[0].Root != filepath.Join(repoRoot, ".devin") {
		t.Fatalf("Devin destinations = %#v, want one adapter root under .devin", devin)
	}
	codex, err := ResolveDestinations(hcc.TargetCodex, false, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(codex) != 2 || codex[0].Root == codex[1].Root {
		t.Fatalf("Codex destinations = %#v, want two distinct descriptor partitions", codex)
	}
}
