package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The member's profile (D8, C3, M2-Q6): the main name (members.display_name), at most one pseudonym and up to five
// self-declared external links. Only the member reads and changes their own profile; no route shows anyone else's.
//
//   - Names have 1 to 64 characters without control or format characters (store.NormalizeName). A main name or a
//     pseudonym must not be the member's own login name, and must differ from every other member's main name and
//     pseudonym, compared as store.NameKey (letter case and width do not make a different name), members who left
//     included; a member's pseudonym must differ from their own main name. A taken name is 409 name_taken, which
//     says which of the request's fields it is about, never whose name it is or whether it is a main name or a
//     pseudonym. Uniqueness is checked in the transaction that saves the names, and only for a name that changes, so
//     that a member whose name an older version let others share can still change the rest of their profile.
//   - The pseudonym cannot be cleared while the member has recommendations signed with it (409 pseudonym_in_use);
//     changing it renames all of them at once, since signatures are resolved when a recommendation is read.
//   - Setting, changing or clearing the pseudonym writes the audit entry members.pseudonym_change with the old and the
//     new pseudonym: only maintainers can read the audit log (E2 lets them know who a pseudonym is).
//   - External links are https URLs with a host and no user name or password, with an optional label; they are not
//     verified. Readers see them only next to main-name signatures, never next to a pseudonym (domain.ResolveSignature,
//     E3).

// ProfilePath is GET ([Profile]) and POST ([ProfileUpdate], answered with the saved [Profile]) of the caller's own
// profile.
const ProfilePath = "/api/osfm/me/profile"

// Error codes of the profile. The name errors carry data.field: "displayName" or "pseudonym".
const (
	CodeNameInvalid          = "name_invalid"           // 400: empty, too long, or with control or format characters
	CodeNameIsLoginName      = "name_is_login_name"     // 400: the member's own login name
	CodeNameTaken            = "name_taken"             // 409: in use; whose name it is and as what is not said
	CodePseudonymInUse       = "pseudonym_in_use"       // 409: clearing a pseudonym that signs recommendations
	CodeExternalLinksInvalid = "external_links_invalid" // 400: too many, not https, or a label too long
)

// The fields that data.field of a name error names.
const (
	ProfileFieldDisplayName = "displayName"
	ProfileFieldPseudonym   = "pseudonym"
)

// Limits of the profile, for the form to check before sending.
const (
	// MaxNameLength is the longest main name or pseudonym, in characters (store.MaxNameLength).
	MaxNameLength = 64
	// MaxExternalLinks is the number of external links a member can have (domain.MaxExternalLinks).
	MaxExternalLinks = 5
	// MaxExternalLinkURLLength is the longest link, in characters.
	MaxExternalLinkURLLength = 2000
	// MaxExternalLinkLabelLength is the longest label of a link, in characters.
	MaxExternalLinkLabelLength = 64
)

// AuditPseudonymChange is the audit action of setting, changing or clearing a pseudonym; details are the pseudonym
// before ("from", empty when there was none) and after ("to", empty when cleared).
const AuditPseudonymChange = "members.pseudonym_change"

// maxProfileBody bounds the body of POST [ProfilePath]: five links of the longest length, labels and names fit.
const maxProfileBody = 16 << 10

// Profile is the caller's own profile.
type Profile struct {
	// DisplayName is the main name.
	DisplayName string `json:"displayName"`
	// Pseudonym is empty when the member has none: then they cannot sign a recommendation with one (C3).
	Pseudonym string `json:"pseudonym"`
	// ExternalLinks are shown next to the member's main-name signatures only.
	ExternalLinks []ExternalLink `json:"externalLinks"`
	// PseudonymInUse is true while recommendations of the member (not deleted) are signed with the pseudonym: it can
	// be changed then, but not cleared.
	PseudonymInUse bool `json:"pseudonymInUse"`
	// DisplayNameIsLoginName is true for a main name that is the member's login name (older versions used the login
	// name when none was given): the member should change it. An update that sends the main name is refused until it
	// is another one.
	DisplayNameIsLoginName bool `json:"displayNameIsLoginName"`
}

// ProfileUpdate is the body of POST [ProfilePath]. A field that is absent (or null) keeps its saved value; an empty
// pseudonym clears it.
type ProfileUpdate struct {
	DisplayName   *string         `json:"displayName,omitempty"`
	Pseudonym     *string         `json:"pseudonym,omitempty"`
	ExternalLinks *[]ExternalLink `json:"externalLinks,omitempty"`
}

func registerProfile(se *core.ServeEvent, s *server) {
	se.Router.GET(ProfilePath, handleProfile)
	se.Router.POST(ProfilePath, handleUpdateProfile).Bind(s.WriteBudget())
}

