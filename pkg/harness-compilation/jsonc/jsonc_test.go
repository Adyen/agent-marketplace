package jsonc

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
)

// fakeSecret is a recognizable non-credential stand-in used to prove that no
// diagnostic echoes a value.
const fakeSecret = "NOT-A-REAL-SECRET-c0ffee"

func TestParseRoundTripsBytesExactly(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"object with line comments", "{\n  // leading\n  \"a\": 1, // trailing\n  \"b\": [1, 2]\n}\n"},
		{"object with block comment", "/* header */\n{\n  /* about a */ \"a\": 1\n}\n"},
		{"empty object with comment", "{\n  // nothing here\n}\n"},
		{"empty array", "[]"},
		{"nested", "{\"a\":{\"b\":[{\"c\":null}]}}"},
		{"scalars", "{\"s\":\"x\\\"y\",\"n\":-1.5e3,\"t\":true,\"f\":false,\"z\":null}"},
		{"tabs and crlf", "{\r\n\t\"a\"\t:\t1\r\n}\r\n"},
		{"unicode escape key", "{\"a\\u0062\":1}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse("f.json", []byte(tt.in))
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.in, err)
			}
			if got := string(doc.Bytes()); got != tt.in {
				t.Fatalf("Bytes() = %q, want byte-identical %q", got, tt.in)
			}
			if doc.Changed() {
				t.Fatal("Changed() = true without a patch")
			}
		})
	}
}

func TestParseRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"truncated object", `{"a":`},
		{"trailing comma object", `{"a":1,}`},
		{"trailing comma array", `[1,]`},
		{"trailing data", `{"a":1} {"b":2}`},
		{"garbage after document", `{"a":1} nope`},
		{"unterminated string", `{"a":"x`},
		{"non-string key", `{1:2}`},
		{"missing colon", `{"a" 1}`},
		{"bad literal", `{"a":tru}`},
		{"unterminated block comment", "{\"a\":1 /* oops"},
		{"deep nesting", strings.Repeat(`{"a":`, maxDepth+5) + "1" + strings.Repeat("}", maxDepth+5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("f.json", []byte(tt.in))
			if err == nil {
				t.Fatalf("Parse(%q) accepted malformed input", tt.in)
			}
			coded, ok := diag.AsCodedError(err)
			if !ok {
				t.Fatalf("error %T is not a *diag.CodedError", err)
			}
			if coded.Code != diag.CodeJSONMalformed {
				t.Fatalf("code = %q, want %q", coded.Code, diag.CodeJSONMalformed)
			}
			if coded.Severity != diag.SeverityError {
				t.Fatalf("severity = %v, want error", coded.Severity)
			}
			if coded.Source.File != "f.json" {
				t.Fatalf("source file = %q, want f.json", coded.Source.File)
			}
		})
	}
}

func TestParseAbsorbsDuplicateKeyDetection(t *testing.T) {
	data := []byte("{\n  \"mcpServers\": {\n    \"docs\": {\n      \"headers\": {\n        \"Authorization\": \"" + fakeSecret + "\",\n        \"Authorization\": \"" + fakeSecret + "\"\n      }\n    }\n  }\n}\n")
	_, err := Parse("mcp_config.json", data)
	if !errors.Is(err, jsonutil.ErrDuplicateKey) {
		t.Fatalf("Parse error = %v, want duplicate-key error", err)
	}
	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("error %T is not a *diag.CodedError", err)
	}
	if coded.Code != diag.CodeJSONDuplicateKey {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeJSONDuplicateKey)
	}
	if coded.Severity != diag.SeverityError {
		t.Fatalf("severity = %v, want error", coded.Severity)
	}
	if coded.Source.File != "mcp_config.json" {
		t.Fatalf("source file = %q, want mcp_config.json", coded.Source.File)
	}
	if coded.Source.Line != 6 {
		t.Fatalf("source line = %d, want 6", coded.Source.Line)
	}
	for _, want := range []string{"mcp_config.json", "$.mcpServers.docs.headers", `"Authorization"`, diag.CodeJSONDuplicateKey} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), fakeSecret) {
		t.Fatalf("error %q leaks the duplicated value", err.Error())
	}
}

