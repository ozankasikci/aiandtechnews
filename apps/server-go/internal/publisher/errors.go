package publisher

import "errors"

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
