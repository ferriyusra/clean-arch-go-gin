package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/api/middleware"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
	userSvc "github.com/ferriyusra/boilerplate-golang-gin/internal/service/user"
)

// serviceErrorStatus maps each known service error to its HTTP status.
// Anything absent from this table is treated as an internal failure.
var serviceErrorStatus = map[error]int{
	userSvc.ErrEmailAlreadyRegistered: http.StatusConflict,
	userSvc.ErrInvalidCredentials:     http.StatusUnauthorized,
	userSvc.ErrInvalidRefreshToken:    http.StatusUnauthorized,
	userSvc.ErrRefreshTokenRevoked:    http.StatusUnauthorized,
	userSvc.ErrRefreshTokenExpired:    http.StatusUnauthorized,
	userSvc.ErrUserNotFound:           http.StatusNotFound,
	userSvc.ErrInvalidUserID:          http.StatusBadRequest,
}

// respondServiceError translates a service error into a client response.
//
// Only messages from known sentinel errors are echoed back. Unrecognised errors
// are logged with their full wrapped chain and answered with a generic 500, so
// database and driver detail never reaches the client.
func respondServiceError(c *gin.Context, err error) {
	for sentinel, status := range serviceErrorStatus {
		if errors.Is(err, sentinel) {
			c.JSON(status, response.Err(sentinel.Error()))
			return
		}
	}

	slog.Error("unhandled service error",
		slog.Any("error", err),
		slog.String("method", c.Request.Method),
		slog.String("path", c.Request.URL.Path),
		slog.String("request_id", middleware.GetRequestIDFromContext(c)),
	)
	c.JSON(http.StatusInternalServerError, response.Err("Internal server error"))
}

// respondBindError answers a request whose body failed to bind or validate.
func respondBindError(c *gin.Context, err error) {
	if fields := validationErrors(err); len(fields) > 0 {
		c.JSON(http.StatusBadRequest, response.ValidationErr("Validation failed", fields))
		return
	}
	c.JSON(http.StatusBadRequest, response.Err("Invalid request body"))
}
