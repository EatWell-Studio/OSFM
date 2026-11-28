package store

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// RevisionSnapshot is the content of a recommendation as recommendation_revisions.snapshot keeps it: the fields an edit
// can change, as they were before the edit (C5). The keys are the recommendation's field names. A snapshot holds no
// author, editor, signature kind or visibility: the revision's editor_role and editor say who edited, and only the
// role ever leaves the server.
//
// Code that writes revisions takes the snapshot with [SnapshotOf]; code that reads them decodes it with
// [ReadSnapshot], so that both agree on the keys.
type RevisionSnapshot struct {
	Attitude  string           `json:"attitude"`
	Text      string           `json:"text"`
	Dishes    []string         `json:"dishes"`
	TimeSlots []string         `json:"time_slots"`
	Season    string           `json:"season"`
	Sources   []SnapshotSource `json:"sources"`
	// VisitedOn is the date of the last visit, YYYY-MM-DD; empty when not given.
	VisitedOn string `json:"visited_on"`
}

// SnapshotSource is one source of a [RevisionSnapshot], as recommendations.sources stores it.
type SnapshotSource struct {
	URL     string `json:"url,omitempty"`
	Kind    string `json:"kind"`
	Excerpt string `json:"excerpt,omitempty"`
}

// SnapshotOf returns the content of the recommendation record r as a revision keeps it.
func SnapshotOf(r *core.Record) RevisionSnapshot {
	s := RevisionSnapshot{
		Attitude:  r.GetString("attitude"),
		Text:      r.GetString("text"),
		Dishes:    []string{},
		TimeSlots: r.GetStringSlice("time_slots"),
		Season:    r.GetString("season"),
		Sources:   []SnapshotSource{},
	}
	_ = r.UnmarshalJSONField("dishes", &s.Dishes)   // anything else stored is no dishes
	_ = r.UnmarshalJSONField("sources", &s.Sources) // anything else stored is no sources
	if d := r.GetDateTime("visited_on"); !d.IsZero() {
		s.VisitedOn = d.Time().UTC().Format(time.DateOnly)
	}
	return s.normalized()
}

// ReadSnapshot returns the snapshot of the recommendation_revisions record rev. Keys it does not know are ignored; a
// snapshot that is not an object reads as empty content.
func ReadSnapshot(rev *core.Record) RevisionSnapshot {
	var s RevisionSnapshot
	_ = rev.UnmarshalJSONField("snapshot", &s)
	return s.normalized()
}

// normalized returns s with empty lists instead of nil ones, so that they encode as [].
func (s RevisionSnapshot) normalized() RevisionSnapshot {
	if s.Dishes == nil {
		s.Dishes = []string{}
	}
	if s.TimeSlots == nil {
		s.TimeSlots = []string{}
	}
	if s.Sources == nil {
		s.Sources = []SnapshotSource{}
	}
	return s
}
