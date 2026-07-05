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
	case "ignores":
		return runManifestIgnores(args[1:], stdout, stderr)
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
	dryRun := fs.Bool("dry-run", false, "scan workspace without writing manifest file")
	jsonOutput := fs.Bool("json", false, "print manifest scan result as JSON")
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
	if *dryRun {
		if *jsonOutput {
			return writeManifestScanJSON(stdout, workspace, m, true, "")
		}
		fmt.Fprintf(stdout, "manifest scanned: %d files\n", len(m.Items))
		fmt.Fprintln(stdout, "dry run: true")
		return nil
	}
	out := *outputPath
	if strings.TrimSpace(out) == "" {
		out = filepath.Join(root, ".synchub", "manifest.json")
	}
	if err := writeJSONFile(out, m, 0o600); err != nil {
		return err
	}
	if *jsonOutput {
		return writeManifestScanJSON(stdout, workspace, m, false, out)
	}
	fmt.Fprintf(stdout, "manifest scanned: %d files\n", len(m.Items))
	fmt.Fprintf(stdout, "output: %s\n", out)
	return nil
}

type manifestScanSnapshot struct {
	Workspace manifestScanWorkspace `json:"workspace"`
	DryRun    bool                  `json:"dry_run"`
	Output    string                `json:"output,omitempty"`
	Manifest  manifest.Manifest     `json:"manifest"`
}

type manifestScanWorkspace struct {
	Root       string `json:"root"`
	RemotePath string `json:"remote_path"`
	UserEmail  string `json:"user_email"`
	DeviceID   string `json:"device_id,omitempty"`
}

func writeManifestScanJSON(stdout io.Writer, workspace workspaceConfig, m manifest.Manifest, dryRun bool, output string) error {
	if m.Items == nil {
		m.Items = []manifest.Entry{}
	}
	snapshot := manifestScanSnapshot{
		Workspace: manifestScanWorkspace{
			Root:       workspace.Root,
			RemotePath: workspace.RemotePath,
			UserEmail:  workspace.UserEmail,
			DeviceID:   workspace.DeviceID,
		},
		DryRun:   dryRun,
		Output:   strings.TrimSpace(output),
		Manifest: m,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func runManifestIgnores(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("manifest ignores", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	jsonOutput := fs.Bool("json", false, "print ignore rules as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := resolveWorkspaceRoot(*rootPath)
	if err != nil {
		return err
	}
	rules, err := manifest.LoadIgnoreRules(root)
	if err != nil {
		return err
	}
	patterns := rules.Patterns()
	ignoreFiles := manifest.IgnoreFilePaths(root)
	if *jsonOutput {
		return writeManifestIgnoresJSON(stdout, root, ignoreFiles, patterns)
	}
	for _, ignoreFile := range ignoreFiles {
		fmt.Fprintf(stdout, "ignore file: %s\n", ignoreFile)
	}
	fmt.Fprintf(stdout, "rules: %d\n", len(patterns))
	for _, pattern := range patterns {
		fmt.Fprintf(stdout, "%s\n", pattern)
	}
	return nil
}

type manifestIgnoresSnapshot struct {
	Root        string   `json:"root"`
	IgnoreFile  string   `json:"ignore_file"`
	IgnoreFiles []string `json:"ignore_files"`
	Rules       []string `json:"rules"`
}

func writeManifestIgnoresJSON(stdout io.Writer, root string, ignoreFiles []string, patterns []string) error {
	if patterns == nil {
		patterns = []string{}
	}
	ignoreFile := ""
	if len(ignoreFiles) > 0 {
		ignoreFile = ignoreFiles[0]
	}
	snapshot := manifestIgnoresSnapshot{
		Root:        root,
		IgnoreFile:  ignoreFile,
		IgnoreFiles: ignoreFiles,
		Rules:       patterns,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
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

func writeManifest(path string, m manifest.Manifest) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("manifest path is required")
	}
	return writeJSONFile(path, m, 0o600)
}
