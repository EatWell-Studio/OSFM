package migrations

import (
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The fields M2 needs on the collections T005 created (T023):
//
//   - recommendations: hidden_kind, hidden_at and hidden_by, why, when and by whom a recommendation was hidden
//     (hidden and hidden_reason stay); a partial unique index on (place, author) over the live records: a member has
//     at most one live recommendation per place.
//   - reports: uuid; reason becomes a reason code (store.ReportReasons) with an optional note; resolved_at,
//     resolved_by and resolution, how a maintainer resolved the report.
//   - recommendation_revisions, notices: uuid.
//   - members: display_name_key and pseudonym_key, the store.NameKey of the names, for the uniqueness check of names.
//   - places: category and status are optional (B2).
//   - instance_settings: geocoder_enabled, on by default.
//
// Rows that exist before the migration get what the new fields need: uuids, name keys, a reason code (a free-text
// reason becomes the note of reason "other"), and the hidden kind "maintainer" for a hidden recommendation.
func init() {
	m.Register(upM2Model, downM2Model)
}

// Names of the indexes this migration adds.
const (
	idxRecommendationsPlaceAuthor = "idx_recommendations_place_author"
	idxMembersDisplayNameKey      = "idx_members_display_name_key"
	idxMembersPseudonymKey        = "idx_members_pseudonym_key"
)

// m2ReportFields are the fields this migration adds to reports, besides the uuid.
var m2ReportFields = []string{"note", "resolved_at", "resolved_by", "resolution"}

func upM2Model(app core.App) error {
	membersID, err := collectionID(app, store.Members)
	if err != nil {
		return err
	}
	if err := upM2Recommendations(app, membersID); err != nil {
		return fmt.Errorf("%s: %w", store.Recommendations, err)
	}
	if err := upM2Reports(app, membersID); err != nil {
		return fmt.Errorf("%s: %w", store.Reports, err)
	}
	for _, name := range []string{store.RecommendationRevisions, store.Notices} {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			return err
		}
		if err := addUUIDAndBackfill(app, c, nil); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := upM2Members(app); err != nil {
		return fmt.Errorf("%s: %w", store.Members, err)
	}
	if err := setPlaceFieldsRequired(app, false); err != nil {
		return fmt.Errorf("%s: %w", store.Places, err)
	}
	if err := upGeocoderEnabled(app); err != nil {
		return fmt.Errorf("%s: %w", store.InstanceSettings, err)
	}
	return nil
}

func upM2Recommendations(app core.App, membersID string) error {
	c, err := app.FindCollectionByNameOrId(store.Recommendations)
	if err != nil {
		return err
	}
	// One live recommendation per member and place. Soft delete leaves deleted_at empty while a record is live
	// (store.NotDeleted), so the index covers exactly the live records and a deleted one does not block a new one.
	var pairs int
	if err := app.DB().NewQuery("SELECT COUNT(*) FROM (SELECT 1 FROM {{" + store.Recommendations + "}} " +
		"WHERE [[" + store.FieldDeletedAt + "]] = '' GROUP BY [[place]], [[author]] HAVING COUNT(*) > 1)").
		Row(&pairs); err != nil {
		return err
	}
	if pairs > 0 {
		return fmt.Errorf("%d members have more than one live recommendation of the same place; "+
			"delete all but one of each before upgrading", pairs)
	}
	c.Fields.Add(
		selectOne("hidden_kind", false, store.HiddenKinds...), // empty while not hidden
		&core.DateField{Name: "hidden_at"},
		relation("hidden_by", membersID, false, false), // empty when hidden automatically
	)
	c.AddIndex(idxRecommendationsPlaceAuthor, true, "`place`, `author`", "`"+store.FieldDeletedAt+"` = ''")
	if err := app.Save(c); err != nil {
		return err
	}
	// A recommendation hidden before there were hidden kinds can only have been hidden by hand.
	_, err = app.DB().Update(store.Recommendations, dbx.Params{"hidden_kind": store.HiddenByMaintainer},
		dbx.HashExp{"hidden": true, "hidden_kind": ""}).Execute()
	return err
}

func upM2Reports(app core.App, membersID string) error {
	c, err := app.FindCollectionByNameOrId(store.Reports)
	if err != nil {
		return err
	}
	c.Fields.Add(
		reportReasonField(),
		text("note", store.MaxReportNoteLength),
		&core.DateField{Name: "resolved_at"}, // empty while the report is open
		relation("resolved_by", membersID, false, false),
		selectOne("resolution", false, store.Resolutions...),
	)
	return addUUIDAndBackfill(app, c, func() error {
		// Reasons were free text: one that is not a reason code becomes the note of reason "other".
		_, err := app.DB().Update(store.Reports, dbx.Params{
			"note":   dbx.NewExp(fmt.Sprintf("substr(trim([[reason]]), 1, %d)", store.MaxReportNoteLength)),
			"reason": store.ReasonOther,
		}, dbx.Not(dbx.In("reason", anySlice(store.ReportReasons)...))).Execute()
		return err
	})
}

// reportReasonField is reports.reason as a reason code. It stays a text field, as T005 created it, with a pattern that
// accepts only the codes: PocketBase cannot change a field's type, and a field of another type under the same name
// would make T005's migration fail when the migrations run again.
func reportReasonField() *core.TextField {
	return &core.TextField{
		Name: "reason", Required: true, Max: 32, Pattern: "^(" + strings.Join(store.ReportReasons, "|") + ")$",
	}
}

// addUUIDAndBackfill adds the uuid field and its unique index to c, with the other changes already made to c, and
// saves it. When the collection has rows without a uuid, it saves first without the index, runs fill (if not nil; the
// rows then have the new fields), gives every row a uuid and then adds the index. A collection without rows (a new
// database) is saved only once: every collection save rebuilds indexes, and the migrations run for every test app.
func addUUIDAndBackfill(app core.App, c *core.Collection, fill func() error) error {
	hasRows := false
	if c.Fields.GetByName(store.FieldUUID) == nil {
		var n int
		if err := app.DB().Select("COUNT(*)").From(c.Name).Row(&n); err != nil {
			return err
		}
		hasRows = n > 0
	}
	c.Fields.Add(&core.TextField{Name: store.FieldUUID, Required: true, Min: 36, Max: 36, Pattern: store.UUIDPattern})
	index := func() { c.AddIndex("idx_"+c.Name+"_uuid", true, "`"+store.FieldUUID+"`", "") }
	if !hasRows {
		index()
	}
	if err := app.Save(c); err != nil {
		return err
	}
	if fill != nil {
		if err := fill(); err != nil {
			return err
		}
	}
	if !hasRows {
		return nil
	}
	// With SQL, not records: the store hooks reject any update of a recommendation_revisions record.
	if err := backfillUUIDs(app, c.Name); err != nil {
		return err
	}
	index()
	return app.Save(c)
}

// upM2Members adds the name keys. Main names and pseudonyms must be unique across both fields together, which a
// unique index on each column cannot express (a main name equal to another member's pseudonym), so the indexes are
// plain ones for the check the profile code makes in a transaction; members from before this migration may also share
// a name already.
func upM2Members(app core.App) error {
	c, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		return err
	}
	// No explicit maximum (PocketBase's default, 5000): NFKC can make a 64-character name several times longer.
	c.Fields.Add(&core.TextField{Name: store.FieldDisplayNameKey}, &core.TextField{Name: store.FieldPseudonymKey})
	c.AddIndex(idxMembersDisplayNameKey, false, "`"+store.FieldDisplayNameKey+"`", "")
	c.AddIndex(idxMembersPseudonymKey, false, "`"+store.FieldPseudonymKey+"`", "`"+store.FieldPseudonymKey+"` != ''")
	if err := app.Save(c); err != nil {
		return err
	}
	return backfillNameKeys(app)
}

