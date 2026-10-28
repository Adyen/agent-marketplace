//go:build !(cgo && (darwin || linux))

package main

import "errors"

type nativeState struct{}

func (s *nativeState) approveClose() {}

func run() error {
	return errors.New("the desktop host requires cgo on macOS or Linux")
}
