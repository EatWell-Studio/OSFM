package store_test

import (
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// savePlaces saves places with the given names and returns them.
func savePlaces(t *testing.T, app core.App, names ...string) []*core.Record {
	t.Helper()
	var out []*core.Record
	for i, name := range names {
		p := newPlace(t, app, 31+float64(i)/100, 121)
		p.Set("name", name)
		if err := app.Save(p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func names(t *testing.T, records []*core.Record) []string {
	t.Helper()
	var out []string
	for _, r := range records {
		out = append(out, r.GetString("name"))
	}
	return out
}

// TestSoftDeletedMatchesSchema keeps store.SoftDeleted in step with the collections that have a deleted_at field.
func TestSoftDeletedMatchesSchema(t *testing.T) {
	app := newApp(t)
	collections, err := app.FindAllCollections()
	if err != nil {
		t.Fatal(err)
	}
	var withField []string
	for _, c := range collections {
		if c.Fields.GetByName(store.FieldDeletedAt) != nil {
			withField = append(withField, c.Name)
		}
	}
	slices.Sort(withField)
	want := slices.Sorted(slices.Values(store.SoftDeleted))
	if !slices.Equal(withField, want) {
		t.Errorf("collections with %s: %v; store.SoftDeleted: %v", store.FieldDeletedAt, withField, want)
	}
}

func TestSoftDelete(t *testing.T) {
	app := newApp(t)
	places := savePlaces(t, app, "Alpha", "Beta", "Gamma")
	beta := places[1]

	if store.IsDeleted(beta) {
		t.Fatal("a new place is deleted")
	}
	if err := store.SoftDelete(app, beta); err != nil {
		t.Fatal(err)
	}
	if !store.IsDeleted(beta) {
		t.Fatal("not deleted after SoftDelete")
	}

	// The row stays, with its uuid and the deletion time.
	stored, err := app.FindRecordById(store.Places, beta.Id)
	if err != nil {
		t.Fatalf("the row is gone: %v", err)
	}
	deletedAt := stored.GetDateTime(store.FieldDeletedAt)
	if deletedAt.IsZero() || stored.GetString(store.FieldUUID) != beta.GetString(store.FieldUUID) {
		t.Errorf("tombstone: deleted_at %v, uuid %q", deletedAt, stored.GetString(store.FieldUUID))
	}

	// Deleting again keeps the first deletion time.
	if err := store.SoftDelete(app, stored); err != nil {
		t.Fatal(err)
	}
	again, err := app.FindRecordById(store.Places, beta.Id)
	if err != nil {
		t.Fatal(err)
	}
	if !again.GetDateTime(store.FieldDeletedAt).Equal(deletedAt) {
		t.Errorf("deletion time changed: %v, then %v", deletedAt, again.GetDateTime(store.FieldDeletedAt))
	}
}

func TestQueryLeavesOutDeleted(t *testing.T) {
	app := newApp(t)
	places := savePlaces(t, app, "Alpha", "Beta", "Gamma")
	if err := store.SoftDelete(app, places[1]); err != nil {
		t.Fatal(err)
	}

	live, err := store.Query(app, store.Places).OrderBy("name").All()
	if err != nil {
		t.Fatal(err)
	}
	if got := names(t, live); !slices.Equal(got, []string{"Alpha", "Gamma"}) {
		t.Errorf("default query: %v", got)
	}
	all, err := store.Query(app, store.Places).IncludeDeleted().OrderBy("name").All()
	if err != nil {
		t.Fatal(err)
	}
	if got := names(t, all); !slices.Equal(got, []string{"Alpha", "Beta", "Gamma"}) {
		t.Errorf("with deleted: %v", got)
	}

	// Conditions are ANDed with the filter: an OR inside a condition cannot bring the deleted place back.
	either := dbx.Or(dbx.HashExp{"name": "Beta"}, dbx.HashExp{"name": "Gamma"})
	got, err := store.Query(app, store.Places).Where(either).All()
	if err != nil {
		t.Fatal(err)
	}
	if n := names(t, got); !slices.Equal(n, []string{"Gamma"}) {
		t.Errorf("OR condition: %v", n)
	}

	// Count, limit and offset.
	if n, err := store.Query(app, store.Places).Count(); err != nil || n != 2 {
		t.Errorf("Count = %d, %v; want 2", n, err)
	}
	if n, err := store.Query(app, store.Places).IncludeDeleted().Limit(1).Count(); err != nil || n != 3 {
		t.Errorf("Count with deleted = %d, %v; want 3 (the limit does not apply)", n, err)
	}
	page, err := store.Query(app, store.Places).OrderBy("name DESC").Limit(1).Offset(1).All()
	if err != nil {
		t.Fatal(err)
	}
	if n := names(t, page); !slices.Equal(n, []string{"Alpha"}) {
		t.Errorf("second page of one: %v", n)
	}

	// One.
	first, err := store.Query(app, store.Places).Where(dbx.HashExp{"name": "Gamma"}).One()
	if err != nil || first.Id != places[2].Id {
		t.Errorf("One: %v, %v", first, err)
	}
	if _, err := store.Query(app, store.Places).Where(dbx.HashExp{"name": "Beta"}).One(); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("One for a deleted place: %v, want sql.ErrNoRows", err)
	}
}

func TestFindByUUID(t *testing.T) {
	app := newApp(t)
	places := savePlaces(t, app, "Alpha", "Beta")
	alpha, beta := places[0], places[1]
	if err := store.SoftDelete(app, beta); err != nil {
		t.Fatal(err)
	}

	got, err := store.FindByUUID(app, store.Places, alpha.GetString(store.FieldUUID))
	if err != nil || got.Id != alpha.Id {
		t.Errorf("live place: %v, %v", got, err)
	}
	if _, err := store.FindByUUID(app, store.Places, beta.GetString(store.FieldUUID)); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("deleted place: %v, want sql.ErrNoRows", err)
	}
	unknown, err := store.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindByUUID(app, store.Places, unknown); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown uuid: %v, want sql.ErrNoRows", err)
	}
	// The internal ID is not a uuid.
	if _, err := store.FindByUUID(app, store.Places, alpha.Id); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("internal ID as uuid: %v, want sql.ErrNoRows", err)
	}
	tomb, err := store.Query(app, store.Places).IncludeDeleted().
		Where(dbx.HashExp{store.FieldUUID: beta.GetString(store.FieldUUID)}).One()
	if err != nil || tomb.Id != beta.Id || !store.IsDeleted(tomb) {
		t.Errorf("deleted place with IncludeDeleted: %v, %v", tomb, err)
	}
}

