package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkConvertSourceDisk(b *testing.B) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(old) })
	root := b.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(`{"name":"conversion-control","version":"1.0.0"}`), 0o640); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("probe-%02d", i)
		dir := filepath.Join(root, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: conversion control\n---\n\ninert body\n"), 0o640); err != nil {
			b.Fatal(err)
		}
	}
	output := filepath.Join(b.TempDir(), "out")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := runConvert(io.Discard, root, "claude", "factory", output, false, false, true); err != nil {
			b.Fatal(err)
		}
	}
}
