package licentivo

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"unicode"

	"licentivo-launch/internal/ports"
)

// APIError retains structured diagnostics without exposing response bodies or credentials.
type APIError struct {
	Status                   int
	Code, Message, RequestID string
	cause                    error
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return e.Code
	}
	return fmt.Sprintf("Licentivo HTTP %d", e.Status)
}

func (e *APIError) Unwrap() error { return e.cause }

type TransportError struct {
	Reason string
	Cause  error
}

func (e *TransportError) Error() string   { return e.Reason }
func (e *TransportError) Unwrap() []error { return []error{ErrUnavailable, e.Cause} }

func transportError(err error) error {
	reason := "LICENTIVO_NETWORK_ERROR"
	var timeout net.Error
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var certificate x509.CertificateInvalidError
	if errors.As(err, &timeout) && timeout.Timeout() {
		reason = "LICENTIVO_TIMEOUT"
	} else if errors.As(err, &authority) || errors.As(err, &hostname) || errors.As(err, &certificate) {
		reason = "LICENTIVO_TLS_ERROR"
	}
	return &TransportError{Reason: reason, Cause: err}
}

var diagnosticCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,79}$`)
var diagnosticRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

func decodeAPIError(status int, body []byte, key string) *APIError {
	out := &APIError{Status: status, cause: ErrUnavailable}
	switch status {
	case 401, 403:
		out.cause = ErrCredentials
	case 409:
		out.cause = ErrConflict
	case 404, 501:
		out.cause = ports.ErrContractUnsupported
	}
	var envelope struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Error == nil {
		return out
	}
	if diagnosticCode.MatchString(envelope.Error.Code) {
		out.Code = envelope.Error.Code
	}
	if diagnosticRequestID.MatchString(envelope.RequestID) && envelope.RequestID != key {
		out.RequestID = envelope.RequestID
	}
	message := envelope.Error.Message
	if key != "" {
		message = strings.ReplaceAll(message, key, "[redacted]")
	}
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, message)
	if len([]rune(message)) > 600 {
		message = string([]rune(message)[:600]) + "…"
	}
	out.Message = strings.TrimSpace(message)
	return out
}
