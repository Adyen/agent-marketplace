package jsonc_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func TestJSONReadersRetainNestedDiagnosticPaths(t *testing.T) {
	for _, fixture := range []struct {
		data, path string
	}{
		{`{"items":[{},{},{},{},{},{},{},{},{},{},{"Name":{"Child":{"x":0,"x":"VALUE_MUST_NOT_LEAK"}}}]}`, "$.items[10].Name.Child"},
		{`{"a":{"seen":{"one":1}},"b":[{},{"target":{"x":0,"x":"VALUE_MUST_NOT_LEAK"}}]}`, "$.b[1].target"},
		{`{"a.b":{"[x]":[{"x":0,"x":"VALUE_MUST_NOT_LEAK"}]}}`, "$.a.b.[x][0]"},
		{`{"\u0078":0,"x":"VALUE_MUST_NOT_LEAK"}`, "$"},
	} {
		for name, read := range map[string]func([]byte) error{
			"json":  func(data []byte) error { return jsonutil.Validate("fixture.json", data) },
			"jsonc": func(data []byte) error { _, err := jsonc.Parse("fixture.json", data); return err },
		} {
			t.Run(name+"/"+fixture.path, func(t *testing.T) {
				data := []byte(fixture.data)
				err := read(data)
				var duplicate *jsonutil.DuplicateKeyError
				if !errors.As(err, &duplicate) || duplicate.Path != fixture.path || duplicate.Key != "x" {
					t.Fatalf("diagnostic = %v, duplicate = %+v", err, duplicate)
				}
				coded, ok := diag.AsCodedError(err)
				if !ok || coded.Code != diag.CodeJSONDuplicateKey || coded.Source.File != "fixture.json" || coded.Source.Line != 1 {
					t.Fatalf("coded diagnostic = %+v", coded)
				}
				if strings.Contains(err.Error(), "VALUE_MUST_NOT_LEAK") {
					t.Fatal("diagnostic includes a value")
				}
				if duplicate.Offset <= 0 || duplicate.Offset > int64(len(data)) || data[duplicate.Offset-1] != '"' {
					t.Fatalf("duplicate key offset = %d", duplicate.Offset)
				}
			})
		}
	}
}

func TestJSONReadersRetainAcceptedDepthAndNativeBytes(t *testing.T) {
	const depth = 512
	key := strings.Repeat("k", 128)
	data := []byte(strings.Repeat(`{"`+key+`":`, depth) + "null" + strings.Repeat("}", depth))
	if err := jsonutil.ValidateUniqueKeys(data); err != nil {
		t.Fatal(err)
	}
	doc, err := jsonc.Parse("aggregate.jsonc", data)
	if err != nil || !bytes.Equal(doc.Bytes(), data) {
		t.Fatalf("accepted nested document changed: %v", err)
	}
	excessive := []byte(strings.Repeat("[", 1005) + "0" + strings.Repeat("]", 1005))
	if err := jsonutil.ValidateUniqueKeys(excessive); !errors.Is(err, jsonutil.ErrMaxDepth) {
		t.Fatalf("JSON depth refusal = %v", err)
	}
	if _, err := jsonc.Parse("aggregate.jsonc", excessive); !errors.Is(err, jsonutil.ErrMaxDepth) {
		t.Fatalf("JSONC depth refusal = %v", err)
	}
}
