package response

import (
	"time"

	"github.com/google/uuid"
)

// Session is one active refresh-token session, as a client is allowed to see it.
//
// SECURITY: this type deliberately has no field for the token or its digest,
// not even a truncated one. The stored digest is the only thing standing
// between a leaked database row and a replayable credential, and a "first eight
// characters, just for display" field would put a piece of it into a response
// body, a browser cache and every log that records one. The session id is
// enough to name a session for revocation and is worth nothing to an attacker.
//
// There is a test in internal/api/handler that serialises this type and fails
// if the digest appears in the output, because a comment does not survive a
// refactor and a test does.
type Session struct {
	ID uuid.UUID `json:"id"`
	// Current marks the session the calling request is authenticated by, which
	// is what lets a "sign out my other devices" screen avoid signing the user
	// out of the device they are looking at.
	Current   bool      `json:"current"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// SessionList is what UserService.ListSessions returns.
//
// Meta is tagged json:"-" because it does not belong in the envelope's data:
// the handler passes it to response.OKWithMeta, which puts it in the envelope's
// own meta field alongside the data. Carrying it here keeps the service the
// place that knows the total, rather than making the handler count.
type SessionList struct {
	Sessions []Session `json:"sessions"`
	Meta     *Meta     `json:"-"`
}
