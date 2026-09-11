package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if len(os.Args) != 3 {
		os.Exit(2)
	}
	result, err := hcc.Convert(os.DirFS(os.Args[1]), hcc.Harness("factory"), hcc.Harness(os.Args[2]))
	if err != nil {
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(1)
	}
}
