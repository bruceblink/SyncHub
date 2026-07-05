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
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/internal/watch"
	"github.com/bruceblink/SyncHub/pkg/client"
)

type syncAgentState struct {
	Version             int        `json:"version"`
	Root                string     `json:"root"`
	Status              string     `json:"status"`
	CyclesRun           int        `json:"cycles_run"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastSuccessAt       *time.Time `json:"last_success_at"`
	LastFailureAt       *time.Time `json:"last_failure_at"`
	LastError           string     `json:"last_error"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type syncAgentControl struct {
	Version   int       `json:"version"`
	Paused    bool      `json:"paused"`
	UpdatedAt time.Time `json:"updated_at"`
}

type syncStatusSnapshot struct {
	Workspace      syncStatusWorkspace         `json:"workspace"`
	Manifest       syncStatusManifest          `json:"manifest"`
	PendingChanges syncStatusChangeSummary     `json:"pending_changes,omitempty"`
	Trash          syncStatusTrashSummaryValue `json:"trash,omitempty"`
	Daemon         syncStatusAgentSummaryValue `json:"daemon,omitempty"`
	Remote         *syncStatusRemoteSummary    `json:"remote,omitempty"`
	Conflicts      *syncStatusConflictSummary  `json:"conflicts,omitempty"`
	Next           string                      `json:"next,omitempty"`
}

type syncStatusWorkspace struct {
	Root                string `json:"root"`
	RemotePath          string `json:"remote_path"`
	UserEmail           string `json:"user_email"`
	DeviceID            string `json:"device_id,omitempty"`
	DeviceName          string `json:"device_name,omitempty"`
	DevicePlatform      string `json:"device_platform,omitempty"`
	LastAppliedChangeID int64  `json:"last_applied_change_id,omitempty"`
}

type syncStatusManifest struct {
	Path               string                 `json:"path"`
	Missing            bool                   `json:"missing"`
	Files              int                    `json:"files"`
	RemoteTracked      int                    `json:"remote_tracked"`
	LocalOnly          int                    `json:"local_only"`
	RemoteVersionRange syncStatusVersionRange `json:"remote_version_range"`
	LastScan           *time.Time             `json:"last_scan,omitempty"`
}

type syncStatusVersionRange struct {
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

type syncStatusChangeSummary struct {
	Total   int `json:"total"`
	Created int `json:"created"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`
	Moved   int `json:"moved"`
}

type syncStatusTrashSummaryValue struct {
	Entries int         `json:"entries"`
	Latest  *trashEntry `json:"latest,omitempty"`
}

type syncStatusAgentSummaryValue struct {
	State        *syncAgentState   `json:"state,omitempty"`
	Control      *syncAgentControl `json:"control,omitempty"`
	HasRun       bool              `json:"has_run"`
	Paused       bool              `json:"paused"`
	ControlFound bool              `json:"control_found"`
}

type syncStatusRemoteSummary struct {
	Skipped    bool                 `json:"skipped"`
	Reason     string               `json:"reason,omitempty"`
	Changes    []client.ChangeEvent `json:"changes,omitempty"`
	NextCursor int64                `json:"next_cursor"`
}

type syncStatusConflictSummary struct {
	Items []client.SyncConflict `json:"items"`
}

func runSyncStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	loginConfigPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	manifestPath := fs.String("manifest", "", "manifest file path")
	showRemote := fs.Bool("show-remote", false, "include pending remote changes")
	remoteLimit := fs.Int("remote-limit", 100, "maximum remote changes to fetch")
	showConflicts := fs.Bool("show-conflicts", false, "include pending remote conflicts")
	conflictLimit := fs.Int("conflict-limit", 100, "maximum conflicts to fetch")
	jsonOutput := fs.Bool("json", false, "print sync status as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *remoteLimit <= 0 {
		return errors.New("remote limit must be positive")
	}
	if *conflictLimit <= 0 {
		return errors.New("conflict limit must be positive")
	}

	root, err := resolveWorkspaceRoot(*rootPath)
	if err != nil {
		return err
	}
	workspacePath := *workspaceConfigPath
	if strings.TrimSpace(workspacePath) == "" {
		workspacePath = filepath.Join(root, ".synchub", "workspace.json")
	}
	workspace, err := readWorkspaceConfig(workspacePath)
	if err != nil {
		return err
	}
	if workspace.Root != "" {
		root = workspace.Root
	}
	localManifestPath := *manifestPath
	if strings.TrimSpace(localManifestPath) == "" {
		localManifestPath = filepath.Join(root, ".synchub", "manifest.json")
	}

	status := syncStatusSnapshot{
		Workspace: syncStatusWorkspace{
			Root:                root,
			RemotePath:          workspace.RemotePath,
			UserEmail:           workspace.UserEmail,
			DeviceID:            workspace.DeviceID,
			DeviceName:          workspace.DeviceName,
			DevicePlatform:      workspace.DevicePlatform,
			LastAppliedChangeID: workspace.LastAppliedChangeID,
		},
		Manifest: syncStatusManifest{
			Path: localManifestPath,
		},
	}
	m, err := readManifest(localManifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.Manifest.Missing = true
			status.Next = "run synchub-cli sync once --path ."
		} else {
			return err
		}
	} else {
		remoteTracked, localOnly, minRemoteVersion, maxRemoteVersion := manifestRemoteVersionSummary(m.Items)
		status.Manifest.Files = len(m.Items)
		status.Manifest.RemoteTracked = remoteTracked
		status.Manifest.LocalOnly = localOnly
		status.Manifest.RemoteVersionRange = syncStatusVersionRange{Min: minRemoteVersion, Max: maxRemoteVersion}
		lastScan := m.GeneratedAt
		status.Manifest.LastScan = &lastScan
		changes, err := scanManifestChanges(ctx, root, workspace.RemotePath, localManifestPath)
		if err != nil {
			return err
		}
		status.PendingChanges = syncStatusChangeSummaryFromChanges(changes)
	}
	trash, err := syncStatusTrashSummary(root)
	if err != nil {
		return err
	}
	status.Trash = trash
	daemon, err := syncStatusAgentSummary(root)
	if err != nil {
		return err
	}
	status.Daemon = daemon
	if *showRemote {
		remote, err := buildSyncStatusRemoteSummary(ctx, root, workspace, *loginConfigPath, *remoteLimit)
		if err != nil {
			return err
		}
		status.Remote = &remote
	}
	if *showConflicts {
		conflicts, err := buildSyncStatusConflictSummary(ctx, workspace, *loginConfigPath, *conflictLimit)
		if err != nil {
			return err
		}
		status.Conflicts = &conflicts
	}
	status.Next = syncStatusNextAction(status)
	if *jsonOutput {
		return writeSyncStatusJSON(stdout, status)
	}
	return printSyncStatusText(stdout, status)
}

