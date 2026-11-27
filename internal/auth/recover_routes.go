package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Paths of the recovery of a passkey-only member (recover.go); all public ([Public]), POST and rate-limited.
const (
	// RecoverPath takes the login name and a recovery code and returns a recovery ticket.
	RecoverPath = boundary.OSFMPrefix + "recover"
	// RecoverPasswordStartPath and RecoverPasswordConfirmPath enrol password and TOTP with the ticket.
	RecoverPasswordStartPath   = boundary.OSFMPrefix + "recover/password/start"
	RecoverPasswordConfirmPath = boundary.OSFMPrefix + "recover/password/confirm"
	// RecoverPasskeyOptionsPath and RecoverPasskeyPath enrol a passkey with the ticket.
	RecoverPasskeyOptionsPath = boundary.OSFMPrefix + "recover/passkey/options"
	RecoverPasskeyPath        = boundary.OSFMPrefix + "recover/passkey"
)

// Error codes of the recovery.
const (
	CodeRecoveryFailed      = "recovery_failed"
	CodeRecoveryUsePassword = "recovery_use_password"
	CodeRecoveryExpired     = "recovery_expired"
)

// MethodRecovery is the sessions.signin method after a recovery enrolment.
const MethodRecovery = "recovery"

// LimitRecoverEnrol: the enrolment steps of a recovery; the last one writes the sign-in audit entry.
var LimitRecoverEnrol = Limit{signIn: MethodRecovery}

// RecoverRequest is the body of POST [RecoverPath].
type RecoverRequest struct {
	LoginName string `json:"loginName"`
	Code      string `json:"code"`
}

// RecoverTicketResponse is the response of POST [RecoverPath]: the ticket for the enrolment, valid until expiresAt.
// It is not a session.
type RecoverTicketResponse struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// RecoverPasswordStartRequest is the body of POST [RecoverPasswordStartPath].
type RecoverPasswordStartRequest struct {
	Ticket   string `json:"ticket"`
	Password string `json:"password"`
}

// RecoverPasswordConfirmRequest is the body of POST [RecoverPasswordConfirmPath].
type RecoverPasswordConfirmRequest struct {
	Ticket    string `json:"ticket"`
	Enrolment string `json:"enrolment"`
	Code      string `json:"code"`
}

// RecoverPasskeyOptionsRequest is the body of POST [RecoverPasskeyOptionsPath].
type RecoverPasskeyOptionsRequest struct {
	Ticket string `json:"ticket"`
}

// RecoverPasskeyRequest is the body of POST [RecoverPasskeyPath].
type RecoverPasskeyRequest struct {
	Ticket     string          `json:"ticket"`
	State      string          `json:"state"`
	Credential json.RawMessage `json:"credential"`
}

// EnrolledResponse is the response (201) of the last step of a recovery or a reset: the member, now signed in, the
// new recovery codes, shown this once, and how many passkeys the member has besides a new one (after a recovery, the
// old ones the member may want to remove).
type EnrolledResponse struct {
	Me            Me       `json:"me"`
	RecoveryCodes []string `json:"recoveryCodes"`
	OtherPasskeys int64    `json:"otherPasskeys"`
}

func bindRecoverRoutes(se *core.ServeEvent) {
	Public(se.Router.POST(RecoverPath, handleRecover)).Bind(RateLimit(LimitRecover))
	Public(se.Router.POST(RecoverPasswordStartPath, handleRecoverPasswordStart)).Bind(RateLimit(LimitPublic))
	Public(se.Router.POST(RecoverPasswordConfirmPath, handleRecoverPasswordConfirm)).Bind(RateLimit(LimitRecoverEnrol))
	Public(se.Router.POST(RecoverPasskeyOptionsPath, handleRecoverPasskeyOptions)).Bind(RateLimit(LimitPublic))
	Public(se.Router.POST(RecoverPasskeyPath, handleRecoverPasskey)).Bind(RateLimit(LimitRecoverEnrol))
}

// recoverError answers the errors of the recovery routes; the others as the registration and passkey routes do.
func recoverError(e *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errRecoveryFailed):
		return authError(e, http.StatusUnauthorized, CodeRecoveryFailed, "Wrong login name or recovery code.")
	case errors.Is(err, errRecoveryUsePassword):
		return authError(e, http.StatusConflict, CodeRecoveryUsePassword,
			"This member has a password: sign in with it and the recovery code.")
	case errors.Is(err, errRecoveryTicketInvalid):
		return authError(e, http.StatusUnauthorized, CodeRecoveryExpired, "The recovery has expired. Start again.")
	default:
		return passkeyError(e, err)
	}
}

func handleRecover(e *core.RequestEvent) error {
	var req RecoverRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	ticket, member, err := startRecovery(e.App, req.LoginName, req.Code)
	if err != nil {
		return recoverError(e, err)
	}
	details := map[string]any{}
	if left, err := RemainingRecoveryCodes(e.App, member); err == nil {
		details["recoveryCodesLeft"] = left
	}
	auditApp(e.App, nil, AuditRecoveryStart, member.GetString(store.FieldUUID), details)
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, RecoverTicketResponse{Ticket: ticket,
		ExpiresAt: now().Add(recoveryTicketLifetime).UTC().Truncate(time.Second)})
}

func handleRecoverPasswordStart(e *core.RequestEvent) error {
	var req RecoverPasswordStartRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	start, err := startRecoveryPassword(e.Request.Context(), e.App, req.Ticket, req.Password)
	if err != nil {
		return recoverError(e, err)
	}
	return enrolmentStarted(e, start)
}

// enrolled signs member in after a recovery or reset enrolment and answers 201 with [EnrolledResponse].
func enrolled(e *core.RequestEvent, member *core.Record, codes []string, newPasskey bool) error {
	if err := SignIn(e, member); err != nil {
		return e.InternalServerError("", err)
	}
	passkeys, _, err := signInMethods(e.App, member)
	if err != nil {
		return e.InternalServerError("", err)
	}
	if newPasskey {
		passkeys--
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusCreated, EnrolledResponse{Me: meOf(member), RecoveryCodes: codes, OtherPasskeys: passkeys})
}

func handleRecoverPasswordConfirm(e *core.RequestEvent) error {
	var req RecoverPasswordConfirmRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	member, codes, err := confirmRecoveryPassword(e.App, req.Ticket, req.Enrolment, req.Code)
	if err != nil {
		if errors.Is(err, errCodeInvalid) || errors.Is(err, errEnrolmentInvalid) {
			return registrationError(e, err)
		}
		return recoverError(e, err)
	}
	return enrolled(e, member, codes, false)
}

func handleRecoverPasskeyOptions(e *core.RequestEvent) error {
	var req RecoverPasskeyOptionsRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	options, state, err := beginRecoveryPasskey(e.App, req.Ticket)
	if err != nil {
		return recoverError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, PasskeyOptionsResponse{Options: options, State: state})
}

func handleRecoverPasskey(e *core.RequestEvent) error {
	var req RecoverPasskeyRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return badRequest(e)
	}
	member, codes, err := finishRecoveryPasskey(e.App, req.Ticket, req.State, req.Credential)
	if err != nil {
		return recoverError(e, err)
	}
	return enrolled(e, member, codes, true)
}
