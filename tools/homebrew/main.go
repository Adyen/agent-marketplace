package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "homebrew:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homebrew <generate|publish> [flags]")
	}
	switch args[0] {
	case "generate":
		fs := flag.NewFlagSet("generate", flag.ContinueOnError)
		repository := fs.String("repository", "", "OWNER/REPO")
		tag := fs.String("tag", "", "vX.Y.Z release tag")
		releaseJSON := fs.String("release-json", "", "uploaded release metadata JSON path")
		dist := fs.String("dist", "", "directory containing the four release binaries")
		output := fs.String("output", "", "output directory for Formula/ and VERSION")
		visibility := fs.String("visibility", "", "public, private, or internal")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return generate(*repository, *tag, *releaseJSON, *dist, *output, *visibility)
	case "publish":
		fs := flag.NewFlagSet("publish", flag.ContinueOnError)
		repository := fs.String("repository", "", "OWNER/REPO")
		input := fs.String("input", "", "generated output directory containing Formula/ and VERSION")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return publish(*repository, *input)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
