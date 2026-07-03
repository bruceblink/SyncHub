package main

import (
	"fmt"
	"io"
)

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli register --server http://localhost:8765 --email user@example.com --password password")
	fmt.Fprintln(w, "  synchub-cli login --server http://localhost:8765 --email user@example.com --password password")
	fmt.Fprintln(w, "  synchub-cli logout")
	fmt.Fprintln(w, "  synchub-cli workspace init --path . --remote-path /workspace")
	fmt.Fprintln(w, "  synchub-cli manifest scan --path .")
	fmt.Fprintln(w, "  synchub-cli file versions --path . --remote-path /workspace/readme.txt")
	fmt.Fprintln(w, "  synchub-cli file restore --path . --remote-path /workspace/readme.txt --version 1")
	fmt.Fprintln(w, "  synchub-cli file pin --path . --remote-path /workspace/readme.txt --version 1")
	fmt.Fprintln(w, "  synchub-cli sync once --path .")
	fmt.Fprintln(w, "  synchub-cli sync status --path .")
	fmt.Fprintln(w, "  synchub-cli sync push --path .")
	fmt.Fprintln(w, "  synchub-cli sync push --path . --device-name laptop --platform windows")
	fmt.Fprintln(w, "  synchub-cli sync pull --path .")
	fmt.Fprintln(w, "  synchub-cli sync pull --path . --reset-cursor")
	fmt.Fprintln(w, "  synchub-cli sync watch --path .")
	fmt.Fprintln(w, "  synchub-cli sync conflicts --path .")
	fmt.Fprintln(w, "  synchub-cli sync conflicts resolve --path . --id conf_1 --resolution keep_both")
}

func printWorkspaceUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli workspace init --path . --remote-path /workspace")
}

func printManifestUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli manifest scan --path .")
}

func printFileUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli file versions --path . --remote-path /workspace/readme.txt")
	fmt.Fprintln(w, "  synchub-cli file versions --path . --file-id file_1")
	fmt.Fprintln(w, "  synchub-cli file restore --path . --remote-path /workspace/readme.txt --version 1")
	fmt.Fprintln(w, "  synchub-cli file restore --path . --file-id file_1 --version 1")
	fmt.Fprintln(w, "  synchub-cli file pin --path . --remote-path /workspace/readme.txt --version 1")
	fmt.Fprintln(w, "  synchub-cli file unpin --path . --file-id file_1 --version 1")
}

func printSyncUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli sync once --path .")
	fmt.Fprintln(w, "  synchub-cli sync status --path .")
	fmt.Fprintln(w, "  synchub-cli sync push --path .")
	fmt.Fprintln(w, "  synchub-cli sync push --path . --device-name laptop --platform windows")
	fmt.Fprintln(w, "  synchub-cli sync pull --path .")
	fmt.Fprintln(w, "  synchub-cli sync pull --path . --reset-cursor")
	fmt.Fprintln(w, "  synchub-cli sync watch --path .")
	fmt.Fprintln(w, "  synchub-cli sync conflicts --path .")
	fmt.Fprintln(w, "  synchub-cli sync conflicts resolve --path . --id conf_1 --resolution keep_both")
}

func printSyncConflictsUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli sync conflicts --path .")
	fmt.Fprintln(w, "  synchub-cli sync conflicts resolve --path . --id conf_1 --resolution keep_both")
}
