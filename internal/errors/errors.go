// Package errors provides the typed error model used across calendarctl.
//
// Errors are designed to be safe to log: they never contain private calendar
// payloads (titles, notes, attendees). Every error has a stable machine code,
// a human message, and an exit code suitable for shell scripting.
package errors

import (
	"errors"
	"fmt"
)

// Exit codes. They are part of the public CLI contract.
const (
	ExitOK           = 0
	ExitGeneral      = 1
	ExitUsage        = 2
	ExitNotFound     = 3
	ExitPermission   = 4
	ExitAlreadyExist = 5
	ExitCancelled    = 6
	ExitUnavailable  = 7
	ExitInvalidInput = 8
)

// Code is a stable, machine-readable error identifier.
type Code string

const (
	CodeGeneral      Code = "internal_error"
	CodeUsage        Code = "usage_error"
	CodeNotFound     Code = "not_found"
	CodePermission   Code = "permission_denied"
	CodeAlreadyExist Code = "already_exists"
	CodeCancelled    Code = "cancelled"
	CodeUnavailable  Code = "unavailable"
	CodeInvalidInput Code = "invalid_input"
	CodeAmbiguous    Code = "ambiguous"
	CodeDuplicate    Code = "duplicate"
)

// Error is the structured error type used throughout the application.
type Error struct {
	Code    Code
	Message string
	Hint    string
	Op      string
	Err     error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error { return e.Err }

// ExitCode maps an error to a process exit status.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var ce *Error
	if errors.As(err, &ce) {
		switch ce.Code {
		case CodeUsage:
			return ExitUsage
		case CodeNotFound:
			return ExitNotFound
		case CodePermission:
			return ExitPermission
		case CodeAlreadyExist, CodeDuplicate:
			return ExitAlreadyExist
		case CodeCancelled:
			return ExitCancelled
		case CodeUnavailable:
			return ExitUnavailable
		case CodeInvalidInput, CodeAmbiguous:
			return ExitInvalidInput
		}
	}
	return ExitGeneral
}

// ExitCodes documents the exit status contract.
var ExitCodes = []struct {
	Code int
	Name string
}{
	{ExitOK, "success"},
	{ExitGeneral, "general error"},
	{ExitUsage, "usage error"},
	{ExitNotFound, "not found"},
	{ExitPermission, "permission denied"},
	{ExitAlreadyExist, "already exists / duplicate"},
	{ExitCancelled, "cancelled by user"},
	{ExitUnavailable, "provider unavailable"},
	{ExitInvalidInput, "invalid input"},
}

func New(code Code, op, message string) *Error {
	return &Error{Code: code, Op: op, Message: message}
}

func Wrap(code Code, op string, err error) *Error {
	return &Error{Code: code, Op: op, Message: err.Error(), Err: err}
}

func NotFound(op, what string) *Error {
	return &Error{Code: CodeNotFound, Op: op, Message: what + " not found"}
}

func InvalidInput(op, message string) *Error {
	return &Error{Code: CodeInvalidInput, Op: op, Message: message}
}

func Permission(op, message, hint string) *Error {
	return &Error{Code: CodePermission, Op: op, Message: message, Hint: hint}
}

func Unavailable(op, message string) *Error {
	return &Error{Code: CodeUnavailable, Op: op, Message: message}
}

func AlreadyExists(op, message string) *Error {
	return &Error{Code: CodeAlreadyExist, Op: op, Message: message}
}

func Cancelled(op string) *Error {
	return &Error{Code: CodeCancelled, Op: op, Message: "cancelled"}
}

func WithHint(err *Error, hint string) *Error {
	err.Hint = hint
	return err
}

// Render formats an error for stderr. Color is applied by the CLI layer.
func Render(err error) string {
	if err == nil {
		return ""
	}
	var ce *Error
	if errors.As(err, &ce) {
		s := fmt.Sprintf("error: %s", ce.Message)
		if ce.Hint != "" {
			s += "\nhint: " + ce.Hint
		}
		return s
	}
	return "error: " + err.Error()
}
