package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

func RegisterCore(reg *Registry) error {
	if err := reg.Register(ToolSpec{Name: "fs.read", Required: []string{"path"}}, fsRead); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.list", Required: []string{"path"}}, fsList); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.write", Required: []string{"path", "content"}}, fsWrite); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.edit", Required: []string{"path", "old", "new"}}, fsEdit); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "code.search", Required: []string{"pattern"}}, codeSearch); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "shell.exec", Required: []string{"command"}}, shellExec); err != nil {
		return err
	}
	return nil
}

func fsRead(_ context.Context, req Request) (map[string]any, error) {
	path, err := getString(req.Args, "path")
	if err != nil {
		return nil, err
	}

	if req.Policy == nil {
		return nil, errors.New("policy is required")
	}
	resolved, err := req.Policy.ResolveReadPath(req.Workspace, path)
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}

	return map[string]any{"path": path, "content": string(b)}, nil
}

func fsList(_ context.Context, req Request) (map[string]any, error) {
	path, err := getString(req.Args, "path")
	if err != nil {
		return nil, err
	}

	if req.Policy == nil {
		return nil, errors.New("policy is required")
	}
	resolved, err := req.Policy.ResolveReadPath(req.Workspace, path)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
	}
	sort.Strings(out)

	return map[string]any{"path": path, "entries": out}, nil
}

func fsWrite(_ context.Context, req Request) (map[string]any, error) {
	path, err := getString(req.Args, "path")
	if err != nil {
		return nil, err
	}
	content, err := getString(req.Args, "content")
	if err != nil {
		return nil, err
	}

	if req.Policy == nil {
		return nil, errors.New("policy is required")
	}
	resolved, err := req.Policy.ResolveWritePath(req.Workspace, path)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(resolved, []byte(content), 0o600); err != nil {
		return nil, err
	}

	return map[string]any{"path": path, "bytes": len(content)}, nil
}

func fsEdit(_ context.Context, req Request) (map[string]any, error) {
	path, err := getString(req.Args, "path")
	if err != nil {
		return nil, err
	}
	oldText, err := getString(req.Args, "old")
	if err != nil {
		return nil, err
	}
	newText, err := getString(req.Args, "new")
	if err != nil {
		return nil, err
	}

	if req.Policy == nil {
		return nil, errors.New("policy is required")
	}
	resolved, err := req.Policy.ResolveWritePath(req.Workspace, path)
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}

	orig := string(b)
	updated := strings.Replace(orig, oldText, newText, 1)
	if updated == orig {
		return nil, errors.New("edit pattern not found")
	}

	if err := os.WriteFile(resolved, []byte(updated), 0o600); err != nil {
		return nil, err
	}

	return map[string]any{"path": path, "updated": true}, nil
}

func codeSearch(_ context.Context, req Request) (map[string]any, error) {
	pattern, err := getString(req.Args, "pattern")
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	root := req.Workspace
	if custom, ok := req.Args["path"]; ok {
		customPath, ok := custom.(string)
		if !ok {
			return nil, fmt.Errorf("path must be string")
		}
		if req.Policy != nil {
			resolved, err := req.Policy.ResolveReadPath(req.Workspace, customPath)
			if err != nil {
				return nil, err
			}
			root = resolved
		} else {
			root = customPath
		}
	}

	files, err := listFiles(root)
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0)
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil || !isText(data) {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if re.MatchString(line) {
				rel, _ := filepath.Rel(req.Workspace, p)
				results = append(results, map[string]any{
					"path": rel,
					"line": lineNo,
					"text": line,
				})
			}
		}
	}

	return map[string]any{"matches": results}, nil
}

func shellExec(ctx context.Context, req Request) (map[string]any, error) {
	if req.Shell == nil {
		return nil, errors.New("shell executor is not configured")
	}
	command, err := getString(req.Args, "command")
	if err != nil {
		return nil, err
	}

	args := []string{}
	if raw, ok := req.Args["args"]; ok {
		switch t := raw.(type) {
		case []any:
			for _, item := range t {
				args = append(args, fmt.Sprintf("%v", item))
			}
		case []string:
			args = append(args, t...)
		default:
			return nil, fmt.Errorf("args must be an array")
		}
	}

	stdout, stderr, exitCode, execErr := req.Shell.Exec(ctx, command, args)
	res := map[string]any{"stdout": stdout, "stderr": stderr, "exit_code": exitCode}
	if execErr != nil {
		res["error"] = execErr.Error()
	}

	if timeoutRaw, ok := req.Args["timeout_ms"]; ok {
		res["timeout_ms"] = normalizeInt(timeoutRaw)
	}
	return res, execErr
}

func normalizeInt(v any) int {
	s := fmt.Sprintf("%v", v)
	n, _ := strconv.Atoi(s)
	return n
}

func listFiles(root string) ([]string, error) {
	stack := []string{root}
	files := make([]string, 0, 32)

	for len(stack) > 0 {
		n := len(stack) - 1
		dir := stack[n]
		stack = stack[:n]

		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			full := filepath.Join(dir, e.Name())
			if e.IsDir() {
				stack = append(stack, full)
				continue
			}
			files = append(files, full)
		}
	}

	return files, nil
}

func isText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	return true
}

func getString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing argument: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument must be string: %s", key)
	}
	return s, nil
}
