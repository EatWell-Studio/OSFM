package domain

// Attitudes of a recommendation (recommendations.attitude, C2).
const (
	AttitudeMustTry = "must_try"
	AttitudeGood    = "good"
	AttitudeOK      = "ok"
	AttitudeAvoid   = "avoid"
)

// Attitudes lists the attitudes in the order the app shows them.
var Attitudes = []string{AttitudeMustTry, AttitudeGood, AttitudeOK, AttitudeAvoid}

// AttitudeCounts is the number of people per attitude at a place (A4): numbers only, never an average or a list of
// who.
type AttitudeCounts struct {
	MustTry int `json:"mustTry"`
	Good    int `json:"good"`
	OK      int `json:"ok"`
	Avoid   int `json:"avoid"`
}

// Total returns the number of people counted.
func (c AttitudeCounts) Total() int { return c.MustTry + c.Good + c.OK + c.Avoid }

// CountedRecommendation is a recommendation with its attitude, for [CountAttitudes].
type CountedRecommendation struct {
	Recommendation
	Attitude string
}

// CountAttitudes counts the people per attitude among recommendations, a place's recommendations (A4):
//
//   - only recommendations v may see count ([CanSeeRecommendation]), so the numbers never reveal a private or hidden
//     recommendation (privacy invariant 4);
//   - hidden recommendations do not count even for those who may see them (the author, maintainers): the card shows
//     them as hidden, and a number that changes with the viewer's role would tell members apart;
//   - each author counts once, with the attitude of their first recommendation in the given order (pass them newest
//     first). A member has at most one live recommendation per place (M2-Q1, enforced by a unique index); this is the
//     second line of defence, so that a main-name and a pseudonym recommendation of one member could never add up to
//     two people;
//   - unknown attitudes are not counted.
func CountAttitudes(v Viewer, recommendations []CountedRecommendation) AttitudeCounts {
	var c AttitudeCounts
	seen := map[string]bool{}
	for _, r := range recommendations {
		if r.Hidden || !CanSeeRecommendation(v, r.Recommendation) || seen[r.AuthorID] {
			continue
		}
		counted := true
		switch r.Attitude {
		case AttitudeMustTry:
			c.MustTry++
		case AttitudeGood:
			c.Good++
		case AttitudeOK:
			c.OK++
		case AttitudeAvoid:
			c.Avoid++
		default:
			counted = false
		}
		if counted {
			seen[r.AuthorID] = true
		}
	}
	return c
}
