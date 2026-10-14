package redact

import (
	"errors"
	"fmt"
	"net/url"
	"testing"
)

func TestError(t *testing.T) {
	if Error(nil) != nil {
		t.Fatal("nil error changed")
	}
	sentinel := errors.New("offline")
	err := Error(fmt.Errorf("post https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.com/v1?token=YOUR_QUERY_HERE %w", sentinel))
	if got, want := err.Error(), "post https://***@example.com/v1?*** offline"; got != want || !errors.Is(err, sentinel) {
		t.Fatalf("Error() = %q, errors.Is = %v; want %q and the cause retained", got, errors.Is(err, sentinel), want)
	}
	parseErr := &url.Error{Op: "parse", URL: "https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.com/x?token=FIRST SECOND", Err: errors.New("invalid")}
	var urlErr *url.Error
	wrapped := Error(fmt.Errorf("endpoint: %w", parseErr))
	if got, want := wrapped.Error(), `endpoint: parse "https://***@example.com/x?***": invalid`; got != want || !errors.As(wrapped, &urlErr) {
		t.Fatalf("Error() = %q, want %q with the *url.Error retained", got, want)
	}
}

func TestURLValue(t *testing.T) {
	for _, tt := range []struct{ value, want string }{
		{"", ""},
		{"https://example.com/v1/metrics", "https://example.com/v1/metrics"},
		{"https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.com:8443/v1?token=FIRST SECOND#FRAGMENT", "https://***@example.com:8443/v1?***"},
		{"https://example.com/v1#FRAGMENT SECOND", "https://example.com/v1#***"},
		{"https://YOUR_USER_HERE:YOUR TOKEN@example.com/v1", "[redacted URL]"},
		{"https://example.com/v1\x7f?token=FIRST SECOND", "[redacted URL]"},
		{"YOUR_USER_HERE:YOUR_TOKEN_HERE@example.com", "[redacted URL]"},
	} {
		if got := URLValue(tt.value); got != tt.want {
			t.Errorf("URLValue(%q) = %q, want %q", tt.value, got, tt.want)
		}
		if got := URLValue(tt.want); tt.want != "[redacted URL]" && got != tt.want {
			t.Errorf("URLValue is not idempotent for %q: %q", tt.want, got)
		}
	}
}

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
		{name: "SCP URL", value: "git@example.com:repo.git", want: "git@example.com:repo.git"},
		{name: "SCP credentials", value: "user:token@example.com:repo.git", want: "***@example.com:repo.git"},
		{name: "SSH URI credentials", value: "ssh://user:token@example.com/repo.git", want: "ssh://***@example.com/repo.git"},
		{name: "credentials with port", value: "https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.com:8443/repo.git?token=YOUR_QUERY_HERE", want: "https://***@example.com:8443/repo.git?***"},
		{name: "SSH URI credentials with port", value: "ssh://user:token@example.com:22/repo.git", want: "ssh://***@example.com:22/repo.git"},
		{name: "query at sign", value: "https://example.com?email=user@example.net", want: "https://example.com?***"},
		{name: "fragment at sign", value: "https://example.com#user@example.net", want: "https://example.com#***"},
		{name: "all credential fields", value: "https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.com/repo?key=YOUR_QUERY_HERE#YOUR_FRAGMENT_HERE", want: "https://***@example.com/repo?***"},
		{name: "escaped suffix", value: "https://example.com/repo?key=YOUR%20TOKEN#fragment", want: "https://example.com/repo?***"},
		{name: "malformed suffix", value: "https://example.com/repo?key=%ZZ\"YOUR_TOKEN_HERE", want: "https://example.com/repo?***"},
		{name: "diagnostic", value: "failed https://example.com/repo?key=YOUR_TOKEN_HERE request", want: "failed https://example.com/repo?*** request"},
		{name: "multiple URLs", value: "https://one.example/repo?one=SECRET https://two.example/repo#SECRET", want: "https://one.example/repo?*** https://two.example/repo#***"},
		{name: "non URL punctuation", value: "why? retain # details", want: "why? retain # details"},
		{name: "empty suffix", value: "https://example.com/repo?", want: "https://example.com/repo?***"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := URL(tt.value); got != tt.want {
				t.Fatalf("URL(%q) = %q, want %q", tt.value, got, tt.want)
			}
			if got := URL(tt.want); got != tt.want {
				t.Fatalf("redaction is not idempotent: %q", got)
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
