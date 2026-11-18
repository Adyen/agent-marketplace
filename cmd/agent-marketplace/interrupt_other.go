//go:build !unix

package main

import "context"

func commandInterruptContext() (context.Context, func()) {
	return context.Background(), func() {}
}
