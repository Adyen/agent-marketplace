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
asset=${url##*/}
if [ "$asset" = "$CURL_FAIL_ASSET" ]; then exit 6; fi
if [ "$format" = '%{url_effective}' ]; then printf '%s' "$LATEST_URL"; exit 0; fi
if [ -f "$FIXTURE_DIR/$asset" ]; then
  cat "$FIXTURE_DIR/$asset" > "$out"
  code=200
else
  code=404
fi
printf '%s' "$code"
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

func (i installer) run(t *testing.T, latest, pin, dir, path string, env ...string) (string, string, error) {
	return i.runScript(t, "../site/public/install.sh", latest, pin, dir, path, env...)
}

func (i installer) runScript(t *testing.T, script, latest, pin, dir, path string, env ...string) (string, string, error) {
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
	cmd.Env = append(cmd.Env, env...)
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

// installerTools are the external commands the installer needs besides curl and uname.
var installerTools = []string{"awk", "cat", "chmod", "cp", "mkdir", "mktemp", "mv", "rm"}

// isolatedPath links only the named tools into the fake bin directory, so a fake or absent tool is the only one the installer can find.
func (i installer) isolatedPath(t *testing.T, tools ...string) string {
	t.Helper()
	for _, tool := range tools {
		source, err := exec.LookPath(tool)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(source, filepath.Join(i.bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	return i.bin
}

func (i installer) fakeTool(t *testing.T, name, body string) {
	t.Helper()
	writeFile(t, filepath.Join(i.bin, name), "#!/bin/sh\n"+body+"\n", 0o700)
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
	for _, tc := range []struct{ name, system, machine, value string }{
		{"unsupported operating system", "FreeBSD", "x86_64", "FreeBSD"},
		{"unsupported processor architecture", "Linux", "i386", "i386"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, tc.system, tc.machine)
			_, stderr, err := i.run(t, latestURL, "", "", i.path("/usr/bin", "/bin"))
			if err == nil || !strings.Contains(stderr, tc.value) || i.requests(t) != "" {
				t.Fatalf("unsupported platform accepted: err=%v stderr=%q requests=%s", err, stderr, i.requests(t))
			}
		})
	}
}

// spec: install-script/latest-stable-release-is-installed-by-default
// spec: install-script/pinned-version-is-installed
func TestInstallerSelectsLatestOrPinnedRelease(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pin    string
		latest bool
	}{
		{"latest stable release", "", true},
		{"pinned release", "v1.2.3", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
			_, stderr, err := i.run(t, latestURL, tc.pin, "", i.path("/usr/bin", "/bin"))
			requests := i.requests(t)
			if err != nil || strings.Contains(requests, "/releases/latest") != tc.latest ||
				!strings.Contains(requests, "/releases/download/v1.2.3/"+linuxAMD) {
				t.Fatalf("release selection err=%v stderr=%q requests=%s", err, stderr, requests)
			}
		})
	}
}

// spec: install-script/malformed-or-unresolvable-version-is-refused
func TestInstallerRejectsBadVersion(t *testing.T) {
	for _, tc := range []struct {
		name        string
		pin, latest string
		lookup      bool
	}{
		{"malformed pin", "latest", latestURL, false},
		{"unresolvable latest tag", "", "https://github.com/Adyen/agent-marketplace/releases/tag/preview", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			_, stderr, err := i.run(t, tc.latest, tc.pin, "", i.path("/usr/bin", "/bin"))
			requests := i.requests(t)
			if err == nil || !strings.HasPrefix(stderr, "agent-marketplace install: ") ||
				strings.Contains(requests, "/releases/download/") || strings.Contains(requests, "/releases/latest") != tc.lookup {
				t.Fatalf("bad version accepted: err=%v stderr=%q requests=%s", err, stderr, requests)
			}
		})
	}
}

