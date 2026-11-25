package store

// Collection names. Every collection's API rules are nil (superusers only): clients never use PocketBase's collection
// API, only /api/osfm/* (AD2). The schema itself is created by the migrations in /migrations.
const (
	// Members and authentication (M1).
	Members          = "members" // the auth collection, renamed from PocketBase's default "users"
	Credentials      = "credentials"
	RecoveryCodes    = "recovery_codes"
	Invites          = "invites"
	ResetLinks       = "reset_links"
	AuditLog         = "audit_log"
	InstanceSettings = "instance_settings"

	// Places and recommendations (M2; created early so that the fields reserved for federation are fixed).
	Places                  = "places"
	Recommendations         = "recommendations"
	RecommendationRevisions = "recommendation_revisions"
	Reports                 = "reports"
	Notices                 = "notices"
)

// Field names shared by several collections.
const (
	FieldUUID      = "uuid"       // external ID, UUIDv7, set by the server (AD4)
	FieldCreated   = "created"    // autodate
	FieldUpdated   = "updated"    // autodate
	FieldDeletedAt = "deleted_at" // soft delete: empty while the record is live
)

// SettingsID is the record ID of the single instance_settings row. The collection's ID field accepts only this value,
// so a second row cannot be inserted.
const SettingsID = "instancesetting"

// AppendOnly lists the collections whose records are never updated once written.
var AppendOnly = []string{AuditLog, RecommendationRevisions}

// PlaceCategories is the food whitelist for places.category. The values are OpenStreetMap tag values, so that a place
// linked to OSM (osm_ref) maps to a category without a translation table: amenity=restaurant, fast_food, cafe,
// food_court, ice_cream, bar, pub and biergarten; shop=bakery and shop=pastry; street_vendor for stalls and carts,
// which OSM tags with street_vendor=yes rather than with an amenity or shop value of their own. Bars, pubs and beer
// gardens are included because they are commonly recommended for their food too.
var PlaceCategories = []string{
	"restaurant", "fast_food", "cafe", "food_court", "ice_cream", "bakery", "pastry", "street_vendor", "bar", "pub",
	"biergarten",
}