func writeSyncStatusJSON(stdout io.Writer, status syncStatusSnapshot) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(status)
}

func printSyncStatusText(stdout io.Writer, status syncStatusSnapshot) error {
	fmt.Fprintf(stdout, "workspace: %s\n", status.Workspace.Root)
	fmt.Fprintf(stdout, "remote path: %s\n", status.Workspace.RemotePath)
	fmt.Fprintf(stdout, "user: %s\n", status.Workspace.UserEmail)
	if status.Workspace.DeviceID != "" {
		fmt.Fprintf(stdout, "device: %s\n", status.Workspace.DeviceID)
		if strings.TrimSpace(status.Workspace.DeviceName) != "" {
			fmt.Fprintf(stdout, "device name: %s\n", status.Workspace.DeviceName)
		}
		if strings.TrimSpace(status.Workspace.DevicePlatform) != "" {
			fmt.Fprintf(stdout, "device platform: %s\n", status.Workspace.DevicePlatform)
		}
		fmt.Fprintf(stdout, "last applied change: %d\n", status.Workspace.LastAppliedChangeID)
	}
	if status.Manifest.Missing {
		fmt.Fprintln(stdout, "manifest: missing")
		printSyncStatusChanges(stdout, status.PendingChanges)
		printSyncStatusTrash(stdout, status.Trash)
		printSyncStatusAgent(stdout, status.Daemon)
		if status.Remote != nil {
			printSyncStatusRemote(stdout, *status.Remote)
		}
		if status.Conflicts != nil {
			printSyncStatusConflicts(stdout, *status.Conflicts)
		}
		printSyncStatusNext(stdout, status)
		return nil
	}
	fmt.Fprintf(stdout, "manifest: %s\n", status.Manifest.Path)
	fmt.Fprintf(stdout, "files: %d\n", status.Manifest.Files)
	fmt.Fprintf(stdout, "remote tracked: %d\n", status.Manifest.RemoteTracked)
	fmt.Fprintf(stdout, "local only: %d\n", status.Manifest.LocalOnly)
	if status.Manifest.RemoteTracked > 0 {
		fmt.Fprintf(stdout, "remote version range: %d-%d\n", status.Manifest.RemoteVersionRange.Min, status.Manifest.RemoteVersionRange.Max)
	} else {
		fmt.Fprintln(stdout, "remote version range: -")
	}
	lastScan := "-"
	if status.Manifest.LastScan != nil {
		lastScan = status.Manifest.LastScan.Format(time.RFC3339)
	}
	fmt.Fprintf(stdout, "last scan: %s\n", lastScan)
	printSyncStatusChanges(stdout, status.PendingChanges)
	printSyncStatusTrash(stdout, status.Trash)
	printSyncStatusAgent(stdout, status.Daemon)
	if status.Remote != nil {
		printSyncStatusRemote(stdout, *status.Remote)
	}
	if status.Conflicts != nil {
		printSyncStatusConflicts(stdout, *status.Conflicts)
	}
	printSyncStatusNext(stdout, status)
	return nil
}