// backfillNameKeys sets the name keys of every member whose keys do not match the names.
func backfillNameKeys(app core.App) error {
	var rows []struct {
		ID             string `db:"id"`
		DisplayName    string `db:"display_name"`
		Pseudonym      string `db:"pseudonym"`
		DisplayNameKey string `db:"display_name_key"`
		PseudonymKey   string `db:"pseudonym_key"`
	}
	if err := app.DB().Select("id", "display_name", "pseudonym", store.FieldDisplayNameKey, store.FieldPseudonymKey).
		From(store.Members).All(&rows); err != nil {
		return err
	}
	for _, r := range rows {
		dk, pk := store.NameKey(r.DisplayName), store.NameKey(r.Pseudonym)
		if dk == r.DisplayNameKey && pk == r.PseudonymKey {
			continue
		}
		if _, err := app.DB().Update(store.Members,
			dbx.Params{store.FieldDisplayNameKey: dk, store.FieldPseudonymKey: pk},
			dbx.HashExp{"id": r.ID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// setPlaceFieldsRequired makes places.category and places.status required or optional. Only the name and the
// coordinates are required of a place (B2); a status nobody has confirmed is empty rather than "open".
func setPlaceFieldsRequired(app core.App, required bool) error {
	c, err := app.FindCollectionByNameOrId(store.Places)
	if err != nil {
		return err
	}
	for _, name := range []string{"category", "status"} {
		f, ok := c.Fields.GetByName(name).(*core.SelectField)
		if !ok {
			return fmt.Errorf("field %s is missing or not a select field", name)
		}
		f.Required = required
	}
	return app.Save(c)
}

// upGeocoderEnabled adds instance_settings.geocoder_enabled and turns it on when the field is new; when it already
// exists (the migration runs again), the maintainers' setting is kept.
func upGeocoderEnabled(app core.App) error {
	c, err := app.FindCollectionByNameOrId(store.InstanceSettings)
	if err != nil {
		return err
	}
	isNew := c.Fields.GetByName("geocoder_enabled") == nil
	c.Fields.Add(&core.BoolField{Name: "geocoder_enabled"})
	if err := app.Save(c); err != nil {
		return err
	}
	if !isNew {
		return nil
	}
	_, err = app.DB().Update(store.InstanceSettings, dbx.Params{"geocoder_enabled": true}, nil).Execute()
	return err
}

func downM2Model(app core.App) error {
	settings, err := app.FindCollectionByNameOrId(store.InstanceSettings)
	if err != nil {
		return err
	}
	settings.Fields.RemoveByName("geocoder_enabled")
	if err := app.Save(settings); err != nil {
		return fmt.Errorf("%s: %w", store.InstanceSettings, err)
	}

	if err := setPlaceFieldsRequired(app, true); err != nil {
		return fmt.Errorf("%s: %w", store.Places, err)
	}

	members, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		return err
	}
	members.RemoveIndex(idxMembersDisplayNameKey)
	members.RemoveIndex(idxMembersPseudonymKey)
	members.Fields.RemoveByName(store.FieldDisplayNameKey)
	members.Fields.RemoveByName(store.FieldPseudonymKey)
	if err := app.Save(members); err != nil {
		return fmt.Errorf("%s: %w", store.Members, err)
	}

	for _, name := range []string{store.Notices, store.RecommendationRevisions} {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			return err
		}
		c.RemoveIndex("idx_" + c.Name + "_uuid")
		c.Fields.RemoveByName(store.FieldUUID)
		if err := app.Save(c); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}

	reports, err := app.FindCollectionByNameOrId(store.Reports)
	if err != nil {
		return err
	}
	// Back to a free-text reason: the code, followed by the note when there is one.
	if _, err := app.DB().Update(store.Reports,
		dbx.Params{"reason": dbx.NewExp("substr([[reason]] || ': ' || [[note]], 1, 1000)")},
		dbx.NewExp("[[note]] != ''")).Execute(); err != nil {
		return fmt.Errorf("%s: %w", store.Reports, err)
	}
	reports.RemoveIndex("idx_" + reports.Name + "_uuid")
	reports.Fields.RemoveByName(store.FieldUUID)
	for _, name := range m2ReportFields {
		reports.Fields.RemoveByName(name)
	}
	reports.Fields.Add(text("reason", 1000)) // as T005 created it
	if err := app.Save(reports); err != nil {
		return fmt.Errorf("%s: %w", store.Reports, err)
	}

	recs, err := app.FindCollectionByNameOrId(store.Recommendations)
	if err != nil {
		return err
	}
	recs.RemoveIndex(idxRecommendationsPlaceAuthor)
	for _, name := range []string{"hidden_kind", "hidden_at", "hidden_by"} {
		recs.Fields.RemoveByName(name)
	}
	if err := app.Save(recs); err != nil {
		return fmt.Errorf("%s: %w", store.Recommendations, err)
	}
	return nil
}

func anySlice(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
