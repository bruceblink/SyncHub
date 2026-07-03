package domain

import (
	"path"
	"strings"
)

func NormalizePath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return "", E(CodeInvalidArgument, "path is required", nil)
	}
	if strings.ContainsRune(p, 0) {
		return "", E(CodeInvalidArgument, "path contains null byte", nil)
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return "", E(CodeInvalidArgument, "path traversal is not allowed", nil)
		}
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		cleaned = "/"
	}
	return cleaned, nil
}

func SplitPath(p string) (parentPath string, name string, err error) {
	normalized, err := NormalizePath(p)
	if err != nil {
		return "", "", err
	}
	if normalized == "/" {
		return "", "", E(CodeInvalidArgument, "root path is not a file node", nil)
	}
	name = path.Base(normalized)
	parentPath = path.Dir(normalized)
	if parentPath == "." {
		parentPath = "/"
	}
	return parentPath, name, nil
}
