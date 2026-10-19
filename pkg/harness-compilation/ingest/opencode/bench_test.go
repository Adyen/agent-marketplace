package opencode

import (
	"fmt"
	"testing"
	"testing/fstest"
)

func BenchmarkLoadBundle(b *testing.B) {
	source := fstest.MapFS{}
	for index := range 32 {
		name := fmt.Sprintf("resource-%02d", index)
		source["commands/"+name+".md"] = &fstest.MapFile{Data: []byte("---\ndescription: Check\nmodel: provider/model\n---\nCheck $ARGUMENTS\n")}
		source["agents/"+name+".md"] = &fstest.MapFile{Data: []byte("---\ndescription: Review\nmode: subagent\npermission:\n  bash:\n    '*': deny\n    'git *': allow\n---\nReview\n")}
	}
	b.ReportAllocs()
	for b.Loop() {
		bundle, err := LoadBundle(source)
		if err != nil || len(bundle.Resources) != 64 || len(bundle.Diagnostics) != 0 {
			b.Fatalf("load: %v %#v", err, bundle)
		}
	}
}
