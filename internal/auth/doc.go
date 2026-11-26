// Package auth implements registration, sign-in with a passkey or with password and TOTP, recovery codes, reset links
// and sessions.
//
// Sessions (AD6): [Bind] installs the session middlewares and the routes GET /api/osfm/me and POST /api/osfm/logout.
// Every path under /api/osfm/ and /tiles/ needs a session unless its route is marked [Public]; state-changing
// requests to /api/osfm/* need the X-OSFM header. Sign-in endpoints start a session with [SignIn]; [RevokeSessions]
// ends every session of a member.
package auth
