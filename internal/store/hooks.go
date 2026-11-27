package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// CoordDecimals is the number of decimal places a place's coordinates are stored with (B12, H3): about 1 m.
const CoordDecimals = 5

// ErrAppendOnly is returned when a record of an append-only collection is about to be changed.
var ErrAppendOnly = errors.New("store: append-only record cannot be changed")

// ErrUUIDChanged is returned when an existing record's uuid is about to be changed.
var ErrUUIDChanged = errors.New("store: uuid cannot be changed")

// UUIDPattern matches a UUIDv7 in canonical lowercase form, as [NewUUID] produces it; the uuid fields accept nothing
// else.
const UUIDPattern = `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`

var uuidRegexp = regexp.MustCompile(UUIDPattern)

// NewUUID returns a new UUIDv7 in its canonical lowercase form. External identifiers are always made with it (AD4).
func NewUUID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("store: generate uuid: %w", err)
	}
	return id.String(), nil
}

// IsUUID reports whether s is a UUIDv7 in canonical lowercase form, the only form of an external identifier: a
// request naming an object by anything else cannot match one.
func IsUUID(s string) bool {
	return uuidRegexp.MatchString(s)
}

// RoundCoord rounds a latitude or longitude to [CoordDecimals] decimal places.
func RoundCoord(v float64) float64 {
	const scale = 1e5 // 10^CoordDecimals
	return math.Round(v*scale) / scale
}

// BindHooks registers the record hooks that hold the data model's invariants whatever code saves a record:
//
//   - a record of any collection with a uuid field gets a new UUIDv7 when it is created without one, and its uuid
//     never changes afterwards (AD4);
//   - a place's lat and lon are rounded to [CoordDecimals] decimal places on every create and update (B12);
//   - a member's display_name_key and pseudonym_key are set to the [NameKey] of display_name and pseudonym on every
//     create and update;
//   - records of the [AppendOnly] collections cannot be updated;
//   - records of the [SoftDeleted] collections cannot be removed: they are deleted with [SoftDelete].
//
// The hooks run before PocketBase validates and writes the record, so the collections' field rules (the uuid
// pattern, the coordinate ranges) apply to the values set here.
func BindHooks(app core.App) {
	app.OnRecordCreate().BindFunc(func(e *core.RecordEvent) error {
		if err := setUUID(e.Record); err != nil {
			return err
		}
		switch e.Record.Collection().Name {
		case Places:
			roundPlaceCoords(e.Record)
		case Members:
			setNameKeys(e.Record)
		}
		return e.Next()
	})

	app.OnRecordUpdate().BindFunc(func(e *core.RecordEvent) error {
		name := e.Record.Collection().Name
		if slices.Contains(AppendOnly, name) {
			return fmt.Errorf("%w: %s %s", ErrAppendOnly, name, e.Record.Id)
		}
		if err := keepUUID(e.App, e.Record); err != nil {
			return err
		}
		switch name {
		case Places:
			roundPlaceCoords(e.Record)
		case Members:
			setNameKeys(e.Record)
		}
		return e.Next()
	})

	app.OnRecordDelete().BindFunc(func(e *core.RecordEvent) error {
		if name := e.Record.Collection().Name; HasSoftDelete(name) {
			return fmt.Errorf("%w: %s %s", ErrHardDelete, name, e.Record.Id)
		}
		return e.Next()
	})
}

// setUUID gives a new record of a collection with a uuid field a fresh UUIDv7 if it has none yet.
func setUUID(record *core.Record) error {
	if record.Collection().Fields.GetByName(FieldUUID) == nil || record.GetString(FieldUUID) != "" {
		return nil
	}
	id, err := NewUUID()
	if err != nil {
		return err
	}
	record.Set(FieldUUID, id)
	return nil
}

// keepUUID rejects a change of an existing record's uuid. A record saved before it had a uuid (rows that existed before
// the migration added the field) may get one. The stored value is read from the database: a record's Original() is
// blank when the record was created in memory rather than loaded.
func keepUUID(app core.App, record *core.Record) error {
	if record.Collection().Fields.GetByName(FieldUUID) == nil {
		return nil
	}
	var old string
	err := app.DB().Select(FieldUUID).From(record.Collection().Name).
		Where(dbx.HashExp{"id": record.LastSavedPK()}).Row(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if old == "" {
		return setUUID(record)
	}
	if record.GetString(FieldUUID) != old {
		return fmt.Errorf("%w: %s %s", ErrUUIDChanged, record.Collection().Name, record.Id)
	}
	return nil
}

func roundPlaceCoords(record *core.Record) {
	for _, field := range []string{"lat", "lon"} {
		record.Set(field, RoundCoord(record.GetFloat(field)))
	}
}

// Members' name keys: the [NameKey] of a member's display_name and pseudonym.
const (
	FieldDisplayNameKey = "display_name_key"
	FieldPseudonymKey   = "pseudonym_key"
)

// setNameKeys sets a member's name keys from the names, when the collection has them (before the migration that adds
// them, it does not).
func setNameKeys(record *core.Record) {
	fields := record.Collection().Fields
	if fields.GetByName(FieldDisplayNameKey) != nil {
		record.Set(FieldDisplayNameKey, NameKey(record.GetString("display_name")))
	}
	if fields.GetByName(FieldPseudonymKey) != nil {
		record.Set(FieldPseudonymKey, NameKey(record.GetString("pseudonym")))
	}
}
