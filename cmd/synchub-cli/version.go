package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/bruceblink/SyncHub/internal/version"
)

type versionSnapshot struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func runVersion(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "print version as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *jsonOutput {
		return writeVersionJSON(stdout)
	}
	printVersion(stdout)
	return nil
}

func writeVersionJSON(stdout io.Writer) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(versionSnapshot{
		Name:    version.Name,
		Version: version.Version,
	})
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "%s %s\n", version.Name, version.Version)
}
