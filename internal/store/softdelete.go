package store

import (
	"errors"
	"fmt"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Soft delete (F5). The records of the [SoftDeleted] collections are never removed: deleting one sets its deleted_at
// field, and the row stays as a tombstone with its uuid, so that exports (and, later, other instances) learn about
// the deletion instead of an object silently disappearing.
//
//   - Read through [Query] or [FindByUUID]: they leave out deleted records unless the caller opts in with
//     [RecordQuery.IncludeDeleted]. Reading these collections with PocketBase's own finders (app.FindRecordById,
//     app.RecordQuery, …) skips the filter and is reserved for code that handles tombstones itself.
//   - Delete with [SoftDelete]. A hard delete (app.Delete) of such a record is rejected by the hook [BindHooks]
//     registers, whatever code attempts it.
//   - Whether a tombstone keeps or erases the record's content is decided by the business rules that delete (a member
//     leaving, an author deleting a recommendation), not here.

// SoftDeleted lists the collections whose records are soft-deleted: the ones with a deleted_at field.
var SoftDeleted = []string{Members, Places, Recommendations}

// ErrHardDelete is returned when a record of a [SoftDeleted] collection is about to be removed.
var ErrHardDelete = errors.New("store: records of this collection are soft-deleted, not removed")

// ErrNoSoftDelete is returned by [SoftDelete] for a record of a collection without soft delete.
var ErrNoSoftDelete = errors.New("store: collection has no soft delete")

// HasSoftDelete reports whether the records of the collection called name are soft-deleted.
func HasSoftDelete(name string) bool {
	return slices.Contains(SoftDeleted, name)
}

// IsDeleted reports whether record has been soft-deleted.
func IsDeleted(record *core.Record) bool {
	return !record.GetDateTime(FieldDeletedAt).IsZero()
}

// SoftDelete marks record as deleted now and saves it, through app so that it can run inside a transaction. A record
// that is already deleted keeps its deletion time.
func SoftDelete(app core.App, record *core.Record) error {
	name := record.Collection().Name
	if !HasSoftDelete(name) {
		return fmt.Errorf("%w: %s", ErrNoSoftDelete, name)
	}
	if IsDeleted(record) {
		return nil
	}
	record.Set(FieldDeletedAt, types.NowDateTime())
	if err := app.Save(record); err != nil {
		record.Set(FieldDeletedAt, "")
		return err
	}
	return nil
}

// NotDeleted returns the condition that selects the live records of the collection called name: deleted_at is empty
// (PocketBase stores an unset date as an empty string). The column is qualified with the table name, so the condition
// also works in a query that joins other tables.
func NotDeleted(name string) dbx.Expression {
	return dbx.HashExp{name + "." + FieldDeletedAt: ""}
}

// RecordQuery is a query for the records of one collection. Its conditions are always combined with AND, and for a
// [SoftDeleted] collection the condition [NotDeleted] is added unless [RecordQuery.IncludeDeleted] is called; there is
// no way to replace or OR away that filter by accident, as there is with dbx's Where and OrWhere. Build one with
// [Query]; the methods modify the query and return it for chaining.
type RecordQuery struct {
	app            core.App
	collection     string
	includeDeleted bool
	where          []dbx.Expression
	orderBy        []string
	limit, offset  int64
}

// Query returns a query for the records of the collection called name that leaves out soft-deleted records.
func Query(app core.App, name string) *RecordQuery {
	return &RecordQuery{app: app, collection: name}
}

// IncludeDeleted makes the query return soft-deleted records too: an explicit opt-in for exports, tombstones and
// maintenance code.
func (q *RecordQuery) IncludeDeleted() *RecordQuery {
	q.includeDeleted = true
	return q
}

// Where adds a condition; all conditions must hold. A nil condition is ignored.
func (q *RecordQuery) Where(e dbx.Expression) *RecordQuery {
	if e != nil {
		q.where = append(q.where, e)
	}
	return q
}

// OrderBy adds sort columns, as dbx's OrderBy takes them ("created DESC").
func (q *RecordQuery) OrderBy(columns ...string) *RecordQuery {
	q.orderBy = append(q.orderBy, columns...)
	return q
}

// Limit sets the maximum number of records [RecordQuery.All] returns; 0 means no limit.
func (q *RecordQuery) Limit(n int64) *RecordQuery {
	q.limit = n
	return q
}

// Offset sets the number of matching records [RecordQuery.All] skips.
func (q *RecordQuery) Offset(n int64) *RecordQuery {
	q.offset = n
	return q
}

// conditions returns the query's conditions, including the soft-delete filter where it applies.
func (q *RecordQuery) conditions() []dbx.Expression {
	conds := slices.Clone(q.where)
	if !q.includeDeleted && HasSoftDelete(q.collection) {
		conds = append(conds, NotDeleted(q.collection))
	}
	return conds
}

func (q *RecordQuery) build() *dbx.SelectQuery {
	sq := q.app.RecordQuery(q.collection)
	if conds := q.conditions(); len(conds) > 0 {
		sq.AndWhere(dbx.And(conds...))
	}
	if len(q.orderBy) > 0 {
		sq.OrderBy(q.orderBy...)
	}
	if q.limit > 0 {
		sq.Limit(q.limit)
	}
	if q.offset > 0 {
		sq.Offset(q.offset)
	}
	return sq
}

// All returns the matching records.
func (q *RecordQuery) All() ([]*core.Record, error) {
	var records []*core.Record
	if err := q.build().All(&records); err != nil {
		return nil, err
	}
	return records, nil
}

// One returns the first matching record (in the query's order), or an error wrapping sql.ErrNoRows when none matches.
func (q *RecordQuery) One() (*core.Record, error) {
	record := &core.Record{}
	if err := q.build().Limit(1).One(record); err != nil {
		return nil, err
	}
	return record, nil
}

// Count returns the number of matching records; the limit and offset do not apply.
func (q *RecordQuery) Count() (int64, error) {
	return q.app.CountRecords(q.collection, q.conditions()...)
}

// FindByUUID returns the live record of the collection called name with the given uuid, or an error wrapping
// sql.ErrNoRows when there is none or it has been soft-deleted. To find a deleted record too, use
// Query(app, name).IncludeDeleted().Where(dbx.HashExp{FieldUUID: id}).One().
func FindByUUID(app core.App, name, id string) (*core.Record, error) {
	return Query(app, name).Where(dbx.HashExp{name + "." + FieldUUID: id}).One()
}
