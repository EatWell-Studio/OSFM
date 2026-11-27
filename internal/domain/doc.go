// Package domain holds the business rules for places, recommendations, members, invitations and reports, as pure
// functions over plain values: who may see a recommendation or a place, how attitudes are counted, which name a
// recommendation is signed with, what a notice is. It imports neither PocketBase nor the other OSFM packages, so the
// rules are unit-tested without a database (architecture AD3).
//
// internal/store applies the same rules to its queries (store.VisibleRecommendations, store.VisiblePlaces) and
// implements the interfaces declared here (store.Notices for [Notify]); internal/api calls both.
package domain
