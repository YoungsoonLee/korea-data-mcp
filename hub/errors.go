package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Code classifies a tool error so agents can decide what to do next
// (fix arguments, retry later, ask the user for a key, ...).
type Code string

const (
	CodeInvalidArgument Code = "INVALID_ARGUMENT" // caller should fix the arguments
	CodeNotFound        Code = "NOT_FOUND"        // the query was valid but matched nothing
	CodeUnauthenticated Code = "UNAUTHENTICATED"  // API key missing, invalid or expired
	CodePermission      Code = "PERMISSION_DENIED"
	CodeRateLimited     Code = "RATE_LIMITED" // quota or rate limit exceeded; retry later
	CodeUnavailable     Code = "UNAVAILABLE"  // upstream down or timed out; retry later
	CodeUpstream        Code = "UPSTREAM_ERROR"
	CodeInternal        Code = "INTERNAL"
)

// Error is the single error shape every tool returns. Its Error() text is JSON,
// which the SDK places in the tool result content with isError=true, so the
// model sees a stable, parseable structure.
type Error struct {
	Code         Code   `json:"code"`
	Message      string `json:"message"`
	Hint         string `json:"hint,omitempty"`
	Retryable    bool   `json:"retryable"`
	Provider     string `json:"provider,omitempty"`
	UpstreamCode string `json:"upstream_code,omitempty"` // provider's own result code, e.g. DART "013"
	Err          error  `json:"-"`
}

func (e *Error) Error() string {
	b, _ := json.Marshal(e)
	return string(b)
}

func (e *Error) Unwrap() error { return e.Err }

// Errorf creates an Error. Retryable defaults from the code.
func Errorf(code Code, format string, args ...any) *Error {
	return &Error{
		Code:      code,
		Message:   fmt.Sprintf(format, args...),
		Retryable: code == CodeRateLimited || code == CodeUnavailable,
	}
}

func (e *Error) WithHint(hint string) *Error {
	e.Hint = hint
	return e
}

func (e *Error) WithUpstreamCode(code string) *Error {
	e.UpstreamCode = code
	return e
}

func (e *Error) Wrap(err error) *Error {
	e.Err = err
	return e
}

// AsError converts any error into an *Error, preserving one if already present.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var he *Error
	if errors.As(err, &he) {
		return he
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Errorf(CodeUnavailable, "request timed out").Wrap(err)
	}
	if errors.Is(err, context.Canceled) {
		return Errorf(CodeUnavailable, "request canceled").Wrap(err)
	}
	return Errorf(CodeInternal, "%v", err).Wrap(err)
}
