package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Manifest struct {
	Version     int       `json:"version"`
	Root        string    `json:"root"`
	RemotePath  string    `json:"remote_path"`
	GeneratedAt time.Time `json:"generated_at"`
	Items       []Entry   `json:"items"`
}

type Entry struct {
	Path         string    `json:"path"`
	RelativePath string    `json:"relative_path"`
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"mtime"`
	SHA256       string    `json:"sha256"`
}

func Scan(ctx context.Context, root, remotePath string) (Manifest, error) {
	root = filepath.Clean(root)
	remotePath = normalizeRemotePath(remotePath)
	result := Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  remotePath,
		GeneratedAt: time.Now().UTC(),
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			if d.Name() == ".synchub" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		result.Items = append(result.Items, Entry{
			Path:         joinRemotePath(remotePath, relative),
			RelativePath: relative,
			Size:         info.Size(),
			ModTime:      info.ModTime().UTC(),
			SHA256:       sum,
		})
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}
	sort.Slice(result.Items, func(i, j int) bool {
		return result.Items[i].RelativePath < result.Items[j].RelativePath
	})
	return result, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func normalizeRemotePath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return pathpkg.Clean(p)
}

func joinRemotePath(remotePath, relative string) string {
	relative = strings.TrimPrefix(filepath.ToSlash(relative), "/")
	if remotePath == "/" {
		return "/" + relative
	}
	return pathpkg.Join(remotePath, relative)
}