// TestSoftDeleteInJoin checks that the filter's column is qualified: recommendations joined with their places.
func TestSoftDeleteInJoin(t *testing.T) {
	app := newApp(t)
	members, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		t.Fatal(err)
	}
	member := core.NewRecord(members)
	member.Set("login_name", "test_author")
	member.Set("display_name", "Test Author")
	member.Set("role", "member")
	member.SetRandomPassword()
	if err := app.Save(member); err != nil {
		t.Fatal(err)
	}
	recs, err := app.FindCollectionByNameOrId(store.Recommendations)
	if err != nil {
		t.Fatal(err)
	}
	places := savePlaces(t, app, "Alpha", "Beta")
	for _, p := range places {
		r := core.NewRecord(recs)
		r.Set("place", p.Id)
		r.Set("author", member.Id)
		r.Set("display_as", "main")
		r.Set("attitude", "good")
		r.Set("visibility", "circle")
		r.Set("text", "Test text")
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SoftDelete(app, places[1]); err != nil {
		t.Fatal(err)
	}

	// Live recommendations of live places: both tables have a deleted_at column.
	var rows []struct {
		Name string `db:"name"`
	}
	err = app.DB().Select(store.Places+".name").From(store.Recommendations).
		InnerJoin(store.Places, dbx.NewExp("places.id = recommendations.place")).
		Where(dbx.And(store.NotDeleted(store.Recommendations), store.NotDeleted(store.Places))).
		All(&rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Alpha" {
		t.Errorf("join: %+v", rows)
	}
}

func TestHardDeleteIsRejected(t *testing.T) {
	app := newApp(t)
	place := savePlaces(t, app, "Alpha")[0]
	if err := app.Delete(place); !errors.Is(err, store.ErrHardDelete) {
		t.Errorf("app.Delete(place): %v, want ErrHardDelete", err)
	}
	if _, err := app.FindRecordById(store.Places, place.Id); err != nil {
		t.Errorf("the place is gone: %v", err)
	}

	// Collections without soft delete: records are removed, and SoftDelete refuses them.
	c, err := app.FindCollectionByNameOrId(store.Notices)
	if err != nil {
		t.Fatal(err)
	}
	members, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		t.Fatal(err)
	}
	member := core.NewRecord(members)
	member.Set("login_name", "test_reader")
	member.Set("display_name", "Test Reader")
	member.Set("role", "member")
	member.SetRandomPassword()
	if err := app.Save(member); err != nil {
		t.Fatal(err)
	}
	notice := core.NewRecord(c)
	notice.Set("member", member.Id)
	notice.Set("kind", "test")
	if err := app.Save(notice); err != nil {
		t.Fatal(err)
	}
	if err := store.SoftDelete(app, notice); !errors.Is(err, store.ErrNoSoftDelete) {
		t.Errorf("SoftDelete(notice): %v, want ErrNoSoftDelete", err)
	}
	if err := app.Delete(notice); err != nil {
		t.Errorf("app.Delete(notice): %v", err)
	}
}

func TestSoftDeleteInTransaction(t *testing.T) {
	app := newApp(t)
	place := savePlaces(t, app, "Alpha")[0]
	wantErr := errors.New("roll back")
	err := app.RunInTransaction(func(tx core.App) error {
		if err := store.SoftDelete(tx, place); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatal(err)
	}
	if _, err := store.FindByUUID(app, store.Places, place.GetString(store.FieldUUID)); err != nil {
		t.Errorf("the deletion was not rolled back: %v", err)
	}
}

func TestIsUUID(t *testing.T) {
	id, err := store.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if !store.IsUUID(id) {
		t.Errorf("IsUUID(%q) = false", id)
	}
	for _, bad := range []string{"", "abc123def456ghi", "123e4567-e89b-42d3-a456-426614174000", " " + id,
		"0190a3c4-5b6d-7e8f-9a0b-1C2D3E4F5A6B"} {
		if store.IsUUID(bad) {
			t.Errorf("IsUUID(%q) = true", bad)
		}
	}
}
