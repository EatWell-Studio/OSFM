package migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The M2 collections: places, recommendations and what hangs off them. They have no business logic yet; they are
// created now so that the fields reserved for federation (uuid, soft delete, visibility "federated") exist from the
// first release (F5).
func init() {
	m.Register(func(app core.App) error {
		membersID, err := collectionID(app, store.Members)
		if err != nil {
			return err
		}
		if err := upPlaces(app); err != nil {
			return err
		}
		placesID, err := collectionID(app, store.Places)
		if err != nil {
			return err
		}
		if err := upRecommendations(app, placesID, membersID); err != nil {
			return err
		}
		recommendationsID, err := collectionID(app, store.Recommendations)
		if err != nil {
			return err
		}
		if err := upRecommendationRevisions(app, recommendationsID, membersID); err != nil {
			return err
		}
		if err := upReports(app, recommendationsID, membersID); err != nil {
			return err
		}
		return upNotices(app, membersID)
	}, func(app core.App) error {
		for _, name := range []string{
			store.Notices, store.Reports, store.RecommendationRevisions, store.Recommendations, store.Places,
		} {
			if err := deleteIfExists(app, name); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		return nil
	})
}

// places: the places layer, licensed separately (ODbL) and exported on its own (F1, H5). It carries no member fields
// at all: who created a place or confirmed its status is recorded in audit_log.
func upPlaces(app core.App) error {
	c, err := findOrNew(app, store.Places, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	addUUID(c)
	c.Fields.Add(
		requiredText("name", 200),
		&core.JSONField{Name: "aliases"}, // array of strings
		// WGS-84, rounded to store.CoordDecimals decimals by the store hooks (B12, H3). Not "required": PocketBase
		// treats 0 as empty, and 0 is a valid coordinate.
		&core.NumberField{Name: "lat", Min: ptr(-90), Max: ptr(90)},
		&core.NumberField{Name: "lon", Min: ptr(-180), Max: ptr(180)},
		selectOne("precision", true, "exact", "approx", "area"),
		text("position_note", 500),
		&core.JSONField{Name: "landmark"}, // {type: place|text, place_uuid, text}
		&core.TextField{Name: "osm_ref", Max: 32, Pattern: `^(node|way|relation)/[1-9][0-9]*$`},
		selectOne("category", true, store.PlaceCategories...),
		&core.JSONField{Name: "cuisine"}, // array of strings (OSM cuisine=* values)
		selectOne("status", true, "open", "maybe_closed", "closed"),
		&core.DateField{Name: "status_confirmed_at"},
		&core.JSONField{Name: "source_coord"}, // the coordinates as entered: {lat, lon, crs: wgs84|gcj02|bd09, url}
	)
	addDeletedAt(c)
	addTimestamps(c)
	index(c, "lat_lon", "`lat`, `lon`") // map queries by city and viewport
	c.AddIndex("idx_places_osm_ref", false, "`osm_ref`", "`osm_ref` != ''")
	return app.Save(c)
}

// recommendations: the recommendations layer (CC BY-SA 4.0). The author is stored here but never leaves the server
// (E2); display_as decides whether the main name or the pseudonym is shown.
func upRecommendations(app core.App, placesID, membersID string) error {
	c, err := findOrNew(app, store.Recommendations, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	addUUID(c)
	c.Fields.Add(
		relation("place", placesID, true, false),
		relation("author", membersID, true, false),
		selectOne("display_as", true, "main", "pseudonym"),
		selectOne("attitude", true, "must_try", "good", "ok", "avoid"),
		selectOne("visibility", true, "private", "circle", "federated"),
		text("text", 5000),
		&core.JSONField{Name: "dishes"},
		&core.SelectField{
			Name: "time_slots", MaxSelect: 5, Values: []string{"breakfast", "lunch", "afternoon", "dinner", "late_night"},
		},
		text("season", 100),
		&core.JSONField{Name: "sources"}, // [{url, kind: visited|chat|note|other, excerpt}]
		&core.DateField{Name: "visited_on"},
		&core.BoolField{Name: "hidden"},
		text("hidden_reason", 500),
		&core.DateField{Name: "edited_by_maintainer_at"},
	)
	addDeletedAt(c)
	addTimestamps(c)
	index(c, "place", "`place`")
	index(c, "author", "`author`")
	return app.Save(c)
}

// recommendation_revisions: append-only snapshots taken before each edit (C5). Records are never updated (enforced by
// the store hooks); they go only with their recommendation.
func upRecommendationRevisions(app core.App, recommendationsID, membersID string) error {
	c, err := findOrNew(app, store.RecommendationRevisions, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	c.Fields.Add(
		relation("recommendation", recommendationsID, true, true),
		selectOne("editor_role", true, "author", "maintainer"),
		relation("editor", membersID, true, false),
		&core.JSONField{Name: "snapshot", Required: true},
	)
	addCreated(c)
	index(c, "recommendation", "`recommendation`")
	return app.Save(c)
}

// reports: one report per member and recommendation (D11).
func upReports(app core.App, recommendationsID, membersID string) error {
	c, err := findOrNew(app, store.Reports, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	c.Fields.Add(
		relation("recommendation", recommendationsID, true, true),
		relation("reporter", membersID, true, false),
		text("reason", 1000),
	)
	addCreated(c)
	uniqueIndex(c, "recommendation_reporter", "`recommendation`, `reporter`", "")
	return app.Save(c)
}

// notices: messages for a member (for example that a recommendation was hidden or edited by a maintainer).
func upNotices(app core.App, membersID string) error {
	c, err := findOrNew(app, store.Notices, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	c.Fields.Add(
		relation("member", membersID, true, true),
		requiredText("kind", 64),
		text("ref", 200),
		&core.DateField{Name: "read_at"},
	)
	addCreated(c)
	index(c, "member", "`member`")
	return app.Save(c)
}
