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

func NormalizePathPattern(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalidPath
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
	for _, part := range pathParts(clean) {
		if part == "**" {
			continue
		}
		if !validGlobSegment(part) {
			return "", ErrInvalidPath
		}
		if _, err := path.Match(part, "x"); err != nil {
			return "", ErrInvalidPath
		}
	}
	return clean, nil
}

func MatchPathPattern(pattern, target string) bool {
	pattern, err := NormalizePathPattern(pattern)
	if err != nil {
		return false
	}
	target, err = NormalizePath(target)
	if err != nil {
		return false
	}
	if pattern == target {
		return true
	}

	patternParts := pathParts(pattern)
	targetParts := pathParts(target)
	return matchPathParts(patternParts, targetParts)
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

func pathParts(value string) []string {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func validGlobSegment(part string) bool {
	return strings.Count(part, "[") == strings.Count(part, "]") && !strings.Contains(part, "[]")
}

func matchPathParts(patternParts, targetParts []string) bool {
	if len(patternParts) == 0 {
		return len(targetParts) == 0
	}

	part := patternParts[0]
	if part == "**" {
		if len(patternParts) == 1 {
			return true
		}
		for i := 0; i <= len(targetParts); i++ {
			if matchPathParts(patternParts[1:], targetParts[i:]) {
				return true
			}
		}
		return false
	}
	if len(targetParts) == 0 {
		return false
	}
	matched, err := path.Match(part, targetParts[0])
	if err != nil || !matched {
		return false
	}
	return matchPathParts(patternParts[1:], targetParts[1:])
}
