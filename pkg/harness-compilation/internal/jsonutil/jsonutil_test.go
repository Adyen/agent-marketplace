package jsonutil

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// fakeSecret is a recognizable non-credential stand-in used to prove that
// duplicate-key diagnostics never echo the offending value.
const fakeSecret = "NOT-A-REAL-SECRET-c0ffee"

func TestValidateUniqueKeysRejectsNestedDuplicate(t *testing.T) {
	err := ValidateUniqueKeys([]byte(`{"outer":{"items":[{"name":"first","name":"second"}]}}`))
	if !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("ValidateUniqueKeys error = %v, want duplicate-key error", err)
	}
}

func TestValidateUniqueKeysAcceptsValidNestedJSON(t *testing.T) {
	if err := ValidateUniqueKeys([]byte(`{"outer":{"items":[{"name":"first","value":2}]}}`)); err != nil {
		t.Fatalf("ValidateUniqueKeys rejected valid JSON: %v", err)
	}
}

func TestValidateUniqueKeysRejectsAdditionalValueAfterDocument(t *testing.T) {
	err := ValidateUniqueKeys([]byte(`{"a":1} {"b":2}`))
	if err == nil {
		t.Fatal("ValidateUniqueKeys accepted a second JSON value after the document")
	}
	if !strings.Contains(err.Error(), "additional JSON value after document") {
		t.Fatalf("ValidateUniqueKeys error = %v, want additional-value error", err)
	}
}

func TestValidateUniqueKeysRejectsTrailingGarbage(t *testing.T) {
	err := ValidateUniqueKeys([]byte(`{"a":1} not-json`))
	if err == nil {
		t.Fatal("ValidateUniqueKeys accepted trailing garbage")
	}
	if !strings.Contains(err.Error(), "trailing JSON data") {
		t.Fatalf("ValidateUniqueKeys error = %v, want trailing-data error", err)
	}
}

func TestValidateUniqueKeysRejectsTruncatedDocument(t *testing.T) {
	if err := ValidateUniqueKeys([]byte(`{"a":`)); err == nil {
		t.Fatal("ValidateUniqueKeys accepted a truncated document")
	}
}

func TestValidateUniqueKeysRejectsExcessiveNesting(t *testing.T) {
	deep := strings.Repeat(`{"a":`, maxDepth+5) + "1" + strings.Repeat("}", maxDepth+5)
	err := ValidateUniqueKeys([]byte(deep))
	if !errors.Is(err, ErrMaxDepth) {
		t.Fatalf("ValidateUniqueKeys error = %v, want max-depth error", err)
	}
}

func TestValidateUniqueKeysAcceptsNestingBelowCap(t *testing.T) {
	deep := strings.Repeat(`{"a":`, maxDepth-1) + "1" + strings.Repeat("}", maxDepth-1)
	if err := ValidateUniqueKeys([]byte(deep)); err != nil {
		t.Fatalf("ValidateUniqueKeys rejected nesting below the cap: %v", err)
	}
}

// A non-string object key cannot be produced through encoding/json's decoder:
// the scanner rejects it as a syntax error before a token is handed back. The
// guard in validateObject is therefore defensive; this test pins the observable
// behaviour (the document is still rejected) rather than the branch.
func TestValidateUniqueKeysRejectsNonStringObjectKey(t *testing.T) {
	if err := ValidateUniqueKeys([]byte(`{1:"a"}`)); err == nil {
		t.Fatal("ValidateUniqueKeys accepted a non-string object key")
	}
}

func TestValidateReturnsCodedSourceAwareDuplicateKeyError(t *testing.T) {
	data := []byte("{\n  \"mcpServers\": {\n    \"docs\": {\n      \"headers\": {\n        \"Authorization\": \"" + fakeSecret + "\",\n        \"Authorization\": \"" + fakeSecret + "\"\n      }\n    }\n  }\n}\n")

	err := Validate(".mcp.json", data)
	if err == nil {
		t.Fatal("Validate accepted a duplicate key")
	}
	if !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("Validate error = %v, want duplicate-key error", err)
	}

	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("Validate error %T is not a *diag.CodedError", err)
	}
	if coded.Code != diag.CodeJSONDuplicateKey {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeJSONDuplicateKey)
	}
	if coded.Severity != diag.SeverityError {
		t.Fatalf("severity = %v, want error (must stay fail-closed)", coded.Severity)
	}
	if coded.Source.File != ".mcp.json" {
		t.Fatalf("source file = %q, want %q", coded.Source.File, ".mcp.json")
	}
	// The duplicate "Authorization" key sits on line 6 of the document above.
	if coded.Source.Line != 6 {
		t.Fatalf("source line = %d, want 6", coded.Source.Line)
	}

	message := err.Error()
	for _, want := range []string{".mcp.json", "$.mcpServers.docs.headers", `"Authorization"`, diag.CodeJSONDuplicateKey} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q does not contain %q", message, want)
		}
	}
	if strings.Contains(message, fakeSecret) {
		t.Fatalf("error %q leaks the duplicated value", message)
	}
}

func TestValidateReturnsMalformedCodeForSyntaxErrors(t *testing.T) {
	err := Validate("settings.json", []byte(`{"hooks":`))
	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("Validate error %T is not a *diag.CodedError", err)
	}
	if coded.Code != diag.CodeJSONMalformed {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeJSONMalformed)
	}
	if coded.Source.File != "settings.json" {
		t.Fatalf("source file = %q, want %q", coded.Source.File, "settings.json")
	}
}

