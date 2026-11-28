package testapp

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// SeedNotice writes an unread notice of kind about the recommendation with the uuid ref for member, as the features
// that change recommendations do (domain.Notify through store.NoticesIn), and returns it.
func (a *App) SeedNotice(member *core.Record, kind, ref string) *core.Record {
	a.t.Helper()
	if err := domain.Notify(store.NoticesIn(a), member.Id, kind, ref); err != nil {
		a.t.Fatalf("testapp: seed notice: %v", err)
	}
	n, err := store.Query(a, store.Notices).Where(dbx.HashExp{"member": member.Id}).
		OrderBy(store.FieldCreated+" DESC", store.FieldUUID+" DESC").One()
	if err != nil {
		a.t.Fatal(err)
	}
	return n
}
