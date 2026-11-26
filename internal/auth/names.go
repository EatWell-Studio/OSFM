package auth

import (
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Names a member chooses at registration (D3, D8): the login name, which identifies the account and is used to sign
// in with a password, and the display name (main name), which other members see.

// Bounds of the names, as the members collection enforces them.
const (
	MinLoginNameLen   = 3
	MaxLoginNameLen   = 32
	MaxDisplayNameLen = 64
)

var loginNamePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Errors of [NormalizeLoginName], [NormalizeDisplayName] and registration.
var (
	ErrInvalidLoginName   = errors.New("auth: a login name has 3 to 32 characters: lowercase letters, digits, _ and -")
	ErrInvalidDisplayName = errors.New("auth: a display name has at most 64 characters and no control characters")
	ErrLoginNameTaken     = errors.New("auth: the login name is taken")
)

// NormalizeLoginName returns the login name s as it is stored: without surrounding space, in lower case. It returns
// [ErrInvalidLoginName] unless the result has [MinLoginNameLen] to [MaxLoginNameLen] characters out of a–z, 0–9, "_"
// and "-".
func NormalizeLoginName(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < MinLoginNameLen || len(s) > MaxLoginNameLen || !loginNamePattern.MatchString(s) {
		return "", ErrInvalidLoginName
	}
	return s, nil
}

// NormalizeDisplayName returns the display name s without surrounding space, or loginName when that leaves it empty.
// It returns [ErrInvalidDisplayName] for a name longer than [MaxDisplayNameLen] characters or with control characters
// or bidirectional formatting characters (which could make a name look like another one).
func NormalizeDisplayName(s, loginName string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return loginName, nil
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxDisplayNameLen {
		return "", ErrInvalidDisplayName
	}
	for _, r := range s {
		if unicode.IsControl(r) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return "", ErrInvalidDisplayName
		}
	}
	return s, nil
}

// loginNameTaken reports whether a member, soft-deleted ones included, has the login name.
func loginNameTaken(app core.App, loginName string) (bool, error) {
	_, err := app.FindFirstRecordByData(store.Members, "login_name", loginName)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
