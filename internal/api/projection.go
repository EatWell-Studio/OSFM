package api

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// RecommendationView is a recommendation as every response shows it (architecture §3.2): the signature the server
// decided, the content, and whether the caller wrote it. It never carries the author's uuid or internal ID, the
// signature kind (main name or pseudonym), editors or reporters (privacy invariants 1, 2, 5).
type RecommendationView struct {
	UUID string `json:"uuid"`
	// DisplayName is the main name or the pseudonym; empty when AuthorLeft.
	DisplayName string `json:"displayName"`
	// AuthorLeft is true for a main-name recommendation whose author has left the circle (M2-Q10): the client shows
	// "a member who left" in its language.
	AuthorLeft bool `json:"authorLeft,omitempty"`
	// ExternalLinks are the author's self-declared links (D8): only with a main name, never with a pseudonym (E3).
	ExternalLinks []ExternalLink `json:"externalLinks,omitempty"`
	Attitude      string         `json:"attitude"`
	Text          string         `json:"text"`
	Dishes        []string       `json:"dishes"`
	TimeSlots     []string       `json:"timeSlots"`
	Season        string         `json:"season"`
	Sources       []Source       `json:"sources"`
	// VisitedOn is the date of the last visit, YYYY-MM-DD; empty when not given.
	VisitedOn            string     `json:"visitedOn,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
	EditedByMaintainerAt *time.Time `json:"editedByMaintainerAt,omitempty"`
	// IsMine is true only when the caller is the author.
	IsMine bool `json:"isMine"`
	// Visibility is given to the author only (private or circle), for their own badge and edit form.
	Visibility string `json:"visibility,omitempty"`
	// Hidden is given only for a hidden recommendation, which only its author and the maintainers see (C6).
	Hidden *HiddenInfo `json:"hidden,omitempty"`
}

// ExternalLink is a link a member declared on their profile (D8).
type ExternalLink struct {
	URL   string `json:"url"`
	Label string `json:"label,omitempty"`
}

// Source is where a recommendation's knowledge comes from: a link, its kind (visited, chat, note, other) and a short
// excerpt.
type Source struct {
	URL     string `json:"url,omitempty"`
	Kind    string `json:"kind"`
	Excerpt string `json:"excerpt,omitempty"`
}

// HiddenInfo says why a recommendation is hidden: the kind (maintainer, reports, member_removed) and, except for
// reports, the reason the maintainer wrote. It names no maintainer and no reporter (privacy invariant 5).
type HiddenInfo struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitempty"`
}

// RecommendationViews projects recommendation records for the viewer v, in the given order. Records v may not see are
// left out (a second line of defence: callers read through store.VisibleRecommendations already). The authors are
// loaded in one query, the deleted ones too (their main-name recommendations show as by a member who left).
func RecommendationViews(app core.App, v domain.Viewer, recs []*core.Record) ([]RecommendationView, error) {
	visible, err := store.FilterVisible(app, v, recs)
	if err != nil {
		return nil, err
	}
	authorIDs := []any{}
	seen := map[string]bool{}
	for _, r := range visible {
		if id := r.GetString("author"); !seen[id] {
			seen[id] = true
			authorIDs = append(authorIDs, id)
		}
	}
	authors := map[string]domain.Author{}
	if len(authorIDs) > 0 {
		members, err := store.Query(app, store.Members).IncludeDeleted().
			Where(dbx.In(store.Members+".id", authorIDs...)).All()
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			authors[m.Id] = authorOf(m)
		}
	}
	views := make([]RecommendationView, 0, len(visible))
	for _, r := range visible {
		author, ok := authors[r.GetString("author")]
		if !ok {
			author = domain.Author{Left: true} // no such member: never guess a name
		}
		views = append(views, recommendationView(v, r, author))
	}
	return views, nil
}

// authorOf returns what the signature needs to know about the member record m (store.AuthorOf).
func authorOf(m *core.Record) domain.Author { return store.AuthorOf(m) }

// recommendationView projects r, which v may see, signed as domain.ResolveSignature decides for author.
func recommendationView(v domain.Viewer, r *core.Record, author domain.Author) RecommendationView {
	sig := domain.ResolveSignature(r.GetString("display_as"), author)
	view := RecommendationView{
		UUID:        r.GetString(store.FieldUUID),
		DisplayName: sig.DisplayName,
		AuthorLeft:  sig.AuthorLeft,
		Attitude:    r.GetString("attitude"),
		Text:        r.GetString("text"),
		Dishes:      stringList(r, "dishes"),
		TimeSlots:   r.GetStringSlice("time_slots"),
		Season:      r.GetString("season"),
		Sources:     sources(r),
		CreatedAt:   r.GetDateTime(store.FieldCreated).Time().UTC(),
		UpdatedAt:   r.GetDateTime(store.FieldUpdated).Time().UTC(),
		IsMine:      v.IsAuthor(r.GetString("author")),
	}
	for _, l := range sig.Links {
		view.ExternalLinks = append(view.ExternalLinks, ExternalLink{URL: l.URL, Label: l.Label})
	}
	if d := r.GetDateTime("visited_on"); !d.IsZero() {
		view.VisitedOn = d.Time().UTC().Format(time.DateOnly)
	}
	if d := r.GetDateTime("edited_by_maintainer_at"); !d.IsZero() {
		t := d.Time().UTC()
		view.EditedByMaintainerAt = &t
	}
	if view.IsMine {
		view.Visibility = r.GetString("visibility")
	}
	if store.IsHidden(r) && (view.IsMine || v.Maintainer) {
		kind := r.GetString("hidden_kind")
		if kind == "" {
			kind = store.HiddenByMaintainer
		}
		view.Hidden = &HiddenInfo{Kind: kind}
		if kind != store.HiddenByReports {
			view.Hidden.Reason = r.GetString("hidden_reason")
		}
	}
	return view
}

// stringList returns the JSON field of r as a list of strings; empty when it is not one.
func stringList(r *core.Record, field string) []string {
	out := []string{}
	_ = r.UnmarshalJSONField(field, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

// sources returns the sources of r; empty when the field holds anything else.
func sources(r *core.Record) []Source {
	out := []Source{}
	_ = r.UnmarshalJSONField("sources", &out)
	if out == nil {
		out = []Source{}
	}
	return out
}
