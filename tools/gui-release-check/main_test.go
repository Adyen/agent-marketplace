package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writePackageMatrix(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{
		"agent-marketplace-gui-v1.2.3-macos-universal.dmg",
		"agent-marketplace-gui-v1.2.3-macos-universal.zip",
		"agent-marketplace-gui-v1.2.3-linux-amd64.rpm",
		"agent-marketplace-gui-v1.2.3-linux-amd64.deb",
		"agent-marketplace-gui-v1.2.3-linux-arm64.rpm",
		"agent-marketplace-gui-v1.2.3-linux-arm64.deb",
	} {
		putFile(t, root, name, "asset:"+name)
	}
}

func writePackageSums(t *testing.T, root string) {
	t.Helper()
	var lines []string
	for _, name := range []string{
		"agent-marketplace-gui-v1.2.3-macos-universal.dmg",
		"agent-marketplace-gui-v1.2.3-macos-universal.zip",
		"agent-marketplace-gui-v1.2.3-linux-amd64.rpm",
		"agent-marketplace-gui-v1.2.3-linux-amd64.deb",
		"agent-marketplace-gui-v1.2.3-linux-arm64.rpm",
		"agent-marketplace-gui-v1.2.3-linux-arm64.deb",
	} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		lines = append(lines, fmt.Sprintf("%s  %s", hex.EncodeToString(digest[:]), name))
	}
	putFile(t, root, "SHA256SUMS", strings.Join(lines, "\n")+"\n")
}

// spec: gui-distribution/tagged-release-publishes-the-gui-package-matrix
func TestCheckPackageMatrixRequiresEveryReleaseAsset(t *testing.T) {
	root := t.TempDir()
	writePackageMatrix(t, root)
	writePackageSums(t, root)
	if err := checkMatrix(root, "v1.2.3"); err != nil {
		t.Fatalf("complete matrix rejected: %v", err)
	}
	putFile(t, root, "SHA256SUMS", "0000000000000000000000000000000000000000000000000000000000000000  agent-marketplace-gui-v1.2.3-macos-universal.dmg\n")
	if err := checkMatrix(root, "v1.2.3"); err == nil {
		t.Fatal("release matrix with missing or incorrect GUI checksums was accepted")
	}
}

// spec: gui-distribution/missing-gui-asset-fails-the-release
func TestCheckPackageMatrixRejectsMissingReleaseAsset(t *testing.T) {
	root := t.TempDir()
	writePackageMatrix(t, root)
	if err := os.Remove(filepath.Join(root, "agent-marketplace-gui-v1.2.3-linux-arm64.deb")); err != nil {
		t.Fatal(err)
	}
	if err := checkMatrix(root, "v1.2.3"); err == nil {
		t.Fatal("incomplete release matrix was accepted")
	}
}

// spec: gui-distribution/workflows-contain-no-signing-material
func TestSigningAuditAllowsAdHocSigningAndRejectsSigningMaterial(t *testing.T) {
	root := t.TempDir()
	putFile(t, root, ".github/workflows/ci.yml", "run: codesign --force --options runtime -s - app\n")
	putFile(t, root, "packaging/gui/linux/nfpm.yaml", "contents: []\n")
	if err := auditSigningMaterial(root); err != nil {
		t.Fatalf("ad-hoc signing configuration rejected: %v", err)
	}
	for _, planted := range []string{
		"codesign --sign 'Developer ID Application: Example Org'",
		"APPLE_TEAM_ID: ABCDE12345",
		"APPLE_CERTIFICATE: ${{ secrets.APPLE_CERTIFICATE }}",
		"signing-key: /private/keys/distribution.p12",
	} {
		t.Run(strings.ReplaceAll(planted, " ", "-"), func(t *testing.T) {
			root := t.TempDir()
			putFile(t, root, ".github/workflows/ci.yml", planted+"\n")
			if err := auditSigningMaterial(root); err == nil {
				t.Fatalf("signing material was accepted: %q", planted)
			}
		})
	}
}

// spec: gui-distribution/ci-re-signs-copies-of-the-app-and-rpms
func TestArtifactDigestCheckDetectsMutation(t *testing.T) {
	root := t.TempDir()
	putFile(t, root, "gui.zip", "original app")
	putFile(t, root, "linux.rpm", "original rpm")
	before, err := artifactDigests(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyArtifactDigests(root, before); err != nil {
		t.Fatalf("unchanged artifacts rejected: %v", err)
	}
	putFile(t, root, "linux.rpm", "signed copy accidentally replaced original")
	if err := verifyArtifactDigests(root, before); err == nil {
		t.Fatal("mutated uploaded artifact was accepted")
	}
}

func writeCLIReleaseFixture(t *testing.T, root string) {
	t.Helper()
	var lines []string
	for _, name := range []string{
		"agent-marketplace-v1.2.3-darwin-arm64",
		"agent-marketplace-v1.2.3-darwin-amd64",
		"agent-marketplace-v1.2.3-linux-amd64",
		"agent-marketplace-v1.2.3-linux-arm64",
	} {
		content := "CLI bytes:" + name
		putFile(t, root, name, content)
		digest := sha256.Sum256([]byte(content))
		lines = append(lines, fmt.Sprintf("%s  %s", hex.EncodeToString(digest[:]), name))
	}
	putFile(t, root, "SHA256SUMS", strings.Join(lines, "\n")+"\n")
}

// spec: gui-distribution/adding-gui-packages-does-not-change-cli-artifacts
func TestCLIReleaseComparisonRejectsChangedBytesAndGUINameCollision(t *testing.T) {
	expected, actual := t.TempDir(), t.TempDir()
	writeCLIReleaseFixture(t, expected)
	writeCLIReleaseFixture(t, actual)
	if err := compareCLIAssets(actual, expected, "v1.2.3"); err != nil {
		t.Fatalf("identical CLI release rejected: %v", err)
	}
	putFile(t, actual, "agent-marketplace-v1.2.3-linux-amd64", "changed CLI bytes")
	if err := compareCLIAssets(actual, expected, "v1.2.3"); err == nil {
		t.Fatal("changed CLI binary was accepted")
	}
	writeCLIReleaseFixture(t, actual)
	putFile(t, actual, "agent-marketplace-v1.2.3-gui-macos.dmg", "collision")
	if err := compareCLIAssets(actual, expected, "v1.2.3"); err == nil {
		t.Fatal("GUI asset matching the CLI pattern was accepted")
	}
}
