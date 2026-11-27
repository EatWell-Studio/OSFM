package store

import (
	"strings"
	"unicode"

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
