package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/boundary"
)

// Paths of the password and TOTP routes, all public ([Public]) and all POST (so the X-OSFM header is required).
const (
	// RegisterPasswordStartPath starts a registration with password and TOTP: checks the invite, login name and
	// password, and returns the TOTP secret with a pending enrolment.
	RegisterPasswordStartPath = boundary.OSFMPrefix + "register/password/start"
	// RegisterPasswordConfirmPath finishes it with one code from the authenticator app: creates the member, returns
	// the recovery codes and starts a session.
	RegisterPasswordConfirmPath = boundary.OSFMPrefix + "register/password/confirm"
	// SignInPasswordPath checks login name and password and returns a sign-in ticket for the second step.
	SignInPasswordPath = boundary.OSFMPrefix + "signin/password"
	// SignInTOTPPath checks the ticket and a TOTP code and starts a session.
	SignInTOTPPath = boundary.OSFMPrefix + "signin/totp"
	// SignInRecoveryPath checks the ticket and a recovery code, uses the code up and starts a session.
	SignInRecoveryPath = boundary.OSFMPrefix + "signin/recovery"
)

// maxAuthBody bounds the request bodies of these routes; a pending enrolment is well under 1 KiB.
const maxAuthBody = 4 << 10

// Error codes in [AuthError] bodies, for the web app to choose its message by.
const (
	CodeInvalidRequest     = "invalid_request"
	CodeInviteInvalid      = "invite_invalid"
	CodeLoginNameInvalid   = "login_name_invalid"
	CodeLoginNameTaken     = "login_name_taken"
	CodeDisplayNameInvalid = "display_name_invalid"
	CodeDisplayNameIsLogin = "display_name_is_login_name"
	CodeNameTaken          = "name_taken" // another member's main name or pseudonym; which is not said (M2-Q6)
	CodePasswordTooShort   = "password_too_short"
	CodePasswordTooLong    = "password_too_long"
	CodePasswordIsLogin    = "password_is_login_name"
	CodeEnrolmentExpired   = "enrolment_expired"
	CodeCodeInvalid        = "code_invalid"
	CodeCredentialsInvalid = "credentials_invalid"
	CodeTicketExpired      = "ticket_expired"
	CodeCannotSignIn       = "cannot_sign_in"
)

