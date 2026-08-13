package bundle

import (
	"testing"
	"testing/fstest"
)

func TestLoadSubagentRejectsMalformedMaxNestingAndPreservesZero(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  int
	}{
		{name: "fractional", value: "1.5"},
		{name: "malformed", value: "not-a-number"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadSubagent(fstest.MapFS{"agents/a.md": &fstest.MapFile{Data: []byte("---\nname: a\nmax-nesting: " + test.value + "\n---\nbody\n")}}, "agents/a.md")
			if err == nil {
				t.Fatalf("LoadSubagent(%q) error = nil", test.value)
			}
		})
	}

	subagent, err := LoadSubagent(fstest.MapFS{"agents/a.md": &fstest.MapFile{Data: []byte("---\nname: a\nmax-nesting: 0\n---\nbody\n")}}, "agents/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if subagent.MaxNesting == nil || *subagent.MaxNesting != 0 {
		t.Fatalf("max nesting = %#v, want explicit zero", subagent.MaxNesting)
	}
}
