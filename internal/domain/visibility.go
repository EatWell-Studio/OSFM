package domain

// Visibility of a recommendation (recommendations.visibility, C4).
const (
	VisibilityPrivate   = "private"   // only its author sees it
	VisibilityCircle    = "circle"    // every member sees it
	VisibilityFederated = "federated" // reserved for federation; within the instance it is treated as circle
)

// SharedVisibilities are the visibilities that let members other than the author see a recommendation. A value
// that is not one of the known visibilities is treated as private.
var SharedVisibilities = []string{VisibilityCircle, VisibilityFederated}

// Viewer is the caller a read is made for: a signed-in member with their role, or nobody (the zero Viewer). Build one
// with api.ViewerOf from the request's session; never from anything the client sends.
type Viewer struct {
	// MemberID is the internal record ID of the signed-in member, "" when nobody is signed in. It is compared with the
	// author of recommendations on the server and is never sent to a client (AD4).
	MemberID string
	// Maintainer is true when the member is a maintainer (D6).
	Maintainer bool
}

// SignedIn reports whether the viewer is a signed-in member.
func (v Viewer) SignedIn() bool { return v.MemberID != "" }

// IsAuthor reports whether the viewer is the member with the internal ID authorID.
func (v Viewer) IsAuthor(authorID string) bool { return v.SignedIn() && authorID == v.MemberID }

// Recommendation is what the visibility rules need to know about a recommendation.
type Recommendation struct {
	// AuthorID is the internal record ID of its author.
	AuthorID string
	// Visibility is one of the Visibility* values.
	Visibility string
	// Hidden is true while it is hidden, by a maintainer, by reports or with its removed author (C6, D7, D11).
	Hidden bool
	// Deleted is true once it is soft-deleted, by its author or as a violation (C6, H6).
	Deleted bool
	// PlaceDeleted is true once its place is soft-deleted (by a maintainer, M2-Q4): its recommendations go with it.
	PlaceDeleted bool
}

// CanSeeRecommendation reports whether v may see r (C4, C6, E1; architecture §3.4):
//
//   - nobody sees a deleted recommendation or one of a deleted place, and nobody who is not signed in sees any;
//   - the author always sees their own, private or hidden;
//   - a private recommendation is seen by its author only, maintainers included;
//   - a hidden recommendation is seen by its author and the maintainers;
//   - otherwise (circle, federated) every member sees it.
//
// store.RecommendationVisibleTo is the same rule as a query condition; the two are tested against each other.
func CanSeeRecommendation(v Viewer, r Recommendation) bool {
	switch {
	case !v.SignedIn() || r.Deleted || r.PlaceDeleted:
		return false
	case v.IsAuthor(r.AuthorID):
		return true
	case !isShared(r.Visibility):
		return false
	case r.Hidden:
		return v.Maintainer
	default:
		return true
	}
}

func isShared(visibility string) bool {
	for _, s := range SharedVisibilities {
		if visibility == s {
			return true
		}
	}
	return false
}

// CanSeePlace reports whether v may see a place (M2-Q3): the place is not deleted and at least one of its
// recommendations is visible to v. A place whose only recommendations are someone else's private (or hidden, for a
// member) ones does not exist for v: showing it would tell that someone noted it privately. recommendations are all
// of the place's recommendations, deleted ones included or not.
func CanSeePlace(v Viewer, placeDeleted bool, recommendations []Recommendation) bool {
	if placeDeleted {
		return false
	}
	for _, r := range recommendations {
		if CanSeeRecommendation(v, r) {
			return true
		}
	}
	return false
}
