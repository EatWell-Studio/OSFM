package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/boundary"
)

// Paths of the passkey routes; all public ([Public]) and, being POST, they need the X-OSFM header.
const (
	// PasskeyRegisterOptionsPath starts a registration with a passkey: invite code, login name and display name in,
	// creation options and the sealed state out.
	PasskeyRegisterOptionsPath = boundary.OSFMPrefix + "passkey/register/options"
	// PasskeyRegisterPath completes it: the member is created, signed in, and receives the recovery codes.
	PasskeyRegisterPath = boundary.OSFMPrefix + "passkey/register"
	// PasskeySignInOptionsPath starts a sign-in with a passkey: request options and the sealed state out.
	PasskeySignInOptionsPath = boundary.OSFMPrefix + "passkey/login/options"
	// PasskeySignInPath completes it and signs the member in.
	PasskeySignInPath = boundary.OSFMPrefix + "passkey/login"
)

// maxPasskeyBody bounds the request bodies of the passkey routes. An attestation of format "none" is a few hundred
// bytes; this leaves room for the largest credential IDs and keys.
const maxPasskeyBody = 64 << 10

// Error codes of the passkey routes, in the "code" member of an error response ([AuthError]).
const (
	CodeBadRequest          = "bad_request"
	CodeInvalidInvite       = "invalid_invite"
	CodeInvalidLoginName    = "invalid_login_name"
	CodeInvalidDisplayName  = "invalid_display_name"
	CodeLoginNameTaken      = "login_name_taken"
	CodePasskeyFailed       = "passkey_failed"
	CodePasskeysUnavailable = "passkeys_unavailable"
)

// AuthError is the body of an unsuccessful response of the registration and sign-in routes: PocketBase's error shape
// (status, message) with a code the web app can tell apart.
type AuthError struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// PasskeyRegisterOptionsRequest is the body of POST [PasskeyRegisterOptionsPath]. displayName may be empty: the login
// name is used then.
type PasskeyRegisterOptionsRequest struct {
	Code        string `json:"code"`
	LoginName   string `json:"loginName"`
	DisplayName string `json:"displayName"`
}

// PasskeyOptionsResponse is the response of both options routes: options is the CredentialCreationOptions or
// CredentialRequestOptions in their JSON form ({"publicKey": {…}}, binary values base64url-encoded), state the sealed
// ceremony state to send back unchanged.
type PasskeyOptionsResponse struct {
	Options any    `json:"options"`
	State   string `json:"state"`
}

// PasskeyRegisterRequest is the body of POST [PasskeyRegisterPath]: the same invite code, the state, and the
// PublicKeyCredential from navigator.credentials.create() in its JSON form (RegistrationResponseJSON).
type PasskeyRegisterRequest struct {
	Code       string          `json:"code"`
	State      string          `json:"state"`
	Credential json.RawMessage `json:"credential"`
}

// PasskeyRegisterResponse is the response of POST [PasskeyRegisterPath] (201): the member's recovery codes, shown
// this once. The response also sets the session cookie.
type PasskeyRegisterResponse struct {
	RecoveryCodes []string `json:"recoveryCodes"`
}

// PasskeySignInRequest is the body of POST [PasskeySignInPath]: the state and the PublicKeyCredential from
// navigator.credentials.get() in its JSON form (AuthenticationResponseJSON).
type PasskeySignInRequest struct {
	State      string          `json:"state"`
	Credential json.RawMessage `json:"credential"`
}

func bindPasskeyRoutes(se *core.ServeEvent) {
	Public(se.Router.POST(PasskeyRegisterOptionsPath, handlePasskeyRegisterOptions))
	Public(se.Router.POST(PasskeyRegisterPath, handlePasskeyRegister))
	Public(se.Router.POST(PasskeySignInOptionsPath, handlePasskeySignInOptions))
	Public(se.Router.POST(PasskeySignInPath, handlePasskeySignIn))
}

func authError(e *core.RequestEvent, status int, code, message string) error {
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(status, AuthError{Status: status, Message: message, Code: code})
}

// readPasskeyJSON decodes a JSON body of at most maxPasskeyBody bytes into v.
func readPasskeyJSON(e *core.RequestEvent, v any) error {
	body, err := io.ReadAll(http.MaxBytesReader(e.Response, e.Request.Body, maxPasskeyBody))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// registrationError answers the errors of the registration routes that are not about the passkey itself.
func registrationError(e *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, ErrInvalidLoginName):
		return authError(e, http.StatusBadRequest, CodeInvalidLoginName, "The login name is not valid.")
	case errors.Is(err, ErrInvalidDisplayName):
		return authError(e, http.StatusBadRequest, CodeInvalidDisplayName, "The display name is not valid.")
	case errors.Is(err, ErrInvalidInvite):
		return authError(e, http.StatusForbidden, CodeInvalidInvite, "The invite is invalid or has expired.")
	case errors.Is(err, ErrLoginNameTaken):
		return authError(e, http.StatusConflict, CodeLoginNameTaken, "The login name is taken.")
	case errors.Is(err, ErrPasskeyFailed):
		return authError(e, http.StatusBadRequest, CodePasskeyFailed, "The passkey could not be verified.")
	case errors.Is(err, ErrPasskeysUnavailable):
		e.App.Logger().Warn("passkeys are not available", "error", err)
		return authError(e, http.StatusServiceUnavailable, CodePasskeysUnavailable, "Passkeys are not available here.")
	default:
		return e.InternalServerError("", err)
	}
}

func handlePasskeyRegisterOptions(e *core.RequestEvent) error {
	var req PasskeyRegisterOptionsRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return authError(e, http.StatusBadRequest, CodeBadRequest, "The request is not valid.")
	}
	options, state, err := BeginPasskeyRegistration(e.App, req.Code, req.LoginName, req.DisplayName)
	if err != nil {
		return registrationError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, PasskeyOptionsResponse{Options: options, State: state})
}

func handlePasskeyRegister(e *core.RequestEvent) error {
	var req PasskeyRegisterRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return authError(e, http.StatusBadRequest, CodeBadRequest, "The request is not valid.")
	}
	member, codes, err := FinishPasskeyRegistration(e.App, req.Code, req.State, req.Credential)
	if err != nil {
		return registrationError(e, err)
	}
	if err := SignIn(e, member); err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusCreated, PasskeyRegisterResponse{RecoveryCodes: codes})
}

func handlePasskeySignInOptions(e *core.RequestEvent) error {
	options, state, err := BeginPasskeySignIn(e.App)
	if err != nil {
		return registrationError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, PasskeyOptionsResponse{Options: options, State: state})
}

// handlePasskeySignIn answers 204 with the session cookie, or 401 with the same body for every failure: the response
// does not tell an unknown passkey from a bad signature, a used state or a member who has left.
func handlePasskeySignIn(e *core.RequestEvent) error {
	var req PasskeySignInRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return authError(e, http.StatusBadRequest, CodeBadRequest, "The request is not valid.")
	}
	member, err := VerifyPasskeySignIn(e.App, req.State, req.Credential)
	if err == nil {
		err = SignIn(e, member)
	}
	switch {
	case err == nil:
		e.Response.Header().Set("Cache-Control", "no-store")
		return e.NoContent(http.StatusNoContent)
	case errors.Is(err, ErrPasskeyFailed), errors.Is(err, ErrCannotSignIn):
		return authError(e, http.StatusUnauthorized, CodePasskeyFailed, "The passkey sign-in failed.")
	case errors.Is(err, ErrPasskeysUnavailable):
		return registrationError(e, err)
	default:
		return e.InternalServerError("", err)
	}
}
