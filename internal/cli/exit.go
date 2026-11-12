package cli

import "errors"

// Exit codes of the check command.
const (
	// ExitDrift is returned when at least one section drifted.
	ExitDrift = 1
	// ExitFailure is returned for usage and runtime errors.
	ExitFailure = 2
)

// exitError carries a process exit code through cobra. Err may be nil when the
// exit code alone is the result (drift found); nothing is printed for it.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

// ExitCode maps an error returned by Execute to a process exit code:
// 0 for nil, the carried code for an exit error, ExitFailure for anything else.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return ExitFailure
}
