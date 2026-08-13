package harness

import (
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func BenchmarkMachineRoot(b *testing.B) {
	targets := hcc.Targets()
	if len(targets) == 0 {
		b.Fatal("target registry is empty")
	}

	var root string
	var ok bool
	i := 0
	b.ReportAllocs()
	for b.Loop() {
		root, ok = MachineRoot(targets[i%len(targets)])
		if !ok || root == "" {
			b.Fatal("target has no machine root")
		}
		i++
	}
}