// AuthError is the body of an error response of these routes: PocketBase's error shape (status, message, data) with
// a machine-readable code.
type AuthError struct {
	Status  int            `json:"status"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
	Code    string         `json:"code"`
}

// RegisterPasswordStartRequest is the body of POST [RegisterPasswordStartPath]. displayName, the main name, is required:
// it must differ from the login name and from every member's main name and pseudonym (M2-Q6).
type RegisterPasswordStartRequest struct {
	Invite      string `json:"invite"`
	LoginName   string `json:"loginName"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

// RegisterPasswordStartResponse is the response of POST [RegisterPasswordStartPath]: the TOTP secret in base32 (to
// type into the app), its otpauth:// URI (for the QR code), and the pending enrolment to send back with the
// confirming code before expiresAt.
type RegisterPasswordStartResponse struct {
	Enrolment  string    `json:"enrolment"`
	Secret     string    `json:"secret"`
	OTPAuthURI string    `json:"otpauthUri"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// RegisterPasswordConfirmRequest is the body of POST [RegisterPasswordConfirmPath].
type RegisterPasswordConfirmRequest struct {
	Invite    string `json:"invite"`
	Enrolment string `json:"enrolment"`
	Code      string `json:"code"`
}

// RegisteredResponse is the response of a successful registration (201): the new member, signed in, and their
// recovery codes, shown this once.
type RegisteredResponse struct {
	Me            Me       `json:"me"`
	RecoveryCodes []string `json:"recoveryCodes"`
}

// SignInPasswordRequest is the body of POST [SignInPasswordPath].
type SignInPasswordRequest struct {
	LoginName string `json:"loginName"`
	Password  string `json:"password"`
}

// SignInTicketResponse is the response of POST [SignInPasswordPath]: the ticket for the second step, valid until
// expiresAt. It is not a session.
type SignInTicketResponse struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// SignInCodeRequest is the body of POST [SignInTOTPPath] and POST [SignInRecoveryPath].
type SignInCodeRequest struct {
	Ticket string `json:"ticket"`
	Code   string `json:"code"`
}

// SignedInResponse is the response of a successful sign-in: the member. After a recovery code, recoveryCodesLeft
// says how many are left.
type SignedInResponse struct {
	Me                Me     `json:"me"`
	RecoveryCodesLeft *int64 `json:"recoveryCodesLeft,omitempty"`
}

func bindPasswordTOTPRoutes(se *core.ServeEvent) {
	Public(se.Router.POST(RegisterPasswordStartPath, handleRegisterPasswordStart)).
		Bind(RateLimit(LimitRegistration("")))
	Public(se.Router.POST(RegisterPasswordConfirmPath, handleRegisterPasswordConfirm)).
		Bind(RateLimit(LimitRegistration(MethodPasswordTOTP)))
	Public(se.Router.POST(SignInPasswordPath, handleSignInPassword)).Bind(RateLimit(LimitSignInPassword))
	Public(se.Router.POST(SignInTOTPPath, handleSignInTOTP)).Bind(RateLimit(LimitSignInTOTP))
	Public(se.Router.POST(SignInRecoveryPath, handleSignInRecovery)).Bind(RateLimit(LimitSignInRecovery))
}

// authError answers an [AuthError] and records its code for [RateLimit].
func authError(e *core.RequestEvent, status int, code, message string) error {
	e.Set(authErrorCodeKey, code)
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(status, AuthError{Status: status, Message: message, Data: map[string]any{}, Code: code})
}

func badRequest(e *core.RequestEvent) error {
	return authError(e, http.StatusBadRequest, CodeInvalidRequest, "The request is not valid.")
}

// registrationError answers one of the registration errors; anything else is a 500.
func registrationError(e *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, ErrInvalidInvite):
		return authError(e, http.StatusForbidden, CodeInviteInvalid, "The invite is invalid or has expired.")
	case errors.Is(err, errLoginNameInvalid):
		return authError(e, http.StatusBadRequest, CodeLoginNameInvalid,
			"A login name has 3 to 32 characters: lowercase letters, digits, \"_\" and \"-\".")
	case errors.Is(err, errLoginNameTaken):
		return authError(e, http.StatusConflict, CodeLoginNameTaken, "This login name is taken.")
	case errors.Is(err, errDisplayNameInvalid):
		return authError(e, http.StatusBadRequest, CodeDisplayNameInvalid,
			"A display name has 1 to 64 characters and no control characters.")
	case errors.Is(err, errDisplayNameIsLogin):
		return authError(e, http.StatusBadRequest, CodeDisplayNameIsLogin,
			"The display name must not be the login name.")
	case errors.Is(err, errNameTaken):
		return authError(e, http.StatusConflict, CodeNameTaken, "This name is taken.")
	case errors.Is(err, errPasswordTooShort):
		return authError(e, http.StatusBadRequest, CodePasswordTooShort, "The password is too short.")
	case errors.Is(err, errPasswordTooLong):
		return authError(e, http.StatusBadRequest, CodePasswordTooLong, "The password is too long.")
	case errors.Is(err, errPasswordIsLogin):
		return authError(e, http.StatusBadRequest, CodePasswordIsLogin, "The password must not be the login name.")
	case errors.Is(err, errEnrolmentInvalid):
		return authError(e, http.StatusBadRequest, CodeEnrolmentExpired,
			"The registration has expired. Start again.")
	case errors.Is(err, errCodeInvalid):
		return authError(e, http.StatusBadRequest, CodeCodeInvalid, "The code is not valid.")
	default:
		return e.InternalServerError("", err)
	}
}

// signInError answers one of the sign-in errors; anything else is a 500.
func signInError(e *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errBadCredentials):
		return authError(e, http.StatusUnauthorized, CodeCredentialsInvalid, "Wrong login name or password.")
	case errors.Is(err, errTicketInvalid):
		return authError(e, http.StatusUnauthorized, CodeTicketExpired, "The sign-in has expired. Start again.")
	case errors.Is(err, errCodeInvalid):
		return authError(e, http.StatusUnauthorized, CodeCodeInvalid, "The code is not valid.")
	case errors.Is(err, ErrCannotSignIn):
		return authError(e, http.StatusForbidden, CodeCannotSignIn, "This member cannot sign in.")
	default:
		return e.InternalServerError("", err)
	}
}

// readAuthJSON decodes the request body into v; an empty or malformed body is false.
func readAuthJSON(e *core.RequestEvent, v any) bool {
	return readJSONLimit(e, v, maxAuthBody) == nil
}

func handleRegisterPasswordStart(e *core.RequestEvent) error {
	var req RegisterPasswordStartRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	start, err := startPasswordRegistration(e.Request.Context(), e.App, req.Invite, req.LoginName, req.DisplayName,
		req.Password)
	if err != nil {
		return registrationError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, RegisterPasswordStartResponse{
		Enrolment: start.Enrolment, Secret: start.Secret, OTPAuthURI: start.OTPAuthURI, ExpiresAt: start.Expires,
	})
}

func handleRegisterPasswordConfirm(e *core.RequestEvent) error {
	var req RegisterPasswordConfirmRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	member, codes, err := confirmPasswordRegistration(e.App, req.Invite, req.Enrolment, req.Code)
	if err != nil {
		return registrationError(e, err)
	}
	// The member exists now with both factors and recovery codes: two-step verification is complete (D3). The web
	// app shows the codes and asks for confirmation that they were saved before it opens the map.
	if err := SignIn(e, member); err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusCreated, RegisteredResponse{Me: meOf(member), RecoveryCodes: codes})
}

func handleSignInPassword(e *core.RequestEvent) error {
	var req SignInPasswordRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	ticket, err := signInWithPassword(e.Request.Context(), e.App, req.LoginName, req.Password)
	if err != nil {
		return signInError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, SignInTicketResponse{
		Ticket: ticket, ExpiresAt: now().Add(ticketLifetime).UTC().Truncate(time.Second),
	})
}

func handleSignInTOTP(e *core.RequestEvent) error {
	var req SignInCodeRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	member, err := signInWithTOTP(e.App, req.Ticket, req.Code)
	if err != nil {
		return signInError(e, err)
	}
	return signedIn(e, member, nil)
}

func handleSignInRecovery(e *core.RequestEvent) error {
	var req SignInCodeRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	member, left, err := signInWithRecoveryCode(e.App, req.Ticket, req.Code)
	if err != nil {
		return signInError(e, err)
	}
	return signedIn(e, member, &left)
}

func signedIn(e *core.RequestEvent, member *core.Record, recoveryCodesLeft *int64) error {
	if err := SignIn(e, member); err != nil {
		return signInError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, SignedInResponse{Me: meOf(member), RecoveryCodesLeft: recoveryCodesLeft})
}
