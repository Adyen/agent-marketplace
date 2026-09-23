package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const readinessURL = "http://127.0.0.1:9090/readyz"

func main() {
	os.Exit(run(context.Background(), readinessURL, os.Stderr))
}

func run(parent context.Context, url string, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	if err := check(ctx, url); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func check(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create readiness request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request readiness endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness endpoint returned %s", resp.Status)
	}
	return nil
}
