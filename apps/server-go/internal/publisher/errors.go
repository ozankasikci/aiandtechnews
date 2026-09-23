package publisher

import (
	"errors"
	"net/http"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
)

// permanentError marks failures that retrying cannot fix (policy, duplicate,
// unusable source, rejected rewrite). Everything else is treated as transient.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent wraps err so the publisher marks the candidate failed instead of retrying.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

func IsPermanent(err error) bool {
	var permanent permanentError
	return errors.As(err, &permanent)
}

// systemFaultError marks configuration problems (missing or rejected API key,
// unknown model) that no candidate is to blame for. The publisher retries
// them later without consuming the candidate's attempts.
type systemFaultError struct{ err error }

func (e systemFaultError) Error() string { return e.err.Error() }
func (e systemFaultError) Unwrap() error { return e.err }

// SystemFault wraps err so the publisher requeues the candidate without using an attempt.
func SystemFault(err error) error {
	if err == nil {
		return nil
	}
	return systemFaultError{err: err}
}

func IsSystemFault(err error) bool {
	var fault systemFaultError
	return errors.As(err, &fault)
}

// ClassifyGeminiError sorts a Gemini failure into the publisher's error
// classes: a missing key or 401/403/404 is a system fault, a safety block or
// any other non-transient response is permanent, and the rest (429, 5xx,
// network) is transient.
func ClassifyGeminiError(err error) error {
	if errors.Is(err, gemini.ErrMissingAPIKey) {
		return SystemFault(err)
	}
	if errors.Is(err, gemini.ErrBlocked) {
		return Permanent(err)
	}
	var apiErr *gemini.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return SystemFault(err)
		}
		if !apiErr.Transient() {
			return Permanent(err)
		}
	}
	return err
}