func printSyncStatusNext(stdout io.Writer, status syncStatusSnapshot) {
	if strings.TrimSpace(status.Next) != "" {
		fmt.Fprintf(stdout, "next: %s\n", status.Next)
	}
}

func syncStatusNextAction(status syncStatusSnapshot) string {
	if status.Manifest.Missing {
		return "run synchub-cli sync once --path ."
	}
	if status.Conflicts != nil && len(status.Conflicts.Items) > 0 {
		return "run synchub-cli sync conflicts --path ."
	}
	if status.PendingChanges.Total > 0 {
		return "run synchub-cli sync once --path . --dry-run"
	}
	if status.Remote != nil {
		if status.Remote.Skipped {
			if strings.Contains(status.Remote.Reason, "device") {
				return "run synchub-cli sync once --path ."
			}
		} else if len(status.Remote.Changes) > 0 {
			return "run synchub-cli sync pull --path ."
		}
	}
	if status.Daemon.Paused {
		return "run synchub-cli sync daemon --path . --resume"
	}
	if status.Trash.Entries > 0 {
		return "run synchub-cli sync trash --path ."
	}
	if strings.TrimSpace(status.Workspace.DeviceID) == "" {
		return "run synchub-cli sync once --path ."
	}
	return ""
}

func syncStatusChangeSummaryFromChanges(changes []watch.Change) syncStatusChangeSummary {
	summary := syncStatusChangeSummary{Total: len(changes)}
	for _, change := range changes {
		switch change.Type {
		case watch.ChangeCreated:
			summary.Created++
		case watch.ChangeUpdated:
			summary.Updated++
		case watch.ChangeDeleted:
			summary.Deleted++
		case watch.ChangeMoved:
			summary.Moved++
		}
	}
	return summary
}

