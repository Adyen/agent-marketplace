package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestAdministrationValidation(t *testing.T) {
	valid := Administration{Enabled: true, TrustedProxies: []string{"127.0.0.1/32", "::1/128"}, Origin: "https://marketplace.test", Issuer: "proxy", SubjectHeader: "X-User", GroupsHeader: "X-Groups", AdminGroups: []string{"admins"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://marketplace.test:8443", "https://xn--bcher-kva.test", "https://127.0.0.1", "https://[::1]:8443"} {
		a := valid
		a.Origin = origin
		if err := a.Validate(); err != nil {
			t.Fatalf("canonical origin %q rejected: %v", origin, err)
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*Administration)
	}{
		{"http", func(a *Administration) { a.Origin = "http://marketplace.test" }},
		{"path", func(a *Administration) { a.Origin = "https://marketplace.test/" }},
		{"query", func(a *Administration) { a.Origin = "https://marketplace.test?" }},
		{"credentials", func(a *Administration) { a.Origin = "https://user@marketplace.test" }},
		{"port", func(a *Administration) { a.Origin = "https://marketplace.test:99999" }},
		{"mixed-case scheme", func(a *Administration) { a.Origin = "HTTPS://marketplace.test" }},
		{"mixed-case host", func(a *Administration) { a.Origin = "https://Marketplace.test" }},
		{"default port", func(a *Administration) { a.Origin = "https://marketplace.test:443" }},
		{"trailing-dot host", func(a *Administration) { a.Origin = "https://marketplace.test." }},
		{"leading-zero port", func(a *Administration) { a.Origin = "https://marketplace.test:08080" }},
		{"unicode host", func(a *Administration) { a.Origin = "https://bücher.test" }},
		{"decimal ipv4 host", func(a *Administration) { a.Origin = "https://2130706433" }},
		{"hex ipv4 host", func(a *Administration) { a.Origin = "https://0x7f000001" }},
		{"dotted octal ipv4 host", func(a *Administration) { a.Origin = "https://0177.0.0.1" }},
		{"dotted hex ipv4 host", func(a *Administration) { a.Origin = "https://0x7f.0.0.1" }},
		{"dotted leading-zero ipv4 host", func(a *Administration) { a.Origin = "https://127.0.0.01" }},
		{"short dotted ipv4 host", func(a *Administration) { a.Origin = "https://127.1" }},
		{"all ipv4", func(a *Administration) { a.TrustedProxies = []string{"0.0.0.0/0"} }},
		{"all ipv6", func(a *Administration) { a.TrustedProxies = []string{"::/0"} }},
		{"bare ip", func(a *Administration) { a.TrustedProxies = []string{"127.0.0.1"} }},
		{"no proxies", func(a *Administration) { a.TrustedProxies = nil }},
		{"same headers", func(a *Administration) { a.GroupsHeader = "x-user" }},
		{"invalid header", func(a *Administration) { a.SubjectHeader = "X User" }},
		{"forwarded subject", func(a *Administration) { a.SubjectHeader = "X-Forwarded-User" }},
		{"forwarded groups", func(a *Administration) { a.GroupsHeader = "x-FoRwArDeD-Groups" }},
		{"reserved subject", func(a *Administration) { a.SubjectHeader = "Authorization" }},
		{"reserved groups", func(a *Administration) { a.GroupsHeader = "Cookie" }},
		{"non-identity token", func(a *Administration) { a.SubjectHeader = "X_User" }},
		{"no issuer", func(a *Administration) { a.Issuer = "" }},
		{"no groups", func(a *Administration) { a.AdminGroups = nil }},
		{"empty group", func(a *Administration) { a.AdminGroups = []string{" "} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := valid
			tc.change(&a)
			if a.Validate() == nil {
				t.Fatal("unsafe admin config accepted")
			}
		})
	}
	var cfg Config
	applyDefaults(&cfg)
	if cfg.Admin.Enabled || cfg.Admin.Validate() != nil {
		t.Fatal("admin enabled by default")
	}
	if err := yaml.Unmarshal([]byte("admin:\n  enabled: true\n  origin: https://marketplace.test\n  issuer: proxy\n  trusted_proxies: [127.0.0.1/32]\n  subject_header: X-User\n  groups_header: X-Groups\n  admin_groups: [admins]\n"), &cfg); err != nil || cfg.Admin.Validate() != nil {
		t.Fatalf("admin yaml: %+v %v", cfg.Admin, err)
	}
}

func TestAdministrationCanonicalOrigin(t *testing.T) {
	valid := Administration{Enabled: true, TrustedProxies: []string{"127.0.0.1/32"}, Origin: "https://marketplace.test", Issuer: "proxy", SubjectHeader: "X-User", GroupsHeader: "X-Groups", AdminGroups: []string{"admins"}}
	for _, origin := range []string{
		"https://marketplace.test",
		"https://marketplace.test:8443",
		"https://xn--bcher-kva.test",
		"https://a.b.c.d.test",
		"https://127.0.0.1",
		"https://[::1]",
		"https://[::1]:8443",
		"https://[2001:db8::1]",
		"https://" + strings.Repeat("a", 63) + ".test",
	} {
		a := valid
		a.Origin = origin
		if err := a.Validate(); err != nil {
			t.Fatalf("canonical origin %q rejected: %v", origin, err)
		}
	}
	for _, origin := range []string{
		"https://[0:0:0:0:0:0:0:1]",
		"https://[2001:0db8:0:0:0:0:0:1]",
		"https://[::ffff:7f00:1]",
		"https://[::ffff:7f00:0001]",
		"https://[::ffff:127.0.0.1]",
		"https://[::ffff:192.0.2.1]",
		"https://[::ffff:c000:0201]",
		"https://[0:0:0:0:0:ffff:192.0.2.1]",
		"https://[::1%25eth0]",
		"https://[fe80::1%25eth0]",
		"https://market%20place.test",
		"https://market%2eplace.test",
		"https://market|place.test",
		"https://market_place.test",
		`https://market\place.test`,
		"https://4294967296",
		"https://99999999999999999999",
		"https://0x100000000",
		"https://0xffffffff",
		"https://0xdeadbeef",
		"https://a..test",
		"https://.test",
		"https://marketplace..test",
		"https://marketplace.test.",
		"https://-marketplace.test",
		"https://marketplace-.test",
		"https://mar-ketplace.-test",
		"https://Marketplace.test",
		"https://bücher.test",
		"https://2130706433",
		"https://0x7f000001",
		"https://0177.0.0.1",
		"https://0x7f.0.0.1",
		"https://127.0.0.01",
		"https://127.1",
		"https://999.999.999.999",
		"https://marketplace.test:443",
		"https://marketplace.test:08080",
		"https://marketplace.test:0",
		"https://marketplace.test:65536",
		"https://marketplace.test:",
		"https://marketplace.test:8443x",
		"https://" + strings.Repeat("a", 64) + ".test",
		"https://" + strings.Repeat("a", 250) + ".test",
		"HTTPS://marketplace.test",
		"https://user@marketplace.test",
		"https://marketplace.test/",
		"https://marketplace.test?",
		"https://marketplace.test#x",
	} {
		a := valid
		a.Origin = origin
		if err := a.Validate(); err == nil {
			t.Fatalf("non-canonical origin %q accepted", origin)
		}
	}
}

func TestAdministrationLocalHTTP(t *testing.T) {
	for _, origin := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080", "http://192.168.1.1", "http://marketplace.test", "http://localhost.example", "http://0.0.0.0", "http://127.0.0.1.example", "http://localhost/path", "http://localhost?", "http://user@localhost", "http://localhost:99999", "http://localhost:08080", "http://[::1%25lo0]", "http://[::ffff:127.0.0.1]:8080", "http://localhost:80", "http://127.0.0.1:80", "http://[::1]:80", "http://LOCALHOST:8080", "https://marketplace.test"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/opt-in=%v", origin, enabled), func(t *testing.T) {
				var cfg Config
				input := fmt.Sprintf("admin:\n  enabled: true\n  allow_insecure_localhost: %v\n  origin: %s\n  issuer: proxy\n  trusted_proxies: [127.0.0.1/32]\n  subject_header: X-User\n  groups_header: X-Groups\n  admin_groups: [admins]\n", enabled, origin)
				if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
					t.Fatal(err)
				}
				want := origin == "https://marketplace.test" || enabled && (origin == "http://localhost:8080" || origin == "http://127.0.0.1:8080" || origin == "http://[::1]:8080")
				if err := cfg.Admin.Validate(); (err == nil) != want {
					t.Fatalf("Validate() = %v, want valid=%v", err, want)
				}
				if want {
					cfg.Admin.TrustedProxies = nil
					if cfg.Admin.Validate() == nil {
						t.Fatal("local HTTP must not bypass proxy validation")
					}
				}
			})
		}
	}
}

