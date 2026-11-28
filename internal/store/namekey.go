package store

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// NameKey returns the form in which members' main names and pseudonyms are compared: two names are the same name when
// their keys are equal. The key is what members.display_name_key and members.pseudonym_key hold (the hooks
// [BindHooks] registers keep them in step with display_name and pseudonym); the uniqueness of names across both
// fields is checked against them.
//
// The name is normalised with NFKC, case-folded and normalised again (full-width and half-width forms, compatibility
// characters and letter case do not make a different name); invisible characters (format characters such as zero-width
// spaces and joiners, control characters, variation selectors and Hangul fillers) are removed; runs of white space
// become one space, and white space at both ends is removed. An empty or blank name has the empty key.
//
// Changing this function changes the keys of names already stored: a change needs a migration that recomputes them.
func NameKey(name string) string {
	s := norm.NFKC.String(name)
	s = cases.Fold().String(s) // a Caser is not safe for concurrent use: a new one each time
	s = norm.NFKC.String(s)

	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = b.Len() > 0
		case invisible(r):
		default:
			if space {
				b.WriteByte(' ')
				space = false
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// invisible reports whether r is a character that displays as nothing, so that a name with it looks like the name
// without it.
func invisible(r rune) bool {
	switch r {
	case '\u034f', // combining grapheme joiner
		'\u115f', '\u1160', '\u3164', '\uffa0': // Hangul fillers
		return true
	}
	return unicode.Is(unicode.Cf, r) || unicode.IsControl(r) || unicode.Is(unicode.Variation_Selector, r)
}

// MaxNameLength is the longest main name or pseudonym, in characters (members.display_name and members.pseudonym).
const MaxNameLength = 64

// NormalizeName returns a main name or pseudonym as it is stored: with white space at both ends removed. ok is false
// when the name is empty, longer than [MaxNameLength] characters, not valid UTF-8, contains control or format
// characters, or is made only of characters that [NameKey] removes (so that it would look like no name at all). The
// same rules apply at registration and on the profile (M2-Q6).
func NormalizeName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxNameLength {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", false
		}
	}
	if NameKey(name) == "" {
		return "", false
	}
	return name, true
}

// NameTaken reports whether a member other than the one with the internal ID except (none when empty) has a main name
// or a pseudonym whose [NameKey] is key (M2-Q6). Members who left and deleted members count too: their pseudonym
// recommendations keep showing the pseudonym (M2-Q10), and a reused name would pass as theirs. An empty key is never
// taken. Call it inside the transaction that saves the name, so that two members cannot take the same name at once.
func NameTaken(app core.App, key, except string) (bool, error) {
	if key == "" {
		return false, nil
	}
	q := Query(app, Members).IncludeDeleted().Where(dbx.Or(
		dbx.HashExp{Members + "." + FieldDisplayNameKey: key},
		dbx.HashExp{Members + "." + FieldPseudonymKey: key},
	))
	if except != "" {
		q.Where(dbx.Not(dbx.HashExp{Members + ".id": except}))
	}
	n, err := q.Count()
	return n > 0, err
}
