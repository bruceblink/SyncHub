package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type trashEntry struct {
	Batch string `json:"batch"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"is_dir"`
}

func runSyncTrash(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "restore":
			return runSyncTrashRestore(args[1:], stdout, stderr)
		case "help", "-h", "--help":
			printSyncTrashUsage(stdout)
			return nil
		}
	}
	fs := flag.NewFlagSet("sync trash", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	limit := fs.Int("limit", 100, "maximum trash entries to list")
	jsonOutput := fs.Bool("json", false, "print trash entries as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("limit must be positive")
	}

	root, workspace, _, err := loadWorkspace(*rootPath, *workspaceConfigPath)
	if err != nil {
		return err
	}
	entries, err := listTrashEntries(root, *limit)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSyncTrashJSON(stdout, root, workspace, entries)
	}
	printTrashEntries(stdout, entries)
	return nil
}

func runSyncTrashRestore(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync trash restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	batch := fs.String("batch", "", "trash batch timestamp")
	entryPath := fs.String("entry", "", "trash entry path")
	jsonOutput := fs.Bool("json", false, "print trash restore result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, workspace, _, err := loadWorkspace(*rootPath, *workspaceConfigPath)
	if err != nil {
		return err
	}
	cleanedEntry, err := cleanTrashEntryPath(*entryPath)
	if err != nil {
		return err
	}
	restored, err := restoreTrashEntry(root, *batch, *entryPath)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSyncTrashRestoreJSON(stdout, root, workspace, *batch, cleanedEntry, restored)
	}
	fmt.Fprintf(stdout, "restored: %s\n", restored)
	return nil
}

type syncTrashSnapshot struct {
	Workspace syncTrashWorkspace `json:"workspace"`
	Items     []trashEntry       `json:"items"`
}

type syncTrashRestoreSnapshot struct {
	Workspace    syncTrashWorkspace `json:"workspace"`
	Action       string             `json:"action"`
	Batch        string             `json:"batch"`
	Entry        string             `json:"entry"`
	RestoredPath string             `json:"restored_path"`
}

type syncTrashWorkspace struct {
	Root       string `json:"root"`
	RemotePath string `json:"remote_path"`
	UserEmail  string `json:"user_email"`
	DeviceID   string `json:"device_id,omitempty"`
}

func writeSyncTrashJSON(stdout io.Writer, root string, workspace workspaceConfig, entries []trashEntry) error {
	if entries == nil {
		entries = []trashEntry{}
	}
	snapshot := syncTrashSnapshot{
		Workspace: syncTrashWorkspace{
			Root:       root,
			RemotePath: workspace.RemotePath,
			UserEmail:  workspace.UserEmail,
			DeviceID:   workspace.DeviceID,
		},
		Items: entries,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func writeSyncTrashRestoreJSON(stdout io.Writer, root string, workspace workspaceConfig, batch, entry, restoredPath string) error {
	snapshot := syncTrashRestoreSnapshot{
		Workspace: syncTrashWorkspace{
			Root:       root,
			RemotePath: workspace.RemotePath,
			UserEmail:  workspace.UserEmail,
			DeviceID:   workspace.DeviceID,
		},
		Action:       "restore",
		Batch:        strings.TrimSpace(batch),
		Entry:        entry,
		RestoredPath: restoredPath,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func listTrashEntries(root string, limit int) ([]trashEntry, error) {
	trashRoot := filepath.Join(root, ".synchub", "trash")
	if _, err := os.Stat(trashRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	entries := []trashEntry{}
	err := filepath.WalkDir(trashRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == trashRoot {
			return nil
		}
		relative, err := filepath.Rel(trashRoot, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) <= 1 {
			return nil
		}
		if len(parts) > 2 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entry := trashEntry{
			Batch: parts[0],
			Path:  parts[1],
			IsDir: d.IsDir(),
		}
		if d.IsDir() {
			entry.Path += "/"
			entries = append(entries, entry)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry.Size = info.Size()
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Batch == entries[j].Batch {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Batch > entries[j].Batch
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func printTrashEntries(stdout io.Writer, entries []trashEntry) {
	fmt.Fprintf(stdout, "trash entries: %d\n", len(entries))
	for _, entry := range entries {
		if entry.IsDir {
			fmt.Fprintf(stdout, "%s %s\n", entry.Batch, entry.Path)
			continue
		}
		fmt.Fprintf(stdout, "%s %s size=%d\n", entry.Batch, entry.Path, entry.Size)
	}
}

func restoreTrashEntry(root, batch, entryPath string) (string, error) {
	batch = strings.TrimSpace(batch)
	if batch == "" || strings.Contains(batch, "/") || strings.Contains(batch, "\\") || batch == "." || batch == ".." {
		return "", errors.New("valid trash batch is required")
	}
	relative, err := cleanTrashEntryPath(entryPath)
	if err != nil {
		return "", err
	}
	trashPath := filepath.Join(root, ".synchub", "trash", batch, filepath.FromSlash(relative))
	if _, err := os.Stat(trashPath); err != nil {
		if os.IsNotExist(err) {
			return "", errors.New("trash entry not found")
		}
		return "", err
	}
	targetPath := filepath.Join(root, filepath.FromSlash(relative))
	if err := ensureLocalPathInsideRoot(root, targetPath); err != nil {
		return "", err
	}
	if _, err := os.Stat(targetPath); err == nil {
		return "", fmt.Errorf("restore target already exists: %s", targetPath)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(trashPath, targetPath); err != nil {
		return "", err
	}
	return targetPath, nil
}

func cleanTrashEntryPath(entryPath string) (string, error) {
	entryPath = strings.TrimSpace(strings.ReplaceAll(entryPath, "\\", "/"))
	entryPath = strings.Trim(entryPath, "/")
	if entryPath == "" {
		return "", errors.New("trash entry is required")
	}
	for _, segment := range strings.Split(entryPath, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("trash entry path is invalid")
		}
	}
	return entryPath, nil
}
