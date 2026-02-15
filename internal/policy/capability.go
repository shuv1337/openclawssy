package policy

import "fmt"

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
			set[tool] = true
		}
		m[agent] = set
	}

	return &Enforcer{Workspace: workspace, Capabilities: m}
}

func (e *Enforcer) CheckTool(agentID, tool string) error {
	agentCaps, ok := e.Capabilities[agentID]
	if !ok {
		return &CapabilityError{AgentID: agentID, Tool: tool}
	}
	if !agentCaps[tool] {
		return &CapabilityError{AgentID: agentID, Tool: tool}
	}
	return nil
}
