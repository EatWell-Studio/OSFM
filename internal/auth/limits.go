package auth

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"

	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Rate limits and lockouts of the public authentication routes (N5; architecture §3.3, "Rate limits and lockouts").
//
// Every limited route has a request budget per client and counts its failed attempts per client. The sign-in steps
// also count per account:
//
//   - password step: per client and login name, and per login name across all clients (a higher threshold, reached
//     only from several clients). Login names are counted as entered (normalised) whether or not a member has them,
//     and a refusal happens before any lookup, so a lockout says nothing about whether a login name exists;
//   - TOTP step and recovery-code step: per member, the member named by the sign-in ticket. The ticket is stateless
//     and could otherwise be retried with new codes for its whole lifetime; this counter is what stops that. The two
//     factors are counted apart: recovery codes cannot be guessed, so a TOTP lockout leaves them usable.
//
// Passkey sign-in has no login name and is counted per client only: password or code failures never block it.
//
// The client is the address from PocketBase's RealIP (its trustedProxy setting decides whether a proxy header is
// believed), an IPv6 address shortened to its /64 network. Addresses and login names live only in memory, in tables of
// bounded size, and are never logged or written to the audit log (§3.9).

// Methods of the sessions.signin and members.register audit entries.
const (
	MethodPasswordTOTP = "password_totp"
	MethodRecoveryCode = "recovery_code"
	MethodPasskey      = "passkey"
)

// Audit log actions of registration and sign-in. None of them records a password, code, secret, ticket, invite code,
// login name as entered, or address.
const (
	// AuditRegister: a member registered (actor: the new member; details: method, the invite's uuid).
	AuditRegister = "members.register"
	// AuditSignIn: a member signed in (actor: the member; details: method; recoveryCodesLeft after a recovery code).
	AuditSignIn = "sessions.signin"
	// AuditSignOut: a member signed out (actor: the member).
	AuditSignOut = "sessions.signout"
	// AuditSignInFailed: a wrong TOTP or recovery code after a correct password (target: the member; details: factor).
	AuditSignInFailed = "sessions.signin_failed"
	// AuditSignInLocked: sign-in attempts for a member were locked (target: the member; details: factor, failures,
	// seconds).
	AuditSignInLocked = "sessions.signin_locked"
)

// CodeRateLimited is the [AuthError] code of a 429 response; the Retry-After header says when to try again.
const CodeRateLimited = "rate_limited"

// Middleware ID and priority of [RateLimit]: after the session, the default deny and the CSRF check, so that a request
// those refuse costs nothing here, and before the handler.
const (
	RateLimitMiddlewareID       = "osfmRateLimit"
	RateLimitMiddlewarePriority = CSRFMiddlewarePriority + 1
)

// Limits configures the rate limits ([Options].Limits). A zero Rate or Policy disables that limit.
type Limits struct {
	// Requests is every client's budget of requests to the limited routes, all of them together.
	Requests ratelimit.Rate
	// ClientFailures counts every client's failed attempts on any limited route.
	ClientFailures ratelimit.Policy
	// LoginFailures counts failed password steps per client and login name.
	LoginFailures ratelimit.Policy
	// AccountFailures counts failed password steps per login name, from all clients together.
	AccountFailures ratelimit.Policy
	// TOTPFailures and RecoveryFailures count failed second steps per member.
	TOTPFailures     ratelimit.Policy
	RecoveryFailures ratelimit.Policy
	// Capacity is the number of keys each counter holds at most; zero or less means [DefaultLimitCapacity].
	Capacity int
}

// DefaultLimitCapacity is the default [Limits].Capacity.
const DefaultLimitCapacity = 10_000

