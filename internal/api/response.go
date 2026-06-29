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
	c.JSON(http.StatusOK, Response{Code: 0, Message: "ok", Data: data})
}

func created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Response{Code: 0, Message: "ok", Data: data})
}

func fail(c *gin.Context, err error) {
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		appErr = domain.E(domain.CodeInternal, "internal error", err)
	}
	c.JSON(statusFor(appErr.Code), Response{Code: appErr.Code, Message: appErr.Error()})
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
	default:
		return http.StatusInternalServerError
	}
}