func syncStatusAgentSummary(root string) (syncStatusAgentSummaryValue, error) {
	control, controlOK, err := readSyncAgentControl(root)
	if err != nil {
		return syncStatusAgentSummaryValue{}, err
	}
	state, ok, err := readSyncAgentState(root)
	if err != nil {
		return syncStatusAgentSummaryValue{}, err
	}
	summary := syncStatusAgentSummaryValue{HasRun: ok, ControlFound: controlOK}
	if ok {
		state := state
		summary.State = &state
	}
	if controlOK {
		control := control
		summary.Control = &control
		summary.Paused = control.Paused
	}
	return summary, nil
}

func printSyncStatusAgent(stdout io.Writer, summary syncStatusAgentSummaryValue) {
	if !summary.HasRun || summary.State == nil {
		fmt.Fprintln(stdout, "daemon: not run")
		if summary.Control != nil {
			printSyncAgentControl(stdout, *summary.Control)
		}
		return
	}
	state := summary.State
	fmt.Fprintf(stdout, "daemon: %s\n", state.Status)
	fmt.Fprintf(stdout, "daemon cycles: %d\n", state.CyclesRun)
	fmt.Fprintf(stdout, "daemon consecutive failures: %d\n", state.ConsecutiveFailures)
	fmt.Fprintf(stdout, "daemon last success: %s\n", formatOptionalTime(state.LastSuccessAt))
	fmt.Fprintf(stdout, "daemon last failure: %s\n", formatOptionalTime(state.LastFailureAt))
	if strings.TrimSpace(state.LastError) != "" {
		fmt.Fprintf(stdout, "daemon last error: %s\n", state.LastError)
	}
	fmt.Fprintf(stdout, "daemon updated: %s\n", state.UpdatedAt.UTC().Format(time.RFC3339))
	if summary.Control != nil {
		printSyncAgentControl(stdout, *summary.Control)
	}
}

func printSyncAgentControl(stdout io.Writer, control syncAgentControl) {
	paused := "no"
	if control.Paused {
		paused = "yes"
	}
	fmt.Fprintf(stdout, "daemon paused: %s\n", paused)
	fmt.Fprintf(stdout, "daemon control updated: %s\n", control.UpdatedAt.UTC().Format(time.RFC3339))
}

func readSyncAgentState(root string) (syncAgentState, bool, error) {
	path := filepath.Join(root, ".synchub", "daemon-state.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return syncAgentState{}, false, nil
		}
		return syncAgentState{}, false, err
	}
	var state syncAgentState
	if err := json.Unmarshal(raw, &state); err != nil {
		return syncAgentState{}, false, fmt.Errorf("read daemon state: %w", err)
	}
	return state, true, nil
}

func readSyncAgentControl(root string) (syncAgentControl, bool, error) {
	path := filepath.Join(root, ".synchub", "daemon-control.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return syncAgentControl{}, false, nil
		}
		return syncAgentControl{}, false, err
	}
	var control syncAgentControl
	if err := json.Unmarshal(raw, &control); err != nil {
		return syncAgentControl{}, false, fmt.Errorf("read daemon control: %w", err)
	}
	return control, true, nil
}

func syncStatusTrashSummary(root string) (syncStatusTrashSummaryValue, error) {
	entries, err := listTrashEntries(root, 0)
	if err != nil {
		return syncStatusTrashSummaryValue{}, err
	}
	summary := syncStatusTrashSummaryValue{Entries: len(entries)}
	if len(entries) > 0 {
		entry := entries[0]
		summary.Latest = &entry
	}
	return summary, nil
}

func printSyncStatusTrash(stdout io.Writer, summary syncStatusTrashSummaryValue) {
	fmt.Fprintf(stdout, "trash entries: %d\n", summary.Entries)
	if summary.Latest != nil {
		fmt.Fprintf(stdout, "latest trash: %s %s\n", summary.Latest.Batch, summary.Latest.Path)
	}
}

