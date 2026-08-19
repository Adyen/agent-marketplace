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
		for i := 0; i < v.Len(); i++ {
			walkValue(v.Index(i), fmt.Sprintf("%s/%d", path, i), out)
		}
	case reflect.Map:
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		for _, key := range keys {
			if key.Kind() != reflect.String {
				continue
			}
			walkValue(v.MapIndex(key), fmt.Sprintf("%s/%s", path, key.String()), out)
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
