package telemetry

import "testing"

func TestSanitizeRegistryTelemetry(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantID       string
		wantLocation string
		wantLocal    bool
		wantOK       bool
	}{
		{name: "trim and redact URL", raw: "  https://user:secret@GIT.EXAMPLE/Team/Repo.git?token=hidden#private  ", wantID: "git.example/team/repo", wantLocation: "https://git.example/Team/Repo.git", wantOK: true},
		{name: "SCP remote", raw: "git@Git.Example:Team/Repo.git?token=hidden#private", wantID: "git.example/team/repo", wantLocation: "git.example:Team/Repo.git", wantOK: true},
		{name: "SSH transport identity", raw: "ssh://git@GIT.EXAMPLE/Team/Repo.git/", wantID: "git.example/team/repo", wantLocation: "ssh://git.example/Team/Repo.git", wantOK: true},
		{name: "SSH default port identity", raw: "ssh://git@GIT.EXAMPLE:22/Team/Repo.git/", wantID: "git.example/team/repo", wantLocation: "ssh://git.example:22/Team/Repo.git", wantOK: true},
		{name: "HTTPS default port identity", raw: "https://GIT.EXAMPLE:443/Team/Repo.git/", wantID: "git.example/team/repo", wantLocation: "https://git.example:443/Team/Repo.git", wantOK: true},
		{name: "non-default port identity", raw: "ssh://git@GIT.EXAMPLE:2222/Team/Repo.git/", wantID: "git.example:2222/team/repo", wantLocation: "ssh://git.example:2222/Team/Repo.git", wantOK: true},
		{name: "encoded URL path", raw: "https://git.example/Team/My%20Repo.git", wantID: "git.example/team/my repo", wantLocation: "https://git.example/Team/My%20Repo.git", wantOK: true},
		{name: "invalid UTF-8 escape", raw: "https://git.example/Team/%FFRepo.git", wantOK: false},
		{name: "current directory git path", raw: "./private/repo.git", wantID: "local", wantLocation: "local", wantLocal: true, wantOK: true},
		{name: "parent directory git path", raw: "../private/repo.git", wantID: "local", wantLocation: "local", wantLocal: true, wantOK: true},
		{name: "uppercase file URI", raw: "FILE:///Users/private/repo.git", wantID: "local", wantLocation: "local", wantLocal: true, wantOK: true},
		{name: "mixed file URI", raw: "FiLe:///Users/private/repo.git", wantID: "local", wantLocation: "local", wantLocal: true, wantOK: true},
		{name: "single slash file URI", raw: "file:/Users/private/repo.git", wantID: "local", wantLocation: "local", wantLocal: true, wantOK: true},
		{name: "localhost file URI", raw: "file://localhost/Users/private/repo.git", wantID: "local", wantLocation: "local", wantLocal: true, wantOK: true},
		{name: "encoded file path", raw: "file://localhost/Users/%70rivate/repo.git", wantID: "local", wantLocation: "local", wantLocal: true, wantOK: true},
		{name: "multiple at SCP", raw: "token@secret@Git.Example:Team/Repo.git", wantOK: false},
		{name: "missing SCP host", raw: "git@:Team/Repo.git", wantOK: false},
		{name: "missing SCP path", raw: "git@Git.Example:", wantOK: false},
		{name: "missing URL path", raw: "https://user:secret@git.example?token=hidden", wantOK: false},
		{name: "malformed remote escape", raw: "https://user:secret@git.example/Team/%zz?token=hidden", wantOK: false},
		{name: "malformed local escape", raw: "FILE:///Users/private/%zz", wantOK: false},
		{name: "encoded scheme", raw: "%66ile:///Users/private/repo.git", wantOK: false},
		{name: "newline", raw: "https://git.example/Team/Repo.git\nsecret", wantOK: false},
		{name: "encoded control", raw: "https://git.example/Team/%0aRepo.git", wantOK: false},
		{name: "ambiguous relative path", raw: "private/repo.git", wantOK: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id, location, local, ok := SanitizeRegistryTelemetry(test.raw)
			if id != test.wantID || location != test.wantLocation || local != test.wantLocal || ok != test.wantOK {
				t.Fatalf("SanitizeRegistryTelemetry(%q) = (%q, %q, %v, %v), want (%q, %q, %v, %v)", test.raw, id, location, local, ok, test.wantID, test.wantLocation, test.wantLocal, test.wantOK)
			}
		})
	}
}
