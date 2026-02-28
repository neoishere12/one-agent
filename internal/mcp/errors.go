package mcp

import "fmt"

const (
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternal       = -32000
)

type toolError struct {
	Code    int
	Message string
	Cause   error
}

func (e *toolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *toolError) unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func invalidRequest(message string) *toolError {
	return &toolError{Code: errCodeInvalidRequest, Message: message}
}

func invalidParams(message string, cause error) *toolError {
	return &toolError{Code: errCodeInvalidParams, Message: message, Cause: cause}
}

func methodNotFound(method string) *toolError {
	return &toolError{
		Code:    errCodeMethodNotFound,
		Message: fmt.Sprintf("unknown MCP tool %q", method),
	}
}

func internalError(message string, cause error) *toolError {
	return &toolError{Code: errCodeInternal, Message: message, Cause: cause}
}