// spec: install-script/malformed-or-unresolvable-version-is-refused
func TestInstallerRefusesUnresolvedLatestBeforeDownload(t *testing.T) {
	for _, tc := range []struct{ name, latest, failAsset, want string }{
		{"latest lookup fails", latestURL, "latest", "could not resolve latest release"},
		{"latest is not a tag", "https://github.com/Adyen/agent-marketplace/releases", "", "did not resolve to a stable tag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
			_, stderr, err := i.run(t, tc.latest, "", "", i.path("/usr/bin", "/bin"), "CURL_FAIL_ASSET="+tc.failAsset)
			if err == nil || !strings.HasPrefix(stderr, "agent-marketplace install: ") || !strings.Contains(stderr, tc.want) ||
				strings.Contains(i.requests(t), "/releases/download/") {
				t.Fatalf("unresolved latest release: err=%v stderr=%q requests=%s", err, stderr, i.requests(t))
			}
		})
	}
}

// spec: install-script/home-unset-is-refused-for-the-default-directory
// spec: install-script/absolute-override-needs-no-home
// spec: install-script/tilde-override-expands-to-home
// spec: install-script/relative-install-directory-is-refused
// spec: install-script/directory-at-the-target-is-refused
func TestInstallerResolvesInstallDirectoryBeforeRequests(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		unsetHome  bool
		dir        func(i installer) string
		prepare    func(t *testing.T, i installer)
		target     func(i installer) string
	}{
		{name: "HOME unset for default directory", want: "HOME is not set", unsetHome: true,
			dir: func(installer) string { return "" }},
		{name: "HOME unset with absolute override", unsetHome: true,
			dir:    func(i installer) string { return filepath.Join(i.tmp, "abs-bin") },
			target: func(i installer) string { return filepath.Join(i.tmp, "abs-bin", "agent-marketplace") }},
		{name: "tilde expands to HOME", dir: func(installer) string { return "~/x" },
			target: func(i installer) string { return filepath.Join(i.home, "x", "agent-marketplace") }},
		{name: "bare tilde expands to HOME", dir: func(installer) string { return "~" },
			target: func(i installer) string { return filepath.Join(i.home, "agent-marketplace") }},
		{name: "relative override", want: "must be an absolute path: bin", dir: func(installer) string { return "bin" }},
		{name: "directory at target", want: "is a directory",
			dir: func(i installer) string { return filepath.Join(i.home, "custom") },
			prepare: func(t *testing.T, i installer) {
				if err := os.MkdirAll(filepath.Join(i.home, "custom", "agent-marketplace"), 0o700); err != nil {
					t.Fatal(err)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
			if tc.prepare != nil {
				tc.prepare(t, i)
			}
			var env []string
			if tc.unsetHome {
				env = []string{"HOME="}
			}
			_, stderr, err := i.run(t, latestURL, "", tc.dir(i), i.path("/usr/bin", "/bin"), env...)
			if tc.target == nil {
				if err == nil || !strings.HasPrefix(stderr, "agent-marketplace install: ") || !strings.Contains(stderr, tc.want) || i.requests(t) != "" {
					t.Fatalf("refused directory accepted: err=%v stderr=%q requests=%s", err, stderr, i.requests(t))
				}
				return
			}
			got, readErr := os.ReadFile(tc.target(i))
			if err != nil || readErr != nil || string(got) != fixtureBin {
				t.Fatalf("install err=%v readErr=%v stderr=%q bytes=%q", err, readErr, stderr, got)
			}
		})
	}
}

// spec: install-script/unverifiable-download-leaves-the-installation-unchanged
func TestInstallerLocalFailuresStopBeforeInstall(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		setup      func(t *testing.T, i *installer) string
	}{
		{"temporary directory", "could not create a temporary directory", func(t *testing.T, i *installer) string {
			i.fakeTool(t, "mktemp", "exit 1")
			return ""
		}},
		{"install directory beneath a file", "could not create install directory", func(t *testing.T, i *installer) string {
			blocker := filepath.Join(i.home, "file")
			writeFile(t, blocker, "", 0o600)
			return filepath.Join(blocker, "bin")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
			dir := tc.setup(t, &i)
			_, stderr, err := i.run(t, latestURL, "", dir, i.path("/usr/bin", "/bin"))
			if err == nil || !strings.HasPrefix(stderr, "agent-marketplace install: ") || !strings.Contains(stderr, tc.want) {
				t.Fatalf("local failure: err=%v stderr=%q requests=%s", err, stderr, i.requests(t))
			}
		})
	}
}

