package compile

import "testing"

func TestFrontmatterPreservesInsertionOrder(t *testing.T) {
	var fm Frontmatter
	fm.Set("name", "demo")
	fm.Set("description", "Demo resource")
	fm.Set("triggers", []string{"user"})

	got, err := fm.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "---\nname: demo\ndescription: Demo resource\ntriggers:\n  - user\n---\n"
	if got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestFrontmatterSetKeepsOriginalPositionOnOverwrite(t *testing.T) {
	var fm Frontmatter
	fm.Set("name", "first")
	fm.Set("description", "Demo resource")
	fm.Set("name", "second")

	keys := fm.Keys()
	if len(keys) != 2 || keys[0] != "name" || keys[1] != "description" {
		t.Fatalf("Keys() = %v, want [name description]", keys)
	}
	if value := fm.Value("name"); value != "second" {
		t.Errorf("Value(name) = %v, want second", value)
	}
}

func TestFrontmatterRendersValueShapes(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "empty list", value: []string{}, want: "---\nkey: []\n---\n"},
		{name: "nil", value: nil, want: "---\nkey: null\n---\n"},
		{name: "bool", value: true, want: "---\nkey: true\n---\n"},
		{name: "map", value: map[string]any{"a": 1}, want: "---\nkey:\n  a: 1\n---\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fm Frontmatter
			fm.Set("key", tt.value)
			got, err := fm.Render()
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got != tt.want {
				t.Errorf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFrontmatterHasReportsSetKeys(t *testing.T) {
	var fm Frontmatter
	if fm.Has("name") {
		t.Error("Has(name) = true on an empty Frontmatter")
	}
	fm.Set("name", "demo")
	if !fm.Has("name") {
		t.Error("Has(name) = false after Set")
	}
}

func TestFrontmatterKeysReturnsCopy(t *testing.T) {
	var fm Frontmatter
	fm.Set("name", "demo")
	fm.Set("description", "Demo resource")

	keys := fm.Keys()
	keys[0] = "tampered"

	got, err := fm.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "---\nname: demo\ndescription: Demo resource\n---\n"
	if got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
}
