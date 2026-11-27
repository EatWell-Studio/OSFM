// Package api holds the HTTP handlers of the OSFM features under /api/osfm/ and the request and response types that
// clients see. Responses are projections: they never carry internal IDs, author IDs or pseudonym mappings (E2, AD4).
// The TypeScript types of the web app are generated from this package's exported types (web/src/api/types.gen.ts, see
// tygo.yaml); keep request and response types here, and give every field a json tag. tygo copies the exported constants
// of the files it reads (every file except api.go, budget.go, doc.go, request.go and viewer.go) as they are written:
// make them literals ("/api/osfm/places", "already_recommended"), not expressions that refer to other packages, or the
// generated file does not compile (the web app's typecheck catches it). After changing a type, run
// `go tool tygo generate` and commit web/src/api/types.gen.ts with it (bin/check-api-types.sh, in the gate and CI).
//
// # Layout
//
// [Bind] registers every feature when the app serves; cmd/osfm calls it once. Each feature has its own file with one
// register function, called from [Bind]'s list, so that features developed side by side do not touch the same file:
//
//	settings.go         registerSettings         GET/POST /api/osfm/instance (T024)
//	places.go           registerPlaces           creating places and recommendations, nearby places (T026)
//	map.go              registerMap              map data, the place card, signatures (T027)
//	recommendations.go  registerRecommendations  editing, revisions, deleting; editing places (T031)
//	reports.go          registerReports          hiding, reports and their review (T036)
//	notices.go          registerNotices          the member's notices (T037)
//	profile.go          registerProfile          main name, pseudonym, external links (T033)
//	governance.go       registerGovernance       member invites, the invite tree, removal, roles (T034, T035)
//	leave.go            registerLeave            leaving the circle (T038)
//	personal_export.go  registerPersonalExport   the personal data export (T040)
//	layer_export.go     registerLayerExport      the per-layer GeoJSON exports (T041)
//	backups.go          registerBackups          backups (T042)
//	geocode.go          registerGeocode          place-name search (T039)
//
// A register function adds routes to se.Router; s carries what the routes share:
//
//	func registerPlaces(se *core.ServeEvent, s *server) {
//		se.Router.POST(PlacesPath, handleCreatePlace).Bind(s.WriteBudget())
//		se.Router.GET(PlacesNearbyPath, handleNearby)
//	}
//
// Every route then also gets probes in the internal ID scan (cmd/osfm/probes_<feature>_test.go) and integration tests
// as the four identities (internal/testapp).
//
// # Rules every handler follows
//
//   - Who: the session middleware of internal/auth has answered 401 already; [ViewerOf] gives the caller as a
//     [domain.Viewer]. Maintainer routes carry [MaintainersOnly], routes that need a fresh proof auth.RequireFresh().
//   - Reads of recommendations and places go through store.VisibleRecommendations and store.VisiblePlaces (or the
//     condition store.RecommendationVisibleTo); a recommendation leaves the server only as a [RecommendationView]
//     made by [RecommendationViews].
//   - Writes: the X-OSFM header is checked by internal/auth; bind [server.WriteBudget] (per member); read the body with
//     [readJSON] and a size limit; name objects by uuid ([pathUUID]: anything else is 404).
//   - Errors are [Error] bodies with a machine-readable code ([fail] and its shorthands).
//   - Member and maintainer responses are not cached ([respond] sets Cache-Control: no-store unless the handler set
//     another policy, as the map data does).
//   - No coordinates, search terms or recommendation text in logs (architecture §3.9).
package api
