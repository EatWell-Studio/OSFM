package domain_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/EatWell-Studio/OSFM/internal/domain"
)

// The four identities of the permission tests, as viewers: nobody signed in, a member, the author and a maintainer.
const authorID, memberID, maintainerID = "authorrecordid1", "memberrecordid1", "maintrecordid01"

var (
	anonymous  = domain.Viewer{}
	member     = domain.Viewer{MemberID: memberID}
	author     = domain.Viewer{MemberID: authorID}
	maintainer = domain.Viewer{MemberID: maintainerID, Maintainer: true}
	identities = map[string]domain.Viewer{
		"anonymous": anonymous, "member": member, "author": author, "maintainer": maintainer,
	}
)

// TestCanSeeRecommendation goes through every state a recommendation by the author can be in, for each identity.
func TestCanSeeRecommendation(t *testing.T) {
	type want struct{ anonymous, member, author, maintainer bool }
	for _, tc := range []struct {
		name string
		rec  domain.Recommendation
		want want
	}{
		{"circle", domain.Recommendation{Visibility: domain.VisibilityCircle}, want{false, true, true, true}},
		{"federated is circle within the instance", domain.Recommendation{Visibility: domain.VisibilityFederated},
			want{false, true, true, true}},
		{"private", domain.Recommendation{Visibility: domain.VisibilityPrivate}, want{false, false, true, false}},
		{"unknown visibility is private", domain.Recommendation{Visibility: "public"}, want{false, false, true, false}},
		{"empty visibility is private", domain.Recommendation{}, want{false, false, true, false}},
		{"hidden", domain.Recommendation{Visibility: domain.VisibilityCircle, Hidden: true},
			want{false, false, true, true}},
		{"hidden and private", domain.Recommendation{Visibility: domain.VisibilityPrivate, Hidden: true},
			want{false, false, true, false}},
		{"deleted", domain.Recommendation{Visibility: domain.VisibilityCircle, Deleted: true},
			want{false, false, false, false}},
		{"deleted and private", domain.Recommendation{Visibility: domain.VisibilityPrivate, Deleted: true},
			want{false, false, false, false}},
		{"of a deleted place", domain.Recommendation{Visibility: domain.VisibilityCircle, PlaceDeleted: true},
			want{false, false, false, false}},
		{"private, of a deleted place", domain.Recommendation{Visibility: domain.VisibilityPrivate, PlaceDeleted: true},
			want{false, false, false, false}},
		{"deleted and hidden", domain.Recommendation{Visibility: domain.VisibilityCircle, Hidden: true, Deleted: true},
			want{false, false, false, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.rec.AuthorID = authorID
			for name, v := range identities {
				w := map[string]bool{"anonymous": tc.want.anonymous, "member": tc.want.member,
					"author": tc.want.author, "maintainer": tc.want.maintainer}[name]
				if got := domain.CanSeeRecommendation(v, tc.rec); got != w {
					t.Errorf("%s: %v, want %v", name, got, w)
				}
			}
		})
	}
	// A maintainer's own private and hidden recommendations are theirs: they see them as their author.
	own := domain.Recommendation{AuthorID: maintainerID, Visibility: domain.VisibilityPrivate, Hidden: true}
	if !domain.CanSeeRecommendation(maintainer, own) || domain.CanSeeRecommendation(member, own) {
		t.Error("a maintainer's own private recommendation")
	}
	// An empty author ID never matches the anonymous viewer's empty member ID.
	if domain.CanSeeRecommendation(anonymous, domain.Recommendation{Visibility: domain.VisibilityPrivate}) {
		t.Error("anonymous sees a recommendation without an author")
	}
}

// TestCanSeePlace: a place exists for a viewer only with a recommendation they may see (M2-Q3).
func TestCanSeePlace(t *testing.T) {
	private := domain.Recommendation{AuthorID: authorID, Visibility: domain.VisibilityPrivate}
	hidden := domain.Recommendation{AuthorID: authorID, Visibility: domain.VisibilityCircle, Hidden: true}
	deleted := domain.Recommendation{AuthorID: authorID, Visibility: domain.VisibilityCircle, Deleted: true}
	shared := domain.Recommendation{AuthorID: memberID, Visibility: domain.VisibilityCircle}
	for _, tc := range []struct {
		name    string
		deleted bool
		recs    []domain.Recommendation
		want    []string // the identities that see the place
	}{
		{"no recommendations", false, nil, nil},
		{"only the author's private one", false, []domain.Recommendation{private}, []string{"author"}},
		{"only a hidden one", false, []domain.Recommendation{hidden}, []string{"author", "maintainer"}},
		{"only a deleted one", false, []domain.Recommendation{deleted}, nil},
		{"private and shared", false, []domain.Recommendation{private, shared},
			[]string{"author", "maintainer", "member"}},
		{"a deleted place", true, []domain.Recommendation{shared}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for name, v := range identities {
				if got, want := domain.CanSeePlace(v, tc.deleted, tc.recs), slices.Contains(tc.want, name); got != want {
					t.Errorf("%s: %v, want %v", name, got, want)
				}
			}
		})
	}
}

