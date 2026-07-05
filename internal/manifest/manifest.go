package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

const ignoreFileName = ".synchubignore"

type Manifest struct {
	Version     int       `json:"version"`
	Root        string    `json:"root"`
	RemotePath  string    `json:"remote_path"`
	GeneratedAt time.Time `json:"generated_at"`
	Items       []Entry   `json:"items"`
	Skipped     []Issue   `json:"-"`
}

type Entry struct {
	Path          string    `json:"path"`
	RelativePath  string    `json:"relative_path"`
	Size          int64     `json:"size"`
	ModTime       time.Time `json:"mtime"`
	SHA256        string    `json:"sha256"`
	RemoteVersion *int64    `json:"remote_version,omitempty"`
}

type Issue struct {
	Path         string
	RelativePath string
	Err          error
}

func Scan(ctx context.Context, root, remotePath string) (Manifest, error) {
	root = filepath.Clean(root)
	remotePath = normalizeRemotePath(remotePath)
	ignoreRules, err := LoadIgnoreRules(root)
	if err != nil {
		return Manifest{}, err
	}
	result := Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  remotePath,
		GeneratedAt: time.Now().UTC(),
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path != root && isTransientFileReadError(err) {
				if relative, relErr := filepath.Rel(root, path); relErr == nil {
					relative = filepath.ToSlash(relative)
					result.Skipped = append(result.Skipped, Issue{
						Path:         joinRemotePath(remotePath, relative),
						RelativePath: relative,
						Err:          err,
					})
				}
				return nil
			}
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
			if path != root {
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				if ignoreRules.Match(filepath.ToSlash(relative), true) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative != ignoreFileName && ignoreRules.Match(relative, false) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if isTransientFileReadError(err) {
				result.Skipped = append(result.Skipped, Issue{
					Path:         joinRemotePath(remotePath, relative),
					RelativePath: relative,
					Err:          err,
				})
				return nil
			}
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		sum, err := fileSHA256(path)
		if err != nil {
			if isTransientFileReadError(err) {
				result.Skipped = append(result.Skipped, Issue{
					Path:         joinRemotePath(remotePath, relative),
					RelativePath: relative,
					Err:          err,
				})
				return nil
			}
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
	sort.Slice(result.Skipped, func(i, j int) bool {
		return result.Skipped[i].RelativePath < result.Skipped[j].RelativePath
	})
	return result, nil
}

type IgnoreRules []ignoreRule

type ignoreRule struct {
	pattern   string
	directory bool
}

func LoadIgnoreRules(root string) (IgnoreRules, error) {
	rules := IgnoreRules{}
	raw, err := os.ReadFile(filepath.Join(root, ignoreFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return rules, nil
		}
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	for _, line := range lines {
		line = strings.TrimPrefix(line, "\ufeff")
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(filepath.ToSlash(line), "/")
		directory := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		if line == "" {
			continue
		}
		rules = append(rules, ignoreRule{pattern: line, directory: directory})
	}
	return rules, nil
}

func IgnoreFilePaths(root string) []string {
	return []string{filepath.Join(root, ignoreFileName)}
}

func (rules IgnoreRules) Match(relativePath string, directory bool) bool {
	relativePath = strings.TrimPrefix(filepath.ToSlash(relativePath), "/")
	if relativePath == "" {
		return false
	}
	for _, rule := range rules {
		if rule.directory && !directory {
			continue
		}
		if matchIgnoreRule(rule.pattern, relativePath) {
			return true
		}
	}
	return false
}

func (rules IgnoreRules) Patterns() []string {
	patterns := make([]string, 0, len(rules))
	for _, rule := range rules {
		pattern := rule.pattern
		if rule.directory {
			pattern += "/"
		}
		patterns = append(patterns, pattern)
	}
	return patterns
}

func matchIgnoreRule(pattern, relativePath string) bool {
	if strings.Contains(pattern, "/") {
		ok, err := pathpkg.Match(pattern, relativePath)
		return err == nil && ok
	}
	for _, part := range strings.Split(relativePath, "/") {
		ok, err := pathpkg.Match(pattern, part)
		if err == nil && ok {
			return true
		}
	}
	return false
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

func isTransientFileReadError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || os.IsPermission(err) {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == 32 || errno == 33
	}
	return false
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
