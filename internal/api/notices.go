package api

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The member's notices (C5, C6, D11, M2-Q14): what happened to their recommendations that someone else did. T031 and
// T036 write them with domain.Notify in the transaction of the change; this file reads them and marks them read.
//
//   - A member reads and marks only their own notices. A uuid of another member's notice is answered as one that does
//     not exist (404), and marking it read changes nothing, not even the caller's other notices of the same request.
//   - A notice carries what happened (kind), when, whether it was read, and the recommendation it is about. Never who
//     did it: no maintainer, no reporter, no number of reports (privacy invariant 5); the notices collection does not
//     store any of it.
//   - The recommendation is resolved when the notice is read, through store.VisibleRecommendations and only among the
//     member's own recommendations: target names it, its place and the place's name while the member can still see
//     it (hidden recommendations included: their author sees them). Once it is deleted (by its author, as a violation)
//     or its place is, target is null and the client shows a generic text instead of a link.

// Paths of the notices.
const (
	// NoticesPath is GET: the caller's notices, newest first, with the number of unread ones ([NoticeList]).
	NoticesPath = "/api/osfm/me/notices"
	// NoticesReadPath is POST ([MarkNoticesRead]): marks notices read; answered with the [NoticeList] afterwards.
	NoticesReadPath = "/api/osfm/me/notices/read"
)

// Kinds of notices (domain.NoticeKinds; the client shows them by translation key). A notice of a kind the client does
// not know gets a generic text.
const (
	NoticeKindMaintainerEdited   = "maintainer_edited"    // a maintainer edited the recommendation (C5)
	NoticeKindHiddenByMaintainer = "hidden_by_maintainer" // a maintainer hid it (C6)
	NoticeKindHiddenByReports    = "hidden_by_reports"    // it was hidden automatically after reports (D11)
	NoticeKindRestored           = "restored"             // it is shown again
	NoticeKindViolationDeleted   = "violation_deleted"    // a maintainer deleted it as a violation (H6)
)

// Limits of the notices.
const (
	// MaxNoticesListed is the number of notices GET [NoticesPath] returns at most: the newest ones. Unread counts all.
	MaxNoticesListed = 100
	// MaxNoticesPerRead is the number of uuids one POST [NoticesReadPath] may name.
	MaxNoticesPerRead = 100
)

// maxNoticesReadBody bounds the body of POST [NoticesReadPath]: MaxNoticesPerRead quoted uuids and some.
const maxNoticesReadBody = 8 << 10

// NoticeList is the caller's notices.
type NoticeList struct {
	// Notices are the newest [MaxNoticesListed] notices, newest first.
	Notices []Notice `json:"notices"`
	// Unread is the number of all the caller's unread notices, listed or not.
	Unread int `json:"unread"`
}

