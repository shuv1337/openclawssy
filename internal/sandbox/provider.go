package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var (
	ErrExecDenied      = errors.New("sandbox: exec denied")
	ErrNotStarted      = errors.New("sandbox: provider not started")
	ErrUnknownProvider = errors.New("sandbox: unknown provider")
)

type Command struct {
	Name string
	Args []string
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Provider interface {
	Start(runCtx context.Context) error
	Exec(cmd Command) (Result, error)
	Stop() error
}

type providerState interface {
	providerName() string
	isStarted() bool
}

func NewProvider(name string, workspace string) (Provider, error) {
	switch name {
	case "none":
		return &NoneProvider{}, nil
	case "local":
		return NewLocalProvider(workspace)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, name)
	}
}

func ShellExecAllowed(active Provider) bool {
	if active == nil {
		return false
	}
	s, ok := active.(providerState)
	if !ok {
		return false
	}
	return s.providerName() != "none" && s.isStarted()
}

type NoneProvider struct {
	mu      sync.RWMutex
	started bool
}

func (p *NoneProvider) Start(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = true
	return nil
}

func (p *NoneProvider) Exec(Command) (Result, error) {
	return Result{}, ErrExecDenied
}

func (p *NoneProvider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = false
	return nil
}

func (p *NoneProvider) providerName() string { return "none" }

func (p *NoneProvider) isStarted() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.started
}

type LocalProvider struct {
	workspace string

	mu      sync.RWMutex
	started bool
	runCtx  context.Context
	cancel  context.CancelFunc
}

func NewLocalProvider(workspace string) (*LocalProvider, error) {
	if workspace == "" {
		return nil, errors.New("sandbox: workspace is required")
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve workspace: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("sandbox: stat workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("sandbox: workspace must be a directory")
	}
	return &LocalProvider{workspace: abs}, nil
}

func (p *LocalProvider) Start(runCtx context.Context) error {
	if runCtx == nil {
		runCtx = context.Background()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runCtx, p.cancel = context.WithCancel(runCtx)
	p.started = true
	return nil
}

func (p *LocalProvider) Exec(cmd Command) (Result, error) {
	p.mu.RLock()
	started := p.started
	runCtx := p.runCtx
	workspace := p.workspace
	p.mu.RUnlock()

	if !started {
		return Result{}, ErrNotStarted
	}
	if cmd.Name == "" {
		return Result{}, errors.New("sandbox: command name is required")
	}

	proc := exec.CommandContext(runCtx, cmd.Name, cmd.Args...)
	proc.Dir = workspace

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr

	err := proc.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
	if err == nil {
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, err
	}

	result.ExitCode = -1
	return result, err
}

func (p *LocalProvider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
	}
	p.runCtx = nil
	p.cancel = nil
	p.started = false
	return nil
}

func (p *LocalProvider) providerName() string { return "local" }

func (p *LocalProvider) isStarted() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.started
}
