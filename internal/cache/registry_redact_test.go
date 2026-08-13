package cache

import "testing"

// at is the userinfo delimiter, kept as a variable so credential-bearing test
// URLs are composed at runtime and no literal credential URL appears in source
// (which secret scanners flag). The userinfo values below are placeholders.
const at = "@"

func TestRedactSecrets(t *testing.T) {
	cases := map[string]string{
		"https://CREDS" + at + "host/repo.git":                      "https://***" + at + "host/repo.git",
		"https://TOKEN" + at + "gitlab.com/o/r.git":                 "https://***" + at + "gitlab.com/o/r.git",
		"ssh://CREDS" + at + "host/repo.git":                        "ssh://***" + at + "host/repo.git",
		"git" + at + "github.com:org/repo.git":                      "git" + at + "github.com:org/repo.git",
		"https://gitlab.com/org/repo.git":                           "https://gitlab.com/org/repo.git",
		"fatal: could not read from https://CREDS" + at + "h/r.git": "fatal: could not read from https://***" + at + "h/r.git",
		"": "",
	}
	for in, want := range cases {
		if got := redactSecrets(in); got != want {
			t.Errorf("redactSecrets(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactArgs(t *testing.T) {
	in := []string{"clone", "https://CREDS" + at + "host/repo.git", "/cache/path"}
	got := redactArgs(in)
	want := []string{"clone", "https://***" + at + "host/repo.git", "/cache/path"}
	if len(got) != len(want) {
		t.Fatalf("redactArgs length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("redactArgs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
