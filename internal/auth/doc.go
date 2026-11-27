// Package auth implements registration, sign-in with a passkey or with password and TOTP, recovery codes, reset links
// and sessions.
//
// Sessions (AD6): [Bind] installs the session middlewares and the routes GET /api/osfm/me and POST /api/osfm/logout.
// Every path under /api/osfm/ and /tiles/ needs a session unless its route is marked [Public]; state-changing
// requests to /api/osfm/* need the X-OSFM header. Sign-in endpoints start a session with [SignIn]; [RevokeSessions]
// ends every session of a member.
//
// Invites (D1, D2): maintainers create, list and revoke invites under /api/osfm/invites ([RequireMaintainer] guards
// maintainer routes); POST /api/osfm/invites/check is public and only says whether a code can be used. Registration
// creates a member only through [RegisterWithInvite], which consumes the invite in the same transaction.
//
// Password and TOTP (D3, D4): registration in two public steps under /api/osfm/register/password/ (start: invite,
// login name, password; confirm: one TOTP code), sign-in in two under /api/osfm/signin/ (password, then a TOTP code or
// a recovery code). See passwordtotp.go. Every new member gets one-time recovery codes ([NewRecoveryCodes]).
//
// Passkeys (D3, D4): the public routes under /api/osfm/passkey/ register a member with an invite and a passkey
// ([BeginPasskeyRegistration], [FinishPasskeyRegistration]) and sign a member in with a discoverable passkey, without a
// login name ([BeginPasskeySignIn], [VerifyPasskeySignIn]). Registration hands out the recovery codes
// ([NewRecoveryCodes]).
//
// First-start setup (G3): while no maintainer exists, the routes under /api/osfm/setup create the first one, the only
// member created without an invite, with the setup code the server prints when it starts; the new maintainer then
// saves the instance's name and cities. See setup.go.
//
// Rate limits (N5): every public authentication route carries the [RateLimit] middleware, which keeps a request
// budget and a failure count per client, and for the sign-in steps per login name or per member, with temporary
// lockouts (429 with Retry-After); it also writes the audit entries of registration and sign-in. See limits.go.
package auth
