package config

import "testing"

func BenchmarkParseV4(b *testing.B) {
	data := []byte(`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include:
      - tools/skill/review: {as: review}
      - tools/skill/deploy: {as: deploy}
      - tools/skill/audit: {as: audit}
`)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseV4(data); err != nil {
			b.Fatal(err)
		}
	}
}
