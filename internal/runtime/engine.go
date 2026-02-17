package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/artifacts"
	"openclawssy/internal/audit"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/policy"
	"openclawssy/internal/sandbox"
	"openclawssy/internal/secrets"
	"openclawssy/internal/tools"
)

var promptDocOrder = []string{"SOUL.md", "RULES.md", "TOOLS.md", "SPECPLAN.md", "DEVPLAN.md", "HANDOFF.md"}

type Engine struct {
	rootDir      string
	workspaceDir string
	agentsDir    string
}

type RunResult struct {
	RunID        string
	FinalText    string
	ArtifactPath string
	DurationMS   int64
	ToolCalls    int
	Provider     string
	Model        string
	Trace        map[string]any
}

type ExecuteInput struct {
	AgentID   string
	Message   string
	Source    string
	SessionID string
}

func NewEngine(rootDir string) (*Engine, error) {
	if rootDir == "" {
		return nil, errors.New("runtime: root dir is required")
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("runtime: resolve root: %w", err)
	}
	return &Engine{
		rootDir:      absRoot,
		workspaceDir: filepath.Join(absRoot, "workspace"),
		agentsDir:    filepath.Join(absRoot, ".openclawssy", "agents"),
	}, nil
}

func (e *Engine) Init(agentID string, force bool) error {
	if err := os.MkdirAll(e.workspaceDir, 0o755); err != nil {
		return fmt.Errorf("runtime: create workspace: %w", err)
	}

	agentRoot := filepath.Join(e.agentsDir, agentID)
	if err := os.MkdirAll(filepath.Join(agentRoot, "memory"), 0o755); err != nil {
		return fmt.Errorf("runtime: create memory dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(agentRoot, "audit"), 0o755); err != nil {
		return fmt.Errorf("runtime: create audit dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(agentRoot, "runs"), 0o755); err != nil {
		return fmt.Errorf("runtime: create runs dir: %w", err)
	}

	files := map[string]string{
		"SOUL.md":     "# SOUL\n\nMission and behavior contract for this agent.\n",
		"RULES.md":    "# RULES\n\n- Follow workspace-only write policy.\n- Respect tool capabilities.\n",
		"TOOLS.md":    "# TOOLS\n\nEnabled core tools: fs.read, fs.list, fs.write, fs.edit, code.search.\n",
		"SPECPLAN.md": "# SPECPLAN\n\nDescribe specs and acceptance requirements before coding.\n",
		"DEVPLAN.md":  "# DEVPLAN\n\n- [ ] Implement task\n- [ ] Add tests\n- [ ] Update handoff\n",
		"HANDOFF.md":  "# HANDOFF\n\nStatus: initialized\n\nNext:\n- Define first run objective.\n",
	}
	for name, body := range files {
		path := filepath.Join(agentRoot, name)
		if !force {
			if _, err := os.Stat(path); err == nil {
				continue
			}
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return fmt.Errorf("runtime: write %s: %w", name, err)
		}
	}

	cfgPath := filepath.Join(e.rootDir, ".openclawssy", "config.json")
	if force || !fileExists(cfgPath) {
		cfg := config.Default()
		cfg.Workspace.Root = e.workspaceDir
		if err := config.Save(cfgPath, cfg); err != nil {
			return fmt.Errorf("runtime: write config: %w", err)
		}
	}

	return nil
}

func (e *Engine) Execute(ctx context.Context, agentID, message string) (RunResult, error) {
	return e.ExecuteWithInput(ctx, ExecuteInput{AgentID: agentID, Message: message})
}

func (e *Engine) ExecuteWithInput(ctx context.Context, in ExecuteInput) (RunResult, error) {
	agentID := strings.TrimSpace(in.AgentID)
	message := strings.TrimSpace(in.Message)
	source := strings.TrimSpace(in.Source)
	sessionID := strings.TrimSpace(in.SessionID)

	if agentID == "" {
		return RunResult{}, errors.New("runtime: agent id is required")
	}
	if message == "" {
		return RunResult{}, errors.New("runtime: message is required")
	}

	if err := os.MkdirAll(e.workspaceDir, 0o755); err != nil {
		return RunResult{}, fmt.Errorf("runtime: create workspace dir: %w", err)
	}

	cfg, err := config.LoadOrDefault(filepath.Join(e.rootDir, ".openclawssy", "config.json"))
	if err != nil {
		return RunResult{}, fmt.Errorf("runtime: load config: %w", err)
	}

	runID := fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
	docs, err := e.loadPromptDocs(agentID)
	if err != nil {
		return RunResult{}, err
	}

	auditPath := filepath.Join(e.agentsDir, agentID, "audit", "events.jsonl")
	aud, err := audit.NewLogger(auditPath, policy.RedactValue)
	if err != nil {
		return RunResult{}, fmt.Errorf("runtime: init audit logger: %w", err)
	}

	startEvent := map[string]any{"run_id": runID, "agent_id": agentID, "message": message}
	if source != "" {
		startEvent["source"] = source
	}
	if sessionID != "" {
		startEvent["session_id"] = sessionID
	}
	_ = aud.LogEvent(ctx, audit.EventRunStart, startEvent)

	allowedTools := e.allowedTools(cfg)
	traceCollector := newRunTraceCollector(runID, sessionID, source, message)
	ctx = withRunTraceCollector(ctx, traceCollector)
	enforcer := policy.NewEnforcer(e.workspaceDir, map[string][]string{agentID: allowedTools})
	registry := tools.NewRegistry(enforcer, aud)
	if err := tools.RegisterCoreWithOptions(registry, tools.CoreOptions{
		EnableShellExec: cfg.Shell.EnableExec && cfg.Sandbox.Active && strings.ToLower(cfg.Sandbox.Provider) != "none",
	}); err != nil {
		return RunResult{}, fmt.Errorf("runtime: register core tools: %w", err)
	}

	var provider sandbox.Provider
	if cfg.Sandbox.Active {
		provider, err = sandbox.NewProvider(cfg.Sandbox.Provider, e.workspaceDir)
		if err != nil {
			return RunResult{}, fmt.Errorf("runtime: create sandbox provider: %w", err)
		}
		if err := provider.Start(ctx); err != nil {
			return RunResult{}, fmt.Errorf("runtime: start sandbox provider: %w", err)
		}
		defer provider.Stop()
		if cfg.Shell.EnableExec && sandbox.ShellExecAllowed(provider) {
			registry.SetShellExecutor(&sandboxShellExecutor{provider: provider})
		}
	}

	secretStore, _ := secrets.NewStore(cfg)
	lookup := func(name string) (string, bool, error) {
		if secretStore == nil {
			return "", false, nil
		}
		return secretStore.Get(name)
	}

	model, err := NewProviderModel(cfg, lookup)
	if err != nil {
		return RunResult{}, err
	}

	runner := agent.Runner{
		Model:             model,
		ToolExecutor:      &RegistryExecutor{Registry: registry, AgentID: agentID, Workspace: e.workspaceDir},
		MaxToolIterations: 8,
	}

	modelMessages := []agent.ChatMessage{{Role: "user", Content: message}}
	if sessionID != "" {
		history, historyErr := e.loadSessionMessages(sessionID, 200)
		if historyErr != nil {
			return RunResult{}, historyErr
		}
		if len(history) > 0 {
			modelMessages = history
		}
	}
	if len(modelMessages) == 0 || !hasTrailingUserMessage(modelMessages, message) {
		modelMessages = append(modelMessages, agent.ChatMessage{Role: "user", Content: message})
	}

	start := time.Now().UTC()
	out, runErr := runner.Run(ctx, agent.RunInput{
		AgentID:           agentID,
		RunID:             runID,
		Message:           message,
		Messages:          modelMessages,
		ArtifactDocs:      docs,
		PerFileByteLimit:  16 * 1024,
		MaxToolIterations: 8,
		ToolTimeoutMS:     int((45 * time.Second) / time.Millisecond),
		AllowedTools:      allowedTools,
	})

	artifactPath := ""
	if runErr == nil {
		durationMS := time.Since(start).Milliseconds()
		toolCount := len(out.ToolCalls)
		toolLines := make([]string, 0, len(out.ToolCalls))
		for _, rec := range out.ToolCalls {
			b, mErr := json.Marshal(rec)
			if mErr != nil {
				runErr = mErr
				break
			}
			toolLines = append(toolLines, string(b))
		}
		if runErr == nil {
			artifactPath, err = artifacts.WriteRunBundleV1(e.rootDir, agentID, runID, artifacts.BundleV1Input{
				Input:     map[string]any{"agent_id": agentID, "message": message},
				PromptMD:  out.Prompt,
				ToolCalls: toolLines,
				OutputMD:  out.FinalText,
				Meta: map[string]any{
					"started_at":      out.StartedAt,
					"completed_at":    out.CompletedAt,
					"duration_ms":     durationMS,
					"tool_call_count": toolCount,
					"provider":        model.ProviderName(),
					"model":           model.ModelName(),
				},
				MirrorJSON: true,
			})
		}
		if err != nil {
			runErr = err
		}
		if runErr == nil && sessionID != "" {
			if err := e.appendRunConversation(sessionID, runID, out); err != nil {
				runErr = err
			}
		}
	}

	fields := map[string]any{"run_id": runID, "agent_id": agentID}
	if source != "" {
		fields["source"] = source
	}
	if sessionID != "" {
		fields["session_id"] = sessionID
	}
	if runErr != nil {
		fields["error"] = runErr.Error()
	} else {
		fields["artifact_path"] = artifactPath
	}
	_ = aud.LogEvent(ctx, audit.EventRunEnd, fields)

	traceCollector.RecordToolExecution(out.ToolCalls)
	traceSnapshot := traceCollector.Snapshot()

	if runErr != nil {
		return RunResult{
			RunID:        runID,
			ArtifactPath: artifactPath,
			DurationMS:   time.Since(start).Milliseconds(),
			ToolCalls:    len(out.ToolCalls),
			Provider:     model.ProviderName(),
			Model:        model.ModelName(),
			Trace:        traceSnapshot,
		}, runErr
	}

	return RunResult{
		RunID:        runID,
		FinalText:    out.FinalText,
		ArtifactPath: artifactPath,
		DurationMS:   time.Since(start).Milliseconds(),
		ToolCalls:    len(out.ToolCalls),
		Provider:     model.ProviderName(),
		Model:        model.ModelName(),
		Trace:        traceSnapshot,
	}, nil
}

func (e *Engine) loadSessionMessages(sessionID string, limit int) ([]agent.ChatMessage, error) {
	store, err := chatstore.NewStore(e.agentsDir)
	if err != nil {
		return nil, fmt.Errorf("runtime: init chat store: %w", err)
	}
	history, err := store.ReadRecentMessages(sessionID, chatstore.ClampHistoryCount(limit, 200))
	if err != nil {
		return nil, fmt.Errorf("runtime: read chat history: %w", err)
	}
	out := make([]agent.ChatMessage, 0, len(history))
	for _, msg := range history {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role == "" {
			role = "user"
		}
		if role == "tool" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		out = append(out, agent.ChatMessage{
			Role:       role,
			Content:    content,
			Name:       strings.TrimSpace(msg.ToolName),
			ToolCallID: strings.TrimSpace(msg.ToolCallID),
			TS:         msg.TS,
		})
	}
	return out, nil
}

func hasTrailingUserMessage(messages []agent.ChatMessage, currentMessage string) bool {
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	role := strings.ToLower(strings.TrimSpace(last.Role))
	if role != "" && role != "user" {
		return false
	}
	return strings.TrimSpace(last.Content) == strings.TrimSpace(currentMessage)
}

func (e *Engine) appendRunConversation(sessionID, runID string, out agent.RunOutput) error {
	store, err := chatstore.NewStore(e.agentsDir)
	if err != nil {
		return fmt.Errorf("runtime: init chat store: %w", err)
	}
	for _, rec := range out.ToolCalls {
		payload, marshalErr := json.Marshal(map[string]any{
			"tool":   rec.Request.Name,
			"id":     rec.Request.ID,
			"output": rec.Result.Output,
			"error":  rec.Result.Error,
		})
		if marshalErr != nil {
			return fmt.Errorf("runtime: marshal tool result: %w", marshalErr)
		}
		ts := rec.CompletedAt
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		if err := store.AppendMessage(sessionID, chatstore.Message{
			Role:       "tool",
			Content:    string(payload),
			TS:         ts,
			RunID:      runID,
			ToolCallID: rec.Request.ID,
			ToolName:   rec.Request.Name,
		}); err != nil {
			return fmt.Errorf("runtime: append tool message: %w", err)
		}
	}
	if strings.TrimSpace(out.FinalText) != "" {
		ts := out.CompletedAt
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		if err := store.AppendMessage(sessionID, chatstore.Message{Role: "assistant", Content: out.FinalText, TS: ts, RunID: runID}); err != nil {
			return fmt.Errorf("runtime: append assistant message: %w", err)
		}
	}
	return nil
}

func (e *Engine) loadPromptDocs(agentID string) ([]agent.ArtifactDoc, error) {
	agentRoot := filepath.Join(e.agentsDir, agentID)
	docs := make([]agent.ArtifactDoc, 0, len(promptDocOrder)+2)
	for _, name := range promptDocOrder {
		path := filepath.Join(agentRoot, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("runtime: read %s: %w", name, err)
		}
		docs = append(docs, agent.ArtifactDoc{Name: name, Content: string(data)})
	}
	docs = append(docs, agent.ArtifactDoc{Name: "RUNTIME_CONTEXT.md", Content: runtimeContextDoc(e.workspaceDir)})
	docs = append(docs, agent.ArtifactDoc{Name: "TOOL_CALLING_BEST_PRACTICES.md", Content: toolCallingBestPracticesDoc()})
	return docs, nil
}

func runtimeContextDoc(workspaceDir string) string {
	return fmt.Sprintf(
		"# RUNTIME_CONTEXT\n\n- Workspace root: %s\n- File tools (fs.read/fs.list/fs.write/fs.edit/code.search) can only access paths inside workspace root.\n- Paths outside workspace (for example /home, ~, ..) are blocked by policy.\n- If shell.exec is enabled by policy, run shell commands through `bash -lc` in shell.exec args.\n- Paths outside workspace (for example /home, ~, ..) are blocked by policy even when using shell.exec.\n- If the user asks about files in home directory, explain this limitation and offer to list the workspace instead.\n- Keep responses task-focused; do not mention HANDOFF/SPECPLAN/DEVPLAN unless the user explicitly asks about them.\n",
		workspaceDir,
	)
}

func toolCallingBestPracticesDoc() string {
	return "# TOOL_CALLING_BEST_PRACTICES\n\n- Use only registered tool names: fs.read, fs.list, fs.write, fs.edit, code.search, time.now, shell.exec.\n- Preferred format for tool calls is a fenced JSON object with tool_name and arguments.\n- Example:\n```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```\n- For bash commands use shell.exec with command=`bash` and args=[\"-lc\", \"<script>\"].\n- Do not invent tool names (for example time.sleep is invalid).\n- Do not claim file edits or command results until a matching tool.result is observed.\n- Keep one clear tool call at a time, then continue after reading the result.\n"
}

type RegistryExecutor struct {
	Registry  *tools.Registry
	AgentID   string
	Workspace string
}

func (r *RegistryExecutor) Execute(ctx context.Context, call agent.ToolCallRequest) (agent.ToolCallResult, error) {
	if r == nil || r.Registry == nil {
		return agent.ToolCallResult{ID: call.ID}, errors.New("runtime: tool registry is not configured")
	}

	args := map[string]any{}
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return agent.ToolCallResult{ID: call.ID}, fmt.Errorf("runtime: invalid tool args: %w", err)
		}
	}
	args = normalizeToolArgs(call.Name, args)

	res, err := r.Registry.Execute(ctx, r.AgentID, call.Name, r.Workspace, args)
	if err != nil {
		return agent.ToolCallResult{ID: call.ID}, err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return agent.ToolCallResult{ID: call.ID}, err
	}
	return agent.ToolCallResult{ID: call.ID, Output: string(b)}, nil
}

