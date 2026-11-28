package store

import (
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/domain"
)

// AuthorOf returns what domain.ResolveSignature needs to know about the member record m, the author of a
// recommendation: the main name, the pseudonym, the external links as stored, and whether the member has left the
// circle (left_at set) or was deleted. The API's projections and the exports both use it, so that a recommendation is
// signed the same way everywhere.
func AuthorOf(m *core.Record) domain.Author {
	var links []domain.ExternalLink
	_ = m.UnmarshalJSONField("external_links", &links) // anything else stored shows no links
	return domain.Author{
		DisplayName: m.GetString("display_name"),
		Pseudonym:   m.GetString("pseudonym"),
		Left:        !m.GetDateTime("left_at").IsZero() || IsDeleted(m),
		Links:       links,
	}
}
