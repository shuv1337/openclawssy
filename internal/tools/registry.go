package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Policy interface {
	CheckTool(agentID, tool string) error
	ResolveReadPath(workspace, target string) (string, error)
	ResolveWritePath(workspace, target string) (string, error)
}

type Auditor interface {
	LogEvent(ctx context.Context, eventType string, fields map[string]any) error
}

type ToolSpec struct {
	Name        string
	Description string
	Required    []string
}

type ShellExecutor interface {
	Exec(ctx context.Context, command string, args []string) (stdout string, stderr string, exitCode int, err error)
}

type Handler func(ctx context.Context, req Request) (map[string]any, error)

type Request struct {
	AgentID   string
	Tool      string
	Workspace string
	Args      map[string]any
	Policy    Policy
	Shell     ShellExecutor
}

type registryItem struct {
	spec    ToolSpec
	handler Handler
}

type Registry struct {
	policy Policy
	audit  Auditor
	shell  ShellExecutor
	mu     sync.RWMutex
	tools  map[string]registryItem
}

func NewRegistry(policy Policy, audit Auditor) *Registry {
	return &Registry{
		policy: policy,
		audit:  audit,
		tools:  make(map[string]registryItem),
	}
}

func (r *Registry) SetShellExecutor(shell ShellExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shell = shell
}

func (r *Registry) Register(spec ToolSpec, handler Handler) error {
	if spec.Name == "" {
		return errors.New("tool name is required")
	}
	if handler == nil {
		return errors.New("tool handler is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[spec.Name]; exists {
		return fmt.Errorf("tool already registered: %s", spec.Name)
	}
	r.tools[spec.Name] = registryItem{spec: spec, handler: handler}
	return nil
}

func (r *Registry) List() []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]ToolSpec, 0, len(r.tools))
	for _, item := range r.tools {
		out = append(out, item.spec)
	}
	return out
}

func (r *Registry) Execute(ctx context.Context, agentID, name, workspace string, args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}

	_ = r.emit(ctx, "tool.call", map[string]any{
		"agent_id": agentID,
		"tool":     name,
		"args":     args,
	})

	r.mu.RLock()
	item, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		err := &ToolError{Code: ErrCodeNotFound, Tool: name, Message: "tool not registered"}
		_ = r.emit(ctx, "tool.result", map[string]any{"agent_id": agentID, "tool": name, "error": err.Error()})
		return nil, err
	}

	for _, required := range item.spec.Required {
		if _, ok := args[required]; !ok {
			err := &ToolError{Code: ErrCodeInvalidInput, Tool: name, Message: "missing required field: " + required}
			_ = r.emit(ctx, "tool.result", map[string]any{"agent_id": agentID, "tool": name, "error": err.Error()})
			return nil, err
		}
	}

	if r.policy != nil {
		if err := r.policy.CheckTool(agentID, name); err != nil {
			denied := wrapError(ErrCodePolicyDenied, name, err)
			_ = r.emit(ctx, "policy.denied", map[string]any{
				"agent_id": agentID,
				"tool":     name,
				"error":    denied.Error(),
			})
			_ = r.emit(ctx, "tool.result", map[string]any{"agent_id": agentID, "tool": name, "error": denied.Error()})
			return nil, denied
		}
	}

	res, err := item.handler(ctx, Request{
		AgentID:   agentID,
		Tool:      name,
		Workspace: workspace,
		Args:      args,
		Policy:    r.policy,
		Shell:     r.shell,
	})
	if err != nil {
		errCode := ErrCodeExecution
		if errors.Is(err, context.DeadlineExceeded) {
			errCode = ErrCodeTimeout
		}
		execErr := wrapError(errCode, name, err)
		_ = r.emit(ctx, "tool.result", map[string]any{"agent_id": agentID, "tool": name, "error": execErr.Error()})
		return nil, execErr
	}

	_ = r.emit(ctx, "tool.result", map[string]any{
		"agent_id": agentID,
		"tool":     name,
		"result":   res,
	})
	return res, nil
}

func (r *Registry) emit(ctx context.Context, eventType string, fields map[string]any) error {
	if r.audit == nil {
		return nil
	}
	return r.audit.LogEvent(ctx, eventType, fields)
}
