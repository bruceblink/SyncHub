package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bruceblink/SyncHub/internal/manifest"
)

func runManifest(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printManifestUsage(stderr)
		return errors.New("manifest command is required")
	}
	switch args[0] {
	case "scan":
		return runManifestScan(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printManifestUsage(stdout)
		return nil
	default:
		printManifestUsage(stderr)
		return fmt.Errorf("unknown manifest command: %s", args[0])
	}
}

func runManifestScan(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("manifest scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	outputPath := fs.String("output", "", "manifest output file path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, err := resolveWorkspaceRoot(*rootPath)
	if err != nil {
		return err
	}
	configPath := *workspaceConfigPath
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(root, ".synchub", "workspace.json")
	}
	workspace, err := readWorkspaceConfig(configPath)
	if err != nil {
		return err
	}
	if workspace.Root != "" {
		root = workspace.Root
	}
	m, err := manifest.Scan(ctx, root, workspace.RemotePath)
	if err != nil {
		return err
	}
	out := *outputPath
	if strings.TrimSpace(out) == "" {
		out = filepath.Join(root, ".synchub", "manifest.json")
	}
	if err := writeJSONFile(out, m, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "manifest scanned: %d files\n", len(m.Items))
	fmt.Fprintf(stdout, "output: %s\n", out)
	return nil
}

func readManifest(path string) (manifest.Manifest, error) {
	if strings.TrimSpace(path) == "" {
		return manifest.Manifest{}, errors.New("manifest path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return manifest.Manifest{}, err
	}
	var m manifest.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return manifest.Manifest{}, err
	}
	return m, nil
}
