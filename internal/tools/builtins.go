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
	"time"
	"unicode/utf8"
)

const (
	defaultFSReadMaxBytes   = 256 * 1024
	defaultSearchMaxFiles   = 2000
	defaultSearchMaxFileBty = 512 * 1024
)

var workspaceControlPlaneFilenames = map[string]bool{
	"SOUL.MD":     true,
	"RULES.MD":    true,
	"TOOLS.MD":    true,
	"HANDOFF.MD":  true,
	"DEVPLAN.MD":  true,
	"SPECPLAN.MD": true,
}

type CoreOptions struct {
	EnableShellExec bool
}

func RegisterCore(reg *Registry) error {
	return RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true})
}

func RegisterCoreWithOptions(reg *Registry, opts CoreOptions) error {
	if err := reg.Register(ToolSpec{Name: "fs.read", Description: "Read text file", Required: []string{"path"}}, fsRead); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.list", Description: "List directory entries", Required: []string{"path"}}, fsList); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.write", Description: "Write text file", Required: []string{"path", "content"}}, fsWrite); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.edit", Description: "Apply line-based file edits", Required: []string{"path"}}, fsEdit); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "code.search", Description: "Search code with regex", Required: []string{"pattern"}}, codeSearch); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "time.now", Description: "Get current time"}, timeNow); err != nil {
		return err
	}
	if opts.EnableShellExec {
		if err := reg.Register(ToolSpec{Name: "shell.exec", Description: "Run command in sandbox", Required: []string{"command"}}, shellExec); err != nil {
			return err
		}
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
	maxBytes := getIntArg(req.Args, "max_bytes", defaultFSReadMaxBytes)
	if maxBytes <= 0 {
		maxBytes = defaultFSReadMaxBytes
	}
	if len(b) > maxBytes {
		b = b[:maxBytes]
	}
	return map[string]any{"path": path, "content": string(b), "truncated": len(b) == maxBytes}, nil
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
	if err := guardWorkspaceControlPlaneFilename(req.Workspace, resolved, req.AgentID); err != nil {
		return nil, err
	}
	if err := os.WriteFile(resolved, []byte(content), 0o600); err != nil {
		return nil, err
	}
	lines := 0
	if content != "" {
		lines = strings.Count(content, "\n") + 1
	}
	return map[string]any{
		"path":    path,
		"bytes":   len(content),
		"lines":   lines,
		"summary": fmt.Sprintf("wrote %d line(s) to %s", lines, path),
	}, nil
}

type lineEdit struct {
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	NewText   string `json:"newText"`
}

func fsEdit(_ context.Context, req Request) (map[string]any, error) {
	path, err := getString(req.Args, "path")
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
	if err := guardWorkspaceControlPlaneFilename(req.Workspace, resolved, req.AgentID); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}

	// Backward compatibility path.
	if oldText, ok := req.Args["old"]; ok {
		oldS, ok := oldText.(string)
		if !ok {
			return nil, errors.New("old must be string")
		}
		newS, err := getString(req.Args, "new")
		if err != nil {
			return nil, err
		}
		orig := string(b)
		updated := strings.Replace(orig, oldS, newS, 1)
		if updated == orig {
			return nil, errors.New("edit pattern not found")
		}
		if err := os.WriteFile(resolved, []byte(updated), 0o600); err != nil {
			return nil, err
		}
		return map[string]any{"path": path, "updated": true, "mode": "replace_once"}, nil
	}

	rawEdits, ok := req.Args["edits"]
	if !ok {
		return nil, errors.New("missing argument: edits")
	}
	edits, err := parseLineEdits(rawEdits)
	if err != nil {
		return nil, err
	}
	updated, applied, err := applyLineEdits(string(b), edits)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(resolved, []byte(updated), 0o600); err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "updated": true, "applied_edits": applied, "mode": "line_patch"}, nil
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

	files, err := listFiles(root, getIntArg(req.Args, "max_files", defaultSearchMaxFiles))
	if err != nil {
		return nil, err
	}

	maxFileBytes := getIntArg(req.Args, "max_file_bytes", defaultSearchMaxFileBty)
	if maxFileBytes <= 0 {
		maxFileBytes = defaultSearchMaxFileBty
	}

	results := make([]map[string]any, 0)
	for _, p := range files {
		info, err := os.Stat(p)
		if err != nil || info.Size() > int64(maxFileBytes) {
			continue
		}
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
				results = append(results, map[string]any{"path": rel, "line": lineNo, "text": line})
			}
		}
	}

	return map[string]any{"matches": results}, nil
}

