package main

import (
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
	Batch string
	Path  string
	Size  int64
	IsDir bool
}

func runSyncTrash(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync trash", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	limit := fs.Int("limit", 100, "maximum trash entries to list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("limit must be positive")
	}

	root, _, _, err := loadWorkspace(*rootPath, *workspaceConfigPath)
	if err != nil {
		return err
	}
	entries, err := listTrashEntries(root, *limit)
	if err != nil {
		return err
	}
	printTrashEntries(stdout, entries)
	return nil
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
