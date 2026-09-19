package user

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
)

// DeleteAccount removes a user and every session that belongs to them.
//
// The two deletes are one transaction because half of this is worse than
// neither half. Deleting the user but leaving the refresh tokens would leave
// live credentials pointing at an account that is gone; deleting the tokens but
// leaving the user would sign someone out of an account that still exists and
// still holds their data.
//
// Note the asymmetry in what "delete" means here. UserEntity carries
// gorm.DeletedAt, so the user row is soft-deleted: it stops being visible to
// every query the application makes — including the lookups behind login and
// refresh — but the row survives. RefreshTokenEntity deliberately has no
// gorm.DeletedAt, so its rows are really gone; a revoked credential that
// lingers is one that can be restored.
func (s *userService) DeleteAccount(ctx context.Context, userID uuid.UUID) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	user, err := s.userRepository.FindByID(ctx, userID)
	if err != nil {
		return apperr.Internal(fmt.Errorf("finding user by id: %w", err))
	}
	if user == nil {
		return apperr.ErrUserNotFound
	}

	return s.txManager.WithinTx(ctx, func(ctx context.Context) error {
		// Sessions first: if the transaction fails after this point nothing is
		// committed, and if it succeeds there is no window in which a token
		// outlives the account it belongs to.
		if revokeErr := s.RevokeRefreshTokens(ctx, userID); revokeErr != nil {
			return revokeErr
		}

		if delErr := s.userRepository.Delete(ctx, userID); delErr != nil {
			return apperr.Internal(fmt.Errorf("deleting user: %w", delErr))
		}
		return nil
	})
}
