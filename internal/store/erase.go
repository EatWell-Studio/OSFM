package store

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// ErasedFields are the content fields of a recommendation that [DeleteRecommendation] erases (M2-Q9): the text, the
// dishes, the sources, the season and the date of the last visit. What stays in the tombstone is what it needs to be
// one: its uuid, place, author, signature kind, visibility, attitude, time slots, hidden state and dates.
var ErasedFields = []string{"text", "dishes", "sources", "season", "visited_on"}

// DeleteRecommendation deletes the recommendation record rec as M2-Q9 says every deletion of a recommendation does,
// whoever deletes it (its author, T031; a maintainer deleting a violation, T036; a member leaving with their
// recommendations deleted, T038): it erases [ErasedFields], soft-deletes it ([SoftDelete]) and removes its revisions
// (they belong to a recommendation that exists; once it is deleted, they go too). Run it in the transaction of the
// change, through its app (tx), so that all of it happens or none. Reports of the recommendation are kept (with the
// resolution the caller sets); notices and audit entries are the caller's.
//
// A recommendation deleted already is erased again (a no-op for one deleted this way) and keeps its deletion time.
func DeleteRecommendation(app core.App, rec *core.Record) error {
	if rec.Collection().Name != Recommendations {
		return fmt.Errorf("store: DeleteRecommendation: record of %s", rec.Collection().Name)
	}
	rec.Set("text", "")
	rec.Set("dishes", []string{})
	rec.Set("sources", []any{})
	rec.Set("season", "")
	rec.Set("visited_on", "")
	if IsDeleted(rec) {
		if err := app.Save(rec); err != nil {
			return err
		}
	} else if err := SoftDelete(app, rec); err != nil {
		return err
	}
	revisions, err := app.FindAllRecords(RecommendationRevisions, dbx.HashExp{"recommendation": rec.Id})
	if err != nil {
		return err
	}
	for _, r := range revisions {
		if err := app.Delete(r); err != nil {
			return err
		}
	}
	return nil
}