// spec: install-script/checksums-are-fetched-before-the-binary
func TestInstallerFetchesSumsBeforeBinary(t *testing.T) {
	i := newInstaller(t, "Linux", "amd64")
	i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
	_, stderr, err := i.run(t, latestURL, "", "", i.path("/usr/bin", "/bin"))
	requests := i.requests(t)
	sums := strings.Index(requests, "/releases/download/v1.2.3/SHA256SUMS")
	binary := strings.Index(requests, "/releases/download/v1.2.3/"+linuxAMD)
	if err != nil || sums < 0 || binary < sums {
		t.Fatalf("request order err=%v stderr=%q requests=%s", err, stderr, requests)
	}
}

// spec: install-script/missing-sha256sums-is-reported-without-fetching-the-binary
// spec: install-script/network-failure-is-reported-distinctly-from-missing-sha256sums
func TestInstallerReportsMissingSumsDistinctly(t *testing.T) {
	for _, tc := range []struct{ name, failAsset, want, other string }{
		{"missing sums", "", "does not publish SHA256SUMS", "could not reach"},
		{"network failure", "SHA256SUMS", "could not reach", "does not publish SHA256SUMS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			writeFile(t, filepath.Join(i.files, linuxAMD), fixtureBin, 0o600)
			_, stderr, err := i.run(t, latestURL, "", "", i.path("/usr/bin", "/bin"), "CURL_FAIL_ASSET="+tc.failAsset)
			requests := i.requests(t)
			if err == nil || !strings.Contains(stderr, tc.want) || strings.Contains(stderr, tc.other) ||
				strings.Contains(requests, "/releases/download/v1.2.3/"+linuxAMD) {
				t.Fatalf("sums failure: err=%v stderr=%q requests=%s", err, stderr, requests)
			}
		})
	}
}

// spec: install-script/temporary-directory-name-does-not-affect-verification
func TestInstallerVerifiesWithBackslashInTempDir(t *testing.T) {
	i := newInstaller(t, "Linux", "amd64")
	i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
	i.tmp = filepath.Join(i.tmp, `back\slash`)
	if err := os.MkdirAll(i.tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := i.run(t, latestURL, "", "", i.path("/usr/bin", "/bin"))
	got, readErr := os.ReadFile(filepath.Join(i.home, ".local/bin", "agent-marketplace"))
	if err != nil || readErr != nil || string(got) != fixtureBin {
		t.Fatalf("install err=%v readErr=%v stderr=%q bytes=%q", err, readErr, stderr, got)
	}
}

// spec: install-script/shadowing-binary-earlier-on-path-is-warned
// spec: install-script/no-shadow-warning-when-the-install-directory-resolves-first
func TestInstallerWarnsWhenAnotherBinaryShadowsInstall(t *testing.T) {
	for _, tc := range []struct {
		name         string
		installFirst bool
	}{
		{"other binary earlier on PATH", false},
		{"install directory first on PATH", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
			dir := filepath.Join(i.home, ".local", "bin")
			other := filepath.Join(i.home, "other-bin")
			if err := os.MkdirAll(other, 0o700); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(other, "agent-marketplace"), "#!/bin/sh\n", 0o700)
			dirs := []string{other, dir}
			if tc.installFirst {
				dirs = []string{dir, other}
			}
			_, stderr, err := i.run(t, latestURL, "", "", i.path(append(dirs, "/usr/bin", "/bin")...))
			want := ""
			if !tc.installFirst {
				want = "agent-marketplace install: warning: " + filepath.Join(other, "agent-marketplace") + " shadows " + dir + "/agent-marketplace on PATH\n"
			}
			if err != nil || stderr != want {
				t.Fatalf("shadow warning err=%v stderr=%q want=%q", err, stderr, want)
			}
		})
	}
}

