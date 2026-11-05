package logvalue

import (
	"errors"
	"testing"
)

func TestSafe(t *testing.T) {
	if Safe(nil) != nil {
		t.Fatal("nil log value changed")
	}
	for _, tc := range []struct {
		value any
		want  string
	}{
		{"normal value", "normal value"},
		{"first\r\nforged\rrecord\n", `first\r\nforged\rrecord\n`},
		{errors.New("error\nforged"), `error\nforged`},
	} {
		if got := Safe(tc.value); got != tc.want {
			t.Fatalf("Safe(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}
