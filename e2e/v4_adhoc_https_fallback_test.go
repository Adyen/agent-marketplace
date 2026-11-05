package e2e_test

import (
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A user without SSH access pastes the web URL of a public repository. SSH
// cannot reach it, so list and install fall back to HTTPS and say so, install
// records the HTTPS URL, and remove accepts the same web URL. The HTTPS server listens on a
// port, which the fallback URL must keep.
//
// spec: resource-installation/unreachable-ssh-web-url-uses-https
// spec: resource-removal/web-url-removes-a-source-stored-in-https-form
func TestV4AdHocWebURLFallsBackToHTTPSWithoutSSH(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.MustRunAM("init", "--harnesses", "factory")
	const project = "example-org/public-skills"
	env.CreateBareRepo(project, map[string]string{
		"skills/public-review/SKILL.md": "---\nname: public-review\ndescription: Public review\n---\n\nPublic review body\n",
	})

	gitServer := newGitHTTPServer(t, env.repos)
	t.Cleanup(gitServer.Close)
	tlsServer := httptest.NewTLSServer(gitServer.Config.Handler)
	t.Cleanup(tlsServer.Close)
	caPath := filepath.Join(t.TempDir(), "git-ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	// SSH always fails; HTTPS trusts only the test server's certificate.
	gitEnv := map[string]string{"GIT_SSH_COMMAND": "false", "GIT_SSL_CAINFO": caPath}
	webURL := tlsServer.URL + "/" + project + "/tree/main/skills"
	httpsURL := tlsServer.URL + "/" + project + ".git"

	list := env.RunAMWithEnv(gitEnv, "list", "--from", webURL)
	if list.ExitCode != 0 {
		t.Fatalf("list from web URL without SSH failed (exit %d):\n%s", list.ExitCode, list.Combined)
	}
	for _, want := range []string{"public-review", "using " + httpsURL + " instead"} {
		if !strings.Contains(list.Combined, want) {
			t.Fatalf("list output missing %q:\n%s", want, list.Combined)
		}
	}

	install := env.RunAMWithEnv(gitEnv, "install", "public-review", "--from", webURL, "--scope", "user")
	if install.ExitCode != 0 {
		t.Fatalf("install from web URL without SSH failed (exit %d):\n%s", install.ExitCode, install.Combined)
	}
	if want := "recorded " + httpsURL + " instead"; !strings.Contains(install.Combined, want) {
		t.Fatalf("install output missing the fallback warning %q:\n%s", want, install.Combined)
	}
	env.AssertFileContains("/root/.factory/skills/public-review/SKILL.md", "Public review body")
	userConfig := env.ReadFile("/root/.agents/config.yaml")
	for _, want := range []string{"git: " + httpsURL, "ref: main", "path: skills", "skill/public-review"} {
		if !strings.Contains(userConfig, want) {
			t.Fatalf("v4 user config missing %q:\n%s", want, userConfig)
		}
	}
	env.AssertExitCode(env.RunAMWithEnv(gitEnv, "sync", "--check"), 0)

	remove := env.RunAMWithEnv(gitEnv, "remove", "public-review", "--from", webURL, "--scope", "user")
	if remove.ExitCode != 0 {
		t.Fatalf("remove through the same web URL failed (exit %d):\n%s", remove.ExitCode, remove.Combined)
	}
	env.AssertFileAbsent("/root/.factory/skills/public-review")
	env.AssertExitCode(env.RunAMWithEnv(gitEnv, "sync", "--check"), 0)
}