// DefaultLimits returns the limits [Bind] uses when [Options].Limits is nil.
func DefaultLimits() Limits {
	day := 24 * time.Hour
	return Limits{
		Requests:         ratelimit.Rate{Burst: 30, Every: 10 * time.Second},
		ClientFailures:   ratelimit.Policy{Threshold: 20, Lockout: time.Minute, MaxLockout: time.Hour, Forget: day},
		LoginFailures:    ratelimit.Policy{Threshold: 5, Lockout: time.Minute, MaxLockout: time.Hour, Forget: day},
		AccountFailures:  ratelimit.Policy{Threshold: 30, Lockout: time.Minute, MaxLockout: 15 * time.Minute, Forget: day},
		TOTPFailures:     ratelimit.Policy{Threshold: 5, Lockout: time.Minute, MaxLockout: time.Hour, Forget: day},
		RecoveryFailures: ratelimit.Policy{Threshold: 10, Lockout: time.Minute, MaxLockout: time.Hour, Forget: day},
		Capacity:         DefaultLimitCapacity,
	}
}

// limiterKey is the app store key of an app's [limiter], set by [Bind].
const limiterKey = "osfmLimiter"

// limiter holds an app's counters.
type limiter struct {
	requests *ratelimit.Budgets
	client   *ratelimit.Lockouts
	login    *ratelimit.Lockouts
	account  *ratelimit.Lockouts
	totp     *ratelimit.Lockouts
	recovery *ratelimit.Lockouts
	// The recovery of a passkey-only member: per client and login name, and per login name.
	recoverLogin   *ratelimit.Lockouts
	recoverAccount *ratelimit.Lockouts

	proxyWarning sync.Once
}

func newLimiter(l Limits) *limiter {
	clock := func() time.Time { return now() }
	c := l.Capacity
	if c <= 0 {
		c = DefaultLimitCapacity
	}
	return &limiter{
		requests: ratelimit.NewBudgets(l.Requests, c, clock),
		client:   ratelimit.NewLockouts(l.ClientFailures, c, clock),
		login:    ratelimit.NewLockouts(l.LoginFailures, c, clock),
		account:  ratelimit.NewLockouts(l.AccountFailures, c, clock),
		totp:     ratelimit.NewLockouts(l.TOTPFailures, c, clock),
		recovery: ratelimit.NewLockouts(l.RecoveryFailures, c, clock),

		recoverLogin:   ratelimit.NewLockouts(l.LoginFailures, c, clock),
		recoverAccount: ratelimit.NewLockouts(l.AccountFailures, c, clock),
	}
}

func limiterOf(app core.App) *limiter {
	l, _ := app.Store().Get(limiterKey).(*limiter)
	return l
}

// accountKind says which account counter a limited route uses.
type accountKind int

const (
	accountNone    accountKind = iota
	accountLogin               // the login name in the body ({"loginName"})
	accountTicket              // the member of the sign-in ticket in the body ({"ticket"})
	accountSession             // the signed-in member; the factor from the form of the code in the body ({"code"})
	accountRecover             // the login name in the body of the recovery of a passkey-only member ({"loginName"})
)

// Limit is what [RateLimit] applies to a route: the account it counts besides the client, and the audit entry a
// successful request writes.
type Limit struct {
	account  accountKind
	factor   string // for accountTicket: factorTOTP or factorRecovery
	signIn   string // audit AuditSignIn with this method on success
	register string // audit AuditRegister with this method on success
}

const (
	factorTOTP     = "totp"
	factorRecovery = "recovery"
)

// The limits of the routes; see the comment at the top of this file.
var (
	// LimitSignInPassword: the password step of the sign-in.
	LimitSignInPassword = Limit{account: accountLogin}
	// LimitSignInTOTP and LimitSignInRecovery: the second step of the sign-in.
	LimitSignInTOTP     = Limit{account: accountTicket, factor: factorTOTP, signIn: MethodPasswordTOTP}
	LimitSignInRecovery = Limit{account: accountTicket, factor: factorRecovery, signIn: MethodRecoveryCode}
	// LimitStepUp: the re-verification of a session with the password and a TOTP or recovery code (stepup.go),
	// counted for the signed-in member in the same counters as the second sign-in step.
	LimitStepUp = Limit{account: accountSession}
	// LimitRecover: the recovery of a passkey-only member with the login name and a recovery code (recover.go),
	// counted per client and login name and per login name, apart from the password step.
	LimitRecover = Limit{account: accountRecover}
	// LimitPasskeySignIn: the second request of a passkey sign-in.
	LimitPasskeySignIn = Limit{signIn: MethodPasskey}
	// LimitPublic: any other public authentication route (budget and failures per client), such as the invite check
	// or the first request of a ceremony.
	LimitPublic = Limit{}
)

