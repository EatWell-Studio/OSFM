package domain

import (
	"errors"
	"fmt"
	"regexp"
)

// Kinds of notices to a member (notices.kind). Clients show them by translation key; the reference names the
// recommendation they are about. store re-exports them as store.Notice*.
const (
	NoticeMaintainerEdited   = "maintainer_edited"    // a maintainer edited the member's recommendation (C5)
	NoticeHiddenByMaintainer = "hidden_by_maintainer" // a maintainer hid it (C6)
	NoticeHiddenByReports    = "hidden_by_reports"    // it was hidden automatically after reports (D11)
	NoticeRestored           = "restored"             // it is shown again
	NoticeViolationDeleted   = "violation_deleted"    // a maintainer deleted it as a violation (H6)
)

// NoticeKinds lists the notice kinds.
var NoticeKinds = []string{
	NoticeMaintainerEdited, NoticeHiddenByMaintainer, NoticeHiddenByReports, NoticeRestored, NoticeViolationDeleted,
}

// Notice is a notice to a member. It names the member, what happened and the object; never who did it: no maintainer,
// no reporter, no count of reports (privacy invariant 5).
type Notice struct {
	// MemberID is the internal record ID of the member the notice is for.
	MemberID string
	// Kind is one of [NoticeKinds].
	Kind string
	// RefUUID is the uuid of the recommendation it is about.
	RefUUID string
}

// NoticeWriter stores notices. store.NoticesIn(tx) is the implementation: it writes into the notices collection within
// the transaction tx, so that a notice exists if and only if the change it reports was committed.
type NoticeWriter interface {
	WriteNotice(Notice) error
}

// ErrInvalidNotice is returned by [Notify] for a notice without a member, of an unknown kind or with a reference that
// is not a uuid.
var ErrInvalidNotice = errors.New("domain: invalid notice")

// uuidPattern is store.UUIDPattern (a UUIDv7 in canonical form), repeated here so that domain stays free of store.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// Notify writes a notice of kind about the object refUUID for the member memberID through w, the notices of the
// transaction that makes the change (T031, T036; T037 shows them):
//
//	err := app.RunInTransaction(func(tx core.App) error {
//		// … edit the recommendation …
//		return domain.Notify(store.NoticesIn(tx), rec.GetString("author"), domain.NoticeMaintainerEdited,
//			rec.GetString(store.FieldUUID))
//	})
func Notify(w NoticeWriter, memberID, kind, refUUID string) error {
	if memberID == "" || !isNoticeKind(kind) || !uuidPattern.MatchString(refUUID) {
		return fmt.Errorf("%w: kind %q", ErrInvalidNotice, kind)
	}
	return w.WriteNotice(Notice{MemberID: memberID, Kind: kind, RefUUID: refUUID})
}

func isNoticeKind(kind string) bool {
	for _, k := range NoticeKinds {
		if k == kind {
			return true
		}
	}
	return false
}