func buildSyncStatusRemoteSummary(ctx context.Context, root string, workspace workspaceConfig, configPath string, limit int) (syncStatusRemoteSummary, error) {
	if strings.TrimSpace(workspace.DeviceID) == "" {
		return syncStatusRemoteSummary{Skipped: true, Reason: "workspace device is not registered"}, nil
	}
	loginConfig, err := readConfigWithRefresh(ctx, configPath)
	if err != nil {
		return syncStatusRemoteSummary{}, err
	}
	serverURL := workspace.ServerURL
	if strings.TrimSpace(serverURL) == "" {
		serverURL = loginConfig.ServerURL
	}
	changes, err := client.New(serverURL).ListChanges(ctx, loginConfig.Tokens.AccessToken, workspace.DeviceID, workspace.LastAppliedChangeID, int32(limit))
	if err != nil {
		if isAPIErrorCode(err, "SYNC_CURSOR_EXPIRED") {
			return syncStatusRemoteSummary{}, fmt.Errorf("sync cursor expired; run synchub-cli sync pull --path %s --reset-cursor to replay the available change feed", root)
		}
		return syncStatusRemoteSummary{}, err
	}
	pending := previewPullChanges(workspace, changes.Items)
	return syncStatusRemoteSummary{Changes: pending, NextCursor: pullNextCursor(changes)}, nil
}

func printSyncStatusRemote(stdout io.Writer, summary syncStatusRemoteSummary) {
	if summary.Skipped {
		fmt.Fprintf(stdout, "remote changes: skipped (%s)\n", summary.Reason)
		return
	}
	fmt.Fprintf(stdout, "remote changes: %d\n", len(summary.Changes))
	for _, event := range summary.Changes {
		fmt.Fprintln(stdout, formatPullPreviewChange(event))
	}
	fmt.Fprintf(stdout, "remote next cursor: %d\n", summary.NextCursor)
}

func buildSyncStatusConflictSummary(ctx context.Context, workspace workspaceConfig, configPath string, limit int) (syncStatusConflictSummary, error) {
	loginConfig, err := readConfigWithRefresh(ctx, configPath)
	if err != nil {
		return syncStatusConflictSummary{}, err
	}
	serverURL := workspace.ServerURL
	if strings.TrimSpace(serverURL) == "" {
		serverURL = loginConfig.ServerURL
	}
	conflicts, err := client.New(serverURL).ListSyncConflicts(ctx, loginConfig.Tokens.AccessToken, "pending", int32(limit))
	if err != nil {
		return syncStatusConflictSummary{}, err
	}
	return syncStatusConflictSummary{Items: conflicts.Items}, nil
}

func printSyncStatusConflicts(stdout io.Writer, summary syncStatusConflictSummary) {
	fmt.Fprintf(stdout, "remote conflicts: %d\n", len(summary.Items))
	printSyncConflictItems(stdout, summary.Items)
}

func printSyncStatusChanges(stdout io.Writer, summary syncStatusChangeSummary) {
	fmt.Fprintf(stdout, "pending changes: %d\n", summary.Total)
	printChangeTypeCounts(stdout, summary)
}

func printChangeTypeCounts(stdout io.Writer, summary syncStatusChangeSummary) {
	fmt.Fprintf(stdout, "created: %d\n", summary.Created)
	fmt.Fprintf(stdout, "updated: %d\n", summary.Updated)
	fmt.Fprintf(stdout, "deleted: %d\n", summary.Deleted)
	fmt.Fprintf(stdout, "moved: %d\n", summary.Moved)
}

func printWatchChangeTypeCounts(stdout io.Writer, changes []watch.Change) {
	printChangeTypeCounts(stdout, syncStatusChangeSummaryFromChanges(changes))
}

func manifestRemoteVersionSummary(items []manifest.Entry) (remoteTracked, localOnly int, minRemoteVersion, maxRemoteVersion int64) {
	for _, item := range items {
		if item.RemoteVersion == nil {
			localOnly++
			continue
		}
		remoteTracked++
		version := *item.RemoteVersion
		if minRemoteVersion == 0 || version < minRemoteVersion {
			minRemoteVersion = version
		}
		if version > maxRemoteVersion {
			maxRemoteVersion = version
		}
	}
	return remoteTracked, localOnly, minRemoteVersion, maxRemoteVersion
}