// LimitRegistration is [LimitPublic] for a registration route; when method is not empty, a successful request (one
// that ends with a signed-in member) writes the AuditRegister entry with it.
func LimitRegistration(method string) Limit {
	return Limit{register: method}
}

// failureCodes are the [AuthError] codes that count as a failed attempt, on every limited route: a wrong password or
// code, an invalid invite or setup code, sealed data (ticket, enrolment, ceremony state) that does not open, or a
// malformed request.
var failureCodes = map[string]bool{
	CodeInvalidRequest:     true,
	CodeInviteInvalid:      true,
	CodeEnrolmentExpired:   true,
	CodeCodeInvalid:        true,
	CodeCredentialsInvalid: true,
	CodeTicketExpired:      true,
	CodePasskeyFailed:      true,
	CodeSetupCodeInvalid:   true,
	CodeRecoveryFailed:     true,
	CodeRecoveryExpired:    true,
	CodeResetInvalid:       true,
}

// Request store keys: the code of the AuthError a handler answered with, and the mark of a failed attempt that
// answered without one.
const (
	authErrorCodeKey = "osfmAuthErrorCode"
	attemptFailedKey = "osfmAttemptFailed"
)

// attemptFailed marks the request as a failed attempt for [RateLimit], for a handler whose failure is not an
// [AuthError] (the invite check answers 200 {"valid": false}).
func attemptFailed(e *core.RequestEvent) {
	e.Set(attemptFailedKey, true)
}

// RateLimit returns the route middleware that applies limit: bind it to every public authentication route,
// route.Bind(auth.RateLimit(auth.LimitPublic)). Over a limit it answers 429 with the [CodeRateLimited] error and a
// Retry-After header, before the handler runs.
func RateLimit(limit Limit) *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       RateLimitMiddlewareID,
		Priority: RateLimitMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			lim := limiterOf(e.App)
			if lim == nil {
				return e.Next()
			}
			return lim.serve(e, limit)
		},
	}
}

// counted is a counter and key that an attempt was admitted to.
type counted struct {
	counter *ratelimit.Lockouts
	key     string
	scope   string // for the lockout's log or audit entry
	reset   bool   // whether a success forgets its failures
}

// Lockout scopes.
const (
	scopeClient   = "client"
	scopeLogin    = "client_login"
	scopeAccount  = "login"
	scopeTOTP     = "member_totp"
	scopeRecovery = "member_recovery"

	scopeRecoverLogin   = "client_login_recover"
	scopeRecoverAccount = "login_recover"
)

