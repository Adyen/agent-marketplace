package scripts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type installer struct {
	bin, files, home, tmp, log, system, machine string
}

func newInstaller(t *testing.T, system, machine string) installer {
	t.Helper()
	root := t.TempDir()
	i := installer{filepath.Join(root, "bin"), filepath.Join(root, "files"), filepath.Join(root, "home"), filepath.Join(root, "tmp"), filepath.Join(root, "curl.log"), system, machine}
	for _, dir := range []string{i.bin, i.files, i.home, i.tmp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(i.bin, "uname"), "#!/bin/sh\ncase \"$1\" in -s) echo \"$INSTALL_OS\";; -m) echo \"$INSTALL_ARCH\";; *) exit 2;; esac\n", 0o700)
	writeFile(t, filepath.Join(i.bin, "curl"), `#!/bin/sh
printf '%s\n' "$*" >> "$CURL_LOG"
out= format= url=
while [ "$#" -gt 0 ]; do
  case "$1" in -o) out=$2; shift 2;; -w) format=$2; shift 2;; --proto) shift 2;; -*) shift;; *) url=$1; shift;; esac
done
if [ "$format" = '%{url_effective}' ]; then printf '%s' "$LATEST_URL"; exit 0; fi
asset=${url##*/}
[ -f "$FIXTURE_DIR/$asset" ] || exit 22
cp "$FIXTURE_DIR/$asset" "$out"
`, 0o700)
	return i
}

