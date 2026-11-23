package targetprofile

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTargetProfileIdentity(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Source
	}{
		{"builtin", "builtin:agent-marketplace", Source{Kind: "builtin", Origin: "agent-marketplace"}},
		{"marketplace", "marketplace:https%3A%2F%2Fmp.example#mp-1/curated/profiles", Source{Kind: "marketplace", Origin: "https://mp.example", InstanceID: "mp-1", RegistryID: "curated", Root: "profiles"}},
		{"git", "git:https%3A%2F%2Fgit.example%2Fteam%2Fprofiles@refs%2Fheads%2Fmain#profiles%2Fherdr-app", Source{Kind: "git", Origin: "https://git.example/team/profiles", Ref: "refs/heads/main", Root: "profiles/herdr-app"}},
		{"directory", "directory:%2Fsrv%2Fprofiles%2Fherdr-app", Source{Kind: "directory", Root: "/srv/profiles/herdr-app"}},
		{"percent data is decoded once", "git:https%3A%2F%2Fgit.example%2Fteam%2Fprofiles@main#profiles%252Falias", Source{Kind: "git", Origin: "https://git.example/team/profiles", Ref: "main", Root: "profiles%2Falias"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSourceID(tt.raw)
			if err != nil {
				t.Fatalf("ParseSourceID() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseSourceID() = %#v, want %#v", got, tt.want)
			}
		})
	}

	invalidSources := []string{
		"marketplace:https%3a%2F%2Fmp.example#mp-1/curated/profiles",
		"marketplace:https://mp.example#mp-1/curated/profiles",
		"git:https%253A%252F%252Fgit.example%252Fteam%252Fprofiles.git@main#profiles",
		"git:https%3A%2F%2Fgit.example%2Fteam%2Fprofiles.git@main#profiles",
		"git:https%3A%2F%2Fgit.example%2Fteam%2Fprofiles@main#profiles%2F..%2Foutside",
		"git:https%3A%2F%2Fgit.example%2Fteam%2Fprofiles@main#profiles%2Fbad%0Apath",
		"directory:/srv/profiles/herdr-app",
		"directory:%252Ftmp%252Fprofiles",
		"marketplace:https%3A%2F%2Fmp.example#mp-1/curated/",
		"unknown:source",
	}
	for _, raw := range invalidSources {
		t.Run("reject "+raw, func(t *testing.T) {
			if got, err := ParseSourceID(raw); err == nil {
				t.Fatalf("ParseSourceID(%q) = %#v, want error", raw, got)
			}
		})
	}

	validDigest := "sha256:" + strings.Repeat("a", 64)
	valid := Identity{TargetID: "herdr-app", SourceID: "builtin:agent-marketplace", RevisionDigest: validDigest}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	wantJSON := `{"target_id":"herdr-app","source_id":"builtin:agent-marketplace","revision_digest":"` + validDigest + `"}`
	if string(encoded) != wantJSON {
		t.Fatalf("json.Marshal() = %s, want %s", encoded, wantJSON)
	}
	for _, tt := range []struct {
		name string
		edit func(*Identity)
	}{
		{"malformed target", func(id *Identity) { id.TargetID = "Invalid ID" }},
		{"malformed digest", func(id *Identity) { id.RevisionDigest = "sha256:" + strings.Repeat("A", 64) }},
		{"malformed source", func(id *Identity) { id.SourceID = "directory:private-source" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id := valid
			tt.edit(&id)
			err := id.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !strings.Contains(err.Error(), identityInvalidCode) || !strings.Contains(err.Error(), id.TargetID) {
				t.Fatalf("Validate() error = %v, want identity diagnostic naming target %q", err, id.TargetID)
			}
			if strings.Contains(err.Error(), id.SourceID) && tt.name == "malformed source" {
				t.Fatalf("Validate() error echoed malformed source: %v", err)
			}
		})
	}

	t.Run("compiled target has a distinct diagnostic", func(t *testing.T) {
		compiled := valid
		compiled.TargetID = "claude"
		if err := compiled.Validate(); err == nil || !strings.HasPrefix(err.Error(), "marketplace.profile.compiled-target-id:") {
			t.Fatalf("Validate() error = %v, want compiled-target diagnostic", err)
		}
	})
}
