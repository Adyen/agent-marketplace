package config

import (
	"gopkg.in/yaml.v3"
	"reflect"
	"testing"
)

func TestAdministrationValidation(t *testing.T) {
	valid := Administration{Enabled: true, TrustedProxies: []string{"127.0.0.1/32", "::1/128"}, Origin: "https://marketplace.test", Issuer: "proxy", SubjectHeader: "X-User", GroupsHeader: "X-Groups", AdminGroups: []string{"admins"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
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
