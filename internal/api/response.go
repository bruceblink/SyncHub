package api

import (
	"errors"
	"net/http"

	"github.com/bruceblink/SyncHub/internal/domain"
	"github.com/gin-gonic/gin"
)

type Response struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
	Details any    `json:"details,omitempty"`
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: 0, Message: "ok", Data: data, TraceID: traceID(c)})
}

func created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Response{Code: 0, Message: "ok", Data: data, TraceID: traceID(c)})
}

func fail(c *gin.Context, err error) {
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		appErr = domain.E(domain.CodeInternal, "internal error", err)
	}
	c.JSON(statusFor(appErr.Code), Response{Code: appErr.Code, Message: appErr.Error(), TraceID: traceID(c)})
}

func traceID(c *gin.Context) string {
	value, ok := c.Get(traceIDKey)
	if !ok {
		return ""
	}
	traceID, _ := value.(string)
	return traceID
}

func statusFor(code domain.ErrorCode) int {
	switch code {
	case domain.CodeInvalidArgument:
		return http.StatusBadRequest
	case domain.CodeUnauthenticated, domain.CodeInvalidCredentials, domain.CodeTokenExpired:
		return http.StatusUnauthorized
	case domain.CodePermissionDenied:
		return http.StatusForbidden
	case domain.CodeAlreadyExists, domain.CodeFileConflict:
		return http.StatusConflict
	case domain.CodeNotFound, domain.CodeFileNotFound:
		return http.StatusNotFound
	case domain.CodeRangeNotSatisfiable:
		return http.StatusRequestedRangeNotSatisfiable
	case domain.CodeUploadSessionExpired:
		return http.StatusGone
	case domain.CodeUploadChecksumMismatch:
		return http.StatusBadRequest
	case domain.CodeStorageQuotaExceeded:
		return http.StatusRequestEntityTooLarge
	case domain.CodeSyncCursorExpired:
		return http.StatusGone
	default:
		return http.StatusInternalServerError
	}
}