func (lim *limiter) serve(e *core.RequestEvent, limit Limit) error {
	client := lim.clientKey(e)
	keys := []counted{{counter: lim.client, key: client, scope: scopeClient}}
	var login, member string
	factor := limit.factor
	switch limit.account {
	case accountLogin:
		if login = loginFromBody(e); login != "" {
			keys = append(keys,
				counted{counter: lim.login, key: client + " " + login, scope: scopeLogin, reset: true},
				counted{counter: lim.account, key: login, scope: scopeAccount, reset: true})
		}
	case accountRecover:
		if login = loginFromBody(e); login != "" {
			keys = append(keys,
				counted{counter: lim.recoverLogin, key: client + " " + login, scope: scopeRecoverLogin, reset: true},
				counted{counter: lim.recoverAccount, key: login, scope: scopeRecoverAccount, reset: true})
		}
	case accountTicket:
		member = ticketMember(e)
	case accountSession:
		if m := CurrentMember(e); m != nil {
			member = m.GetString(store.FieldUUID)
			factor = factorOfCode(e)
		}
	}
	if member != "" {
		c := counted{counter: lim.totp, key: member, scope: scopeTOTP, reset: true}
		if factor == factorRecovery {
			c = counted{counter: lim.recovery, key: member, scope: scopeRecovery, reset: true}
		}
		keys = append(keys, c)
	}

	if ok, wait := lim.requests.Take(client); !ok {
		return tooManyAttempts(e, wait)
	}
	var admitted []counted
	var wait time.Duration
	for _, k := range keys {
		if ok, w := k.counter.Acquire(k.key); ok {
			admitted = append(admitted, k)
		} else {
			wait = max(wait, w)
		}
	}
	if wait > 0 {
		for _, k := range admitted {
			k.counter.Release(k.key, ratelimit.Neutral)
		}
		return tooManyAttempts(e, wait)
	}

	outcome := ratelimit.Neutral // also when the handler panics
	defer func() {
		for _, k := range admitted {
			o := outcome
			if o == ratelimit.Success && !k.reset {
				o = ratelimit.Neutral
			}
			if lock := k.counter.Release(k.key, o); lock.Duration > 0 {
				lim.locked(e.App, k, lock, login, member)
			}
		}
	}()
	err := e.Next()
	outcome = attemptOutcome(e, err)

	switch {
	case outcome == ratelimit.Success:
		auditSuccess(e, limit)
	case outcome == ratelimit.Failure && member != "" && e.Get(authErrorCodeKey) == CodeCodeInvalid:
		details := map[string]any{"factor": factor}
		if limit.account == accountSession {
			details["verify"] = true
		}
		auditApp(e.App, nil, AuditSignInFailed, member, details)
	}
	return err
}

// attemptOutcome classifies a request the handler has answered.
func attemptOutcome(e *core.RequestEvent, err error) ratelimit.Outcome {
	if failed, _ := e.Get(attemptFailedKey).(bool); failed {
		return ratelimit.Failure
	}
	if code, _ := e.Get(authErrorCodeKey).(string); failureCodes[code] {
		return ratelimit.Failure
	}
	if status := e.Status(); err == nil && status >= 200 && status < 300 {
		return ratelimit.Success
	}
	return ratelimit.Neutral
}

// tooManyAttempts answers 429 with a Retry-After of wait, rounded up to whole seconds.
func tooManyAttempts(e *core.RequestEvent, wait time.Duration) error {
	seconds := max(1, int(math.Ceil(wait.Seconds())))
	e.Response.Header().Set("Retry-After", strconv.Itoa(seconds))
	return authError(e, http.StatusTooManyRequests, CodeRateLimited, "Too many attempts. Try again later.")
}

// clientKey returns the counter key of the request's client: the RealIP address, an IPv6 address as its /64 network
// (one host commonly has a whole /64 to pick addresses from).
func (lim *limiter) clientKey(e *core.RequestEvent) string {
	if len(e.App.Settings().TrustedProxy.Headers) == 0 && forwarded(e.Request) {
		lim.proxyWarning.Do(func() {
			e.App.Logger().Warn("requests arrive through a proxy, but no trusted proxy header is configured: every " +
				"client counts as the proxy's address for the sign-in rate limits; set OSFM_TRUSTED_PROXY_HEADERS")
		})
	}
	ip := e.RealIP()
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is6() {
		if p, err := addr.Prefix(64); err == nil {
			return p.String()
		}
	}
	return addr.String()
}

// forwarded reports whether the request carries a header that proxies add.
func forwarded(r *http.Request) bool {
	for _, h := range []string{"X-Forwarded-For", "Forwarded", "X-Real-Ip"} {
		if r.Header.Get(h) != "" {
			return true
		}
	}
	return false
}

// peekBody returns the request body, at most maxAuthBody bytes, and leaves it in place for the handler; nil when it
// is longer (the handler refuses it).
func peekBody(e *core.RequestEvent) []byte {
	body := e.Request.Body
	b, err := io.ReadAll(io.LimitReader(body, maxAuthBody+1))
	rest := io.Reader(bytes.NewReader(b))
	if err != nil || len(b) > maxAuthBody {
		// Not read to the end: the handler reads the rest, and refuses the body as too long.
		rest = io.MultiReader(rest, body)
		b = nil
	}
	// The whole body otherwise: the router's body rewinds at its end, so it must not be read again.
	e.Request.Body = struct {
		io.Reader
		io.Closer
	}{rest, body}
	return b
}