// A commented-out duplicate is NOT a duplicate: the tokenizer ignores comments,
// which is the single-pass equivalent of the strip-then-scan contract pinned by
// bundle's TestLoadMcpServersStripsCommentsBeforeDuplicateKeyScan.
func TestCanonicalizeIgnoresCommentedOutDuplicates(t *testing.T) {
	data := []byte("{\n  \"mcpServers\": {\n    \"docs\": {\n      // \"command\": \"ignored\",\n      \"command\": \"real\"\n    }\n  }\n}")
	canonical, err := Canonicalize("mcp_config.json", data)
	if err != nil {
		t.Fatalf("Canonicalize error: %v", err)
	}
	want := `{"mcpServers":{"docs":{"command":"real"}}}`
	if string(canonical) != want {
		t.Fatalf("Canonicalize = %q, want %q", canonical, want)
	}
	// The canonical bytes must be decodable and must not contain comments.
	var probe map[string]any
	if err := json.Unmarshal(canonical, &probe); err != nil {
		t.Fatalf("canonical bytes are not valid JSON: %v", err)
	}
}

func TestCanonicalizeRejectsRealDuplicateBehindComment(t *testing.T) {
	data := []byte("{\n  // the duplicate below survives\n  \"a\": {\"command\": \"safe\", \"command\": \"rm -rf /\"}\n}")
	_, err := Canonicalize("f.json", data)
	if !errors.Is(err, jsonutil.ErrDuplicateKey) {
		t.Fatalf("Canonicalize error = %v, want duplicate-key error", err)
	}
	if strings.Contains(err.Error(), "rm -rf /") {
		t.Fatalf("error %q leaks the duplicated value", err.Error())
	}
}

func TestUnmarshalDecodesCommentedDocument(t *testing.T) {
	var doc struct {
		Name string `json:"name"`
	}
	if err := Unmarshal("f.json", []byte("{\n// c\n\"name\":\"demo\"}"), &doc); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if doc.Name != "demo" {
		t.Fatalf("name = %q, want demo", doc.Name)
	}
}

func TestUnmarshalReportsTypeMismatchAsMalformed(t *testing.T) {
	var doc struct {
		Name string `json:"name"`
	}
	err := Unmarshal("f.json", []byte(`{"name":42}`), &doc)
	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("error %T is not a *diag.CodedError", err)
	}
	if coded.Code != diag.CodeJSONMalformed {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeJSONMalformed)
	}
}

