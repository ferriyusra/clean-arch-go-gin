package user

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

// ChangePassword replaces a user's password and ends every other session.
//
// Revoking the sessions is not housekeeping, it is the point. A user changes a
// password because they believe someone else may have their credentials; if the
// sessions minted before the change keep working, the attacker keeps their
// access for the full refresh-token lifetime and the change accomplished
// nothing. So the new hash and the revocation are one transaction: a state
// where the password has changed but the old sessions survive must not exist,
// even for the moment between two statements.
//
// The caller's own session is then re-minted inside that same transaction, so
// the client that asked stays signed in while every other device is signed out.
func (s *userService) ChangePassword(
	ctx context.Context,
	userID uuid.UUID,
	req *request.ChangePasswordRequest,
) (*response.ChangePasswordResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	user, err := s.userRepository.FindByID(ctx, userID)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("finding user by id: %w", err))
	}

	// A valid access token for an account that no longer exists (deleted, or
	// deleted between issuing the token and using it) gets the same answer as a
	// wrong password, and costs the same time. Returning here without the bcrypt
	// call would make a missing account answer in microseconds where a wrong
	// password takes ~60ms, which is the difference Login already pays to hide.
	//
	// The client's next move is the same either way: sign in again.
	if user == nil {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash(), []byte(req.CurrentPassword))
		return nil, apperr.ErrInvalidCredentials
	}

	if err = bcrypt.CompareHashAndPassword(user.Password, []byte(req.CurrentPassword)); err != nil {
		return nil, apperr.ErrInvalidCredentials
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("hashing new password: %w", err))
	}

	// Verification and hashing stay outside the transaction on purpose. They
	// are two bcrypt operations, a few hundred milliseconds of CPU, and holding
	// a database connection open across them would pin a connection per
	// in-flight password change for no gain: the check is not one a concurrent
	// writer can invalidate in a way a transaction would repair. Two clients
	// that both know the current password both succeed, last write wins, and
	// both revoke the sessions — which is the safe outcome either way.
	var accessToken, refreshToken string
	err = s.txManager.WithinTx(ctx, func(ctx context.Context) error {
		if updateErr := s.userRepository.Update(ctx, userID, entity.UserEntity{
			Password: hashed,
		}); updateErr != nil {
			return apperr.Internal(fmt.Errorf("updating password: %w", updateErr))
		}

		// Every session goes, including the caller's, before a new one is
		// issued below. Revoking after issuing would delete the pair that was
		// just minted and sign the caller out too.
		if revokeErr := s.RevokeRefreshTokens(ctx, userID); revokeErr != nil {
			return revokeErr
		}

		var issueErr error
		accessToken, refreshToken, issueErr = s.issueTokens(ctx, response.GetUser{
			ID:    user.ID,
			Email: user.Email,
			Name:  user.Name,
		})
		return issueErr
	})
	if err != nil {
		return nil, err
	}

	return &response.ChangePasswordResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}
