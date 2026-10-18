package jsonc

import (
	"bytes"
	"testing"
)

func TestNativeTrailingCommasPreserveTrivia(t *testing.T) {
	for _, input := range []string{`{"a": [1, 2,], /* keep */}`, "{\n // keep\n \"a\": 1,\n}\n"} {
		if _, err := Parse("strict.json", []byte(input)); err == nil {
			t.Fatal("default parser accepted trailing comma")
		}
		doc, err := ParseWithOptions("native.jsonc", []byte(input), ParseOptions{AllowTrailingCommas: true})
		if err != nil || !bytes.Equal(doc.Bytes(), []byte(input)) {
			t.Fatalf("native parse: %v %s", err, doc.Bytes())
		}
		doc.SetMember(doc.Root(), "b", []byte(`true`))
		if _, err := ParseWithOptions("native.jsonc", doc.Bytes(), ParseOptions{AllowTrailingCommas: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ParseWithOptions("native.jsonc", []byte(`{"a":1,"a":2,}`), ParseOptions{AllowTrailingCommas: true}); err == nil {
		t.Fatal("duplicate key accepted")
	}
}