func normalizeToolArgs(toolName string, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}

	ensurePath := func() {
		if getStringArg(args, "path") != "" {
			args["path"] = sanitizePathArg(getStringArg(args, "path"))
			return
		}
		for _, key := range []string{"file", "filename", "target", "name"} {
			if value := getStringArg(args, key); value != "" {
				args["path"] = sanitizePathArg(value)
				return
			}
		}
	}

	switch toolName {
	case "fs.list":
		ensurePath()
		if getStringArg(args, "path") == "" {
			args["path"] = "."
		}
	case "fs.read":
		ensurePath()
	case "fs.write":
		ensurePath()
		if getStringArg(args, "content") == "" {
			for _, key := range []string{"text", "body", "code", "value", "data", "newText"} {
				if value := getStringArg(args, key); value != "" {
					args["content"] = value
					break
				}
			}
		}
		if getStringArg(args, "content") == "" {
			path := getStringArg(args, "path")
			if idx := strings.Index(path, ","); idx > 0 {
				pathPart := strings.TrimSpace(path[:idx])
				rest := strings.TrimSpace(path[idx+1:])
				pathPart = trimMatchingQuotes(pathPart)
				pathPart = strings.Trim(pathPart, `"'`)
				rest = strings.TrimSpace(strings.TrimPrefix(rest, "\"\"\""))
				rest = strings.TrimSpace(strings.TrimSuffix(rest, "\"\"\""))
				rest = trimMatchingQuotes(rest)
				if pathPart != "" {
					args["path"] = pathPart
				}
				if rest != "" {
					args["content"] = rest
				}
			}
		}
	case "fs.edit":
		ensurePath()
		if getStringArg(args, "old") == "" {
			for _, key := range []string{"find", "from"} {
				if value := getStringArg(args, key); value != "" {
					args["old"] = value
					break
				}
			}
		}
		if getStringArg(args, "new") == "" {
			for _, key := range []string{"replace", "to", "newText", "value"} {
				if value := getStringArg(args, key); value != "" {
					args["new"] = value
					break
				}
			}
		}
	case "code.search":
		if getStringArg(args, "pattern") == "" {
			if value := getStringArg(args, "query"); value != "" {
				args["pattern"] = value
			}
		}
	case "shell.exec":
		if getStringArg(args, "command") == "" {
			if value := getStringArg(args, "cmd"); value != "" {
				args["command"] = value
			} else if value := getStringArg(args, "path"); value != "" {
				args["command"] = value
			}
		}
		command := getStringArg(args, "command")
		if command != "" && strings.Contains(command, " ") && len(getStringSliceArg(args, "args")) == 0 {
			args["command"] = "bash"
			args["args"] = []string{"-lc", command}
		}
	}

	return args
}

func getStringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func getStringSliceArg(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	out := make([]string, 0)
	switch raw := v.(type) {
	case []string:
		for _, item := range raw {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
	case []any:
		for _, item := range raw {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func trimMatchingQuotes(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 2 {
		return v
	}
	if (strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`)) || (strings.HasPrefix(v, `'`) && strings.HasSuffix(v, `'`)) {
		return strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}

func sanitizePathArg(path string) string {
	path = strings.TrimSpace(path)
	path = trimMatchingQuotes(path)
	path = strings.Trim(path, "`")
	if path == "```" {
		return ""
	}
	if strings.HasPrefix(path, "```") {
		path = strings.TrimPrefix(path, "```")
	}
	if strings.HasSuffix(path, "```") {
		path = strings.TrimSuffix(path, "```")
	}
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return ""
	}
	return path
}

func (e *Engine) allowedTools(cfg config.Config) []string {
	toolsList := []string{"fs.read", "fs.list", "fs.write", "fs.edit", "code.search", "time.now"}
	if cfg.Shell.EnableExec && cfg.Sandbox.Active && strings.ToLower(cfg.Sandbox.Provider) != "none" {
		toolsList = append(toolsList, "shell.exec")
	}
	return toolsList
}

type sandboxShellExecutor struct {
	provider sandbox.Provider
}

func (s *sandboxShellExecutor) Exec(_ context.Context, command string, args []string) (string, string, int, error) {
	result, err := s.provider.Exec(sandbox.Command{Name: command, Args: args})
	return result.Stdout, result.Stderr, result.ExitCode, err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