func handleProfile(e *core.RequestEvent) error {
	member := auth.CurrentMember(e)
	if member == nil {
		return e.UnauthorizedError("", nil)
	}
	profile, err := profileOf(e.App, member)
	if err != nil {
		return e.InternalServerError("", err)
	}
	return respond(e, http.StatusOK, profile)
}

func handleUpdateProfile(e *core.RequestEvent) error {
	member := auth.CurrentMember(e)
	if member == nil {
		return e.UnauthorizedError("", nil)
	}
	var req ProfileUpdate
	if err := readJSON(e, &req, maxProfileBody); err != nil {
		return requestError(e, err)
	}
	profile, err := UpdateProfile(e.App, member, req)
	var nameErr *nameError
	switch {
	case errors.As(err, &nameErr):
		status := http.StatusBadRequest
		if nameErr.Code == CodeNameTaken {
			status = http.StatusConflict
		}
		e.Response.Header().Set("Cache-Control", "no-store")
		return e.JSON(status, Error{Status: status, Message: nameErr.Error(), Code: nameErr.Code,
			Data: map[string]any{"field": nameErr.Field}})
	case errors.Is(err, ErrPseudonymInUse):
		return fail(e, http.StatusConflict, CodePseudonymInUse,
			"The pseudonym signs recommendations: it can be changed, not removed.")
	case errors.Is(err, ErrExternalLinksInvalid):
		return fail(e, http.StatusBadRequest, CodeExternalLinksInvalid, err.Error())
	case err != nil:
		return e.InternalServerError("", err)
	}
	return respond(e, http.StatusOK, profile)
}

// nameError is a main name or pseudonym that [UpdateProfile] refuses: Code is [CodeNameInvalid],
// [CodeNameIsLoginName] or [CodeNameTaken], Field is [ProfileFieldDisplayName] or [ProfileFieldPseudonym].
type nameError struct {
	Code, Field string
}

func (e *nameError) Error() string {
	switch e.Code {
	case CodeNameIsLoginName:
		return "A name must not be the login name."
	case CodeNameTaken:
		return "This name is taken."
	default:
		return "A name has 1 to 64 characters and no control characters."
	}
}

// Errors of [UpdateProfile] besides nameError.
var (
	ErrPseudonymInUse       = errors.New("api: the pseudonym signs recommendations")
	ErrExternalLinksInvalid = errors.New("api: invalid external links")
)

// UpdateProfile applies req to the profile of member, the caller, with the rules described at the top of this file,
// and returns the profile as saved. The names are checked against the other members' names, and saved with the audit
// entry of a pseudonym change, in one transaction.
func UpdateProfile(app core.App, member *core.Record, req ProfileUpdate) (Profile, error) {
	login := store.NameKey(member.GetString("login_name"))
	var display, pseudonym string
	if req.DisplayName != nil {
		var ok bool
		if display, ok = store.NormalizeName(*req.DisplayName); !ok {
			return Profile{}, &nameError{Code: CodeNameInvalid, Field: ProfileFieldDisplayName}
		}
		if store.NameKey(display) == login {
			return Profile{}, &nameError{Code: CodeNameIsLoginName, Field: ProfileFieldDisplayName}
		}
	}
	if req.Pseudonym != nil && strings.TrimSpace(*req.Pseudonym) != "" {
		var ok bool
		if pseudonym, ok = store.NormalizeName(*req.Pseudonym); !ok {
			return Profile{}, &nameError{Code: CodeNameInvalid, Field: ProfileFieldPseudonym}
		}
		if store.NameKey(pseudonym) == login {
			return Profile{}, &nameError{Code: CodeNameIsLoginName, Field: ProfileFieldPseudonym}
		}
	}
	var links []domain.ExternalLink
	if req.ExternalLinks != nil {
		var err error
		if links, err = normalizeLinks(*req.ExternalLinks); err != nil {
			return Profile{}, err
		}
	}

	var saved *core.Record
	err := app.RunInTransaction(func(tx core.App) error {
		m, err := store.FindByUUID(tx, store.Members, member.GetString(store.FieldUUID))
		if err != nil {
			return err
		}
		oldDisplay, oldPseudonym := m.GetString("display_name"), m.GetString("pseudonym")
		if req.DisplayName == nil {
			display = oldDisplay
		}
		if req.Pseudonym == nil {
			pseudonym = oldPseudonym
		}
		displayKey, pseudonymKey := store.NameKey(display), store.NameKey(pseudonym)
		if pseudonymKey != "" && pseudonymKey == displayKey {
			field := ProfileFieldPseudonym
			if req.Pseudonym == nil {
				field = ProfileFieldDisplayName
			}
			return &nameError{Code: CodeNameTaken, Field: field}
		}
		for _, n := range []struct{ key, old, field string }{
			{displayKey, m.GetString(store.FieldDisplayNameKey), ProfileFieldDisplayName},
			{pseudonymKey, m.GetString(store.FieldPseudonymKey), ProfileFieldPseudonym},
		} {
			if n.key == n.old {
				continue
			}
			taken, err := store.NameTaken(tx, n.key, m.Id)
			if err != nil {
				return err
			}
			if taken {
				return &nameError{Code: CodeNameTaken, Field: n.field}
			}
		}
		if pseudonym == "" && oldPseudonym != "" {
			inUse, err := pseudonymInUse(tx, m)
			if err != nil {
				return err
			}
			if inUse {
				return ErrPseudonymInUse
			}
		}

		m.Set("display_name", display)
		m.Set("pseudonym", pseudonym)
		if req.ExternalLinks != nil {
			m.Set("external_links", links)
		}
		if err := tx.Save(m); err != nil {
			return err
		}
		if pseudonym != oldPseudonym {
			if err := store.WriteAudit(tx, m, AuditPseudonymChange, store.Members, m.GetString(store.FieldUUID),
				map[string]any{"from": oldPseudonym, "to": pseudonym}); err != nil {
				return err
			}
		}
		saved = m
		return nil
	})
	var nameErr *nameError
	switch {
	case errors.As(err, &nameErr), errors.Is(err, ErrPseudonymInUse):
		return Profile{}, err
	case err != nil:
		return Profile{}, fmt.Errorf("api: update the profile: %w", err)
	}
	return profileOf(app, saved)
}

