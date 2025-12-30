package core

import "fmt"

// Exit codes per spec.
const (
	ExitOK       = 0
	ExitRuntime  = 1
	ExitUsage    = 2
	ExitLease    = 3
	ExitNotFound = 4
	ExitConflict = 5
)

// CLIError carries a user-visible message and exit code.
type CLIError struct {
	Code int
	Msg  string
	Err  error
}

func (e *CLIError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

// WrapError creates a CLIError with an underlying error.
func WrapError(code int, msg string, err error) *CLIError {
	return &CLIError{Code: code, Msg: msg, Err: err}
}

// ErrorForReplyCode maps protocol error codes to CLI exit codes.
func ErrorForReplyCode(code string, message string) *CLIError {
	switch code {
	case "LEASE_REQUIRED", "LEASE_MISMATCH":
		return &CLIError{Code: ExitLease, Msg: message}
	case "NOT_FOUND":
		return &CLIError{Code: ExitNotFound, Msg: message}
	case "CONFLICT":
		return &CLIError{Code: ExitConflict, Msg: message}
	case "INVALID":
		return &CLIError{Code: ExitUsage, Msg: message}
	default:
		return &CLIError{Code: ExitRuntime, Msg: message}
	}
}

// ExitCode returns the CLI exit code from error.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	if cliErr, ok := err.(*CLIError); ok {
		return cliErr.Code
	}
	return ExitRuntime
}
