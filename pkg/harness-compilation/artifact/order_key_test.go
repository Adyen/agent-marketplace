package artifact

import "testing"

func TestMergeFragmentsOrdersByExplicitKeyAndRejectsDuplicates(t *testing.T) {
	artifacts := []Artifact{
		{Path: "config.toml", Bytes: []byte("second"), Fragment: true, FragmentOrderKey: "b"},
		{Path: "config.toml", Bytes: []byte("first"), Fragment: true, FragmentOrderKey: "a"},
	}
	got, err := MergeFragments(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[0].Bytes) != "first\nsecond" {
		t.Fatalf("merged bytes = %q", got[0].Bytes)
	}
	artifacts[1].FragmentOrderKey = "b"
	if _, err := MergeFragments(artifacts); err == nil {
		t.Fatal("duplicate FragmentOrderKey succeeded")
	}
	artifacts[1].FragmentOrderKey = ""
	if _, err := MergeFragments(artifacts); err == nil {
		t.Fatal("partially keyed fragment group succeeded")
	}
}
