package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func HasTraversal(path string) bool {
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return true
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts {
		if p == ".." {
			return true
		}
	}
	return false
}

func (e *Enforcer) ResolveReadPath(workspace, target string) (string, error) {
	return resolvePath(workspace, target, false)
}

func (e *Enforcer) ResolveWritePath(workspace, target string) (string, error) {
	return resolvePath(workspace, target, true)
}

func resolvePath(workspace, target string, write bool) (string, error) {
	if workspace == "" {
		return "", fmt.Errorf("workspace is required")
	}
	if target == "" {
		return "", &PathError{Path: target, Reason: "empty path"}
	}
	if HasTraversal(target) {
		return "", &PathError{Path: target, Reason: "path traversal"}
	}

	wsAbs, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	wsReal, err := filepath.EvalSymlinks(wsAbs)
	if err != nil {
		return "", err
	}

	targetAbs := target
	if !filepath.IsAbs(targetAbs) {
		targetAbs = filepath.Join(wsReal, targetAbs)
	}
	targetAbs = filepath.Clean(targetAbs)

	var candidate string
	if write {
		parent := filepath.Dir(targetAbs)
		parentReal, err := filepath.EvalSymlinks(parent)
		if err != nil {
			return "", &PathError{Path: target, Reason: "write parent does not exist or is invalid"}
		}
		candidate = filepath.Join(parentReal, filepath.Base(targetAbs))
	} else {
		candidate, err = filepath.EvalSymlinks(targetAbs)
		if err != nil {
			return "", &PathError{Path: target, Reason: "read path does not exist or is invalid"}
		}
	}

	if !isWithinWorkspace(wsReal, candidate) {
		return "", &PathError{Path: target, Reason: "outside workspace"}
	}

	return candidate, nil
}

func isWithinWorkspace(workspaceReal, pathReal string) bool {
	rel, err := filepath.Rel(workspaceReal, pathReal)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}

func EnsureWorkspace(path string) error {
	if path == "" {
		return fmt.Errorf("workspace path is empty")
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("workspace path is not a directory: %s", path)
	}
	return nil
}
