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
	Sessions         = "sessions" // sign-in sessions: hashes of the session cookie tokens (AD6)

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

// Reasons a member gives when reporting a recommendation (reports.reason, D11). The values are stable codes: clients
// translate them, and they appear in exports. ReasonOther needs a note; the others take an optional one.
const (
	ReasonAdvertising      = "advertising"          // advertising or a conflict of interest
	ReasonAbuse            = "abuse"                // personal attacks or discrimination
	ReasonPoliticalIllegal = "political_or_illegal" // political or illegal content
	ReasonUnverifiedClaim  = "unverified_claim"     // an unverified accusation, such as about hygiene
	ReasonFalseOrDuplicate = "false_or_duplicate"   // clearly untrue, or a duplicate
	ReasonOther            = "other"                // anything else; the note says what
	MaxReportNoteLength    = 500                    // reports.note, in characters
)

// ReportReasons lists the report reason codes, the values reports.reason accepts.
var ReportReasons = []string{
	ReasonAdvertising, ReasonAbuse, ReasonPoliticalIllegal, ReasonUnverifiedClaim, ReasonFalseOrDuplicate, ReasonOther,
}

// Why a recommendation is hidden (recommendations.hidden_kind). It is empty while the recommendation is not hidden.
// recommendations.hidden_by names the maintainer who hid it, or who removed its author; it is empty when it was hidden
// automatically.
const (
	HiddenByMaintainer  = "maintainer"     // a maintainer hid it, with a reason (C6)
	HiddenByReports     = "reports"        // hidden automatically when the open reports reached the threshold (D11)
	HiddenMemberRemoved = "member_removed" // a maintainer removed its author from the circle and chose to hide (D7)
)

// HiddenKinds lists the values recommendations.hidden_kind accepts.
var HiddenKinds = []string{HiddenByMaintainer, HiddenByReports, HiddenMemberRemoved}

// How a maintainer resolved the reports of a recommendation (reports.resolution). It is empty while a report is open;
// only open reports count towards the automatic hiding threshold.
const (
	ResolutionRestored   = "restored"    // the recommendation is shown again
	ResolutionKeptHidden = "kept_hidden" // the recommendation stays hidden
	ResolutionDeleted    = "deleted"     // the recommendation was deleted as a violation (H6)
)

// Resolutions lists the values reports.resolution accepts.
var Resolutions = []string{ResolutionRestored, ResolutionKeptHidden, ResolutionDeleted}
