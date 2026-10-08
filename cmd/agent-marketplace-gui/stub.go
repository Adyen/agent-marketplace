//go:build !(cgo && (darwin || linux))

package main

import "errors"

func run() error {
	return errors.New("the desktop host requires cgo on macOS or Linux")
}