func TestUnmarshalDecodesValidDocument(t *testing.T) {
	var doc struct {
		Name string `json:"name"`
	}
	if err := Unmarshal("plugin.json", []byte(`{"name":"demo"}`), &doc); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if doc.Name != "demo" {
		t.Fatalf("name = %q, want %q", doc.Name, "demo")
	}
}

func TestUnmarshalRejectsDuplicateKeyBeforeDecoding(t *testing.T) {
	var doc struct {
		Name string `json:"name"`
	}
	err := Unmarshal("plugin.json", []byte(`{"name":"first","name":"second"}`), &doc)
	if !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("Unmarshal error = %v, want duplicate-key error", err)
	}
	if doc.Name != "" {
		t.Fatalf("name = %q, want the destination left untouched", doc.Name)
	}
}

func TestUnmarshalReportsTypeMismatchAsMalformed(t *testing.T) {
	var doc struct {
		Name string `json:"name"`
	}
	err := Unmarshal("plugin.json", []byte(`{"name":42}`), &doc)
	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("Unmarshal error %T is not a *diag.CodedError", err)
	}
	if coded.Code != diag.CodeJSONMalformed {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeJSONMalformed)
	}
}

func TestObjectWithUniqueKeys(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		isObject bool
		wantErr  bool
	}{
		{name: "object", data: `{"a":1}`, isObject: true},
		{name: "empty object", data: `{}`, isObject: true},
		{name: "array", data: `[{"a":1}]`},
		{name: "scalar", data: `"a"`},
		{name: "null", data: `null`},
		{name: "empty", data: ``},
		{name: "malformed", data: `{"a":`},
		{name: "trailing data", data: `{"a":1}{"b":2}`},
		{name: "duplicate key", data: `{"a":1,"a":2}`, isObject: true, wantErr: true},
		{name: "nested duplicate key", data: `{"a":{"b":1,"b":2}}`, isObject: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isObject, err := ObjectWithUniqueKeys([]byte(tt.data))
			if isObject != tt.isObject {
				t.Fatalf("isObject = %v, want %v", isObject, tt.isObject)
			}
			if tt.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrDuplicateKey) {
				t.Fatalf("err = %v, want duplicate-key error", err)
			}
		})
	}
}

type aliasNested struct {
	Value string `json:"value"`
}

type AliasPromoted struct {
	Value string `json:"value"`
}

type aliasDocument struct {
	Child  *aliasNested           `json:"child"`
	Items  []aliasNested          `json:"items"`
	Fixed  [2]aliasNested         `json:"fixed"`
	ByName map[string]aliasNested `json:"byName"`
	Values map[string]string      `json:"values"`
	Hidden string                 `json:"-"`
	AliasPromoted
}

func TestUnmarshalRejectsCaseCollidingNestedAliases(t *testing.T) {
	cases := []struct {
		name   string
		data   string
		secret string
	}{
		{name: "pointer", data: `{"child":{"value":"safe","VALUE":"pointer-secret"}}`, secret: "pointer-secret"},
		{name: "slice", data: `{"items":[{"value":"safe","VALUE":"slice-secret"}]}`, secret: "slice-secret"},
		{name: "map", data: `{"byName":{"entry":{"value":"safe","VALUE":"map-secret"}}}`, secret: "map-secret"},
		{name: "promoted", data: `{"value":"safe","VALUE":"promoted-secret"}`, secret: "promoted-secret"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var doc aliasDocument
			err := Unmarshal("aliases.json", []byte(tt.data), &doc)
			coded, ok := diag.AsCodedError(err)
			if !ok || coded.Code != diag.CodeJSONDuplicateKey {
				t.Fatalf("error = %v, want coded duplicate-key error", err)
			}
			if strings.Contains(err.Error(), tt.secret) {
				t.Fatalf("error %q leaked payload value", err)
			}
		})
	}
}

func TestUnmarshalReportsIndexedAliasPaths(t *testing.T) {
	for _, tc := range []struct {
		name, data, path string
	}{
		{name: "second slice element", data: `{"items":[{"value":"safe"},{"value":"safe","VALUE":"ambiguous"}]}`, path: "$.items[1]"},
		{name: "second fixed-array element", data: `{"fixed":[{"value":"safe"},{"value":"safe","VALUE":"ambiguous"}]}`, path: "$.fixed[1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var doc aliasDocument
			err := Unmarshal("aliases.json", []byte(tc.data), &doc)
			var duplicate *DuplicateKeyError
			if !errors.As(err, &duplicate) || duplicate.Path != tc.path {
				t.Fatalf("error = %v, duplicate = %#v, want path %q", err, duplicate, tc.path)
			}
			if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodeJSONDuplicateKey {
				t.Fatalf("error = %v, want coded duplicate-key error", err)
			}
		})
	}
}

func TestUnmarshalPreservesSingletonAliasesAndMapKeys(t *testing.T) {
	var doc aliasDocument
	if err := Unmarshal("aliases.json", []byte(`{"Child":null,"Items":null,"ByName":null,"Values":{"Foo":"one","foo":"two"},"Hidden":"ignored","Value":"promoted"}`), &doc); err != nil {
		t.Fatalf("Unmarshal rejected singleton aliases: %v", err)
	}
	if doc.Values["Foo"] != "one" || doc.Values["foo"] != "two" {
		t.Fatalf("case-distinct map keys = %#v", doc.Values)
	}
	if doc.Hidden != "" || doc.Value != "promoted" {
		t.Fatalf("decoded ignored or promoted fields: hidden=%q promoted=%q", doc.Hidden, doc.Value)
	}
}
