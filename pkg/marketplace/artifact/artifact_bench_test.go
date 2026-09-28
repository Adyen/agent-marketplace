package artifact

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
)

func BenchmarkBuildResourceArtifact(b *testing.B) {
	ctx := context.Background()
	opts := analyze.DefaultOptions()
	s := ir.NewSkill("review", "Review", "Review the code carefully.", []string{"user"}, nil, ir.SourceRef{})
	s.AllowedTools = []string{"read_file"}
	files := analyze.FileSet{"README.md": []byte("# Review\n")}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BuildResourceArtifact(ctx, s, files, nil, opts)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExtractCapabilities(b *testing.B) {
	s := ir.NewSkill("review", "Review", "Body", []string{"user"}, nil, ir.SourceRef{})
	s.AllowedTools = []string{"read_file", "write_file"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ExtractCapabilities(s)
	}
}
