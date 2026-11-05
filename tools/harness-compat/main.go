package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type capability struct {
	cli    string
	npm    string
	reason string
}

var capabilities = map[string]capability{
	"claude":   {cli: "claude", npm: "@anthropic-ai/claude-code"},
	"factory":  {cli: "droid", npm: "droid"},
	"gemini":   {cli: "gemini", npm: "@google/gemini-cli"},
	"devin":    {cli: "devin"},
	"codex":    {cli: "codex", npm: "@openai/codex"},
	"opencode": {cli: "opencode", npm: "opencode-ai"},
	"cursor":   {cli: "agent"},
	"windsurf": {reason: "No verified credential-free Windsurf CLI inspection surface"},
}

type matrixRow struct {
	Target string `json:"target"`
	CLI    string `json:"cli"`
	Image  string `json:"image"`
	Mode   string `json:"mode"`
	Reason string `json:"reason"`
}

type matrix struct {
	Include []matrixRow `json:"include"`
}

func makeMatrix(definitions []target.Definition, repository string, native bool) (matrix, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(repository) {
		return matrix{}, fmt.Errorf("repository must be OWNER/REPO")
	}
	result := matrix{Include: []matrixRow{}}
	for _, definition := range definitions {
		name := string(definition.Target)
		capability, declared := capabilities[name]
		if !declared || capability.cli == "" && capability.reason == "" {
			return matrix{}, fmt.Errorf("target %q lacks an explicit CLI capability declaration", name)
		}
		if native && capability.cli == "" {
			continue
		}
		mode := "probe"
		if capability.cli == "" {
			mode = "unavailable"
		}
		result.Include = append(result.Include, matrixRow{Target: name, CLI: capability.cli, Image: "ghcr.io/" + strings.ToLower(repository) + "/harness-" + name + ":ci", Mode: mode, Reason: capability.reason})
	}
	return result, nil
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: harness-compat matrix|resolve|summary [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	switch args[0] {
	case "matrix":
		repository := flags.String("repository", "", "GitHub OWNER/REPO")
		native := flags.Bool("native", false, "Exclude declared unavailable CLIs")
		if err := flags.Parse(args[1:]); err != nil {
			return fmt.Errorf("invalid matrix flags")
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("matrix does not accept positional arguments")
		}
		result, err := makeMatrix(target.Definitions(), *repository, *native)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(result)
	case "resolve":
		name := flags.String("target", "", "Harness target")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
			return fmt.Errorf("invalid resolve flags")
		}
		client := metadataClient()
		result, err := resolveVersion(*name, func(endpoint string) ([]byte, error) { return fetchMetadata(client, endpoint) })
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(result)
	case "summary":
		report := flags.String("report", "", "Compatibility report JSON")
		image := flags.String("image", "", "Executed image reference")
		created := flags.String("created", "", "Image creation time in RFC3339")
		source := flags.String("source", "", "published, bootstrap, or config-only")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
			return fmt.Errorf("invalid summary flags")
		}
		return summarize(out, *report, *image, *created, *source, time.Now())
	default:
		return fmt.Errorf("unknown subcommand; use matrix, resolve, or summary")
	}
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "harness-compat:", err)
		os.Exit(1)
	}
}
