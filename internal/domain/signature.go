package domain

import (
	"net/url"
	"strings"
)

// How a recommendation is signed (recommendations.display_as, C3). It is chosen when the recommendation is written
// and never changes (M2-Q2), and it never leaves the server: clients get only the resulting name.
const (
	SignAsMain      = "main"      // the author's main name (members.display_name)
	SignAsPseudonym = "pseudonym" // the author's pseudonym (members.pseudonym)
)

// MaxExternalLinks is the number of self-declared external links a member can have (D8).
const MaxExternalLinks = 5

// ExternalLink is a link a member declared on their profile (D8): an https URL and an optional label. It is shown only
// next to their main name, never next to a pseudonym (E3).
type ExternalLink struct {
	URL   string `json:"url"`
	Label string `json:"label,omitempty"`
}

// Author is what the signature needs to know about a recommendation's author.
type Author struct {
	DisplayName string // the main name
	Pseudonym   string // "" without one
	// Left is true once the author left or was removed from the circle (members.left_at), or was deleted.
	Left bool
	// Links are the author's external links, as stored.
	Links []ExternalLink
}

// Signature is how a recommendation is signed for every reader: the server decides it, clients only show it.
type Signature struct {
	// DisplayName is the main name or the pseudonym. It is empty when AuthorLeft is set.
	DisplayName string
	// AuthorLeft is true for a main-name recommendation whose author has left the circle: clients show it as signed by
	// "a member who left" in their language.
	AuthorLeft bool
	// Links are the external links to show with the name: only for a main-name signature of a member still in the
	// circle; never for a pseudonym (E3).
	Links []ExternalLink
}

// ResolveSignature returns the signature of a recommendation signed as displayAs (SignAsMain or SignAsPseudonym) by
// author. It is the one place that decides which name a reader sees (E2, E3, M2-Q10):
//
//   - main name: the author's main name and their external links (https only, at most [MaxExternalLinks]); once the
//     author has left, no name and no links, only AuthorLeft;
//   - pseudonym: the pseudonym and never any link, whether or not the author has left (M2-Q10: a pseudonym keeps
//     showing, so that leaving does not make a main name and a pseudonym disappear together);
//   - anything else, or a pseudonym signature without a pseudonym: an empty name, never the main name.
func ResolveSignature(displayAs string, author Author) Signature {
	switch displayAs {
	case SignAsMain:
		if author.Left {
			return Signature{AuthorLeft: true}
		}
		return Signature{DisplayName: author.DisplayName, Links: shownLinks(author.Links)}
	case SignAsPseudonym:
		return Signature{DisplayName: author.Pseudonym}
	default:
		return Signature{}
	}
}

// shownLinks returns the links that may be shown: absolute https URLs with a host, at most MaxExternalLinks, labels
// trimmed. The profile route checks them when they are saved; this keeps anything else stored from reaching a page.
func shownLinks(links []ExternalLink) []ExternalLink {
	out := []ExternalLink{}
	for _, l := range links {
		u, err := url.Parse(strings.TrimSpace(l.URL))
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			continue
		}
		out = append(out, ExternalLink{URL: u.String(), Label: strings.TrimSpace(l.Label)})
		if len(out) == MaxExternalLinks {
			break
		}
	}
	return out
}