func writeFile(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func (i installer) path(extra ...string) string {
	return strings.Join(append([]string{i.bin}, extra...), string(os.PathListSeparator))
}

func (i installer) run(t *testing.T, latest, pin, dir, path string) (string, string, error) {
	return i.runScript(t, "../site/public/install.sh", latest, pin, dir, path)
}

func (i installer) runScript(t *testing.T, script, latest, pin, dir, path string) (string, string, error) {
	t.Helper()
	cmd := exec.Command("bash", script)
	cmd.Env = []string{"PATH=" + path, "HOME=" + i.home, "TMPDIR=" + i.tmp, "CURL_LOG=" + i.log,
		"FIXTURE_DIR=" + i.files, "LATEST_URL=" + latest, "INSTALL_OS=" + i.system, "INSTALL_ARCH=" + i.machine}
	if pin != "" {
		cmd.Env = append(cmd.Env, "AGENT_MARKETPLACE_VERSION="+pin)
	}
	if dir != "" {
		cmd.Env = append(cmd.Env, "AGENT_MARKETPLACE_INSTALL_DIR="+dir)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func (i installer) requests(t *testing.T) string {
	t.Helper()
	data, _ := os.ReadFile(i.log)
	return string(data)
}

func (i installer) addAsset(t *testing.T, name string, binary []byte, entry, digest string) {
	t.Helper()
	writeFile(t, filepath.Join(i.files, name), string(binary), 0o600)
	if entry == "" {
		entry = name
	}
	if digest == "" {
		sum := sha256.Sum256(binary)
		digest = hex.EncodeToString(sum[:])
	}
	writeFile(t, filepath.Join(i.files, "SHA256SUMS"), fmt.Sprintf("%s  %s\n", digest, entry), 0o600)
}

const (
	latestURL  = "https://github.com/Adyen/agent-marketplace/releases/tag/v1.2.3"
	linuxAMD   = "agent-marketplace-v1.2.3-linux-amd64"
	fixtureBin = "#!/bin/sh\necho 'agent-marketplace v1.2.3'\n"
)

// spec: install-script/supported-platforms-select-their-release-binary
func TestInstallerSupportedPlatforms(t *testing.T) {
	for _, tc := range []struct{ system, machine, osName, arch string }{
		{"Darwin", "arm64", "darwin", "arm64"}, {"Darwin", "x86_64", "darwin", "amd64"},
		{"Linux", "amd64", "linux", "amd64"}, {"Linux", "x86_64", "linux", "amd64"},
		{"Linux", "aarch64", "linux", "arm64"},
	} {
		t.Run(tc.osName+"-"+tc.machine, func(t *testing.T) {
			i := newInstaller(t, tc.system, tc.machine)
			asset := fmt.Sprintf("agent-marketplace-v1.2.3-%s-%s", tc.osName, tc.arch)
			i.addAsset(t, asset, []byte(fixtureBin), "", "")
			_, stderr, err := i.run(t, latestURL, "", "", i.path("/usr/bin", "/bin"))
			if err != nil || !strings.Contains(i.requests(t), "/releases/download/v1.2.3/"+asset) {
				t.Fatalf("install err=%v stderr=%q requests=%s", err, stderr, i.requests(t))
			}
		})
	}
}

// spec: install-script/unsupported-platform-is-refused-before-download
func TestInstallerRejectsUnsupportedPlatform(t *testing.T) {
	for _, tc := range []struct{ system, machine, value string }{{"FreeBSD", "x86_64", "FreeBSD"}, {"Linux", "i386", "i386"}} {
		i := newInstaller(t, tc.system, tc.machine)
		_, stderr, err := i.run(t, latestURL, "", "", i.path("/usr/bin", "/bin"))
		if err == nil || !strings.Contains(stderr, tc.value) || i.requests(t) != "" {
			t.Fatalf("unsupported platform accepted: err=%v stderr=%q requests=%s", err, stderr, i.requests(t))
		}
	}
}

// spec: install-script/latest-stable-release-is-installed-by-default
// spec: install-script/pinned-version-is-installed
func TestInstallerSelectsLatestOrPinnedRelease(t *testing.T) {
	for _, tc := range []struct {
		pin    string
		latest bool
	}{{"", true}, {"v1.2.3", false}} {
		i := newInstaller(t, "Linux", "amd64")
		i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
		_, stderr, err := i.run(t, latestURL, tc.pin, "", i.path("/usr/bin", "/bin"))
		requests := i.requests(t)
		if err != nil || strings.Contains(requests, "/releases/latest") != tc.latest ||
			!strings.Contains(requests, "/releases/download/v1.2.3/"+linuxAMD) {
			t.Fatalf("release selection err=%v stderr=%q requests=%s", err, stderr, requests)
		}
	}
}

// spec: install-script/malformed-or-unresolvable-version-is-refused
func TestInstallerRejectsBadVersion(t *testing.T) {
	for _, tc := range []struct {
		pin, latest string
		lookup      bool
	}{
		{"latest", latestURL, false}, {"", "https://github.com/Adyen/agent-marketplace/releases/tag/preview", true},
	} {
		i := newInstaller(t, "Linux", "amd64")
		_, stderr, err := i.run(t, tc.latest, tc.pin, "", i.path("/usr/bin", "/bin"))
		requests := i.requests(t)
		if err == nil || !strings.HasPrefix(stderr, "agent-marketplace install: ") ||
			strings.Contains(requests, "/releases/download/") || strings.Contains(requests, "/releases/latest") != tc.lookup {
			t.Fatalf("bad version accepted: err=%v stderr=%q requests=%s", err, stderr, requests)
		}
	}
}

// spec: install-script/unverifiable-download-leaves-the-installation-unchanged
func TestInstallerVerificationFailuresPreserveInstall(t *testing.T) {
	for _, tc := range []struct {
		name, want                string
		asset, sum, entry, digest string
		noHash                    bool
	}{
		{"download", linuxAMD, "", "", "", "", false},
		{"missing sums", "SHA256SUMS", "binary", "", "", "", false},
		{"missing entry", "no entry", "binary", "sum", "other", strings.Repeat("0", 64), false},
		{"mismatch", "mismatch", "binary", "sum", linuxAMD, strings.Repeat("0", 64), false},
		{"no hash tool", "sha256sum or shasum", "binary", "sum", linuxAMD, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			binary := []byte(fixtureBin)
			if tc.sum != "" {
				i.addAsset(t, linuxAMD, binary, tc.entry, tc.digest)
			} else if tc.asset != "" {
				writeFile(t, filepath.Join(i.files, linuxAMD), string(binary), 0o600)
			}
			dir := filepath.Join(i.home, "custom-bin")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			target, old := filepath.Join(dir, "agent-marketplace"), "existing binary"
			writeFile(t, target, old, 0o700)
			path := i.path("/usr/bin", "/bin")
			if tc.noHash {
				for _, tool := range []string{"awk", "chmod", "cp", "mkdir", "mktemp", "mv", "rm"} {
					source, err := exec.LookPath(tool)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(source, filepath.Join(i.bin, tool)); err != nil {
						t.Fatal(err)
					}
				}
				path = i.bin
			}
			_, stderr, err := i.run(t, latestURL, "", dir, path)
			got, readErr := os.ReadFile(target)
			entries, tempErr := os.ReadDir(i.tmp)
			if err == nil || !strings.HasPrefix(stderr, "agent-marketplace install: ") || !strings.Contains(stderr, tc.want) ||
				readErr != nil || string(got) != old || tempErr != nil || len(entries) != 0 {
				t.Fatalf("failure did not preserve install: err=%v stderr=%q bytes=%q readErr=%v tmp=%v tempErr=%v", err, stderr, got, readErr, entries, tempErr)
			}
		})
	}
}

// spec: install-script/verified-download-is-installed
// spec: install-script/binary-is-installed-into-the-default-or-overridden-directory
// spec: install-script/existing-installation-is-replaced
func TestInstallerInstallsVerifiedBinaryInUserDirectory(t *testing.T) {
	for _, override := range []bool{false, true} {
		i := newInstaller(t, "Linux", "amd64")
		binary, dir := []byte(fixtureBin), ""
		wantDir := filepath.Join(i.home, ".local/bin")
		if override {
			dir, wantDir = filepath.Join(i.home, "custom-bin"), filepath.Join(i.home, "custom-bin")
		}
		i.addAsset(t, linuxAMD, binary, "", "")
		if err := os.MkdirAll(wantDir, 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(wantDir, "agent-marketplace"), "old", 0o700)
		stdout, stderr, err := i.run(t, latestURL, "", dir, i.path("/usr/bin", "/bin"))
		target := filepath.Join(wantDir, "agent-marketplace")
		got, readErr := os.ReadFile(target)
		info, statErr := os.Stat(target)
		if err != nil || readErr != nil || statErr != nil || string(got) != string(binary) ||
			info.Mode()&0o111 == 0 || !strings.Contains(stdout, "agent-marketplace v1.2.3") {
			t.Fatalf("install err=%v readErr=%v statErr=%v stderr=%q stdout=%q bytes=%q", err, readErr, statErr, stderr, stdout, got)
		}
	}
}

// spec: install-script/path-hint-is-shown-only-when-needed
func TestInstallerPathHintFollowsPathMembership(t *testing.T) {
	for _, onPath := range []bool{false, true} {
		i := newInstaller(t, "Linux", "amd64")
		i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
		dir := filepath.Join(i.home, ".local/bin")
		extra := []string{"/usr/bin", "/bin"}
		if onPath {
			extra = append([]string{dir}, extra...)
		}
		stdout, stderr, err := i.run(t, latestURL, "", "", i.path(extra...))
		hint := fmt.Sprintf("export PATH=\"%s:$PATH\"", dir)
		if err != nil || strings.Contains(stdout, hint) == onPath || strings.Contains(stdout, "is not on PATH") == onPath {
			t.Fatalf("path hint err=%v stderr=%q stdout=%q onPath=%v", err, stderr, stdout, onPath)
		}
	}
}

func TestInstallScriptTruncatedStreamRunsNoCommands(t *testing.T) {
	i := newInstaller(t, "Linux", "amd64")
	i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
	source, err := os.ReadFile("../site/public/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	cut := strings.Index(string(source), `"$install_dir/agent-marketplace" --version`)
	if cut < 0 {
		t.Fatal("could not identify the end of the installer body")
	}
	partial := filepath.Join(i.tmp, "truncated-install.sh")
	writeFile(t, partial, string(source[:cut]), 0o600)
	_, stderr, err := i.runScript(t, partial, latestURL, "", "", i.path("/usr/bin", "/bin"))
	target := filepath.Join(i.home, ".local/bin/agent-marketplace")
	if err == nil || i.requests(t) != "" {
		t.Fatalf("truncated script ran: err=%v stderr=%q requests=%s", err, stderr, i.requests(t))
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("truncated script installed a binary: %v", err)
	}
}

func TestInstallScriptShellcheck(t *testing.T) {
	shellcheck, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("shellcheck is not installed")
	}
	if output, err := exec.Command(shellcheck, "../site/public/install.sh").CombinedOutput(); err != nil {
		t.Fatalf("shellcheck: %v\n%s", err, output)
	}
}