// spec: install-script/unverifiable-download-leaves-the-installation-unchanged
// spec: install-script/install-directory-is-not-created-before-verification
func TestInstallerVerificationFailuresPreserveInstall(t *testing.T) {
	for _, tc := range verificationFailures {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			dir := filepath.Join(i.home, "custom-bin")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			target, old := filepath.Join(dir, "agent-marketplace"), "existing binary"
			writeFile(t, target, old, 0o700)
			path := tc.prepare(t, i)
			if path == "" {
				path = i.path("/usr/bin", "/bin")
			}
			_, stderr, err := i.run(t, latestURL, "", dir, path, "CURL_FAIL_ASSET="+tc.failAsset)
			got, readErr := os.ReadFile(target)
			entries, tempErr := os.ReadDir(i.tmp)
			dirEntries, dirReadErr := os.ReadDir(dir)
			if err == nil || !strings.HasPrefix(stderr, "agent-marketplace install: ") || !strings.Contains(stderr, tc.want) ||
				readErr != nil || string(got) != old || tempErr != nil || len(entries) != 0 ||
				dirReadErr != nil || len(dirEntries) != 1 || dirEntries[0].Name() != "agent-marketplace" {
				t.Fatalf("failure did not preserve install: err=%v stderr=%q bytes=%q readErr=%v dir=%v dirReadErr=%v tmp=%v tempErr=%v",
					err, stderr, got, readErr, dirEntries, dirReadErr, entries, tempErr)
			}
		})
	}
}

// spec: install-script/install-directory-is-not-created-before-verification
func TestInstallerVerificationFailuresDoNotCreateInstallDir(t *testing.T) {
	for _, tc := range verificationFailures {
		if !tc.verifyOnly {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			dir := filepath.Join(i.home, "missing-bin")
			path := tc.prepare(t, i)
			if path == "" {
				path = i.path("/usr/bin", "/bin")
			}
			_, stderr, err := i.run(t, latestURL, "", dir, path, "CURL_FAIL_ASSET="+tc.failAsset)
			if _, statErr := os.Stat(dir); err == nil || !strings.Contains(stderr, tc.want) || !os.IsNotExist(statErr) {
				t.Fatalf("verification failure created install dir: err=%v stderr=%q statErr=%v", err, stderr, statErr)
			}
		})
	}
}