func TestSourceHarnessCapabilities(t *testing.T) {
	expected := []string{"claude", "cursor", "devin", "factory", "gemini", "windsurf"}
	got := SourceHarnesses()
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("harnesses: %v", got)
	}
	got[0] = "mutated"
	if !reflect.DeepEqual(SourceHarnesses(), expected) {
		t.Fatal("caller mutated capabilities")
	}
}

func oidcAdmin() Administration {
	return Administration{Enabled: true, Origin: "https://marketplace.test", AdminGroups: []string{"admins"},
		Authentication: AdminAuthentication{Mode: AdminAuthOIDC, OIDC: AdminOIDC{
			Issuer: "https://issuer.test/oauth2/default", ClientID: "marketplace-admin", SessionSecretEnv: "MARKETPLACE_ADMIN_SESSION_SECRET"}}}
}

func TestAdministrationOIDCValidation(t *testing.T) {
	valid := oidcAdmin()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if valid.AuthMode() != AdminAuthOIDC || valid.SessionLifetime() != 12*time.Hour || valid.ResolvedOIDC().GroupsClaim != "groups" {
		t.Fatalf("resolved OIDC: %+v", valid.ResolvedOIDC())
	}
	for _, tc := range []struct {
		name   string
		change func(*Administration)
	}{
		{"mixed proxy fields", func(a *Administration) { a.SubjectHeader = "X-User" }},
		{"unknown mode", func(a *Administration) { a.Authentication.Mode = "saml" }},
		{"empty mode with oidc fields", func(a *Administration) { a.Authentication.Mode = "" }},
		{"proxy issuer in oidc mode", func(a *Administration) { a.Issuer = "proxy" }},
		{"no issuer", func(a *Administration) { a.Authentication.OIDC.Issuer = "" }},
		{"http issuer", func(a *Administration) { a.Authentication.OIDC.Issuer = "http://issuer.test" }},
		{"issuer trailing slash", func(a *Administration) { a.Authentication.OIDC.Issuer += "/" }},
		{"issuer query", func(a *Administration) { a.Authentication.OIDC.Issuer = "https://issuer.test?x=1" }},
		{"no client id", func(a *Administration) { a.Authentication.OIDC.ClientID = "" }},
		{"unsafe claim", func(a *Administration) { a.Authentication.OIDC.GroupsClaim = "a b" }},
		{"zero ttl", func(a *Administration) { a.Authentication.OIDC.SessionTTL = Duration(-1) }},
		{"ttl over 24h", func(a *Administration) { a.Authentication.OIDC.SessionTTL = Duration(25 * time.Hour) }},
		{"no secret reference", func(a *Administration) { a.Authentication.OIDC.SessionSecretEnv = "" }},
		{"both secret references", func(a *Administration) { a.Authentication.OIDC.SessionSecretFile = "/tmp/s" }},
		{"unsafe env name", func(a *Administration) { a.Authentication.OIDC.SessionSecretEnv = "1BAD-NAME" }},
		{"no authorization", func(a *Administration) { a.AdminGroups = nil; a.AdminUsers = nil }},
		{"bad admin user", func(a *Administration) { a.AdminUsers = []string{" padded "} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := valid
			tc.change(&a)
			if a.Validate() == nil {
				t.Fatal("unsafe admin config accepted")
			}
		})
	}
}