// Notice is one notice to the caller.
type Notice struct {
	UUID string `json:"uuid"`
	// Kind is one of the NoticeKind* constants (or a kind a newer server writes).
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"createdAt"`
	Read      bool      `json:"read"`
	// Target is the recommendation the notice is about while the caller can see it; null once it or its place is
	// deleted.
	Target *NoticeTarget `json:"target" tstype:"NoticeTarget | null,required"`
}

// NoticeTarget is the recommendation a notice is about: where the client links to (the place's card, or the
// recommendation's revisions after a maintainer's edit).
type NoticeTarget struct {
	Recommendation string `json:"recommendation"`
	Place          string `json:"place"`
	PlaceName      string `json:"placeName"`
}

// MarkNoticesRead is the body of POST [NoticesReadPath]: either uuids (1 to [MaxNoticesPerRead] of the caller's own
// notices) or all (every notice of the caller); not both. Notices read already stay as they are.
type MarkNoticesRead struct {
	UUIDs []string `json:"uuids,omitempty"`
	All   bool     `json:"all,omitempty"`
}

func registerNotices(se *core.ServeEvent, s *server) {
	se.Router.GET(NoticesPath, handleNotices)
	se.Router.POST(NoticesReadPath, handleMarkNoticesRead).Bind(s.WriteBudget())
}

func handleNotices(e *core.RequestEvent) error {
	v := ViewerOf(e)
	if !v.SignedIn() {
		return e.UnauthorizedError("", nil)
	}
	list, err := ListNotices(e.App, v, MaxNoticesListed)
	if err != nil {
		return e.InternalServerError("", err)
	}
	return respond(e, http.StatusOK, list)
}

// errNoticeNotFound is a uuid in POST [NoticesReadPath] that names no notice of the caller.
var errNoticeNotFound = errors.New("api: no such notice")

func handleMarkNoticesRead(e *core.RequestEvent) error {
	v := ViewerOf(e)
	if !v.SignedIn() {
		return e.UnauthorizedError("", nil)
	}
	var req MarkNoticesRead
	if err := readJSON(e, &req, maxNoticesReadBody); err != nil {
		return requestError(e, err)
	}
	if req.All == (len(req.UUIDs) > 0) || len(req.UUIDs) > MaxNoticesPerRead {
		return badRequest(e)
	}
	for _, id := range req.UUIDs {
		if !store.IsUUID(id) {
			return badRequest(e)
		}
	}
	err := markNoticesRead(e.App, v, req)
	switch {
	case errors.Is(err, errNoticeNotFound):
		return notFound(e)
	case err != nil:
		return e.InternalServerError("", err)
	}
	list, err := ListNotices(e.App, v, MaxNoticesListed)
	if err != nil {
		return e.InternalServerError("", err)
	}
	return respond(e, http.StatusOK, list)
}

// markNoticesRead marks the notices of req read, in one transaction: all of them or, when a uuid names no unread or
// read notice of v, none ([errNoticeNotFound]).
func markNoticesRead(app core.App, v domain.Viewer, req MarkNoticesRead) error {
	return app.RunInTransaction(func(tx core.App) error {
		q := store.Query(tx, store.Notices).Where(dbx.HashExp{"member": v.MemberID})
		var ids []string
		if !req.All {
			ids = slices.Compact(slices.Sorted(slices.Values(req.UUIDs)))
			q.Where(dbx.In(store.FieldUUID, anyOf(ids)...))
		}
		notices, err := q.All()
		if err != nil {
			return err
		}
		if !req.All && len(notices) != len(ids) {
			return errNoticeNotFound
		}
		now := types.NowDateTime()
		for _, n := range notices {
			if !n.GetDateTime("read_at").IsZero() {
				continue
			}
			n.Set("read_at", now)
			if err := tx.Save(n); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListNotices returns the notices of v, newest first, at most limit of them (all for limit 0), with the number of all
// unread ones; the personal data export (T040) can use it with limit 0.
func ListNotices(app core.App, v domain.Viewer, limit int) (NoticeList, error) {
	list := NoticeList{Notices: []Notice{}}
	if !v.SignedIn() {
		return list, nil
	}
	mine := dbx.HashExp{"member": v.MemberID}
	unread, err := store.Query(app, store.Notices).Where(mine).Where(dbx.HashExp{"read_at": ""}).Count()
	if err != nil {
		return list, err
	}
	list.Unread = int(unread)
	rows, err := store.Query(app, store.Notices).Where(mine).
		OrderBy(store.FieldCreated+" DESC", store.FieldUUID+" DESC").Limit(int64(limit)).All()
	if err != nil {
		return list, err
	}
	targets, err := noticeTargets(app, v, rows)
	if err != nil {
		return list, err
	}
	for _, r := range rows {
		n := Notice{
			UUID:      r.GetString(store.FieldUUID),
			Kind:      r.GetString("kind"),
			CreatedAt: r.GetDateTime(store.FieldCreated).Time().UTC(),
			Read:      !r.GetDateTime("read_at").IsZero(),
		}
		if t, ok := targets[r.GetString("ref")]; ok {
			n.Target = &t
		}
		list.Notices = append(list.Notices, n)
	}
	return list, nil
}

// noticeTargets resolves the references of notices to the recommendations of v that v can see, by recommendation uuid.
func noticeTargets(app core.App, v domain.Viewer, notices []*core.Record) (map[string]NoticeTarget, error) {
	var refs []string
	for _, n := range notices {
		if ref := n.GetString("ref"); store.IsUUID(ref) {
			refs = append(refs, ref)
		}
	}
	targets := map[string]NoticeTarget{}
	if len(refs) == 0 {
		return targets, nil
	}
	recs, err := store.VisibleRecommendations(app, v).
		Where(dbx.HashExp{store.Recommendations + ".author": v.MemberID}).
		Where(dbx.In(store.Recommendations+"."+store.FieldUUID, anyOf(slices.Compact(slices.Sorted(slices.Values(refs))))...)).
		All()
	if err != nil || len(recs) == 0 {
		return targets, err
	}
	placeIDs := make([]any, 0, len(recs))
	for _, r := range recs {
		placeIDs = append(placeIDs, r.GetString("place"))
	}
	places, err := store.VisiblePlaces(app, v).Where(dbx.In(store.Places+".id", placeIDs...)).All()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*core.Record, len(places))
	for _, p := range places {
		byID[p.Id] = p
	}
	for _, r := range recs {
		p, ok := byID[r.GetString("place")]
		if !ok {
			continue
		}
		targets[r.GetString(store.FieldUUID)] = NoticeTarget{
			Recommendation: r.GetString(store.FieldUUID),
			Place:          p.GetString(store.FieldUUID),
			PlaceName:      p.GetString("name"),
		}
	}
	return targets, nil
}
