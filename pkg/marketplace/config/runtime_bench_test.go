package config

import (
	"fmt"
	"testing"
)

func BenchmarkConfigClassification200Registries(b *testing.B) {
	startup, err := ParseConfig([]byte(validConfigYAML()))
	if err != nil {
		b.Fatal(err)
	}
	template := startup.Registries[0]
	startup.Registries = make([]Registry, 200)
	for i := range startup.Registries {
		registry := template
		registry.ID = fmt.Sprintf("registry-%03d", i)
		registry.Slug = registry.ID
		registry.Origin = fmt.Sprintf("https://registry-%03d.example.test", i)
		startup.Registries[i] = registry
	}
	candidate := startup
	candidate.Registries = append([]Registry(nil), startup.Registries...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Classify(startup, startup, candidate); err != nil {
			b.Fatal(err)
		}
	}
}