func TestAdministrationModeCompatibility(t *testing.T) {
	legacy := Administration{Enabled: true, Origin: "https://marketplace.test", Issuer: "proxy", TrustedProxies: []string{"127.0.0.1/32"}, SubjectHeader: "X-User", GroupsHeader: "X-Groups", AdminUsers: []string{"sub-1"}}
	if err := legacy.Validate(); err != nil || legacy.AuthMode() != AdminAuthTrustedProxy {
		t.Fatalf("inferred proxy mode: %v", err)
	}
	explicit := legacy
	explicit.Authentication.Mode = AdminAuthTrustedProxy
	if err := explicit.Validate(); err != nil {
		t.Fatalf("explicit proxy mode: %v", err)
	}
	mixed := legacy
	mixed.Authentication.Mode = AdminAuthTrustedProxy
	mixed.Authentication.OIDC.ClientID = "c"
	if mixed.Validate() == nil {
		t.Fatal("trusted-proxy mode accepted OIDC fields")
	}
	partial := legacy
	partial.GroupsHeader = ""
	partial.Authentication.Mode = ""
	if partial.Validate() == nil {
		t.Fatal("incomplete legacy proxy config accepted without explicit mode")
	}
	oidc := oidcAdmin()
	oidc.AdminGroups = nil
	oidc.AdminUsers = []string{"okta-sub"}
	if oidc.Validate() != nil {
		t.Fatal("subject-only authorization rejected")
	}
	var cfg Config
	input := "admin:\n  enabled: true\n  origin: https://marketplace.test\n  admin_users: [okta-sub]\n  authentication:\n    mode: oidc\n    oidc:\n      issuer: https://issuer.test\n      client_id: marketplace-admin\n      session_secret_env: MARKETPLACE_ADMIN_SESSION_SECRET\n      session_ttl: 8h\n"
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil || cfg.Admin.Validate() != nil || cfg.Admin.SessionLifetime() != 8*time.Hour {
		t.Fatalf("oidc yaml: %+v %v", cfg.Admin, err)
	}
}

