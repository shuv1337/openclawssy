package policy

import (
	"fmt"
	"strings"

	"openclawssy/internal/toolparse"
)

type CapabilityError struct {
	AgentID string
	Tool    string
}

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("capability denied: agent=%q tool=%q", e.AgentID, e.Tool)
}

type PathError struct {
	Path   string
	Reason string
}

func (e *PathError) Error() string {
	return fmt.Sprintf("path denied: %s (%s)", e.Path, e.Reason)
}

type Enforcer struct {
	Workspace    string
	Capabilities map[string]map[string]bool
}

func NewEnforcer(workspace string, grants map[string][]string) *Enforcer {
	m := make(map[string]map[string]bool, len(grants))
	for agent, tools := range grants {
		set := make(map[string]bool, len(tools))
		for _, tool := range tools {
			canonical := canonicalCapabilityTool(tool)
			if canonical == "" {
				continue
			}
			set[canonical] = true
		}
		m[agent] = set
	}

	return &Enforcer{Workspace: workspace, Capabilities: m}
}

func (e *Enforcer) CheckTool(agentID, tool string) error {
	canonical := canonicalCapabilityTool(tool)
	agentCaps, ok := e.Capabilities[agentID]
	if !ok {
		return &CapabilityError{AgentID: agentID, Tool: canonical}
	}
	if !agentCaps[canonical] {
		return &CapabilityError{AgentID: agentID, Tool: canonical}
	}
	return nil
}

func canonicalCapabilityTool(tool string) string {
	candidate := strings.TrimSpace(tool)
	if candidate == "" {
		return ""
	}
	if canonical, ok := toolparse.CanonicalToolName(candidate); ok {
		return canonical
	}
	return strings.ToLower(candidate)
}
