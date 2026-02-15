package tools

import "fmt"

type ErrorCode string

const (
	ErrCodeNotFound     ErrorCode = "tool_not_found"
	ErrCodeInvalidInput ErrorCode = "invalid_input"
	ErrCodePolicyDenied ErrorCode = "policy_denied"
	ErrCodeExecution    ErrorCode = "tool_execution_failed"
)

type ToolError struct {
	Code    ErrorCode
	Tool    string
	Message string
}

func (e *ToolError) Error() string {
	if e.Tool == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s (%s): %s", e.Code, e.Tool, e.Message)
}

func wrapError(code ErrorCode, tool string, err error) error {
	if err == nil {
		return nil
	}
	return &ToolError{Code: code, Tool: tool, Message: err.Error()}
}
