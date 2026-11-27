package pipeline

import (
	"bytes"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

func TestParsePlaceHostVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		output  string
		pattern string
		want    string
		ok      bool
	}{
		{"one capture", "fixture version 1.2.3\n", `(\d+\.\d+\.\d+)`, "1.2.3", true},
		{"whole match", "1.2.3", `\d+\.\d+\.\d+`, "1.2.3", true},
		{"multiple matches", "1.2.3 then 1.2.4", `(\d+\.\d+\.\d+)`, "", false},
		{"multiple captures", "1.2.3", `(\d+)\.(\d+)\.(\d+)`, "", false},
		{"missing capture", "version unknown", `(\d+\.\d+\.\d+)`, "", false},
		{"empty capture", "", `(.*)`, "", false},
		{"suffix is not truncated", "1.2.3-beta", `(.+)`, "", false},
		{"leading zero is not canonical", "01.2.3", `(\d+\.\d+\.\d+)`, "", false},
		{"component too long", "1234567890.2.3", `(\d+\.\d+\.\d+)`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parsePlaceHostVersion([]byte(tc.output), tc.pattern)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("parsePlaceHostVersion() = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestBoundedPlaceProbeOutput(t *testing.T) {
	writer := &boundedPlaceProbeOutput{}
	input := bytes.Repeat([]byte("x"), placeVersionProbeOutputLimit+37)
	n, err := writer.Write(input)
	if err != nil || n != len(input) || !writer.overflow || len(writer.data) != placeVersionProbeOutputLimit {
		t.Fatalf("bounded probe write = n:%d len:%d overflow:%v data:%d err:%v",
			n, len(input), writer.overflow, len(writer.data), err)
	}
}

func TestClassifyPlaceHostVersion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ranges []string
		want   string
	}{
		{"conjunction", []string{">=1.0.0 <2.0.0"}, "verified"},
		{"all supported comparators", []string{">1.0.0 <=1.2.3 =1.2.3"}, "verified"},
		{"exact", []string{"1.2.3"}, "verified"},
		{"any valid entry", []string{"<1.0.0", ">=1.2.0 <1.3.0"}, "verified"},
		{"outside", []string{">=2.0.0"}, "outside-verified-range"},
		{"no entries", nil, "outside-verified-range"},
		{"unsupported range", []string{">=1.0.0 || <2.0.0"}, "unknown"},
		{"malformed token after nonmatch", []string{">=2.0.0 garbage"}, "unknown"},
		{"malformed token after match", []string{">=1.0.0 garbage"}, "unknown"},
		{"malformed and no match", []string{"<1.0.0", ">= 2.0.0"}, "unknown"},
		{"match wins over malformed", []string{"<1.0.0", ">=1.0.0 <2.0.0", ">= 2.0.0"}, "verified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verified := make([]targetprofile.VerifiedRange, len(tc.ranges))
			for i, versionRange := range tc.ranges {
				verified[i].Range = versionRange
			}
			if got := classifyPlaceHostVersion("1.2.3", verified); got != tc.want {
				t.Fatalf("classifyPlaceHostVersion() = %q; want %q", got, tc.want)
			}
		})
	}
}