// profileOf returns the profile of the member record m.
func profileOf(app core.App, m *core.Record) (Profile, error) {
	inUse, err := pseudonymInUse(app, m)
	if err != nil {
		return Profile{}, err
	}
	var stored []domain.ExternalLink
	_ = m.UnmarshalJSONField("external_links", &stored) // anything else stored shows as no links
	links := make([]ExternalLink, 0, len(stored))
	for _, l := range stored {
		links = append(links, ExternalLink{URL: l.URL, Label: l.Label})
	}
	display := m.GetString("display_name")
	return Profile{
		DisplayName:            display,
		Pseudonym:              m.GetString("pseudonym"),
		ExternalLinks:          links,
		PseudonymInUse:         m.GetString("pseudonym") != "" && inUse,
		DisplayNameIsLoginName: store.NameKey(display) == store.NameKey(m.GetString("login_name")),
	}, nil
}

// pseudonymInUse reports whether the member m has recommendations that are not deleted and are signed with their
// pseudonym, whoever may see them (private and hidden ones included).
func pseudonymInUse(app core.App, m *core.Record) (bool, error) {
	n, err := store.Query(app, store.Recommendations).Where(dbx.HashExp{
		store.Recommendations + ".author":     m.Id,
		store.Recommendations + ".display_as": domain.SignAsPseudonym,
	}).Count()
	return n > 0, err
}

// normalizeLinks checks the external links of a profile update and returns them as stored: each URL parsed and
// written back in its canonical form, labels trimmed. It returns an error wrapping [ErrExternalLinksInvalid] for more
// than [MaxExternalLinks] links, a URL that is not an absolute https URL with a host (or has a user name, white space
// or control characters, or is too long), or a label that is too long or has control or format characters.
func normalizeLinks(in []ExternalLink) ([]domain.ExternalLink, error) {
	if len(in) > MaxExternalLinks {
		return nil, fmt.Errorf("%w: at most %d links", ErrExternalLinksInvalid, MaxExternalLinks)
	}
	out := make([]domain.ExternalLink, 0, len(in))
	for i, l := range in {
		raw := strings.TrimSpace(l.URL)
		if !utf8.ValidString(raw) || utf8.RuneCountInString(raw) > MaxExternalLinkURLLength ||
			strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return nil, fmt.Errorf("%w: link %d is not an https URL", ErrExternalLinksInvalid, i+1)
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" {
			return nil, fmt.Errorf("%w: link %d is not an https URL", ErrExternalLinksInvalid, i+1)
		}
		label := strings.TrimSpace(l.Label)
		if !utf8.ValidString(label) || utf8.RuneCountInString(label) > MaxExternalLinkLabelLength ||
			strings.IndexFunc(label, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
			return nil, fmt.Errorf("%w: the label of link %d is too long or has control characters",
				ErrExternalLinksInvalid, i+1)
		}
		out = append(out, domain.ExternalLink{URL: u.String(), Label: label})
	}
	return out, nil
}
