package user

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
)

// ListSessions returns a page of the user's active sessions, newest first.
//
// A "session" here is a live refresh token: that row is the only server-side
// record of a signed-in device, and deleting it is what signing that device out
// means. Expired rows are excluded by the repository, so what comes back is
// what can still authenticate.
//
// Nothing that identifies the token itself crosses this boundary. The digest is
// read to answer one question — is this row the caller's own session — and the
// answer is a bool. See response.Session for why that matters.
func (s *userService) ListSessions(
	ctx context.Context,
	userID uuid.UUID,
	currentRefreshToken string,
	page request.Pagination,
) (*response.SessionList, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	window := page.Normalized()
	now := s.now()

	// Count and list are two reads rather than one transaction. A session that
	// is revoked between them makes the total off by one for a moment, which is
	// what any paginated list of changing data looks like; holding a
	// transaction open to prevent it would buy consistency nobody can observe
	// at the cost of a connection held across two round trips.
	total, err := s.refreshTokenRepository.CountActiveByUserID(ctx, userID, now)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("counting active sessions: %w", err))
	}

	rows, err := s.refreshTokenRepository.ListActiveByUserID(ctx, userID, now, window.Limit, window.Offset())
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("listing active sessions: %w", err))
	}

	// An absent refresh cookie is normal — an access token alone is enough to
	// reach this endpoint — and simply means no row is marked current. Hashing
	// the empty string would produce a real digest that could, in principle,
	// match a stored row, so the empty case is handled before hashing.
	var currentHash string
	if currentRefreshToken != "" {
		currentHash = token.Hash(currentRefreshToken)
	}

	sessions := make([]response.Session, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, response.Session{
			ID:        row.ID,
			Current:   currentHash != "" && row.TokenHash == currentHash,
			CreatedAt: row.CreatedAt,
			ExpiresAt: row.ExpiresAt,
		})
	}

	return &response.SessionList{
		Sessions: sessions,
		Meta:     response.NewMeta(window.Page, window.Limit, total),
	}, nil
}
