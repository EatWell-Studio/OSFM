package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// sessions: the members' sign-in sessions (AD6). The session cookie holds a random token; only its SHA-256 hash is
// stored here, so the database alone cannot be used to sign in. A session is valid while its row exists, it has not
// expired and its member may sign in; revoking sessions means deleting rows. Nothing about the client (address, user
// agent) is kept.
func init() {
	m.Register(func(app core.App) error {
		membersID, err := collectionID(app, store.Members)
		if err != nil {
			return err
		}
		return upSessions(app, membersID)
	}, func(app core.App) error {
		return deleteIfExists(app, store.Sessions)
	})
}

func upSessions(app core.App, membersID string) error {
	c, err := findOrNew(app, store.Sessions, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	addUUID(c) // not exposed yet; a list of the member's own sessions would name them by it
	c.Fields.Add(
		relation("member", membersID, true, true),
		// hex SHA-256 of the cookie token
		&core.TextField{Name: "token_hash", Required: true, Min: 64, Max: 64, Pattern: "^[0-9a-f]{64}$", Hidden: true},
		&core.DateField{Name: "expires_at", Required: true},
		&core.DateField{Name: "last_seen_at"}, // the UTC day of the last use, not the time
	)
	addCreated(c)
	uniqueIndex(c, "token_hash", "`token_hash`", "")
	index(c, "member", "`member`")
	index(c, "expires_at", "`expires_at`")
	return app.Save(c)
}