func TestAdministrationOIDCSecretResolution(t *testing.T) {
	admin := oidcAdmin()
	if _, err := admin.ResolveSessionSecret(); err == nil {
		t.Fatal("unset secret env resolved")
	}
	t.Setenv("MARKETPLACE_ADMIN_SESSION_SECRET", "short")
	if _, err := admin.ResolveSessionSecret(); err == nil {
		t.Fatal("short secret accepted")
	}
	t.Setenv("MARKETPLACE_ADMIN_SESSION_SECRET", "  "+strings.Repeat("x", 40)+"  ")
	if secret, err := admin.ResolveSessionSecret(); err != nil || len(secret) != 40 {
		t.Fatalf("env secret: %v %d", err, len(secret))
	}
	file := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(file, []byte(strings.Repeat("k", 40)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	admin.Authentication.OIDC.SessionSecretEnv = ""
	admin.Authentication.OIDC.SessionSecretFile = file
	if err := admin.Validate(); err != nil {
		t.Fatal(err)
	}
	if secret, err := admin.ResolveSessionSecret(); err != nil || len(secret) != 40 {
		t.Fatalf("file secret: %v %d", err, len(secret))
	}
	link := filepath.Join(t.TempDir(), "secret-link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	admin.Authentication.OIDC.SessionSecretFile = link
	if secret, err := admin.ResolveSessionSecret(); err != nil || len(secret) != 40 {
		t.Fatalf("symlinked file secret: %v %d", err, len(secret))
	}
	oversized := filepath.Join(t.TempDir(), "oversized")
	if err := os.WriteFile(oversized, []byte(strings.Repeat("k", maxAdminSessionSecret+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	admin.Authentication.OIDC.SessionSecretFile = oversized
	if _, err := admin.ResolveSessionSecret(); err == nil {
		t.Fatal("oversized secret file accepted")
	}
}

func TestAdministrationOIDCSecretFIFOChild(t *testing.T) {
	path := os.Getenv("MARKETPLACE_ADMIN_SECRET_FIFO")
	if path == "" {
		return
	}
	admin := oidcAdmin()
	admin.Authentication.OIDC.SessionSecretEnv = ""
	admin.Authentication.OIDC.SessionSecretFile = path
	if _, err := admin.ResolveSessionSecret(); err == nil {
		t.Fatal("FIFO secret accepted")
	}
}

func TestAdministrationOIDCSecretFIFORejectedWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "secret")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAdministrationOIDCSecretFIFOChild$", "-test.v")
	cmd.Env = append(os.Environ(), "MARKETPLACE_ADMIN_SECRET_FIFO="+fifo)
	if output, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("secret FIFO resolution blocked: %v\n%s", ctx.Err(), output)
		}
		t.Fatalf("secret FIFO child: %v\n%s", err, output)
	}
}
