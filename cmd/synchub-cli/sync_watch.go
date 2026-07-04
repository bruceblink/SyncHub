package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/internal/watch"
)

func runSyncWatch(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync watch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	manifestPath := fs.String("manifest", "", "manifest file path")
	interval := fs.Duration("interval", time.Second, "watch polling interval")
	once := fs.Bool("once", false, "scan once against the current manifest and exit")
	jsonOutput := fs.Bool("json", false, "print watch changes as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *interval <= 0 {
		return errors.New("watch interval must be positive")
	}

	root, workspace, localManifestPath, err := loadWorkspaceAndManifestPath(*rootPath, *workspaceConfigPath, *manifestPath)
	if err != nil {
		return err
	}
	if *once {
		changes, err := scanManifestChanges(ctx, root, workspace.RemotePath, localManifestPath)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writeWatchChangesJSON(stdout, workspace, changes)
		}
		printWatchChanges(stdout, changes)
		return nil
	}

	poller, err := watch.NewPoller(ctx, root, workspace.RemotePath)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "watching: %s\n", root)
	return poller.Run(ctx, *interval, func(changes []watch.Change) error {
		if *jsonOutput {
			return writeWatchChangesJSON(stdout, workspace, changes)
		}
		printWatchChanges(stdout, changes)
		return nil
	})
}

func scanManifestChanges(ctx context.Context, root, remotePath, manifestPath string) ([]watch.Change, error) {
	m, err := readManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	if m.Root != "" && filepath.Clean(m.Root) != filepath.Clean(root) {
		return nil, fmt.Errorf("manifest root %s does not match workspace root %s", m.Root, root)
	}
	previous := watch.SnapshotFromManifest(m)
	current, err := watch.Scan(ctx, root, remotePath)
	if err != nil {
		return nil, err
	}
	return watch.Diff(previous, current), nil
}

func printWatchChanges(stdout io.Writer, changes []watch.Change) {
	for _, change := range changes {
		if change.Type == watch.ChangeMoved && change.Before != nil && change.After != nil {
			fmt.Fprintf(stdout, "%s %s -> %s\n", change.Type, change.Before.RelativePath, change.After.RelativePath)
			continue
		}
		fmt.Fprintf(stdout, "%s %s\n", change.Type, displayWatchPath(change))
	}
	fmt.Fprintf(stdout, "changes: %d\n", len(changes))
	printWatchChangeTypeCounts(stdout, changes)
}

func displayWatchPath(change watch.Change) string {
	if strings.TrimSpace(change.RelativePath) != "" {
		return change.RelativePath
	}
	return change.Path
}

type watchChangesSnapshot struct {
	Workspace syncFileWorkspace         `json:"workspace"`
	Count     int                       `json:"count"`
	Counts    syncStatusChangeSummary   `json:"counts"`
	Items     []watchChangeSnapshotItem `json:"items"`
}

type watchChangeSnapshotItem struct {
	Type         string          `json:"type"`
	Path         string          `json:"path"`
	RelativePath string          `json:"relative_path"`
	Before       *manifest.Entry `json:"before,omitempty"`
	After        *manifest.Entry `json:"after,omitempty"`
}

func writeWatchChangesJSON(stdout io.Writer, workspace workspaceConfig, changes []watch.Change) error {
	items := make([]watchChangeSnapshotItem, 0, len(changes))
	for _, change := range changes {
		items = append(items, watchChangeSnapshotItem{
			Type:         change.Type,
			Path:         change.Path,
			RelativePath: change.RelativePath,
			Before:       change.Before,
			After:        change.After,
		})
	}
	snapshot := watchChangesSnapshot{
		Workspace: syncFileWorkspace{
			Root:       workspace.Root,
			RemotePath: workspace.RemotePath,
			UserEmail:  workspace.UserEmail,
			DeviceID:   workspace.DeviceID,
		},
		Count:  len(changes),
		Counts: syncStatusChangeSummaryFromChanges(changes),
		Items:  items,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}
