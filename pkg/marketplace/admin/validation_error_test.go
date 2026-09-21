package admin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestPreviewReportsFieldLevelProblems(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	v, _ := s.Read(ctx, "marketplace")
	existing := v.Snapshot.Registries[1]
	fresh := Registry{ID: "fresh", Slug: "fresh", Origin: "https://git.example.test/fresh.git", Ref: "main", Roots: []store.RegistryRoot{{Path: "."}}, Acquisition: store.AcquisitionIndexOnly}
	for name, tc := range map[string]struct {
		edit   func(*Registry)
		fields map[string]string
	}{
		"slug with slash":    {func(r *Registry) { r.Slug = "mattpocock/skills" }, map[string]string{"slug": "lowercase"}},
		"empty ref":          {func(r *Registry) { r.Ref = "" }, map[string]string{"ref": "required"}},
		"origin authority":   {func(r *Registry) { r.Origin = "https://github.com/mattpocock/skills.git" }, map[string]string{"origin": "git.example.test"}},
		"root escapes":       {func(r *Registry) { r.Roots = []store.RegistryRoot{{Path: "../x"}} }, map[string]string{"roots[0].path": "relative"}},
		"root backslash":     {func(r *Registry) { r.Roots = []store.RegistryRoot{{Path: `skills\x`}} }, map[string]string{"roots[0].path": "backslash"}},
		"unknown profile":    {func(r *Registry) { r.ProfileID = "missing" }, map[string]string{"profile_id": "missing"}},
		"no acquisition":     {func(r *Registry) { r.Acquisition = "" }, map[string]string{"acquisition": "mirror"}},
		"several at once":    {func(r *Registry) { r.Slug, r.Ref = "Bad Slug", "" }, map[string]string{"slug": "lowercase", "ref": "required"}},
		"slug taken":         {func(r *Registry) { r.Slug = existing.Slug }, map[string]string{"slug": string(existing.ID)}},
		"invalid ID chars":   {func(r *Registry) { r.ID = "a/b" }, map[string]string{"id": "must not"}},
		"new but paused":     {func(r *Registry) { r.Paused = true }, map[string]string{}},
		"empty override":     {func(r *Registry) { r.LicenseOverride = &LicensePolicy{} }, map[string]string{"license_override": "allowlist"}},
		"existing gen drift": {func(r *Registry) { *r = existing; r.Generation++ }, map[string]string{}},
	} {
		t.Run(name, func(t *testing.T) {
			r := fresh
			tc.edit(&r)
			m := mutation("upsert_registry", 1)
			m.RegistryID, m.Registry = r.ID, &r
			_, err := s.Preview(ctx, "marketplace", m)
			var invalid *ValidationError
			if !errors.Is(err, ErrInvalidConfiguration) || !errors.As(err, &invalid) {
				t.Fatalf("want ValidationError, got %v", err)
			}
			if invalid.Message == "" {
				t.Fatal("message must describe the problem")
			}
			if len(invalid.Fields) != len(tc.fields) {
				t.Fatalf("fields %v, want keys %v", invalid.Fields, tc.fields)
			}
			for field, fragment := range tc.fields {
				if !strings.Contains(strings.ToLower(invalid.Fields[field]), strings.ToLower(fragment)) {
					t.Fatalf("field %s = %q, want it to mention %q (all: %v)", field, invalid.Fields[field], fragment, invalid.Fields)
				}
			}
		})
	}
}

func TestPreviewReportsOriginMoveConfirmation(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	v, _ := s.Read(ctx, "marketplace")
	r := v.Snapshot.Registries[1]
	r.Origin = "https://git.example.test/moved.git"
	m := mutation("upsert_registry", 1)
	m.RegistryID, m.Registry = r.ID, &r
	var invalid *ValidationError
	if _, err := s.Preview(ctx, "marketplace", m); !errors.As(err, &invalid) || !strings.Contains(invalid.Fields["origin"], "confirm") {
		t.Fatalf("origin move must ask for confirmation on the origin field: %v", err)
	}
	m.ConfirmOriginMove = true
	if _, err := s.Preview(ctx, "marketplace", m); err != nil {
		t.Fatalf("confirmed origin move: %v", err)
	}
}

func TestValidateReportsActionProblemsWithoutFields(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	var invalid *ValidationError
	err := s.Validate(ctx, "marketplace", mutation("restore", 1))
	if !errors.As(err, &invalid) || len(invalid.Fields) != 0 || !strings.Contains(invalid.Message, "restore") {
		t.Fatalf("lifecycle rejection must explain the action: %v", err)
	}
	if _, err := s.Preview(ctx, "marketplace", mutation("pause", 1)); !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "registry draft") {
		t.Fatalf("preview of a lifecycle action must explain itself: %v", err)
	}
}
