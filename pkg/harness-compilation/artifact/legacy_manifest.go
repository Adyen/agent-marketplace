package artifact

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func legacyManifestBytes(m CanonicalManifest) ([]byte, error) {
	var b bytes.Buffer
	if err := appendLegacyJSON(&b, reflect.ValueOf(m)); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func appendLegacyJSON(b *bytes.Buffer, v reflect.Value) error {
	switch v.Kind() {
	case reflect.String:
		return appendJSONString(b, v.String(), true)
	case reflect.Int:
		b.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Slice:
		if v.IsNil() {
			b.WriteString("null")
			return nil
		}
		b.WriteByte('[')
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := appendLegacyJSON(b, v.Index(i)); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case reflect.Struct:
		b.WriteByte('{')
		first := true
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			name, option, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
			field := v.Field(i)
			if name == "" || name == "-" {
				return fmt.Errorf("legacy manifest: unsupported field %s", t.Field(i).Name)
			}
			if option == "omitempty" && (field.IsZero() || field.Kind() == reflect.Slice && field.Len() == 0) {
				continue
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			if err := appendJSONString(b, name, true); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := appendLegacyJSON(b, field); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("legacy manifest: unsupported value kind %s", v.Kind())
	}
	return nil
}
