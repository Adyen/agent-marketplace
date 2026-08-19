package analyze

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// extractResourceSamples walks the exported fields and Extra maps of a resource
// and returns every non-empty string value as a text sample.
func extractResourceSamples(r ir.Resource) []textSample {
	kind := r.Kind().String()
	name := r.Name()
	base := fmt.Sprintf("%s/%s", kind, name)

	out := []textSample{{path: base + "/name", content: name}}
	walkValue(reflect.ValueOf(r), base, &out)
	return out
}

// valueAsString coerces string-like reflect values into a string. It handles
// plain strings, []byte, named []byte aliases such as json.RawMessage, and
// interface or pointer values wrapping those types.
func valueAsString(v reflect.Value) (string, bool) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "", false
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.String:
		return v.String(), true
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return string(asBytes(v)), true
		}
	}
	return "", false
}

// asBytes returns the bytes underlying a []byte or fixed-size byte array.
func asBytes(v reflect.Value) []byte {
	switch v.Kind() {
	case reflect.Slice:
		return v.Bytes()
	case reflect.Array:
		n := v.Len()
		b := make([]byte, n)
		for i := 0; i < n; i++ {
			b[i] = byte(v.Index(i).Uint())
		}
		return b
	}
	return nil
}

// isByteSlice reports whether t is a slice or array of bytes.
func isByteSlice(t reflect.Type) bool {
	k := t.Kind()
	return (k == reflect.Slice || k == reflect.Array) && t.Elem().Kind() == reflect.Uint8
}

// walkValue recursively traverses v and appends string/[]string/map values.
func walkValue(v reflect.Value, path string, out *[]textSample) {
	if !v.IsValid() {
		return
	}

	// Deref pointers and interfaces to reach concrete values.
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.String:
		s := v.String()
		if s != "" {
			*out = append(*out, textSample{path: path, content: s})
		}
	case reflect.Slice, reflect.Array:
		if isByteSlice(v.Type()) {
			if s, ok := valueAsString(v); ok && s != "" {
				*out = append(*out, textSample{path: path, content: s})
			}
			return
		}
		for i := 0; i < v.Len(); i++ {
			walkValue(v.Index(i), fmt.Sprintf("%s/%d", path, i), out)
		}
	case reflect.Map:
		rawKeys := v.MapKeys()
		type keyStr struct {
			rv reflect.Value
			s  string
		}
		keys := make([]keyStr, 0, len(rawKeys))
		for _, k := range rawKeys {
			if s, ok := valueAsString(k); ok {
				keys = append(keys, keyStr{rv: k, s: s})
			}
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].s < keys[j].s })
		for _, key := range keys {
			walkValue(v.MapIndex(key.rv), fmt.Sprintf("%s/%s", path, key.s), out)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" { // unexported
				continue
			}
			walkValue(v.Field(i), fmt.Sprintf("%s/%s", path, field.Name), out)
		}
	}
}