// loginFromBody returns the normalised login name of a password step, or "" when the body has none of a valid form:
// such a name belongs to nobody, and the client's counter covers it.
func loginFromBody(e *core.RequestEvent) string {
	var req SignInPasswordRequest
	if json.Unmarshal(peekBody(e), &req) != nil {
		return ""
	}
	if login := NormalizeLoginName(req.LoginName); validLoginName(login) {
		return login
	}
	return ""
}

// factorOfCode returns the factor a re-verification uses: factorTOTP for a code of TOTP form in the body, else
// factorRecovery.
func factorOfCode(e *core.RequestEvent) string {
	var req VerifyRequest
	if json.Unmarshal(peekBody(e), &req) == nil {
		if _, ok := normalizeTOTPCode(req.Code); ok {
			return factorTOTP
		}
	}
	return factorRecovery
}

// ticketMember returns the uuid of the member named by a valid sign-in ticket in the body, or "".
func ticketMember(e *core.RequestEvent) string {
	var req SignInCodeRequest
	if json.Unmarshal(peekBody(e), &req) != nil || req.Ticket == "" {
		return ""
	}
	var t signInTicket
	if openJSON(e.App, purposeTicket, req.Ticket, ticketAAD, &t) != nil || !store.IsUUID(t.Member) {
		return ""
	}
	return t.Member
}

// locked records a lockout: in the audit log when it concerns a member (the second step, whose caller knows the
// password; or the login name of an existing member, locked from all clients at once), else as a warning in the
// application log, without the address or login name.
func (lim *limiter) locked(app core.App, k counted, lock ratelimit.Lock, login, member string) {
	details := map[string]any{"failures": lock.Failures, "seconds": int(lock.Duration.Seconds())}
	switch k.scope {
	case scopeTOTP, scopeRecovery:
		details["factor"] = factorTOTP
		if k.scope == scopeRecovery {
			details["factor"] = factorRecovery
		}
		auditApp(app, nil, AuditSignInLocked, member, details)
		return
	case scopeAccount:
		if !lock.First {
			return
		}
		// Reached only by failures from several clients; the lookup happens after the response is decided.
		if m, err := findSignInMember(app, login); err == nil && m != nil {
			details["factor"] = "password"
			auditApp(app, nil, AuditSignInLocked, m.GetString(store.FieldUUID), details)
		}
	}
	if lock.First {
		app.Logger().Warn("sign-in attempts locked", "scope", k.scope, "seconds", details["seconds"])
	}
}

// auditSuccess writes the audit entry of a successful limited request: a sign-in or a registration, for the member
// the request ended with.
func auditSuccess(e *core.RequestEvent, limit Limit) {
	m := CurrentMember(e)
	if m == nil {
		return
	}
	uuid := m.GetString(store.FieldUUID)
	switch {
	case limit.signIn != "":
		details := map[string]any{"method": limit.signIn}
		if limit.signIn == MethodRecoveryCode {
			if left, err := RemainingRecoveryCodes(e.App, m); err == nil {
				details["recoveryCodesLeft"] = left
			}
		}
		auditApp(e.App, m, AuditSignIn, uuid, details)
	case limit.register != "":
		details := map[string]any{"method": limit.register}
		if inv, err := e.App.FindFirstRecordByData(store.Invites, "used_by", m.Id); err == nil {
			details["invite"] = inv.GetString(store.FieldUUID)
		}
		auditApp(e.App, m, AuditRegister, uuid, details)
	}
}

// auditApp writes an audit entry about a member; a failure is logged, not returned: the request has been answered.
func auditApp(app core.App, actor *core.Record, action, memberUUID string, details map[string]any) {
	if err := writeAudit(app, actor, action, store.Members, memberUUID, details); err != nil {
		app.Logger().Warn("could not write an audit entry", "action", action, "error", err)
	}
}