func TestDocumentDecodeRejectsCaseCollidingAliases(t *testing.T) {
	doc, err := Parse("f.json", []byte(`{"name":"safe","Name":"name-secret"}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var decoded struct {
		Name string `json:"name"`
	}
	err = doc.Decode(&decoded)
	coded, ok := diag.AsCodedError(err)
	if !ok || coded.Code != diag.CodeJSONDuplicateKey {
		t.Fatalf("error = %v, want coded duplicate-key error", err)
	}
	if strings.Contains(err.Error(), "name-secret") {
		t.Fatalf("error %q leaked payload value", err)
	}
}

func TestSetMemberPreservesCommentsAndKeyOrder(t *testing.T) {
	in := "{\n  // handwritten first\n  \"alpha\": {\"keep\": true},\n  // about beta\n  \"beta\": 2\n}\n"
	doc, err := Parse("f.json", []byte(in))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !doc.SetMember(doc.Root(), "gamma", []byte(`{"generated":1}`)) {
		t.Fatal("SetMember on the root object returned false")
	}
	want := "{\n  // handwritten first\n  \"alpha\": {\"keep\": true},\n  // about beta\n  \"beta\": 2,\n  \"gamma\": {\"generated\":1}\n}\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("Bytes() =\n%q\nwant\n%q", got, want)
	}
	if !doc.Changed() {
		t.Fatal("Changed() = false after appending a member")
	}
	if got := doc.Root().Keys(); strings.Join(got, ",") != "alpha,beta,gamma" {
		t.Fatalf("Keys() = %v, want alpha,beta,gamma", got)
	}
}

func TestSetMemberReplacesInPlaceAndIsIdempotent(t *testing.T) {
	in := "{\n  // about a\n  \"a\": {\"old\": 1},\n  \"b\": 2\n}\n"
	doc, err := Parse("f.json", []byte(in))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	doc.SetMember(doc.Root(), "a", []byte(`{"new":1}`))
	want := "{\n  // about a\n  \"a\": {\"new\":1},\n  \"b\": 2\n}\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}

	again, err := Parse("f.json", []byte(want))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	again.SetMember(again.Root(), "a", []byte(`{"new":1}`))
	if again.Changed() {
		t.Fatal("Changed() = true for a byte-identical set; a no-op must not trigger a write")
	}
	if got := string(again.Bytes()); got != want {
		t.Fatalf("idempotent Bytes() = %q, want %q", got, want)
	}
}

func TestDeleteMemberKeepsNeighbourComments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		key  string
		want string
	}{
		{
			name: "middle member",
			in:   "{\n  // about a\n  \"a\": 1,\n  // about b\n  \"b\": 2,\n  // about c\n  \"c\": 3\n}\n",
			key:  "b",
			want: "{\n  // about a\n  \"a\": 1,\n  // about c\n  \"c\": 3\n}\n",
		},
		{
			name: "first member",
			in:   "{\n  // about a\n  \"a\": 1,\n  // about b\n  \"b\": 2\n}\n",
			key:  "a",
			want: "{\n  // about b\n  \"b\": 2\n}\n",
		},
		{
			name: "last member",
			in:   "{\n  // about a\n  \"a\": 1,\n  // about b\n  \"b\": 2\n}\n",
			key:  "b",
			want: "{\n  // about a\n  \"a\": 1\n}\n",
		},
		{
			name: "only member",
			in:   "{\n  \"a\": 1\n}\n",
			key:  "a",
			want: "{}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse("f.json", []byte(tt.in))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if !doc.DeleteMember(doc.Root(), tt.key) {
				t.Fatalf("DeleteMember(%q) = false", tt.key)
			}
			if got := string(doc.Bytes()); got != tt.want {
				t.Fatalf("Bytes() = %q, want %q", got, tt.want)
			}
			// The result must still parse.
			if _, err := Parse("f.json", doc.Bytes()); err != nil {
				t.Fatalf("patched document no longer parses: %v", err)
			}
		})
	}
}

func TestDeleteMemberMissingKeyIsNoOp(t *testing.T) {
	doc, err := Parse("f.json", []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if doc.DeleteMember(doc.Root(), "zzz") {
		t.Fatal("DeleteMember of an absent key returned true")
	}
	if doc.Changed() {
		t.Fatal("Changed() = true after a no-op delete")
	}
}

// Handwritten hook order is load-bearing: the plan requires that "Handwritten
// hooks keep their relative order and execute before generated hooks".
func TestSetElementsKeepsHandwrittenOrderAndCommentsBeforeGenerated(t *testing.T) {
	in := "{\n  \"PreToolUse\": [\n    // handwritten one\n    {\"h\": 1},\n    // handwritten two\n    {\"h\": 2}\n  ]\n}\n"
	doc, err := Parse("hooks.v1.json", []byte(in))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	arr, ok := doc.Root().Member("PreToolUse")
	if !ok || arr.Kind() != KindArray {
		t.Fatalf("PreToolUse is not an array: %v", ok)
	}
	if arr.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", arr.Len())
	}
	first, _ := arr.Element(0)
	second, _ := arr.Element(1)
	raws := [][]byte{first.Raw(), second.Raw(), []byte(`{"generated":true}`)}
	if !doc.SetElements(arr, raws) {
		t.Fatal("SetElements returned false")
	}
	want := "{\n  \"PreToolUse\": [\n    // handwritten one\n    {\"h\": 1},\n    // handwritten two\n    {\"h\": 2},\n    {\"generated\":true}\n  ]\n}\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("Bytes() =\n%q\nwant\n%q", got, want)
	}
}

// TestSetElementsCarriesCommentsWithMovedElements pins the trivia rule bead
// agent-marketplace-5exj.28 leans on: a comment belongs to the element that
// FOLLOWS it, so an element that survives at a different index takes its own
// comment with it instead of inheriting the trivia of the slot it lands on.
//
// Before .28 the lead trivia was purely positional, so deleting the element in
// front of a commented element silently deleted the comment — the user's
// comment, on the user's hook, removed because a generated sibling above it was
// superseded.
func TestSetElementsCarriesCommentsWithMovedElements(t *testing.T) {
	tests := []struct {
		name string
		in   string
		keep []int // indices of the original elements to keep, in order
		want string
	}{
		{
			name: "deleting the first element keeps the second element's comment",
			in:   "{\n  \"e\": [\n    {\"gen\": 1},\n    // mine\n    {\"mine\": 2}\n  ]\n}\n",
			keep: []int{1},
			want: "{\n  \"e\": [\n    // mine\n    {\"mine\": 2}\n  ]\n}\n",
		},
		{
			name: "deleting a middle element keeps both surviving comments",
			in:   "{\n  \"e\": [\n    // first\n    {\"a\": 1},\n    {\"gen\": 2},\n    // third\n    {\"c\": 3}\n  ]\n}\n",
			keep: []int{0, 2},
			want: "{\n  \"e\": [\n    // first\n    {\"a\": 1},\n    // third\n    {\"c\": 3}\n  ]\n}\n",
		},
		{
			name: "an unchanged list is left alone",
			in:   "{\n  \"e\": [\n    // first\n    {\"a\": 1},\n    {\"b\": 2}\n  ]\n}\n",
			keep: []int{0, 1},
			want: "{\n  \"e\": [\n    // first\n    {\"a\": 1},\n    {\"b\": 2}\n  ]\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse("f.json", []byte(tt.in))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			arr, ok := doc.Root().Member("e")
			if !ok || arr.Kind() != KindArray {
				t.Fatal("e is not an array")
			}
			raws := make([][]byte, 0, len(tt.keep))
			for _, i := range tt.keep {
				el, found := arr.Element(i)
				if !found {
					t.Fatalf("element %d is missing", i)
				}
				raws = append(raws, el.Raw())
			}
			if !doc.SetElements(arr, raws) {
				t.Fatal("SetElements returned false")
			}
			if got := string(doc.Bytes()); got != tt.want {
				t.Fatalf("Bytes() =\n%q\nwant\n%q", got, tt.want)
			}
			if _, err := Parse("f.json", doc.Bytes()); err != nil {
				t.Fatalf("patched document no longer parses: %v", err)
			}
		})
	}
}

func TestSetElementsIdempotentAndShrink(t *testing.T) {
	in := "{\n  \"e\": [\n    {\"h\": 1},\n    {\"h\": 2}\n  ]\n}\n"
	doc, err := Parse("f.json", []byte(in))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	arr, _ := doc.Root().Member("e")
	first, _ := arr.Element(0)
	if !doc.SetElements(arr, [][]byte{first.Raw()}) {
		t.Fatal("SetElements returned false")
	}
	want := "{\n  \"e\": [\n    {\"h\": 1}\n  ]\n}\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}

	stable, err := Parse("f.json", []byte(want))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	arr2, _ := stable.Root().Member("e")
	only, _ := arr2.Element(0)
	stable.SetElements(arr2, [][]byte{only.Raw()})
	if stable.Changed() {
		t.Fatal("Changed() = true for an identical element list")
	}
}

func TestSetElementsEmptyArray(t *testing.T) {
	doc, err := Parse("f.json", []byte("{\n  \"e\": [\n    {\"h\": 1}\n  ]\n}\n"))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	arr, _ := doc.Root().Member("e")
	if !doc.SetElements(arr, nil) {
		t.Fatal("SetElements returned false")
	}
	if got := string(doc.Bytes()); got != "{\n  \"e\": [\n  ]\n}\n" {
		t.Fatalf("Bytes() = %q", got)
	}
	if _, err := Parse("f.json", doc.Bytes()); err != nil {
		t.Fatalf("patched document no longer parses: %v", err)
	}
}

func TestPatchOnNonContainerIsRefused(t *testing.T) {
	doc, err := Parse("f.json", []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	scalar, _ := doc.Root().Member("a")
	if doc.SetMember(scalar, "x", []byte(`1`)) {
		t.Fatal("SetMember on a scalar returned true")
	}
	if doc.DeleteMember(scalar, "x") {
		t.Fatal("DeleteMember on a scalar returned true")
	}
	if doc.SetElements(scalar, nil) {
		t.Fatal("SetElements on a scalar returned true")
	}
	if doc.SetMember(nil, "x", []byte(`1`)) {
		t.Fatal("SetMember on nil returned true")
	}
	if scalar.Keys() != nil {
		t.Fatal("Keys() on a scalar is non-nil")
	}
	if _, ok := scalar.Member("x"); ok {
		t.Fatal("Member() on a scalar returned ok")
	}
	if scalar.Len() != 0 {
		t.Fatal("Len() on a scalar is non-zero")
	}
	if _, ok := scalar.Element(0); ok {
		t.Fatal("Element() on a scalar returned ok")
	}
	if doc.Changed() {
		t.Fatal("Changed() = true after refused patches")
	}
}

// TestDecodeUsesTheParsedDocument proves Decode is a real second entry point on
// an already-parsed document — no reparse, comments already gone, duplicate
// keys already rejected by Parse — so a caller that both decides on values and
// patches concrete syntax pays for exactly one parse.
func TestDecodeUsesTheParsedDocument(t *testing.T) {
	doc, err := Parse("f.json", []byte("{\n  // note\n  \"b\": 2,\n  \"a\": {\"x\": [1, 2]}\n}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var got map[string]any
	if err := doc.Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got) != 2 || got["b"] != float64(2) {
		t.Fatalf("decoded = %v, want the document's members", got)
	}
	inner, ok := got["a"].(map[string]any)
	if !ok || len(inner["x"].([]any)) != 2 {
		t.Fatalf("decoded nested value = %v", got["a"])
	}
	// Decoding must not disturb the document.
	if doc.Changed() {
		t.Fatal("Decode marked the document as changed")
	}
	if string(doc.Bytes()) != "{\n  // note\n  \"b\": 2,\n  \"a\": {\"x\": [1, 2]}\n}\n" {
		t.Fatalf("Decode mutated the document: %q", doc.Bytes())
	}
}

// TestNodeCanonicalDropsCommentsAndKeepsOrder pins the difference between Raw
// (concrete syntax, may contain comments) and Canonical (strict JSON for
// encoding/json), which is why callers must not hand Raw to a decoder.
func TestNodeCanonicalDropsCommentsAndKeepsOrder(t *testing.T) {
	doc, err := Parse("f.json", []byte("{\n  \"o\": {\n    // c\n    \"z\": 1,\n    \"a\": 2\n  }\n}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	node, ok := doc.Root().Member("o")
	if !ok {
		t.Fatal("member o not found")
	}
	if got := string(node.Canonical()); got != `{"z":1,"a":2}` {
		t.Fatalf("Canonical = %s, want source-ordered comment-free JSON", got)
	}
	if !strings.Contains(string(node.Raw()), "// c") {
		t.Fatalf("Raw = %s, want the concrete syntax including the comment", node.Raw())
	}
}

// TestAppendIndentFollowsTheDocumentShape proves an inserted value can be
// indented to match the file it is inserted into rather than a hard-coded
// two spaces.
func TestAppendIndentFollowsTheDocumentShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		key  string
		want string
	}{
		{name: "two spaces", data: "{\n  \"a\": {\n    \"b\": 1\n  }\n}", key: "a", want: "    "},
		{name: "four spaces", data: "{\n    \"a\": {\n        \"b\": 1\n    }\n}", key: "a", want: "        "},
		{name: "tabs", data: "{\n\t\"a\": {\n\t\t\"b\": 1\n\t}\n}", key: "a", want: "\t\t"},
		{name: "empty container", data: "{\n  \"a\": {}\n}", key: "a", want: "  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Parse("f.json", []byte(tc.data))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			node, ok := doc.Root().Member(tc.key)
			if !ok {
				t.Fatalf("member %s not found", tc.key)
			}
			if got := node.AppendIndent(); got != tc.want {
				t.Fatalf("AppendIndent = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReplaceRootKeepsSurroundingTrivia proves the whole-document replacement
// used for a destination with nothing to preserve still keeps a file-header
// comment and the trailing newline, and is a no-op when the bytes match.
func TestReplaceRootKeepsSurroundingTrivia(t *testing.T) {
	doc, err := Parse("f.json", []byte("// generated by hand\n{}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	doc.ReplaceRoot([]byte("{\n  \"a\": 1\n}"))
	want := "// generated by hand\n{\n  \"a\": 1\n}\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("Bytes = %q, want %q", got, want)
	}
	if !doc.Changed() {
		t.Fatal("ReplaceRoot did not mark the document as changed")
	}

	same, err := Parse("f.json", []byte("{\n  \"a\": 1\n}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	same.ReplaceRoot([]byte("{\n  \"a\": 1\n}"))
	if same.Changed() {
		t.Fatal("ReplaceRoot with identical bytes marked the document as changed")
	}
}
