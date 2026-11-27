package main

import (
	"net/http"
	"testing"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// TestIdentities calls the routes that exist so far as each of the four identities of the permission tests
// (docs/architecture.md §6), on the app as `osfm serve` boots it: what an anonymous caller, a member, an author and a
// maintainer may do. The author is a member like any other until the recommendation routes (M2) give authors rights
// over their own recommendations. No response contains an internal ID.
func TestIdentities(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	const (
		anon       = testapp.Anonymous
		member     = testapp.Member
		author     = testapp.Author
		maintainer = testapp.Maintainer
	)
	for _, tc := range []struct {
		name string
		send func(c *testapp.Client) *testapp.Response
		want map[testapp.Identity]int
	}{
		{"who am I", func(c *testapp.Client) *testapp.Response { return c.Get(auth.MePath) },
			map[testapp.Identity]int{anon: 401, member: 200, author: 200, maintainer: 200}},
		{"the instance's name and cities", func(c *testapp.Client) *testapp.Response { return c.Get(api.InstancePath) },
			map[testapp.Identity]int{anon: 401, member: 200, author: 200, maintainer: 200}},
		{"changing the instance settings", func(c *testapp.Client) *testapp.Response {
			return c.Post(api.InstancePath, api.InstanceUpdate{})
		}, map[testapp.Identity]int{anon: 401, member: 403, author: 403, maintainer: 200}},
		{"the city index of the tiles", func(c *testapp.Client) *testapp.Response { return c.Get("/tiles/") },
			map[testapp.Identity]int{anon: 401, member: 200, author: 200, maintainer: 200}},
		{"the invite list", func(c *testapp.Client) *testapp.Response { return c.Get(auth.InvitesPath) },
			map[testapp.Identity]int{anon: 401, member: 403, author: 403, maintainer: 200}},
		{"creating an invite", func(c *testapp.Client) *testapp.Response {
			return c.Post(auth.InvitesPath, auth.CreateInviteRequest{Days: 1})
		}, map[testapp.Identity]int{anon: 401, member: 403, author: 403, maintainer: 201}},
		{"the member list", func(c *testapp.Client) *testapp.Response { return c.Get(auth.MembersPath) },
			map[testapp.Identity]int{anon: 401, member: 403, author: 403, maintainer: 200}},
		{"creating a reset link for the member", func(c *testapp.Client) *testapp.Response {
			return c.Post(auth.ResetLinksPath, auth.CreateResetLinkRequest{Member: a.Member.GetString(store.FieldUUID)})
		}, map[testapp.Identity]int{anon: 401, member: 403, author: 403, maintainer: 201}},
		{"completing the setup", func(c *testapp.Client) *testapp.Response {
			return c.Post(auth.SetupCompletePath, settings.Instance{})
		}, map[testapp.Identity]int{anon: 401, member: 403, author: 403, maintainer: 404}},
		{"the setup status", func(c *testapp.Client) *testapp.Response { return c.Get(auth.SetupPath) },
			map[testapp.Identity]int{anon: 404, member: 404, author: 404, maintainer: 404}},
		{"signing out without the X-OSFM header", func(c *testapp.Client) *testapp.Response {
			r, _ := http.NewRequest(http.MethodPost, auth.LogoutPath, http.NoBody)
			return c.Send(r)
		}, map[testapp.Identity]int{anon: 401, member: 403, author: 403, maintainer: 403}},
		{"signing out", func(c *testapp.Client) *testapp.Response { return c.Post(auth.LogoutPath, nil) },
			map[testapp.Identity]int{anon: 401, member: 204, author: 204, maintainer: 204}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, id := range testapp.Identities {
				// A new session for every call: signing out ends only that one.
				if r := tc.send(a.As(id)); r.Code != tc.want[id] {
					t.Errorf("%s: status %d, want %d: %s", id, r.Code, tc.want[id], r.Body.String())
				}
			}
		})
	}

	// The roles as the members see them.
	for id, role := range map[testapp.Identity]string{member: auth.RoleMember, author: auth.RoleMember,
		maintainer: auth.RoleMaintainer} {
		if me := testapp.Decode[auth.Me](a.As(id).Get(auth.MePath).Expect(http.StatusOK)); me.Role != role {
			t.Errorf("%s: role %q, want %q", id, me.Role, role)
		}
	}
	if got := a.Recommendation.GetString("author"); got != a.Author.Id {
		t.Errorf("the seeded recommendation's author is %s, not the author identity", got)
	}
	a.CheckNoInternalIDs()
}

// TestMembersRegisterAndSignIn registers a member with password and TOTP and one with a passkey through the real
// routes, signs each out and in again, and checks that a session ended on the server is refused.
func TestMembersRegisterAndSignIn(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})

	pw := a.RegisterPasswordTOTP("test_pw")
	pk := a.RegisterPasskey("test_pk")
	for _, m := range []struct {
		name   string
		codes  []string
		client *testapp.Client
		signIn func() *testapp.Client
	}{
		{"password and TOTP", pw.RecoveryCodes, pw.Client, pw.SignIn},
		{"passkey", pk.RecoveryCodes, pk.Client, pk.SignIn},
	} {
		t.Run(m.name, func(t *testing.T) {
			if len(m.codes) != auth.RecoveryCodeCount {
				t.Errorf("%d recovery codes", len(m.codes))
			}
			// Signed in since the registration; signing out ends the session and clears the cookie.
			me := testapp.Decode[auth.Me](m.client.Get(auth.MePath).Expect(http.StatusOK))
			if me.Role != auth.RoleMember {
				t.Errorf("role %q", me.Role)
			}
			m.client.Post(auth.LogoutPath, nil).Expect(http.StatusNoContent)
			if m.client.Session() != "" {
				t.Error("the session cookie is still set after signing out")
			}
			m.client.Get(auth.MePath).Expect(http.StatusUnauthorized)

			c := m.signIn()
			if got := testapp.Decode[auth.Me](c.Get(auth.MePath).Expect(http.StatusOK)); got.UUID != me.UUID {
				t.Errorf("signed in as %s, registered as %s", got.UUID, me.UUID)
			}
			// Ended elsewhere: the server forgets the session, the browser still sends the cookie.
			a.Client().WithSession(c.Session()).Post(auth.LogoutPath, nil).Expect(http.StatusNoContent)
			c.Get(auth.MePath).Expect(http.StatusUnauthorized)
		})
	}
	a.CheckNoInternalIDs()
}
