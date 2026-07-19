// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"syscall"

	"lumio-os/server/internal/files"
	"lumio-os/server/internal/journal"
	"lumio-os/server/internal/services"
	"lumio-os/server/internal/terminal"
)

const (
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeStaleRevision    = "stale_revision"
	CodeValidationFailed = "validation_failed"
	CodeBusy             = "busy"
	CodeUnavailable      = "unavailable"
	CodeInternal         = "internal"
)

type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string {
	return e.Code + ": " + e.Message
}

func NewError(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

var codeStatus = map[string]int{
	CodeUnauthorized:     http.StatusUnauthorized,
	CodeForbidden:        http.StatusForbidden,
	CodeNotFound:         http.StatusNotFound,
	CodeConflict:         http.StatusConflict,
	CodeStaleRevision:    http.StatusConflict,
	CodeValidationFailed: http.StatusBadRequest,
	CodeBusy:             http.StatusConflict,
	CodeUnavailable:      http.StatusServiceUnavailable,
	CodeInternal:         http.StatusInternalServerError,
}

func StatusFor(code string) int {
	if status, ok := codeStatus[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

type envelope struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
}

func WriteData(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, envelope{OK: true, Data: data})
}

func WriteError(w http.ResponseWriter, err error) {
	apiErr := MapError(err)
	if apiErr.Code == CodeInternal {
		log.Printf("internal error: %v", err)
	}
	if apiErr.Code == CodeUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	writeJSON(w, StatusFor(apiErr.Code), envelope{OK: false, Error: apiErr})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func MapError(err error) *Error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	var pathErr *os.PathError
	var linkErr *os.LinkError
	isPathErr := errors.As(err, &pathErr)
	isLinkErr := errors.As(err, &linkErr)
	if isPathErr || isLinkErr {
		path := ""
		if isPathErr {
			path = pathErr.Path
		} else {
			path = linkErr.Old
		}
		details := map[string]any{"path": path}
		switch {
		case errors.Is(err, fs.ErrPermission),
			errors.Is(err, syscall.EACCES),
			errors.Is(err, syscall.EPERM),
			errors.Is(err, syscall.EBUSY):
			return &Error{Code: CodeForbidden, Message: "Permission denied.", Details: details}
		case errors.Is(err, fs.ErrNotExist):
			return &Error{Code: CodeNotFound, Message: "The path does not exist.", Details: details}
		}
	}
	switch {
	case errors.Is(err, files.ErrValidation), errors.Is(err, journal.ErrValidation):
		return &Error{Code: CodeValidationFailed, Message: err.Error()}
	case errors.Is(err, files.ErrStaleRevision):
		var stale *files.StaleError
		if errors.As(err, &stale) {
			return &Error{
				Code:    CodeStaleRevision,
				Message: "The file changed on disk since it was read.",
				Details: map[string]any{"expectedRevision": stale.Expected, "actualRevision": stale.Actual},
			}
		}
		return &Error{Code: CodeStaleRevision, Message: "The file changed on disk since it was read."}
	case errors.Is(err, services.ErrUnavailable):
		return &Error{Code: CodeUnavailable, Message: "The systemd D-Bus connection is unavailable on this host."}
	case errors.Is(err, journal.ErrUnavailable):
		return &Error{Code: CodeUnavailable, Message: "journalctl is not available on this host."}
	case errors.Is(err, terminal.ErrNotFound):
		return &Error{Code: CodeNotFound, Message: "Unknown or expired terminal session."}
	case errors.Is(err, terminal.ErrConflict):
		return &Error{Code: CodeConflict, Message: "The terminal session is already attached."}
	case errors.Is(err, terminal.ErrValidation):
		return &Error{Code: CodeValidationFailed, Message: err.Error()}
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Code: CodeUnavailable, Message: "The system did not answer in time."}
	}
	return &Error{Code: CodeInternal, Message: "Internal server error."}
}
