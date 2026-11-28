package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The personal data export (E6): everything the instance holds about the caller, as one JSON file they download from
// the "me" page. The format is documented in docs/data-format.md; its version is [PersonalExportSchema].
//
//   - It is the caller's own data only. Of other members it names nobody but the inviter, by main name; it carries no
//     member uuid but the caller's own, no pseudonym but the caller's own, nobody who used the caller's invites, no
//     editor but as a role ("author" or "maintainer"), no maintainer who hid a recommendation. Reports name the
//     reported recommendation by uuid, never its author.
//   - Credentials are summarised: kind, device name, dates. No password hash, TOTP secret, passkey ID or public key,
//     no recovery code, session or token.
//   - Recommendations are the caller's own as store.VisibleRecommendations gives them to their author: private,
//     hidden and pseudonym ones included; deleted ones (whose content is erased) are not. One exception to the
//     visibility rule, here only (M2-Q20): the caller's live recommendations of a place a maintainer deleted, which
//     nobody sees any more but whose text is still the author's, are exported too, marked placeDeleted, with the place
//     as stored. Nothing says who deleted the place or why (that is in the audit log only). Each recommendation comes
//     with its place (uuid, name, coordinates) and its revisions, newest first.
//   - A file download (Content-Disposition: attachment), not cached, with a per-member budget of its own: the export
//     reads every collection, and nobody needs it more than a few times in a row.

// PersonalExportPath is GET → the caller's [PersonalExport], as a file download.
const PersonalExportPath = "/api/osfm/me/export"

// PersonalExportSchema is the value of osfm:schema in a personal export: the format and its version. The version
// changes only with incompatible changes; new fields may be added within a version.
const PersonalExportSchema = "osfm-personal/1"

// PersonalExportFilePrefix starts the name of the downloaded file, which ends with the UTC date and ".json".
const PersonalExportFilePrefix = "osfm-personal-export-"

// PersonalExport is the personal data export.
type PersonalExport struct {
	// Schema is [PersonalExportSchema].
	Schema     string          `json:"osfm:schema"`
	ExportedAt time.Time       `json:"exportedAt"`
	Account    PersonalAccount `json:"account"`
	// Invites are the invites the member created, newest first.
	Invites []PersonalInvite `json:"invites"`
	// Credentials are the member's ways to sign in, oldest first.
	Credentials []PersonalCredential `json:"credentials"`
	// Recommendations are the member's recommendations, newest first.
	Recommendations []PersonalRecommendation `json:"recommendations"`
	// Reports are the reports the member submitted, newest first.
	Reports []PersonalReport `json:"reports"`
	// Notices are the member's notices, newest first.
	Notices []PersonalNotice `json:"notices"`
}

// PersonalAccount is the member's account.
type PersonalAccount struct {
	UUID      string `json:"uuid"`
	LoginName string `json:"loginName"`
	// DisplayName is the main name.
	DisplayName string `json:"displayName"`
	// Pseudonym is empty when the member has none.
	Pseudonym     string         `json:"pseudonym"`
	ExternalLinks []ExternalLink `json:"externalLinks"`
	// Role is "member" or "maintainer".
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
	// InvitedBy is the main name of the member who invited them; empty for the first maintainer.
	InvitedBy string `json:"invitedBy"`
}

// PersonalInvite is an invite the member created. Who used it is not part of the member's data.
type PersonalInvite struct {
	UUID string `json:"uuid"`
	// State is active, used, expired or revoked.
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt"`
	RevokedAt *time.Time `json:"revokedAt"`
}

