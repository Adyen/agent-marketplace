package sourcerevision

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWindsurfSourceRevisionCompatibilityVectors(t *testing.T) {
	for _, version := range []string{"v1", "v2", "v3"} {
		for _, sourceHarness := range []string{"windsurf", ""} {
			t.Run(version+"/"+sourceHarness, func(t *testing.T) {
				root := t.TempDir()
				writeFixture(t, filepath.Join(root, ".windsurf", "hooks.json"), `{"hooks":{"pre_run_command":[{"command":"echo first"},{"command":"echo second"}]}}`)
				want, err := os.ReadFile(filepath.Join("testdata", "compatibility-windsurf-"+version+".json"))
				if err != nil {
					t.Fatal(err)
				}
				want = bytes.TrimSpace(want)
				built, err := Build(Input{SnapshotRoot: root, SourceHarness: sourceHarness, RevisionSchema: version})
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(built.Payload, want) {
					t.Fatalf("generator %s/%q changed its compatibility vector\ngot: %s\nwant: %s", version, sourceHarness, built.Payload, want)
				}
				assertWindsurfCatalog(t, built.Catalog, version)
				var manifest sourceRevisionManifest
				if err := json.Unmarshal(want, &manifest); err != nil {
					t.Fatal(err)
				}
				locked := Projection{SourceHarness: "windsurf", RevisionSchema: version, RevisionDigest: manifest.RevisionDigest, Resources: manifest.Resources}
				if _, err := Verify(Input{SnapshotRoot: root}, locked); err != nil {
					t.Fatalf("verify independently authored %s projection: %v", version, err)
				}
				writeFixture(t, filepath.Join(root, ".windsurf", "hooks.json"), `{"hooks":{"pre_run_command":[{"command":"echo changed"},{"command":"echo second"}]}}`)
				if _, err := Verify(Input{SnapshotRoot: root}, locked); !errors.Is(err, ErrManifestMismatch) {
					t.Fatalf("tampered %s source verification error = %v, want manifest mismatch", version, err)
				}
			})
		}
	}
}
