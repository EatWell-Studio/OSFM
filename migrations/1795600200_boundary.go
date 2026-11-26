package migrations

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The PocketBase boundary (AD2): members cannot sign in through any of PocketBase's built-in methods, and the
// application rate limiter is on.
func init() {
	m.Register(func(app core.App) error {
		if err := closeMemberSignIn(app); err != nil {
			return fmt.Errorf("members: %w", err)
		}
		return enableRateLimits(app)
	}, func(app core.App) error {
		if err := reopenMemberSignIn(app); err != nil {
			return fmt.Errorf("members: %w", err)
		}
		return disableRateLimits(app)
	})
}

// closeMemberSignIn turns off every built-in sign-in method of the members collection. Members sign in only through
// the OSFM API, which issues their tokens itself after both factors have passed (D4). The routes of the built-in
// methods are also not served at all (internal/boundary); this keeps them off should a route be reached anyway, for
// example on the admin listener.
//
//   - Password, OTP, OAuth2 and MFA are disabled, and no OAuth2 provider is configured.
//   - The auth rule is nil: PocketBase then refuses to answer any of its own sign-in or refresh requests with a token
//     for a member, whatever method is enabled; only a superuser's impersonation request (admin listener) still gets
//     one. Tokens the OSFM code issues with Record.NewAuthToken do not depend on it.
//   - New-device alerts are off: OSFM sends no e-mail, and the alert keeps a fingerprint derived from the client's IP
//     address.
func closeMemberSignIn(app core.App) error {
	c, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		return err
	}
	c.PasswordAuth.Enabled = false
	c.OTP.Enabled = false
	c.OAuth2.Enabled = false
	c.OAuth2.Providers = nil
	c.MFA.Enabled = false
	c.AuthAlert.Enabled = false
	c.AuthRule = nil
	return app.Save(c)
}

// reopenMemberSignIn restores PocketBase's defaults for an auth collection, as the members collection had them before.
func reopenMemberSignIn(app core.App) error {
	c, err := app.FindCollectionByNameOrId(store.Members)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	c.PasswordAuth.Enabled = true
	c.AuthAlert.Enabled = true
	c.AuthRule = types.Pointer("")
	return app.Save(c)
}

// enableRateLimits turns on PocketBase's rate limiter with its default rules. The limiter is a global middleware, so
// it covers every route, the OSFM ones included: the "/api/" rule (300 requests per 10 seconds per client address)
// applies to /api/osfm/*. Operators can tune the rules in the admin UI; this migration only sets the starting point.
// Per-account limits for sign-in are not something it can do; those are part of the OSFM authentication code.
func enableRateLimits(app core.App) error {
	s := app.Settings()
	s.RateLimits.Enabled = true
	if len(s.RateLimits.Rules) == 0 {
		s.RateLimits.Rules = DefaultRateLimitRules()
	}
	return app.Save(s)
}

func disableRateLimits(app core.App) error {
	s := app.Settings()
	s.RateLimits.Enabled = false
	return app.Save(s)
}

// DefaultRateLimitRules are PocketBase's default rate-limit rules (v0.40), which this migration enables. Each rule
// counts requests per client address in a fixed window; the first rule whose label matches a request applies.
func DefaultRateLimitRules() []core.RateLimitRule {
	return []core.RateLimitRule{
		{Label: "*:auth", MaxRequests: 2, Duration: 3},     // PocketBase's own sign-in routes (admin listener only)
		{Label: "*:create", MaxRequests: 20, Duration: 5},  // PocketBase's record create route (admin listener only)
		{Label: "/api/batch", MaxRequests: 3, Duration: 1}, // PocketBase's batch route (admin listener only)
		{Label: "/api/", MaxRequests: 300, Duration: 10},   // every other API route, /api/osfm/* included
	}
}
