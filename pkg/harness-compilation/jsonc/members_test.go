package jsonc

import (
	"bytes"
	"reflect"
	"sync"
	"testing"
)

func TestMembersPreservesSourceOrderAndReadOnlyState(t *testing.T) {
	source := []byte("{\n // z\n \"z\":3, \"\\u0061\":1, \"m\":2\n}\n")
	doc, err := Parse("", source)
	if err != nil {
		t.Fatal(err)
	}
	read := func() ([]string, []string) {
		var keys, values []string
		for key, value := range doc.Root().Members() {
			keys = append(keys, key)
			values = append(values, string(value.Raw()))
		}
		return keys, values
	}
	for range 2 {
		keys, values := read()
		if !reflect.DeepEqual(keys, []string{"z", "a", "m"}) || !reflect.DeepEqual(values, []string{"3", "1", "2"}) {
			t.Fatalf("members=%v/%v", keys, values)
		}
	}
	var first []string
	for key := range doc.Root().Members() {
		first = append(first, key)
		break
	}
	if !reflect.DeepEqual(first, []string{"z"}) {
		t.Fatalf("early termination: %v", first)
	}
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for range 100 {
				keys, values := read()
				if !reflect.DeepEqual(keys, []string{"z", "a", "m"}) || !reflect.DeepEqual(values, []string{"3", "1", "2"}) {
					t.Errorf("concurrent read changed members: %v/%v", keys, values)
					return
				}
			}
		})
	}
	readers.Wait()
	if doc.Changed() || !bytes.Equal(doc.Bytes(), source) {
		t.Fatal("member reads changed concrete syntax")
	}
	var replaced []string
	for key, value := range doc.Root().Members() {
		if key == "z" && !doc.SetMember(doc.Root(), "a", []byte("4")) {
			t.Fatal("replacement during iteration failed")
		}
		replaced = append(replaced, string(value.Raw()))
	}
	if !reflect.DeepEqual(replaced, []string{"3", "4", "2"}) {
		t.Fatalf("iteration did not observe the replaced value: %v", replaced)
	}
	if !doc.DeleteMember(doc.Root(), "z") || !doc.SetMember(doc.Root(), "a", []byte("4")) || !doc.SetMember(doc.Root(), "b", []byte("5")) {
		t.Fatal("mutation control failed")
	}
	keys, values := read()
	if !reflect.DeepEqual(keys, []string{"a", "m", "b"}) || !reflect.DeepEqual(values, []string{"4", "2", "5"}) {
		t.Fatalf("reads after mutation returned stale members: %v/%v", keys, values)
	}
}

func TestMembersEmptyAndNonObjects(t *testing.T) {
	for _, source := range []string{`{}`, `[]`, `[{"nested":1}]`, `null`, `42`, `"text"`} {
		t.Run(source, func(t *testing.T) {
			doc, err := Parse("", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			for key := range doc.Root().Members() {
				t.Fatalf("non-object or empty object yielded %q", key)
			}
			if doc.Changed() || string(doc.Bytes()) != source {
				t.Fatal("reading empty/non-object members changed bytes")
			}
		})
	}
}

func TestMembersEnumerationDoesNotAllocate(t *testing.T) {
	doc, err := Parse("", []byte(`{"third":3,"first":1,"second":2}`))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	allocations := testing.AllocsPerRun(100, func() {
		count = 0
		for range doc.Root().Members() {
			count++
		}
	})
	if count != 3 || allocations != 0 {
		t.Fatalf("members=%d, allocations=%g; want three members with no lookup/key-copy allocations", count, allocations)
	}
}
