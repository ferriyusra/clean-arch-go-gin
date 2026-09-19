package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/logging"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/csrf"
	userSvc "github.com/ferriyusra/clean-arch-go-gin/internal/service/user"
)

// CookieConfig controls how auth cookies are written.
//
// The TTLs come from the same config values used to sign the tokens, so a
// cookie can no longer outlive (or expire before) the token it carries.
type CookieConfig struct {
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	// Secure is false only in development, where there is no TLS.
	Secure bool
}

// UserHandler handles user-related HTTP requests
type UserHandler struct {
	userService userSvc.UserService
	csrfService csrf.CSRFService
	cookies     CookieConfig
}

// NewUserHandler creates a new instance of UserHandler
func NewUserHandler(userService userSvc.UserService, csrfService csrf.CSRFService, cookies CookieConfig) *UserHandler {
	return &UserHandler{
		userService: userService,
		csrfService: csrfService,
		cookies:     cookies,
	}
}

func (h *UserHandler) setAuthCookies(c *gin.Context, accessToken, refreshToken string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(middleware.AccessTokenCookie, accessToken, int(h.cookies.AccessTTL.Seconds()), "/", "", h.cookies.Secure, true)
	c.SetCookie(middleware.RefreshTokenCookie, refreshToken, int(h.cookies.RefreshTTL.Seconds()), "/", "", h.cookies.Secure, true)
}

func (h *UserHandler) clearAuthCookies(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(middleware.AccessTokenCookie, "", -1, "/", "", h.cookies.Secure, true)
	c.SetCookie(middleware.RefreshTokenCookie, "", -1, "/", "", h.cookies.Secure, true)
}

// Register handles POST /api/v1/auth/register requests
func (h *UserHandler) Register(c *gin.Context) {
	req := &request.RegisterUserRequest{}
	if !BindJSON(c, req) {
		return
	}

	resp, err := h.userService.Register(c.Request.Context(), req)
	if err != nil {
		Fail(c, err)
		return
	}

	h.setAuthCookies(c, resp.AccessToken, resp.RefreshToken)
	OK(c, http.StatusCreated, "Registration successful", resp)
}

// Login handles POST /api/v1/auth/login requests
func (h *UserHandler) Login(c *gin.Context) {
	req := &request.LoginRequest{}
	if !BindJSON(c, req) {
		return
	}

	resp, err := h.userService.Login(c.Request.Context(), req)
	if err != nil {
		Fail(c, err)
		return
	}

	h.setAuthCookies(c, resp.AccessToken, resp.RefreshToken)
	OK(c, http.StatusOK, "Login successful", resp)
}

// Refresh handles POST /api/v1/auth/refresh requests
func (h *UserHandler) Refresh(c *gin.Context) {
	tokenStr, err := c.Cookie(middleware.RefreshTokenCookie)
	if err != nil {
		Fail(c, apperr.ErrMissingRefreshToken.WithCause(err))
		return
	}

	resp, err := h.userService.Refresh(c.Request.Context(), tokenStr)
	if err != nil {
		Fail(c, err)
		return
	}

	// Both cookies are rewritten: refresh rotates the pair, so the refresh token
	// the client holds after this call is a different one.
	h.setAuthCookies(c, resp.AccessToken, resp.RefreshToken)

	OK(c, http.StatusOK, "Token refreshed", nil)
}

// Logout handles POST /api/v1/auth/logout requests.
//
// Revocation failures are logged but never fail the request: the client's
// cookies are cleared either way, so logout must always appear to succeed.
func (h *UserHandler) Logout(c *gin.Context) {
	if userID, err := middleware.GetUserIDFromContext(c); err == nil {
		if revokeErr := h.userService.RevokeRefreshTokens(c.Request.Context(), userID); revokeErr != nil {
			logging.FromContext(c.Request.Context()).Error("revoking refresh tokens on logout",
				"user_id", userID.String(),
				"error", revokeErr.Error(),
			)
		}
	}

	h.clearAuthCookies(c)
	OK(c, http.StatusOK, "Logged out successfully", nil)
}

// GetMe handles GET /api/v1/auth/me requests (protected)
func (h *UserHandler) GetMe(c *gin.Context) {
	userID, err := middleware.GetUserIDFromContext(c)
	if err != nil {
		Fail(c, apperr.ErrUnauthorized.WithCause(err))
		return
	}

	user, err := h.userService.GetUser(c.Request.Context(), userID.String())
	if err != nil {
		Fail(c, err)
		return
	}

	OK(c, http.StatusOK, "User retrieved", user)
}

// ListSessions handles GET /api/v1/auth/sessions requests (protected).
//
// It is a read, so it carries no CSRF middleware. The refresh cookie is read
// only so the service can mark which of the listed sessions belongs to this
// client; a request without one is perfectly valid and simply marks none.
func (h *UserHandler) ListSessions(c *gin.Context) {
	userID, err := middleware.GetUserIDFromContext(c)
	if err != nil {
		Fail(c, apperr.ErrUnauthorized.WithCause(err))
		return
	}

	page, ok := BindPagination(c)
	if !ok {
		return
	}

	// An absent refresh cookie is not an error here: an access token alone is
	// enough to reach this endpoint.
	currentRefreshToken, _ := c.Cookie(middleware.RefreshTokenCookie)

	result, err := h.userService.ListSessions(c.Request.Context(), userID, currentRefreshToken, page)
	if err != nil {
		Fail(c, err)
		return
	}

	OKWithMeta(c, http.StatusOK, "Sessions retrieved", result.Sessions, result.Meta)
}

// ChangePassword handles PATCH /api/v1/auth/password requests (protected, CSRF).
//
// The service ends every session for the account and mints a replacement pair
// for this client; this handler's only extra job is to move that pair into
// cookies, so the caller stays signed in on the device they used and is signed
// out everywhere else.
func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID, err := middleware.GetUserIDFromContext(c)
	if err != nil {
		Fail(c, apperr.ErrUnauthorized.WithCause(err))
		return
	}

	req := &request.ChangePasswordRequest{}
	if !BindJSON(c, req) {
		return
	}

	resp, err := h.userService.ChangePassword(c.Request.Context(), userID, req)
	if err != nil {
		Fail(c, err)
		return
	}

	h.setAuthCookies(c, resp.AccessToken, resp.RefreshToken)
	OK(c, http.StatusOK, "Password changed", nil)
}

// DeleteAccount handles DELETE /api/v1/auth/me requests (protected, CSRF).
//
// The cookies are cleared only after the delete succeeds. Clearing them first
// would sign the user out of an account that still exists if the delete failed,
// leaving them unable to retry without signing in again.
func (h *UserHandler) DeleteAccount(c *gin.Context) {
	userID, err := middleware.GetUserIDFromContext(c)
	if err != nil {
		Fail(c, apperr.ErrUnauthorized.WithCause(err))
		return
	}

	if err := h.userService.DeleteAccount(c.Request.Context(), userID); err != nil {
		Fail(c, err)
		return
	}

	h.clearAuthCookies(c)
	OK(c, http.StatusOK, "Account deleted", nil)
}

// GetCSRFToken handles GET /api/v1/csrf requests
func (h *UserHandler) GetCSRFToken(c *gin.Context) {
	token, err := h.csrfService.GenerateToken()
	if err != nil {
		Fail(c, apperr.Internal(err))
		return
	}

	OK(c, http.StatusOK, "CSRF token generated", response.CSRFTokenResponse{Token: token})
}
