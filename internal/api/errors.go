package api

import (
	"errors"
	"fmt"
)

// Error is the canonical error returned by transports when the upstream
// responds with a non-2xx status. Callers match with errors.As to recover the
// HTTP status and any machine-readable code.
type Error struct {
	Status    int
	Code      string
	Message   string
	RequestID string
	Details   string
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return fmt.Sprintf("linkedin %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("linkedin %d: %s", e.Status, e.Message)
}

// IsUnauthorized reports whether the error carries a 401 response status.
func (e *Error) IsUnauthorized() bool {
	return e != nil && e.Status == 401
}

// IsForbidden reports whether the error carries a 403 response status.
func (e *Error) IsForbidden() bool {
	return e != nil && e.Status == 403
}

// IsNotFound reports whether the error carries a 404 response status.
func (e *Error) IsNotFound() bool {
	return e != nil && e.Status == 404
}

// IsRateLimited reports whether the error carries a 429 response status.
func (e *Error) IsRateLimited() bool {
	return e != nil && e.Status == 429
}

// IsServerError reports whether the error carries a 5xx response status.
func (e *Error) IsServerError() bool {
	return e != nil && e.Status >= 500 && e.Status < 600
}

// IsValidation reports whether the error carries a 422 response status.
func (e *Error) IsValidation() bool {
	return e != nil && e.Status == 422
}

// ErrFeatureUnavailable signals that a feature is not available under the
// current transport. Callers surface this as a status:"unsupported" envelope.
type ErrFeatureUnavailable struct {
	Feature            string
	Reason             string
	SuggestedTransport string
}

// Error implements the error interface.
func (e *ErrFeatureUnavailable) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("feature unavailable: %s: %s", e.Feature, e.Reason)
}

// Is lets callers match any *ErrFeatureUnavailable with errors.Is(err, &ErrFeatureUnavailable{}).
func (e *ErrFeatureUnavailable) Is(target error) bool {
	_, ok := target.(*ErrFeatureUnavailable)
	return ok
}

// AsFeatureUnavailable returns the typed sentinel if err represents an
// unavailable feature, plus true.
func AsFeatureUnavailable(err error) (*ErrFeatureUnavailable, bool) {
	var fe *ErrFeatureUnavailable
	if errors.As(err, &fe) {
		return fe, true
	}
	return nil, false
}

// AsError returns the typed Error if err represents a LinkedIn HTTP
// error, plus true.
func AsError(err error) (*Error, bool) {
	var ae *Error
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}
