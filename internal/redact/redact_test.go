package redact

import "testing"

func TestURL(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "credentials", value: "https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.com/repo.git", want: "https://***@example.com/repo.git"},
		{name: "token", value: "https://YOUR_TOKEN_HERE@example.com/repo.git", want: "https://***@example.com/repo.git"},
		{name: "at sign in credentials", value: "https://YOUR_USER_HERE:YOUR_TOKEN@HERE@example.com/repo.git", want: "https://***@example.com/repo.git"},
		{name: "file URL", value: "file:///tmp/repo", want: "file:///tmp/repo"},
		{name: "SSH URL", value: "git@example.com:repo.git", want: "git@example.com:repo.git"},
		{name: "query at sign", value: "https://example.com?email=user@example.net", want: "https://example.com?email=user@example.net"},
		{name: "fragment at sign", value: "https://example.com#user@example.net", want: "https://example.com#user@example.net"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := URL(tt.value); got != tt.want {
				t.Fatalf("URL(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestStrings(t *testing.T) {
	input := []string{"https://YOUR_TOKEN_HERE@example.com/repo.git", "git@example.com:repo.git"}
	got := Strings(input)
	if got[0] != "https://***@example.com/repo.git" || got[1] != input[1] {
		t.Fatalf("Strings() = %#v", got)
	}
	if input[0] != "https://YOUR_TOKEN_HERE@example.com/repo.git" {
		t.Fatalf("Strings() mutated input: %#v", input)
	}
}
