package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/csrf"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
)

const (
	AccessTokenCookie  = "access_token"
	RefreshTokenCookie = "refresh_token"
	UserIDCtxKey       = "user_id"
	UserEmailCtxKey    = "user_email"
	ClaimsCtxKey       = "claims"
)

// ErrNoUserInContext is returned by GetUserIDFromContext when the request did
// not pass through AuthMiddleware.
var ErrNoUserInContext = errors.New("user not found in context")

// AuthMiddleware validates the JWT carried in the access-token cookie.
func AuthMiddleware(tokenService token.TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr, err := c.Cookie(AccessTokenCookie)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Err(apperr.ErrMissingAccessToken.Message))
			return
		}

		claims, err := tokenService.ValidateAccessToken(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Err(apperr.ErrInvalidAccessToken.Message))
			return
		}

		setAuthContext(c, claims)
		c.Next()
	}
}

// OptionalAuthMiddleware populates the auth context when a valid token is
// present, and lets anonymous requests through untouched.
func OptionalAuthMiddleware(tokenService token.TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr, err := c.Cookie(AccessTokenCookie)
		if err != nil {
			c.Next()
			return
		}

		claims, err := tokenService.ValidateAccessToken(tokenStr)
		if err != nil {
			c.Next()
			return
		}

		setAuthContext(c, claims)
		c.Next()
	}
}

func setAuthContext(c *gin.Context, claims *token.TokenClaims) {
	c.Set(UserIDCtxKey, claims.UserID.String())
	c.Set(UserEmailCtxKey, claims.Email)
	c.Set(ClaimsCtxKey, claims)
}

// CSRFMiddleware validates the double-submit CSRF token on state-changing
// requests. Safe methods pass through untouched.
func CSRFMiddleware(csrfService csrf.CSRFService) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			csrfToken := c.GetHeader("X-CSRF-Token")
			if csrfToken == "" {
				c.AbortWithStatusJSON(http.StatusForbidden, response.Err(apperr.ErrMissingCSRFToken.Message))
				return
			}
			if !csrfService.ValidateToken(csrfToken) {
				c.AbortWithStatusJSON(http.StatusForbidden, response.Err(apperr.ErrInvalidCSRFToken.Message))
				return
			}
		}

		c.Next()
	}
}

// GetUserIDFromContext extracts the authenticated user's id.
//
// Every type assertion below uses the comma-ok form on purpose: a context value
// of an unexpected type must not panic the process.
func GetUserIDFromContext(c *gin.Context) (uuid.UUID, error) {
	value, exists := c.Get(UserIDCtxKey)
	if !exists {
		return uuid.UUID{}, ErrNoUserInContext
	}

	raw, ok := value.(string)
	if !ok {
		return uuid.UUID{}, apperr.ErrInvalidUserID
	}

	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.UUID{}, apperr.ErrInvalidUserID.WithCause(err)
	}

	return id, nil
}

// GetEmailFromContext extracts the authenticated user's email, or "" when the
// request is anonymous.
func GetEmailFromContext(c *gin.Context) string {
	value, exists := c.Get(UserEmailCtxKey)
	if !exists {
		return ""
	}
	email, ok := value.(string)
	if !ok {
		return ""
	}
	return email
}

// GetClaimsFromContext extracts the validated token claims, or nil when the
// request is anonymous.
func GetClaimsFromContext(c *gin.Context) *token.TokenClaims {
	value, exists := c.Get(ClaimsCtxKey)
	if !exists {
		return nil
	}
	claims, ok := value.(*token.TokenClaims)
	if !ok {
		return nil
	}
	return claims
}