func timeNow(_ context.Context, _ Request) (map[string]any, error) {
	now := time.Now().UTC()
	return map[string]any{"rfc3339": now.Format(time.RFC3339), "unix": now.Unix()}, nil
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

func listFiles(root string, maxFiles int) ([]string, error) {
	if maxFiles <= 0 {
		maxFiles = defaultSearchMaxFiles
	}
	stack := []string{root}
	files := make([]string, 0, 64)
	for len(stack) > 0 {
		n := len(stack) - 1
		dir := stack[n]
		stack = stack[:n]
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			name := e.Name()
			if name == ".git" || name == ".openclawssy" {
				continue
			}
			full := filepath.Join(dir, name)
			if e.IsDir() {
				stack = append(stack, full)
				continue
			}
			files = append(files, full)
			if len(files) >= maxFiles {
				return files, nil
			}
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

func parseLineEdits(raw any) ([]lineEdit, error) {
	rows, ok := raw.([]any)
	if !ok {
		return nil, errors.New("edits must be an array")
	}
	edits := make([]lineEdit, 0, len(rows))
	for _, row := range rows {
		obj, ok := row.(map[string]any)
		if !ok {
			return nil, errors.New("each edit must be an object")
		}
		start := getIntArg(obj, "startLine", 0)
		end := getIntArg(obj, "endLine", 0)
		newText, _ := obj["newText"].(string)
		if start <= 0 || end < start {
			return nil, fmt.Errorf("invalid edit range: %d-%d", start, end)
		}
		edits = append(edits, lineEdit{StartLine: start, EndLine: end, NewText: newText})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].StartLine < edits[j].StartLine })
	for i := 1; i < len(edits); i++ {
		if edits[i].StartLine <= edits[i-1].EndLine {
			return nil, errors.New("overlapping edits are not allowed")
		}
	}
	return edits, nil
}

func applyLineEdits(content string, edits []lineEdit) (string, int, error) {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	lineIdx := 1
	applied := 0
	for _, e := range edits {
		if e.EndLine > len(lines) {
			return "", 0, fmt.Errorf("edit range out of bounds: %d-%d", e.StartLine, e.EndLine)
		}
		for lineIdx < e.StartLine {
			out = append(out, lines[lineIdx-1])
			lineIdx++
		}
		if e.NewText != "" {
			out = append(out, strings.Split(e.NewText, "\n")...)
		}
		lineIdx = e.EndLine + 1
		applied++
	}
	for lineIdx <= len(lines) {
		out = append(out, lines[lineIdx-1])
		lineIdx++
	}
	return strings.Join(out, "\n"), applied, nil
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

func getIntArg(args map[string]any, key string, fallback int) int {
	v, ok := args[key]
	if !ok {
		return fallback
	}
	s := fmt.Sprintf("%v", v)
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func guardWorkspaceControlPlaneFilename(workspace, targetAbs, agentID string) error {
	base := strings.ToUpper(filepath.Base(targetAbs))
	if !workspaceControlPlaneFilenames[base] {
		return nil
	}
	within, err := isWithinWorkspace(workspace, targetAbs)
	if err != nil || !within {
		return err
	}
	if agentID == "" {
		agentID = "<agent_id>"
	}
	file := filepath.Base(targetAbs)
	return fmt.Errorf("%s in workspace does not control agent behavior. Control-plane files live at .openclawssy/agents/<agent_id>/%s (for this run: .openclawssy/agents/%s/%s). Edit it via dashboard Agent Files", file, file, agentID, file)
}

func isWithinWorkspace(workspace, target string) (bool, error) {
	wsAbs, err := filepath.Abs(workspace)
	if err != nil {
		return false, err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(wsAbs, targetAbs)
	if err != nil {
		return false, err
	}
	if rel == "." {
		return true, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return !filepath.IsAbs(rel), nil
}
