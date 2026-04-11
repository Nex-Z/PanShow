package service

import (
	"errors"
	"path"
	"strings"
)

var ErrInvalidPath = errors.New("invalid path")

func NormalizePath(raw string) (string, error) {
	if raw == "" {
		return "/", nil
	}
	if strings.Contains(raw, "\\") || strings.Contains(raw, "\x00") {
		return "", ErrInvalidPath
	}
	if strings.Contains(strings.ToLower(raw), "%2e") || strings.Contains(raw, "..") {
		return "", ErrInvalidPath
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	clean := path.Clean(raw)
	if clean == "." {
		clean = "/"
	}
	if !strings.HasPrefix(clean, "/") {
		return "", ErrInvalidPath
	}
	return clean, nil
}

func ParentDir(filePath string) string {
	parent := path.Dir(filePath)
	if parent == "." {
		return "/"
	}
	return parent
}

func DirectoryAncestors(dir string) []string {
	if dir == "" || dir == "/" {
		return []string{"/"}
	}

	parts := strings.Split(strings.Trim(dir, "/"), "/")
	result := []string{"/"}
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		current += "/" + part
		result = append(result, current)
	}
	return result
}