// PersonalCredential is one of the member's ways to sign in, without any secret.
type PersonalCredential struct {
	// Kind is "passkey" or "password_totp".
	Kind string `json:"kind"`
	// DeviceName is the name the member gave a passkey; empty for password and TOTP.
	DeviceName string    `json:"deviceName"`
	CreatedAt  time.Time `json:"createdAt"`
	// LastUsedAt is the day (UTC midnight) it last signed in or re-verified; null before the first time.
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

// PersonalRecommendation is one of the member's recommendations.
type PersonalRecommendation struct {
	UUID string `json:"uuid"`
	// DisplayAs is how it is signed: "main" (the main name) or "pseudonym".
	DisplayAs string `json:"displayAs"`
	// Visibility is "private" or "circle".
	Visibility string   `json:"visibility"`
	Attitude   string   `json:"attitude"`
	Text       string   `json:"text"`
	Dishes     []string `json:"dishes"`
	TimeSlots  []string `json:"timeSlots"`
	Season     string   `json:"season"`
	Sources    []Source `json:"sources"`
	// VisitedOn is the date of the last visit, YYYY-MM-DD; empty when not given.
	VisitedOn            string     `json:"visitedOn"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
	EditedByMaintainerAt *time.Time `json:"editedByMaintainerAt"`
	// Hidden says why it is hidden; null when it is not.
	Hidden *HiddenInfo   `json:"hidden"`
	Place  PersonalPlace `json:"place"`
	// PlaceDeleted is true when a maintainer deleted the place: the recommendation is still the member's, but nobody
	// sees it any more. Place is the place as it was stored.
	PlaceDeleted bool `json:"placeDeleted"`
	// Revisions are the content before each edit, newest first.
	Revisions []PersonalRevision `json:"revisions"`
}

// PersonalPlace is the place of a recommendation in the personal export.
type PersonalPlace struct {
	UUID string  `json:"uuid"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// PersonalRevision is the content of a recommendation before one edit.
type PersonalRevision struct {
	UUID      string    `json:"uuid"`
	CreatedAt time.Time `json:"createdAt"`
	// EditorRole is "author" or "maintainer": who edited is never named.
	EditorRole string                  `json:"editorRole"`
	Content    PersonalRevisionContent `json:"content"`
}

// PersonalRevisionContent is the content a revision kept.
type PersonalRevisionContent struct {
	Attitude  string   `json:"attitude"`
	Text      string   `json:"text"`
	Dishes    []string `json:"dishes"`
	TimeSlots []string `json:"timeSlots"`
	Season    string   `json:"season"`
	Sources   []Source `json:"sources"`
	VisitedOn string   `json:"visitedOn"`
}

// PersonalReport is a report the member submitted.
type PersonalReport struct {
	// Recommendation is the uuid of the reported recommendation.
	Recommendation string `json:"recommendation"`
	// Reason is the reason code; Note what the member wrote with it (may be empty).
	Reason    string    `json:"reason"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
}

// PersonalNotice is a notice to the member.
type PersonalNotice struct {
	UUID string `json:"uuid"`
	Kind string `json:"kind"`
	// Ref is the uuid of the object it is about (a recommendation).
	Ref       string     `json:"ref"`
	CreatedAt time.Time  `json:"createdAt"`
	ReadAt    *time.Time `json:"readAt"`
}

// personalExportBudgetID is the route middleware of the per-member export budget.
const personalExportBudgetID = "osfmAPIPersonalExportBudget"

// personalExportRate is every member's export budget: 3 at once, then one a minute.
var personalExportRate = ratelimit.Rate{Burst: 3, Every: time.Minute}

func registerPersonalExport(se *core.ServeEvent, s *server) {
	se.Router.GET(PersonalExportPath, handlePersonalExport).
		Bind(s.MemberBudget(personalExportBudgetID, personalExportRate))
}

func handlePersonalExport(e *core.RequestEvent) error {
	member := auth.CurrentMember(e)
	if member == nil {
		return e.UnauthorizedError("", nil)
	}
	// The record as saved now, not as the session loaded it: a profile change a moment ago belongs in the file.
	fresh, err := store.FindByUUID(e.App, store.Members, member.GetString(store.FieldUUID))
	if err != nil {
		return e.InternalServerError("", err)
	}
	export, err := BuildPersonalExport(e.App, fresh, time.Now())
	if err != nil {
		return e.InternalServerError("", err)
	}
	body, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return e.InternalServerError("", err)
	}
	noStore(e)
	e.Response.Header().Set("Content-Disposition",
		`attachment; filename="`+PersonalExportFilePrefix+export.ExportedAt.Format(time.DateOnly)+`.json"`)
	return e.Blob(http.StatusOK, "application/json; charset=utf-8", append(body, '\n'))
}

// BuildPersonalExport returns the personal export of member at time now, as described at the top of this file.
func BuildPersonalExport(app core.App, member *core.Record, now time.Time) (*PersonalExport, error) {
	out := &PersonalExport{Schema: PersonalExportSchema, ExportedAt: now.UTC().Truncate(time.Second)}
	var err error
	if out.Account, err = personalAccount(app, member); err != nil {
		return nil, err
	}
	if out.Invites, err = personalInvites(app, member, now); err != nil {
		return nil, err
	}
	if out.Credentials, err = personalCredentials(app, member); err != nil {
		return nil, err
	}
	if out.Recommendations, err = personalRecommendations(app, member); err != nil {
		return nil, err
	}
	if out.Reports, err = personalReports(app, member); err != nil {
		return nil, err
	}
	if out.Notices, err = personalNotices(app, member); err != nil {
		return nil, err
	}
	return out, nil
}

func personalAccount(app core.App, m *core.Record) (PersonalAccount, error) {
	var stored []domain.ExternalLink
	_ = m.UnmarshalJSONField("external_links", &stored) // anything else stored is no links
	links := make([]ExternalLink, 0, len(stored))
	for _, l := range stored {
		links = append(links, ExternalLink{URL: l.URL, Label: l.Label})
	}
	joined := m.GetDateTime("joined_at")
	if joined.IsZero() {
		joined = m.GetDateTime(store.FieldCreated)
	}
	account := PersonalAccount{
		UUID:          m.GetString(store.FieldUUID),
		LoginName:     m.GetString("login_name"),
		DisplayName:   m.GetString("display_name"),
		Pseudonym:     m.GetString("pseudonym"),
		ExternalLinks: links,
		Role:          m.GetString("role"),
		JoinedAt:      joined.Time().UTC(),
	}
	if id := m.GetString("invited_by"); id != "" {
		// The inviter's main name only, whether they are still in the circle or not.
		inviter, err := store.Query(app, store.Members).IncludeDeleted().
			Where(dbx.HashExp{store.Members + ".id": id}).One()
		if err == nil {
			account.InvitedBy = inviter.GetString("display_name")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return PersonalAccount{}, err
		}
	}
	return account, nil
}

func personalInvites(app core.App, m *core.Record, now time.Time) ([]PersonalInvite, error) {
	records, err := app.FindRecordsByFilter(store.Invites, "created_by = {:m}", "-created,-uuid", 0, 0,
		dbx.Params{"m": m.Id})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalInvite, 0, len(records))
	for _, r := range records {
		out = append(out, PersonalInvite{
			UUID:      r.GetString(store.FieldUUID),
			State:     auth.InviteState(r, now),
			CreatedAt: r.GetDateTime(store.FieldCreated).Time().UTC(),
			ExpiresAt: r.GetDateTime("expires_at").Time().UTC(),
			UsedAt:    optionalDate(r, "used_at"),
			RevokedAt: optionalDate(r, "revoked_at"),
		})
	}
	return out, nil
}

func personalCredentials(app core.App, m *core.Record) ([]PersonalCredential, error) {
	records, err := app.FindRecordsByFilter(store.Credentials, "member = {:m}", "created,uuid", 0, 0,
		dbx.Params{"m": m.Id})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalCredential, 0, len(records))
	for _, r := range records {
		// Only these four fields are read: nothing secret can end up in the export by a field added later.
		c := PersonalCredential{
			Kind:       r.GetString("kind"),
			CreatedAt:  r.GetDateTime(store.FieldCreated).Time().UTC(),
			LastUsedAt: optionalDate(r, "last_used_at"),
		}
		if c.Kind == auth.CredentialPasskey {
			c.DeviceName = r.GetString("device_name")
		}
		out = append(out, c)
	}
	return out, nil
}

func personalRecommendations(app core.App, m *core.Record) ([]PersonalRecommendation, error) {
	v := MemberViewer(m)
	byAuthor := dbx.HashExp{store.Recommendations + ".author": m.Id}
	recs, err := store.VisibleRecommendations(app, v).Where(byAuthor).All()
	if err != nil {
		return nil, err
	}
	placeByID := map[string]*core.Record{}
	if len(recs) > 0 {
		placeIDs := make([]any, 0, len(recs))
		for _, r := range recs {
			placeIDs = append(placeIDs, r.GetString("place"))
		}
		places, err := store.VisiblePlaces(app, v).Where(dbx.In(store.Places+".id", placeIDs...)).All()
		if err != nil {
			return nil, err
		}
		for _, p := range places {
			placeByID[p.Id] = p
		}
	}
	// The exception (M2-Q20): the member's live recommendations of deleted places, which no visibility query returns.
	orphans, gonePlaces, err := recommendationsOfDeletedPlaces(app, m)
	if err != nil {
		return nil, err
	}
	recs = append(recs, orphans...)
	for id, p := range gonePlaces {
		placeByID[id] = p
	}
	out := make([]PersonalRecommendation, 0, len(recs))
	if len(recs) == 0 {
		return out, nil
	}
	sort.SliceStable(recs, func(i, j int) bool { // newest first, as the query of each part had it
		ci, cj := recs[i].GetDateTime(store.FieldCreated).Time(), recs[j].GetDateTime(store.FieldCreated).Time()
		if !ci.Equal(cj) {
			return ci.After(cj)
		}
		return recs[i].GetString(store.FieldUUID) > recs[j].GetString(store.FieldUUID)
	})
	recIDs := make([]any, 0, len(recs))
	for _, r := range recs {
		recIDs = append(recIDs, r.Id)
	}
	var revisions []*core.Record
	if err := app.RecordQuery(store.RecommendationRevisions).
		AndWhere(dbx.In("recommendation", recIDs...)).
		OrderBy("created DESC", "uuid DESC").All(&revisions); err != nil {
		return nil, err
	}
	revsByRec := map[string][]PersonalRevision{}
	for _, rev := range revisions {
		id := rev.GetString("recommendation")
		revsByRec[id] = append(revsByRec[id], personalRevision(rev))
	}

	for _, r := range recs {
		place, ok := placeByID[r.GetString("place")]
		if !ok {
			continue // cannot happen: the place is live and visible to the author, or a deleted one loaded above
		}
		view := recommendationView(v, r, domain.Author{})
		rec := PersonalRecommendation{
			UUID:                 view.UUID,
			DisplayAs:            r.GetString("display_as"),
			Visibility:           r.GetString("visibility"),
			Attitude:             view.Attitude,
			Text:                 view.Text,
			Dishes:               view.Dishes,
			TimeSlots:            nonNil(view.TimeSlots),
			Season:               view.Season,
			Sources:              view.Sources,
			VisitedOn:            view.VisitedOn,
			CreatedAt:            view.CreatedAt,
			UpdatedAt:            view.UpdatedAt,
			EditedByMaintainerAt: view.EditedByMaintainerAt,
			Hidden:               view.Hidden,
			Place: PersonalPlace{
				UUID: place.GetString(store.FieldUUID),
				Name: place.GetString("name"),
				Lat:  place.GetFloat("lat"),
				Lon:  place.GetFloat("lon"),
			},
			PlaceDeleted: store.IsDeleted(place),
			Revisions:    revsByRec[r.Id],
		}
		if rec.Revisions == nil {
			rec.Revisions = []PersonalRevision{}
		}
		out = append(out, rec)
	}
	return out, nil
}

// recommendationsOfDeletedPlaces returns the live recommendations of member m whose place was soft-deleted, with those
// places by record ID. It is the export's one exception to the visibility rule (M2-Q20): only the author, only their
// own recommendations that are not deleted themselves, only for their own copy of their data.
func recommendationsOfDeletedPlaces(app core.App, m *core.Record) ([]*core.Record, map[string]*core.Record, error) {
	places, err := store.Query(app, store.Places).IncludeDeleted().
		Where(dbx.Not(store.NotDeleted(store.Places))).
		Where(dbx.Exists(dbx.NewExp("SELECT 1 FROM {{"+store.Recommendations+"}} AS [[ro]] WHERE [[ro.place]] = [["+
			store.Places+".id]] AND [[ro.author]] = {:author} AND [[ro."+store.FieldDeletedAt+"]] = ''",
			dbx.Params{"author": m.Id}))).All()
	if err != nil || len(places) == 0 {
		return nil, nil, err
	}
	byID := make(map[string]*core.Record, len(places))
	ids := make([]any, 0, len(places))
	for _, p := range places {
		byID[p.Id] = p
		ids = append(ids, p.Id)
	}
	recs, err := store.Query(app, store.Recommendations).
		Where(dbx.HashExp{store.Recommendations + ".author": m.Id}).
		Where(dbx.In(store.Recommendations+".place", ids...)).All()
	if err != nil {
		return nil, nil, err
	}
	return recs, byID, nil
}

func personalRevision(rev *core.Record) PersonalRevision {
	s := store.ReadSnapshot(rev)
	sources := make([]Source, 0, len(s.Sources))
	for _, src := range s.Sources {
		sources = append(sources, Source{URL: src.URL, Kind: src.Kind, Excerpt: src.Excerpt})
	}
	return PersonalRevision{
		UUID:       rev.GetString(store.FieldUUID),
		CreatedAt:  rev.GetDateTime(store.FieldCreated).Time().UTC(),
		EditorRole: rev.GetString("editor_role"),
		Content: PersonalRevisionContent{
			Attitude:  s.Attitude,
			Text:      s.Text,
			Dishes:    s.Dishes,
			TimeSlots: s.TimeSlots,
			Season:    s.Season,
			Sources:   sources,
			VisitedOn: s.VisitedOn,
		},
	}
}

func personalReports(app core.App, m *core.Record) ([]PersonalReport, error) {
	records, err := app.FindRecordsByFilter(store.Reports, "reporter = {:m}", "-created,-uuid", 0, 0,
		dbx.Params{"m": m.Id})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalReport, 0, len(records))
	if len(records) == 0 {
		return out, nil
	}
	recIDs := make([]any, 0, len(records))
	for _, r := range records {
		recIDs = append(recIDs, r.GetString("recommendation"))
	}
	// Only the uuid of each reported recommendation, deleted ones included: the member reported it, whatever
	// happened to it since.
	recs, err := store.Query(app, store.Recommendations).IncludeDeleted().
		Where(dbx.In(store.Recommendations+".id", recIDs...)).All()
	if err != nil {
		return nil, err
	}
	uuidByID := make(map[string]string, len(recs))
	for _, r := range recs {
		uuidByID[r.Id] = r.GetString(store.FieldUUID)
	}
	for _, r := range records {
		id, ok := uuidByID[r.GetString("recommendation")]
		if !ok {
			continue
		}
		out = append(out, PersonalReport{
			Recommendation: id,
			Reason:         r.GetString("reason"),
			Note:           r.GetString("note"),
			CreatedAt:      r.GetDateTime(store.FieldCreated).Time().UTC(),
		})
	}
	return out, nil
}

func personalNotices(app core.App, m *core.Record) ([]PersonalNotice, error) {
	records, err := app.FindRecordsByFilter(store.Notices, "member = {:m}", "-created,-uuid", 0, 0,
		dbx.Params{"m": m.Id})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalNotice, 0, len(records))
	for _, r := range records {
		out = append(out, PersonalNotice{
			UUID:      r.GetString(store.FieldUUID),
			Kind:      r.GetString("kind"),
			Ref:       r.GetString("ref"),
			CreatedAt: r.GetDateTime(store.FieldCreated).Time().UTC(),
			ReadAt:    optionalDate(r, "read_at"),
		})
	}
	return out, nil
}

// optionalDate returns the date field of r in UTC, nil when it is empty.
func optionalDate(r *core.Record, field string) *time.Time {
	d := r.GetDateTime(field)
	if d.IsZero() {
		return nil
	}
	t := d.Time().UTC()
	return &t
}

// nonNil returns s, or an empty list for nil, so that it encodes as [].
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