// verificationFailures lists installs that fail after the install directory is resolved.
// prepare writes fixtures and fake tools, and returns a PATH when the default one is wrong.
var verificationFailures = []struct {
	name, want, failAsset string
	verifyOnly            bool
	prepare               func(t *testing.T, i installer) string
}{
	{name: "binary download fails", want: "could not reach", failAsset: linuxAMD, verifyOnly: true, prepare: prepareRelease},
	{name: "sums download fails", want: "could not reach", failAsset: "SHA256SUMS", verifyOnly: true, prepare: prepareRelease},
	{name: "missing sums", want: "does not publish SHA256SUMS", verifyOnly: true,
		prepare: func(t *testing.T, i installer) string {
			writeFile(t, filepath.Join(i.files, linuxAMD), fixtureBin, 0o600)
			return ""
		}},
	{name: "missing binary asset", want: "has no asset", verifyOnly: true,
		prepare: func(t *testing.T, i installer) string {
			prepareRelease(t, i)
			if err := os.Remove(filepath.Join(i.files, linuxAMD)); err != nil {
				t.Fatal(err)
			}
			return ""
		}},
	{name: "missing entry", want: "has no entry", verifyOnly: true,
		prepare: func(t *testing.T, i installer) string {
			i.addAsset(t, linuxAMD, []byte(fixtureBin), "other", "")
			return ""
		}},
	{name: "mismatch", want: "SHA-256 mismatch", verifyOnly: true,
		prepare: func(t *testing.T, i installer) string {
			i.addAsset(t, linuxAMD, []byte(fixtureBin), "", strings.Repeat("0", 64))
			return ""
		}},
	{name: "no hash tool", want: "sha256sum or shasum is required", verifyOnly: true,
		prepare: func(t *testing.T, i installer) string {
			prepareRelease(t, i)
			return i.isolatedPath(t, installerTools...)
		}},
	{name: "sha256sum fails", want: "could not calculate SHA-256", verifyOnly: true,
		prepare: func(t *testing.T, i installer) string {
			prepareRelease(t, i)
			path := i.isolatedPath(t, installerTools...)
			i.fakeTool(t, "sha256sum", "exit 1")
			return path
		}},
	{name: "shasum fails", want: "could not calculate SHA-256", verifyOnly: true,
		prepare: func(t *testing.T, i installer) string {
			prepareRelease(t, i)
			path := i.isolatedPath(t, installerTools...)
			i.fakeTool(t, "shasum", "exit 1")
			return path
		}},
	{name: "copy fails", want: "could not copy verified binary",
		prepare: func(t *testing.T, i installer) string {
			prepareRelease(t, i)
			i.fakeTool(t, "cp", "exit 1")
			return ""
		}},
	{name: "chmod fails", want: "could not make installed binary executable",
		prepare: func(t *testing.T, i installer) string {
			prepareRelease(t, i)
			i.fakeTool(t, "chmod", "exit 1")
			return ""
		}},
	{name: "binary does not run", want: "does not run",
		prepare: func(t *testing.T, i installer) string {
			i.addAsset(t, linuxAMD, []byte("#!/bin/sh\nexit 1\n"), "", "")
			return ""
		}},
	{name: "move fails", want: "could not install binary in",
		prepare: func(t *testing.T, i installer) string {
			prepareRelease(t, i)
			i.fakeTool(t, "mv", "exit 1")
			return ""
		}},
	{name: "install directory not writable", want: "could not create a temporary file",
		prepare: func(t *testing.T, i installer) string {
			if os.Geteuid() == 0 {
				t.Skip("root can write to a read-only directory")
			}
			prepareRelease(t, i)
			dir := filepath.Join(i.home, "custom-bin")
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
			return ""
		}},
}

func prepareRelease(t *testing.T, i installer) string {
	i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
	return ""
}

// spec: install-script/verified-download-is-installed
// spec: install-script/binary-is-installed-into-the-default-or-overridden-directory
// spec: install-script/existing-installation-is-replaced
func TestInstallerInstallsVerifiedBinaryInUserDirectory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		override bool
	}{
		{"default directory", false},
		{"overridden directory", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			binary, dir := []byte(fixtureBin), ""
			wantDir := filepath.Join(i.home, ".local/bin")
			if tc.override {
				dir = filepath.Join(i.home, "custom-bin")
				wantDir = dir
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
		})
	}
}

// spec: install-script/path-hint-is-shown-only-when-needed
func TestInstallerPathHintFollowsPathMembership(t *testing.T) {
	for _, tc := range []struct {
		name          string
		onPath        bool
		trailingSlash bool
	}{
		{"directory not on PATH", false, false},
		{"directory on PATH", true, false},
		{"directory with trailing slash on PATH", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := newInstaller(t, "Linux", "amd64")
			i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
			dir := filepath.Join(i.home, ".local/bin")
			installDir := ""
			if tc.trailingSlash {
				installDir = dir + string(os.PathSeparator)
			}
			extra := []string{"/usr/bin", "/bin"}
			if tc.onPath {
				extra = append([]string{dir}, extra...)
			}
			stdout, stderr, err := i.run(t, latestURL, "", installDir, i.path(extra...))
			hint := fmt.Sprintf("export PATH=\"%s:$PATH\"", dir)
			if err != nil || strings.Contains(stdout, hint) == tc.onPath || strings.Contains(stdout, "is not on PATH") == tc.onPath {
				t.Fatalf("path hint err=%v stderr=%q stdout=%q onPath=%v", err, stderr, stdout, tc.onPath)
			}
		})
	}
}

func TestInstallScriptTruncatedStreamRunsNoCommands(t *testing.T) {
	i := newInstaller(t, "Linux", "amd64")
	i.addAsset(t, linuxAMD, []byte(fixtureBin), "", "")
	source, err := os.ReadFile("../site/public/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	cut := strings.Index(string(source), `"$install_tmp" --version`)
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
