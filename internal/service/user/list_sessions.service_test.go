package user

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

func sessionRow(userID uuid.UUID, hash string) entity.RefreshTokenEntity {
	return entity.RefreshTokenEntity{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: hash,
		CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

// TestListSessionsAppliesThePaginationWindow checks the arithmetic the endpoint
// hands the database: page 3 of 20 starts at row 40, and the limit is whatever
// survived normalisation.
func TestListSessionsAppliesThePaginationWindow(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name       string
		page       request.Pagination
		wantLimit  int
		wantOffset int
		wantPage   int
	}{
		{
			// An absent query string must mean "the first page", not a
			// rejection and not offset -20.
			name:       "an unset window gets the defaults",
			page:       request.Pagination{},
			wantLimit:  request.DefaultLimit,
			wantOffset: 0,
			wantPage:   1,
		},
		{
			name:       "a later page becomes an offset",
			page:       request.Pagination{Page: 3, Limit: 20},
			wantLimit:  20,
			wantOffset: 40,
			wantPage:   3,
		},
		{
			// The binding tag rejects this before the service sees it, but the
			// service must not trust that: a caller inside the process could
			// pass anything.
			name:       "a limit above the ceiling is clamped",
			page:       request.Pagination{Page: 1, Limit: 5000},
			wantLimit:  request.MaxLimit,
			wantOffset: 0,
			wantPage:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)

			deps.refreshTokens.EXPECT().CountActiveByUserID(gomock.Any(), userID, gomock.Any()).
				Return(int64(7), nil)
			deps.refreshTokens.EXPECT().
				ListActiveByUserID(gomock.Any(), userID, gomock.Any(), tt.wantLimit, tt.wantOffset).
				Return(nil, nil)

			result, err := deps.service.ListSessions(context.Background(), userID, "", tt.page)

			testutil.NoError(t, err)
			testutil.Equal(t, result.Meta.Page, tt.wantPage, "meta page")
			testutil.Equal(t, result.Meta.Limit, tt.wantLimit, "meta limit")
			testutil.Equal(t, result.Meta.Total, int64(7), "meta total")
			// A user with no sessions gets an empty list, which serialises as
			// [] rather than null.
			testutil.Equal(t, len(result.Sessions), 0, "session count")
		})
	}
}

// TestListSessionsMarksTheCallersOwnSession is what makes the endpoint usable
// for a "sign out my other devices" screen: without it a client cannot tell
// which row it would be revoking itself with.
func TestListSessionsMarksTheCallersOwnSession(t *testing.T) {
	deps := newTestDeps(t)
	userID := uuid.New()

	presented, hash := issuedRefreshToken(t, deps, userID)
	mine := sessionRow(userID, hash)
	theirs := sessionRow(userID, "some-other-device")

	deps.refreshTokens.EXPECT().CountActiveByUserID(gomock.Any(), userID, gomock.Any()).Return(int64(2), nil)
	deps.refreshTokens.EXPECT().ListActiveByUserID(gomock.Any(), userID, gomock.Any(), gomock.Any(), gomock.Any()).
		Return([]entity.RefreshTokenEntity{theirs, mine}, nil)

	result, err := deps.service.ListSessions(context.Background(), userID, presented, request.Pagination{})

	testutil.NoError(t, err)
	testutil.Equal(t, len(result.Sessions), 2, "session count")
	testutil.Equal(t, result.Sessions[0].Current, false, "another device is not current")
	testutil.Equal(t, result.Sessions[1].Current, true, "the presented token's row is current")
	testutil.Equal(t, result.Sessions[1].ID, mine.ID, "the current session's id")
}

// TestListSessionsWithNoRefreshTokenMarksNothingCurrent covers the request that
// carries an access cookie but no refresh cookie. Hashing "" would produce a
// perfectly valid digest, and a stored row equal to it would be reported as the
// caller's own session.
func TestListSessionsWithNoRefreshTokenMarksNothingCurrent(t *testing.T) {
	deps := newTestDeps(t)
	userID := uuid.New()

	rows := []entity.RefreshTokenEntity{
		sessionRow(userID, token.Hash("")),
		sessionRow(userID, "another"),
	}

	deps.refreshTokens.EXPECT().CountActiveByUserID(gomock.Any(), userID, gomock.Any()).Return(int64(2), nil)
	deps.refreshTokens.EXPECT().ListActiveByUserID(gomock.Any(), userID, gomock.Any(), gomock.Any(), gomock.Any()).
		Return(rows, nil)

	result, err := deps.service.ListSessions(context.Background(), userID, "", request.Pagination{})

	testutil.NoError(t, err)
	for i, session := range result.Sessions {
		testutil.Equal(t, session.Current, false, "session "+string(rune('a'+i))+" is not marked current")
	}
}

// TestListSessionsNeverExposesTheTokenDigest is a security regression test.
//
// The repository hands the service a row that really does carry the digest, and
// the assertion is made on the serialised output — so a new field carrying
// row.TokenHash fails this test no matter what it is called. The end-to-end
// version in internal/di does the same against real database rows; this one
// catches it a layer earlier, before a route is involved.
func TestListSessionsNeverExposesTheTokenDigest(t *testing.T) {
	deps := newTestDeps(t)
	userID := uuid.New()

	const secretHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	deps.refreshTokens.EXPECT().CountActiveByUserID(gomock.Any(), userID, gomock.Any()).Return(int64(1), nil)
	deps.refreshTokens.EXPECT().ListActiveByUserID(gomock.Any(), userID, gomock.Any(), gomock.Any(), gomock.Any()).
		Return([]entity.RefreshTokenEntity{sessionRow(userID, secretHash)}, nil)

	result, err := deps.service.ListSessions(context.Background(), userID, "", request.Pagination{})
	testutil.NoError(t, err)

	testutil.Equal(t, len(result.Sessions), 1, "session count")

	// The digest really is in the row the repository returned, so this searches
	// for it where it would actually surface: the serialised form. Asserting on
	// struct fields instead would pass for any field the assertion did not
	// happen to name, which is the failure mode a leak test exists to prevent.
	encoded, err := json.Marshal(result.Sessions)
	testutil.NoError(t, err)

	testutil.True(t, !strings.Contains(string(encoded), secretHash),
		"the digest does not survive serialisation")
	testutil.True(t, !strings.Contains(string(encoded), secretHash[:8]),
		"not even a prefix of the digest survives")
}

func TestListSessionsReportsRepositoryFailures(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name   string
		expect func(deps *testDeps)
	}{
		{
			name: "a failing count",
			expect: func(deps *testDeps) {
				deps.refreshTokens.EXPECT().CountActiveByUserID(gomock.Any(), userID, gomock.Any()).
					Return(int64(0), errors.New("connection refused"))
			},
		},
		{
			name: "a failing listing",
			expect: func(deps *testDeps) {
				deps.refreshTokens.EXPECT().CountActiveByUserID(gomock.Any(), userID, gomock.Any()).
					Return(int64(3), nil)
				deps.refreshTokens.EXPECT().
					ListActiveByUserID(gomock.Any(), userID, gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, errors.New("connection refused"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)
			tt.expect(deps)

			_, err := deps.service.ListSessions(context.Background(), userID, "", request.Pagination{})

			testutil.ErrorIs(t, err, apperr.ErrInternal)
		})
	}
}

func TestListSessionsContextCancellation(t *testing.T) {
	deps := newTestDeps(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := deps.service.ListSessions(ctx, uuid.New(), "", request.Pagination{})

	testutil.ErrorIs(t, err, context.Canceled)
}