// TestCountAttitudes: only visible, shown recommendations count, each author once.
func TestCountAttitudes(t *testing.T) {
	rec := func(author, attitude, visibility string, hidden, deleted bool) domain.CountedRecommendation {
		return domain.CountedRecommendation{Attitude: attitude, Recommendation: domain.Recommendation{
			AuthorID: author, Visibility: visibility, Hidden: hidden, Deleted: deleted}}
	}
	recs := []domain.CountedRecommendation{
		rec("other1", domain.AttitudeMustTry, domain.VisibilityCircle, false, false),
		rec("other2", domain.AttitudeMustTry, domain.VisibilityFederated, false, false),
		rec("other3", domain.AttitudeAvoid, domain.VisibilityCircle, true, false), // hidden: never counted
		rec("other4", domain.AttitudeOK, domain.VisibilityCircle, false, true),    // deleted
		rec(authorID, domain.AttitudeGood, domain.VisibilityPrivate, false, false),
		// A second recommendation of the same author (which M2-Q1 rules out) does not count again.
		rec(authorID, domain.AttitudeAvoid, domain.VisibilityCircle, false, false),
		rec("other5", "unknown", domain.VisibilityCircle, false, false),
	}
	for name, want := range map[string]domain.AttitudeCounts{
		"anonymous":  {},
		"member":     {MustTry: 2, Avoid: 1},
		"author":     {MustTry: 2, Good: 1},
		"maintainer": {MustTry: 2, Avoid: 1},
	} {
		if got := domain.CountAttitudes(identities[name], recs); got != want {
			t.Errorf("%s: %+v, want %+v", name, got, want)
		}
	}
	if (domain.AttitudeCounts{MustTry: 1, Good: 2, OK: 3, Avoid: 4}).Total() != 10 {
		t.Error("Total")
	}
}

// TestResolveSignature: the main name with links, the pseudonym never with links, a member who left (M2-Q10).
func TestResolveSignature(t *testing.T) {
	links := []domain.ExternalLink{
		{URL: "https://example.test/blog", Label: " Test Blog "},
		{URL: "http://example.test/plain"},       // not https
		{URL: "javascript:alert(1)"},             // not a link
		{URL: "https://user@example.test/login"}, // user info
		{URL: "https:///no-host"},
		{URL: "https://example.test/2"}, {URL: "https://example.test/3"}, {URL: "https://example.test/4"},
		{URL: "https://example.test/5"}, {URL: "https://example.test/6"},
	}
	a := domain.Author{DisplayName: "Test Main", Pseudonym: "Test Pen", Links: links}

	main := domain.ResolveSignature(domain.SignAsMain, a)
	if main.DisplayName != "Test Main" || main.AuthorLeft || len(main.Links) != domain.MaxExternalLinks ||
		main.Links[0] != (domain.ExternalLink{URL: "https://example.test/blog", Label: "Test Blog"}) {
		t.Errorf("main: %+v", main)
	}
	for _, l := range main.Links {
		if l.URL == "http://example.test/plain" || l.URL == "javascript:alert(1)" || l.URL == "https:///no-host" ||
			l.URL == "https://user@example.test/login" {
			t.Errorf("main shows %q", l.URL)
		}
	}
	if p := domain.ResolveSignature(domain.SignAsPseudonym, a); p.DisplayName != "Test Pen" || p.AuthorLeft ||
		len(p.Links) != 0 {
		t.Errorf("pseudonym: %+v", p)
	}

	a.Left = true
	if m := domain.ResolveSignature(domain.SignAsMain, a); m.DisplayName != "" || !m.AuthorLeft || len(m.Links) != 0 {
		t.Errorf("main of a member who left: %+v", m)
	}
	if p := domain.ResolveSignature(domain.SignAsPseudonym, a); p.DisplayName != "Test Pen" || p.AuthorLeft ||
		len(p.Links) != 0 {
		t.Errorf("pseudonym of a member who left: %+v", p)
	}

	// Never the main name in place of a missing pseudonym, nor for an unknown signature kind.
	a = domain.Author{DisplayName: "Test Main"}
	for _, kind := range []string{domain.SignAsPseudonym, "", "other"} {
		if s := domain.ResolveSignature(kind, a); s.DisplayName != "" || len(s.Links) != 0 {
			t.Errorf("%q: %+v", kind, s)
		}
	}
}

type notices []domain.Notice

func (n *notices) WriteNotice(x domain.Notice) error {
	*n = append(*n, x)
	return nil
}

// TestNotify: a notice needs a member, a known kind and a uuid; it is written as given.
func TestNotify(t *testing.T) {
	const ref = "0190a1b2-0000-7000-8000-000000000001"
	var w notices
	for _, kind := range domain.NoticeKinds {
		if err := domain.Notify(&w, authorID, kind, ref); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	if len(w) != len(domain.NoticeKinds) || w[0] != (domain.Notice{MemberID: authorID,
		Kind: domain.NoticeKinds[0], RefUUID: ref}) {
		t.Errorf("written: %+v", w)
	}
	for name, args := range map[string][3]string{
		"no member":      {"", domain.NoticeRestored, ref},
		"unknown kind":   {authorID, "test", ref},
		"not a uuid":     {authorID, domain.NoticeRestored, "not-a-uuid"},
		"no reference":   {authorID, domain.NoticeRestored, ""},
		"uppercase uuid": {authorID, domain.NoticeRestored, "0190A1B2-0000-7000-8000-000000000001"},
	} {
		w = nil
		if err := domain.Notify(&w, args[0], args[1], args[2]); !errors.Is(err, domain.ErrInvalidNotice) || len(w) != 0 {
			t.Errorf("%s: %v, %d written", name, err, len(w))
		}
	}
}
