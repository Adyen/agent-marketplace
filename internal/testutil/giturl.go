package testutil

// GitURLFixture is an independently authored canonical Git URL contract case.
// Canonical is empty for inputs that must be rejected.
type GitURLFixture struct {
	Name      string
	Raw       string
	Canonical string
}

// CanonicalGitURLFixtures covers every accepted spelling and the safety
// boundaries that must remain rejected by all consumers of Git URL identity.
var CanonicalGitURLFixtures = []GitURLFixture{
	{
		Name:      "https default port repeated separators and git suffix",
		Raw:       " HTTPS://Git.Example.COM:443//team/repo.git/ ",
		Canonical: "https://git.example.com/team/repo",
	},
	{
		Name:      "https equivalent spelling",
		Raw:       "https://git.example.com/team/repo",
		Canonical: "https://git.example.com/team/repo",
	},
	{
		Name:      "http default port",
		Raw:       "http://git.example.com:80/team/repo.git",
		Canonical: "http://git.example.com/team/repo",
	},
	{
		Name:      "ssh username default port repeated separators",
		Raw:       "ssh://Git@Git.Example.COM:22//team/repo.git",
		Canonical: "ssh://Git@git.example.com/team/repo",
	},
	{
		Name:      "ssh equivalent spelling",
		Raw:       "ssh://Git@git.example.com/team/repo",
		Canonical: "ssh://Git@git.example.com/team/repo",
	},
	{
		Name:      "scp spelling",
		Raw:       "git@git.example.com:team//repo.git",
		Canonical: "git+scp://git@git.example.com/team/repo",
	},
	{
		Name:      "canonical scp spelling",
		Raw:       "git+scp://git@git.example.com/team/repo",
		Canonical: "git+scp://git@git.example.com/team/repo",
	},
	{
		Name:      "file absolute git suffix",
		Raw:       "file:///tmp/repos/example.git",
		Canonical: "file:///tmp/repos/example",
	},
	{
		Name:      "file equivalent repeated separators",
		Raw:       "file:///tmp/repos//example/",
		Canonical: "file:///tmp/repos/example",
	},
	{
		Name:      "http username without password is stripped",
		Raw:       "http://reader@git.example.com/team/repo",
		Canonical: "http://git.example.com/team/repo",
	},
	{
		Name: "password credential is rejected",
		Raw:  "https://reader:secret@git.example.com/team/repo",
	},
	{
		Name: "query is rejected",
		Raw:  "https://git.example.com/team/repo?ref=main",
	},
	{
		Name: "fragment is rejected",
		Raw:  "https://git.example.com/team/repo#readme",
	},
	{
		Name: "empty fragment is rejected",
		Raw:  "https://git.example.com/team/repo#",
	},
	{
		Name: "encoded slash is rejected",
		Raw:  "https://git.example.com/team%2Frepo",
	},
	{
		Name: "encoded backslash is rejected",
		Raw:  "https://git.example.com/team%5Crepo",
	},
	{
		Name: "parent traversal is rejected",
		Raw:  "https://git.example.com/team/../repo",
	},
	{
		Name: "dot segment is rejected",
		Raw:  "https://git.example.com/team/./repo",
	},
	{
		Name: "file host is rejected",
		Raw:  "file://other-host/tmp/repo",
	},
	{
		Name: "file relative path is rejected",
		Raw:  "file:tmp/repo",
	},
}

// NonEquivalentGitURLPairs are valid identities that must not collapse into
// one cache or lock identity merely because their paths look similar.
var NonEquivalentGitURLPairs = []struct {
	Name        string
	Left, Right string
}{
	{
		Name:  "http and https schemes",
		Left:  "http://git.example.com/team/repo",
		Right: "https://git.example.com/team/repo",
	},
	{
		Name:  "different repository paths",
		Left:  "https://git.example.com/team/repo",
		Right: "https://git.example.com/team/other",
	},
	{
		Name:  "non-default ports",
		Left:  "https://git.example.com:8443/team/repo",
		Right: "https://git.example.com:9443/team/repo",
	},
	{
		Name:  "file paths",
		Left:  "file:///tmp/repos/repo",
		Right: "file:///tmp/other/repo",
	},
	{
		Name:  "file and https schemes",
		Left:  "file:///tmp/repos/repo",
		Right: "https://git.example.com/tmp/repos/repo",
	},
}
