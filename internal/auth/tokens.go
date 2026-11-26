package auth

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Member tokens are PocketBase auth tokens of records of the members collection: JWTs signed with the member's token
// key together with the collection's auth token secret, valid for the collection's auth token duration. PocketBase's
// own sign-in methods are off for members (AD2); these helpers are the only way OSFM issues and checks tokens.

// ErrNotMember is returned for a record, or a token, that does not belong to the members collection.
var ErrNotMember = errors.New("not a member record")

// NewMemberRecord returns a new, unsaved record of the members collection. PocketBase requires every auth record to
// have a password; OSFM never uses it (members' passwords live in the credentials collection, D3), so the record gets
// a random one that is never revealed.
func NewMemberRecord(app core.App) (*core.Record, error) {
	c, err := app.FindCachedCollectionByNameOrId(store.Members)
	if err != nil {
		return nil, err
	}
	r := core.NewRecord(c)
	r.SetRandomPassword()
	return r, nil
}

// IssueToken returns a new auth token for member. It does not depend on any of PocketBase's sign-in methods being
// enabled; the caller must have verified both factors first (D4).
func IssueToken(member *core.Record) (string, error) {
	if !isMember(member) {
		return "", ErrNotMember
	}
	return member.NewAuthToken()
}

// MemberByToken returns the member an auth token issued by [IssueToken] belongs to. It fails for a malformed, expired
// or revoked token, for another kind of PocketBase token, and for the token of any other auth collection (such as a
// superuser's). It does not check the member's state (left or deleted); that is the session code's job.
func MemberByToken(app core.App, token string) (*core.Record, error) {
	r, err := app.FindAuthRecordByToken(token, core.TokenTypeAuth)
	if err != nil {
		return nil, err
	}
	if !isMember(r) {
		return nil, ErrNotMember
	}
	return r, nil
}

// RevokeTokens invalidates every token issued for member so far, by giving the member a new token key, and saves the
// record. Tokens issued afterwards are valid. Other members' tokens are unaffected; rotating the collection's auth
// token secret instead would invalidate everyone's.
func RevokeTokens(app core.App, member *core.Record) error {
	if !isMember(member) {
		return ErrNotMember
	}
	member.RefreshTokenKey()
	return app.Save(member)
}

func isMember(r *core.Record) bool {
	return r != nil && r.Collection().Name == store.Members
}
