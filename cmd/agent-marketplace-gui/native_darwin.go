//go:build cgo && darwin

package main

/*
#cgo darwin LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"
