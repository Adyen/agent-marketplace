package gitidentity

import "testing"

// spec: acquisition-lock-codec/exact-profile-origins-refuse-repeated-git-suffixes
func TestCanonicalRepositorySuffix(t *testing.T) {
	for _, origin := range []struct{ input, canonical string }{
		{"http://git.example/", "http://git.example/"},
		{"https://git.example/", "https://git.example/"},
		{"ssh://git@git.example/", "ssh://git@git.example/"},
		{"git@git.example:", "git+scp://git@git.example/"},
		{"git+scp://git@git.example/", "git+scp://git@git.example/"},
		{"file:///", "file:///"},
	} {
		t.Run(origin.input, func(t *testing.T) {
			for _, leaf := range []string{"tools.git.git", ".git", "..git", "...git", "tools.git%2egit"} {
				raw := origin.input + "team/" + leaf
				if got, err := CanonicalV1(raw); err == nil || got != "" {
					t.Errorf("CanonicalV1(%q) = %q, %v; want refusal", raw, got, err)
				}
			}
			for _, suffix := range []string{"", ".git"} {
				const repo = "team.git/tools"
				want := origin.canonical + repo
				got, err := CanonicalV1(origin.input + repo + suffix)
				if err != nil || got != want {
					t.Errorf("origin with suffix %q = %q, %v; want %q", suffix, got, err, want)
				} else if again, err := CanonicalV1(got); err != nil || again != got {
					t.Errorf("round trip of %q = %q, %v", got, again, err)
				}
			}
		})
	}
}

func TestCanonicalSCPIPv6RoundTrip(t *testing.T) {
	const want = "git+scp://git@[2001:db8::1]/team/repo"
	got, err := CanonicalV1("git+scp://git@[2001:DB8::1]/team/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("canonical origin = %q; want %q", got, want)
	}
	if again, err := CanonicalV1(got); err != nil || again != got {
		t.Fatalf("round trip of %q = %q, %v", got, again, err)
	}
}
