package toolparse

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"openclawssy/internal/agent"
)

var toolAliases = map[string]string{
	"fs.read":       "fs.read",
	"fs.list":       "fs.list",
	"fs.write":      "fs.write",
	"fs.edit":       "fs.edit",
	"code.search":   "code.search",
	"time.now":      "time.now",
	"shell.exec":    "shell.exec",
	"bash.exec":     "shell.exec",
	"terminal.exec": "shell.exec",
	"terminal.run":  "shell.exec",
}

type Extraction struct {
	RawSnippet      string
	ParsedToolName  string
	ParsedArguments json.RawMessage
	Accepted        bool
	Reason          string
}

type ParseResult struct {
	Calls       []agent.ToolCallRequest
	Extractions []Extraction
}

func ParseStrict(content string, allowedTools []string, maxCalls int) ParseResult {
	if maxCalls <= 0 {
		maxCalls = 1
	}
	re := regexp.MustCompile("(?is)```(?:json|tool)\\s*([\\s\\S]*?)```")
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return ParseResult{}
	}

	result := ParseResult{
		Calls:       make([]agent.ToolCallRequest, 0, maxCalls),
		Extractions: make([]Extraction, 0, len(matches)),
	}
	for i, match := range matches {
		extraction := Extraction{}
		if len(match) > 0 {
			extraction.RawSnippet = strings.TrimSpace(match[0])
		}
		if len(match) < 2 {
			extraction.Reason = "invalid fenced block"
			result.Extractions = append(result.Extractions, extraction)
			continue
		}

		parsed, accepted := parseStrictJSONToolCall(strings.TrimSpace(match[1]), i+1, allowedTools)
		extraction.ParsedToolName = parsed.ParsedToolName
		extraction.ParsedArguments = parsed.ParsedArguments
		extraction.Accepted = accepted
		extraction.Reason = parsed.Reason
		result.Extractions = append(result.Extractions, extraction)
		if accepted {
			result.Calls = append(result.Calls, parsed.Call)
			if len(result.Calls) >= maxCalls {
				break
			}
		}
	}
	return result
}

func CanonicalToolName(name string) (string, bool) {
	candidate := strings.ToLower(strings.TrimSpace(name))
	if candidate == "" {
		return "", false
	}
	toolName, ok := toolAliases[candidate]
	return toolName, ok
}

func IsAllowed(toolName string, allowed []string) bool {
	if strings.TrimSpace(toolName) == "" || toolName == "tool.result" {
		return false
	}
	if len(allowed) == 0 {
		return true
	}
	for _, raw := range allowed {
		candidate, ok := CanonicalToolName(raw)
		if !ok {
			candidate = strings.ToLower(strings.TrimSpace(raw))
		}
		if candidate == toolName {
			return true
		}
	}
	return false
}

type parsedCall struct {
	Call            agent.ToolCallRequest
	ParsedToolName  string
	ParsedArguments json.RawMessage
	Reason          string
}

func parseStrictJSONToolCall(raw string, ordinal int, allowedTools []string) (parsedCall, bool) {
	if strings.TrimSpace(raw) == "" {
		return parsedCall{Reason: "empty fenced block"}, false
	}

	rawObj := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &rawObj); err != nil {
		return parsedCall{Reason: "invalid json"}, false
	}
	argsRaw, hasArgs := rawObj["arguments"]
	if !hasArgs {
		return parsedCall{Reason: "missing arguments field"}, false
	}

	var payload struct {
		ToolName string `json:"tool_name"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return parsedCall{Reason: "invalid tool payload"}, false
	}

	toolName, ok := CanonicalToolName(payload.ToolName)
	if !ok || toolName == "" {
		return parsedCall{ParsedToolName: strings.TrimSpace(payload.ToolName), Reason: "unsupported tool name"}, false
	}
	if toolName == "tool.result" {
		return parsedCall{ParsedToolName: toolName, Reason: "tool.result is output-only"}, false
	}
	if !IsAllowed(toolName, allowedTools) {
		return parsedCall{ParsedToolName: toolName, Reason: "tool not in allowlist"}, false
	}

	args := map[string]any{}
	if len(argsRaw) > 0 && string(argsRaw) != "null" {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return parsedCall{ParsedToolName: toolName, Reason: "invalid arguments object"}, false
		}
	}
	argBytes, _ := json.Marshal(args)

	return parsedCall{
		Call: agent.ToolCallRequest{
			ID:        fmt.Sprintf("tool-json-%d", ordinal),
			Name:      toolName,
			Arguments: argBytes,
		},
		ParsedToolName:  toolName,
		ParsedArguments: argBytes,
	}, true
}
