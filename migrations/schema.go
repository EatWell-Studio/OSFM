package migrations

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The helpers below describe the collections declaratively. Each migration builds the wanted state onto the existing
// collection when there is one (fields are replaced by name, indexes by index name), so running an Up migration again
// changes nothing.

// findOrNew returns the collection called name, or a new one of the given type when it does not exist.
func findOrNew(app core.App, name, typ string) (*core.Collection, error) {
	c, err := app.FindCollectionByNameOrId(name)
	switch {
	case err == nil:
		if c.Type != typ {
			return nil, fmt.Errorf("collection %s exists with type %s, want %s", name, c.Type, typ)
		}
		return c, nil
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	case typ == core.CollectionTypeAuth:
		return core.NewAuthCollection(name), nil
	default:
		return core.NewBaseCollection(name), nil
	}
}

// deleteIfExists deletes the collection called name, with its table and records, if it exists.
func deleteIfExists(app core.App, name string) error {
	c, err := app.FindCollectionByNameOrId(name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return app.Delete(c)
}

// superusersOnly sets every API rule to nil: only superusers may use PocketBase's collection API (AD2).
func superusersOnly(c *core.Collection) {
	c.ListRule = nil
	c.ViewRule = nil
	c.CreateRule = nil
	c.UpdateRule = nil
	c.DeleteRule = nil
	if c.IsAuth() {
		c.ManageRule = nil
	}
}

// addUUID adds the required uuid field and its unique index.
func addUUID(c *core.Collection) {
	c.Fields.Add(&core.TextField{Name: store.FieldUUID, Required: true, Min: 36, Max: 36, Pattern: store.UUIDPattern})
	c.AddIndex("idx_"+c.Name+"_uuid", true, "`"+store.FieldUUID+"`", "")
}

// addTimestamps adds the created and updated autodate fields.
func addTimestamps(c *core.Collection) {
	c.Fields.Add(
		&core.AutodateField{Name: store.FieldCreated, OnCreate: true},
		&core.AutodateField{Name: store.FieldUpdated, OnCreate: true, OnUpdate: true},
	)
}

// addCreated adds only the created autodate field, for append-only collections.
func addCreated(c *core.Collection) {
	c.Fields.Add(&core.AutodateField{Name: store.FieldCreated, OnCreate: true})
}

// addDeletedAt adds the soft-delete marker: empty while the record is live.
func addDeletedAt(c *core.Collection) {
	c.Fields.Add(&core.DateField{Name: store.FieldDeletedAt})
}

// text returns an optional text field of at most maxLen characters.
func text(name string, maxLen int) *core.TextField {
	return &core.TextField{Name: name, Max: maxLen}
}

// requiredText returns a required (non-empty) text field of at most maxLen characters.
func requiredText(name string, maxLen int) *core.TextField {
	return &core.TextField{Name: name, Max: maxLen, Required: true}
}

// selectOne returns a single-value select field.
func selectOne(name string, required bool, values ...string) *core.SelectField {
	return &core.SelectField{Name: name, Values: values, MaxSelect: 1, Required: required}
}

// relation returns a single relation to the collection with ID collectionID.
func relation(name, collectionID string, required, cascade bool) *core.RelationField {
	return &core.RelationField{
		Name: name, CollectionId: collectionID, MaxSelect: 1, Required: required, CascadeDelete: cascade,
	}
}

// integer returns an optional integer field with an optional minimum.
func integer(name string, minValue *float64) *core.NumberField {
	return &core.NumberField{Name: name, OnlyInt: true, Min: minValue}
}

func ptr(v float64) *float64 { return &v }

// index adds a non-unique index on columns (a comma-separated list of column names).
func index(c *core.Collection, suffix, columns string) {
	c.AddIndex("idx_"+c.Name+"_"+suffix, false, columns, "")
}

// uniqueIndex adds a unique index on columns, limited to the rows matching where when where is not empty.
func uniqueIndex(c *core.Collection, suffix, columns, where string) {
	c.AddIndex("idx_"+c.Name+"_"+suffix, true, columns, where)
}

// collectionID returns the ID of the collection called name.
func collectionID(app core.App, name string) (string, error) {
	c, err := app.FindCollectionByNameOrId(name)
	if err != nil {
		return "", fmt.Errorf("find collection %s: %w", name, err)
	}
	return c.Id, nil
}
