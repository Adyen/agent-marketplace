package lockfile

import (
	"errors"
	"fmt"
	"testing"
)

func TestRevisionSchemaDispatchAtDecodeBoundary(t *testing.T) {
	for _, version := range []string{"v1", "v2", "v99"} {
		t.Run(version, func(t *testing.T) {
			payload := fmt.Sprintf("apiVersion: agent-marketplace-lock/v1\nscope: repo\nregistries:\n- url: https://example.com/repo\n  ref: main\n  commit: sha1:0123456789abcdef0123456789abcdef01234567\n  roots:\n  - path: .\n    source_harness: claude\n    revision_schema: %s\n    revision_digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", version)
			doc, err := Decode([]byte(payload))
			if version == "v99" {
				var coded *Error
				if !errors.As(err, &coded) || coded.Code != CodeUnsupportedVersion {
					t.Fatalf("unknown generator: %v", err)
				}
				return
			}
			if err != nil || doc.Registries[0].Roots[0].RevisionSchema != version {
				t.Fatalf("supported %s: %+v, %v", version, doc, err)
			}
			if _, err := Encode(doc); err != nil {
				t.Fatal(err)
			}
		})
	}
}
