package main

import (
	"fmt"
	"io"
)

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli login --server http://localhost:8080 --email user@example.com --password password")
	fmt.Fprintln(w, "  synchub-cli workspace init --path . --remote-path /workspace")
	fmt.Fprintln(w, "  synchub-cli manifest scan --path .")
	fmt.Fprintln(w, "  synchub-cli sync status --path .")
	fmt.Fprintln(w, "  synchub-cli sync push --path .")
	fmt.Fprintln(w, "  synchub-cli sync pull --path .")
	fmt.Fprintln(w, "  synchub-cli sync watch --path .")
	fmt.Fprintln(w, "  synchub-cli sync conflicts --path .")
}

func printWorkspaceUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli workspace init --path . --remote-path /workspace")
}

func printManifestUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli manifest scan --path .")
}

func printSyncUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli sync status --path .")
	fmt.Fprintln(w, "  synchub-cli sync push --path .")
	fmt.Fprintln(w, "  synchub-cli sync pull --path .")
	fmt.Fprintln(w, "  synchub-cli sync watch --path .")
	fmt.Fprintln(w, "  synchub-cli sync conflicts --path .")
}
