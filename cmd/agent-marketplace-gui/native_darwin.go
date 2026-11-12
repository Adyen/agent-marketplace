//go:build cgo && darwin

package main

/*
#cgo darwin LDFLAGS: -framework UniformTypeIdentifiers
#cgo darwin LDFLAGS: -Wl,-sectcreate,__TEXT,__info_plist,${SRCDIR}/Info.plist
*/
import "C"
