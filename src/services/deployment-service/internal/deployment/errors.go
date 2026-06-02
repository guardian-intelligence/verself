package deployment

import (
	"errors"
	"fmt"
)

var (
	ErrInvalid      = errors.New("deployment invalid")
	ErrUnauthorized = errors.New("deployment unauthorized")
	ErrUnavailable  = errors.New("deployment unavailable")
	ErrBusy         = errors.New("deployment busy")
	ErrNotFound     = errors.New("deployment not found")
	ErrDuplicate    = errors.New("deployment duplicate")
)

type CodedError struct {
	Code string
	Err  error
}

func (e CodedError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}

func (e CodedError) Unwrap() error {
	return e.Err
}

func coded(code string, err error) error {
	if err == nil {
		err = errors.New(code)
	}
	return CodedError{Code: code, Err: err}
}

func fmtUnauthorized(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrUnauthorized}, args...)...)
}

func ErrorCode(err error) string {
	var codedErr CodedError
	if errors.As(err, &codedErr) && codedErr.Code != "" {
		return codedErr.Code
	}
	switch {
	case errors.Is(err, ErrInvalid):
		return "deployment.invalid"
	case errors.Is(err, ErrUnauthorized):
		return "deployment.unauthorized"
	case errors.Is(err, ErrUnavailable):
		return "deployment.unavailable"
	case errors.Is(err, ErrBusy):
		return "deployment.busy"
	case errors.Is(err, ErrNotFound):
		return "deployment.not_found"
	case errors.Is(err, ErrDuplicate):
		return "deployment.duplicate"
	default:
		return "deployment.failed"
	}
}
