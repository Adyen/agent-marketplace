package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Adyen/agent-marketplace/internal/tui"
)

func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(tui.QAInventory()); err != nil {
		fmt.Fprintf(os.Stderr, "export TUI QA code inventory: %v\n", err)
		os.Exit(1)
	}
}
