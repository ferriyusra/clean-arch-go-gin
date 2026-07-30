package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/api/middleware"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/request"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
	userSvc "github.com/ferriyusra/boilerplate-golang-gin/internal/service/user"
)

// UserHandler handles user-related HTTP requests
type UserHandler struct {
	userService userSvc.UserService
}

// NewUserHandler creates a new instance of UserHandler
func NewUserHandler(userService userSvc.UserService) *UserHandler {
	return &UserHandler{
		userService: userService,
	}
}

// Register handles POST /api/auth/register requests
func (h *UserHandler) Register(c *gin.Context) {
	req := &request.RegisterUserRequest{}
	if err := c.ShouldBindJSON(req); err != nil {
		respondBindError(c, err)
		return
	}

	resp, err := h.userService.Register(c.Request.Context(), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, response.OK("Registration successful", resp))
}

// Login handles POST /api/auth/login requests
func (h *UserHandler) Login(c *gin.Context) {
	req := &request.LoginRequest{}
	if err := c.ShouldBindJSON(req); err != nil {
		respondBindError(c, err)
		return
	}

	resp, err := h.userService.Login(c.Request.Context(), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response.OK("Login successful", resp))
}

// Refresh handles POST /api/auth/refresh requests
func (h *UserHandler) Refresh(c *gin.Context) {
	req := &request.RefreshTokenRequest{}
	if err := c.ShouldBindJSON(req); err != nil {
		respondBindError(c, err)
		return
	}

	resp, err := h.userService.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response.OK("Token refreshed", resp))
}

// Logout handles POST /api/auth/logout requests (protected).
//
// Revoking every refresh token for the user means a stolen refresh token stops
// working at logout, not just the session that called it.
func (h *UserHandler) Logout(c *gin.Context) {
	userID, err := middleware.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, response.Err("Unauthorized"))
		return
	}

	if err := h.userService.RevokeRefreshTokens(c.Request.Context(), userID); err != nil {
		respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response.OK("Logged out successfully", nil))
}

// GetMe handles GET /api/auth/me requests (protected)
func (h *UserHandler) GetMe(c *gin.Context) {
	userID, err := middleware.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, response.Err("Unauthorized"))
		return
	}

	user, err := h.userService.GetUser(c.Request.Context(), userID.String())
	if err != nil {
		respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response.OK("User retrieved", user))
}
