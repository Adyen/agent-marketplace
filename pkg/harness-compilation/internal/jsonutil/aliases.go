package jsonutil

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

func Decode(file string, data []byte, v any) error {
	if err := ValidateFieldAliases(file, data, v); err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return Coded(file, data, err)
	}
	return nil
}

func ValidateFieldAliases(file string, data []byte, v any) error {
	if !hasFieldSchema(reflect.TypeOf(v)) {
		return nil
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return Coded(file, data, err)
	}
	return ValidateFieldAliasesValue(file, raw, v)
}

func ValidateFieldAliasesValue(file string, raw any, v any) error {
	return Coded(file, nil, checkFieldAliases(raw, reflect.TypeOf(v), "$"))
}

func hasFieldSchema(t reflect.Type) bool {
	if t == nil || t == reflect.TypeFor[json.RawMessage]() {
		return false
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return hasFieldSchema(t.Elem())
	case reflect.Struct:
		return true
	}
	return false
}

func checkFieldAliases(raw any, typ reflect.Type, path string) error {
	if !hasFieldSchema(typ) || raw == nil {
		return nil
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		object, ok := raw.(map[string]any)
		if !ok {
			return nil
		}
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if tag == "-" {
				continue
			}
			if f.Anonymous && tag == "" {
				if hasFieldSchema(f.Type) {
					if err := checkFieldAliases(raw, f.Type, path); err != nil {
						return err
					}
				}
				continue
			}
			name := tag
			if name == "" {
				name = f.Name
			}
			matched := ""
			found := false
			for _, key := range keys {
				if strings.EqualFold(key, name) {
					if found {
						return &DuplicateKeyError{Key: key, Path: path}
					}
					matched = key
					found = true
				}
			}
			if found && hasFieldSchema(f.Type) {
				if err := checkFieldAliases(object[matched], f.Type, path+"."+matched); err != nil {
					return err
				}
			}
		}
	case reflect.Map:
		object, ok := raw.(map[string]any)
		if !ok {
			return nil
		}
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := checkFieldAliases(object[key], typ.Elem(), path+"."+key); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		values, ok := raw.([]any)
		if !ok {
			return nil
		}
		for i, value := range values {
			if err := checkFieldAliases(value, typ.Elem(), path+"["+strconv.Itoa(i)+"]"); err != nil {
				return err
			}
		}
	}
	return nil
}
