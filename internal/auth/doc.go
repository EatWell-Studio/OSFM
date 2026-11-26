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
// Passkeys (D3, D4): the public routes under /api/osfm/passkey/ register a member with an invite and a passkey
// ([BeginPasskeyRegistration], [FinishPasskeyRegistration]) and sign a member in with a discoverable passkey, without a
// login name ([BeginPasskeySignIn], [VerifyPasskeySignIn]). Registration hands out the recovery codes
// ([NewRecoveryCodes]).
package auth
