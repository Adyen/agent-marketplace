package mapping

import "testing"

func TestWindsurfShellEnvName(t *testing.T) {
	for _, tc := range []struct {
		value, name string
		ok          bool
	}{
		{"$TOKEN", "TOKEN", true}, {"$_", "_", true}, {"$A_01", "A_01", true}, {"$0", "0", true},
		{"", "", false}, {"$", "", false}, {"${TOKEN}", "", false}, {"$lower", "", false}, {"$$TOKEN", "", false},
		{"prefix $TOKEN", "", false}, {"$TOKEN/path", "", false}, {"$TOKEN\n", "", false}, {"$É", "", false},
	} {
		name, ok := WindsurfShellEnvName(tc.value)
		if name != tc.name || ok != tc.ok {
			t.Errorf("WindsurfShellEnvName(%q) = %q,%t; want %q,%t", tc.value, name, ok, tc.name, tc.ok)
		}
	}
}
