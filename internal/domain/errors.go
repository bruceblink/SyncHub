package domain

import "errors"

type ErrorCode string

const (
	CodeInvalidArgument        ErrorCode = "INVALID_ARGUMENT"
	CodeUnauthenticated        ErrorCode = "AUTH_UNAUTHENTICATED"
	CodeInvalidCredentials     ErrorCode = "AUTH_INVALID_CREDENTIALS"
	CodeTokenExpired           ErrorCode = "AUTH_TOKEN_EXPIRED"
	CodePermissionDenied       ErrorCode = "AUTH_PERMISSION_DENIED"
	CodeAlreadyExists          ErrorCode = "ALREADY_EXISTS"
	CodeNotFound               ErrorCode = "NOT_FOUND"
	CodeFileNotFound           ErrorCode = "FILE_NOT_FOUND"
	CodeFileConflict           ErrorCode = "FILE_CONFLICT"
	CodeRangeNotSatisfiable    ErrorCode = "RANGE_NOT_SATISFIABLE"
	CodeUploadSessionExpired   ErrorCode = "UPLOAD_SESSION_EXPIRED"
	CodeUploadChecksumMismatch ErrorCode = "UPLOAD_CHECKSUM_MISMATCH"
	CodeSyncCursorExpired      ErrorCode = "SYNC_CURSOR_EXPIRED"
	CodeInternal               ErrorCode = "INTERNAL_ERROR"
)

type AppError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *AppError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func E(code ErrorCode, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}

func ErrorCodeOf(err error) ErrorCode {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return CodeInternal
}
