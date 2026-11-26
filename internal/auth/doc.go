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
package auth
