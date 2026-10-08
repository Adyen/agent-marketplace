package sourcerevision

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// spec: mcp-publishing-metadata-native-fence/historical-identity-and-writer-defaults-remain-unchanged
func TestMcpPublishingNativeFenceKeepsHistoricalRevisionIdentity(t *testing.T) {
	const (
		skillContent = "---\nname: demo\ndescription: Demo\n---\nDemo\n"
		mcpContent   = `{"mcpServers":{"meta":{"command":"inert","_agent-marketplace":{"description":"Pre-feature","readme_content":"# Old docs"}},"probe":{"command":"inert","x-vendor-ext":{"keep":true}}}}`
		skillDigest  = "sha256:2502700b0863032a831d779993b26cad5abdbb9f8c29adb125f49fdf3b504bf3"
		mcpDigest    = "sha256:10279c4a9cc52555895cad5ac1a9f0a0ab57e4d4b6142f813a0a91995666016e"
	)
	skillRecord := fmt.Sprintf(`{"kind":"skill","name":"demo","content_digest":%q,"archive_digest":%q}`, skillDigest, skillDigest)
	metaRecord := fmt.Sprintf(`{"kind":"mcpserver","name":"meta","content_digest":%q,"archive_digest":%q}`, mcpDigest, mcpDigest)
	mcpRecord := fmt.Sprintf(`{"kind":"mcpserver","name":"probe","content_digest":%q,"archive_digest":%q}`, mcpDigest, mcpDigest)

	wantPayload := map[string]string{
		"v1": `{"schema_version":"v1","generator":"agent-marketplace source-revision/v1","resources":[` + skillRecord + `],"revision_digest":"sha256:462b7b28ddffa2e56c6d8c431e956eb2284760f48b0b9c2b71c7e727822c021e"}`,
		"v2": `{"schema_version":"v2","generator":"agent-marketplace source-revision/v2","resources":[` + skillRecord + `],"revision_digest":"sha256:242a5ea9ba46ee2490be3bc9b3c9cce1749d4d36cc033a5640178b1e819cb2a8"}`,
		"v3": `{"schema_version":"v3","generator":"agent-marketplace source-revision/v3","resources":[` + skillRecord + `],"revision_digest":"sha256:d4ab35231b24082685b86cf33d88c591ff8548d497c2349818831ec7da536601"}`,
		"v4": `{"schema_version":"v4","generator":"agent-marketplace source-revision/v4","resources":[` + metaRecord + `,` + mcpRecord + `,` + skillRecord + `],"revision_digest":"sha256:eeaab9e06b1808f44ef65c3ab78db724d152bf39e1d07b37ceb2bd62435995a9"}`,
	}

	for _, version := range []string{"v1", "v2", "v3", "v4"} {
		t.Run(version, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, filepath.Join(root, "skills", "demo", "SKILL.md"), skillContent)
			writeFixture(t, filepath.Join(root, "mcp.json"), mcpContent)

			built, err := Build(Input{SnapshotRoot: root, RevisionSchema: version})
			if err != nil {
				t.Fatalf("Build %s: %v", version, err)
			}
			if string(built.Payload) != wantPayload[version] {
				t.Fatalf("%s payload changed:\ngot:  %s\nwant: %s", version, built.Payload, wantPayload[version])
			}
			if _, err := Verify(Input{SnapshotRoot: root}, built.Projection()); err != nil {
				t.Fatalf("Verify %s projection: %v", version, err)
			}
		})
	}
}

// spec: mcp-publishing-metadata-native-fence/historical-identity-and-writer-defaults-remain-unchanged
func TestMcpPublishingNativeFenceKeepsWriterDefaults(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "skills", "demo", "SKILL.md"), "---\nname: demo\ndescription: Demo\n---\nDemo\n")

	built, err := Build(Input{SnapshotRoot: root})
	if err != nil {
		t.Fatalf("Build default: %v", err)
	}
	if built.Manifest.SchemaVersion != "v4" {
		t.Fatalf("default writer schema = %q, want v4", built.Manifest.SchemaVersion)
	}
	if _, err := lockfile.RevisionContract("v5"); err == nil {
		t.Fatal("unsupported future source-revision schema v5 was accepted")
	}
	var coded *lockfile.Error
	if _, err := Build(Input{SnapshotRoot: root, RevisionSchema: "v5"}); !errors.As(err, &coded) || coded.Code != lockfile.CodeUnsupportedVersion {
		t.Fatalf("v5 build did not fail closed: %v", err)
	}
}
