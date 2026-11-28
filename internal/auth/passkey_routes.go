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

// Error codes of the passkey routes besides those of [AuthError] they share with the password routes.
const (
	CodePasskeyFailed       = "passkey_failed"
	CodePasskeysUnavailable = "passkeys_unavailable"
)

// PasskeyRegisterOptionsRequest is the body of POST [PasskeyRegisterOptionsPath]. displayName is required, with the
// rules of [RegisterPasswordStartRequest].
type PasskeyRegisterOptionsRequest struct {
	Invite      string `json:"invite"`
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
	Invite     string          `json:"invite"`
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
	Public(se.Router.POST(PasskeyRegisterOptionsPath, handlePasskeyRegisterOptions)).
		Bind(RateLimit(LimitRegistration("")))
	Public(se.Router.POST(PasskeyRegisterPath, handlePasskeyRegister)).Bind(RateLimit(LimitRegistration(MethodPasskey)))
	Public(se.Router.POST(PasskeySignInOptionsPath, handlePasskeySignInOptions)).Bind(RateLimit(LimitPublic))
	Public(se.Router.POST(PasskeySignInPath, handlePasskeySignIn)).Bind(RateLimit(LimitPasskeySignIn))
}

// readPasskeyJSON decodes a JSON body of at most maxPasskeyBody bytes into v.
func readPasskeyJSON(e *core.RequestEvent, v any) error {
	body, err := io.ReadAll(http.MaxBytesReader(e.Response, e.Request.Body, maxPasskeyBody))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// passkeyError answers the errors of the passkey routes: those about the passkey itself here, the others (invite,
// login name, display name) as the password registration does ([registrationError]).
func passkeyError(e *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, ErrPasskeyFailed):
		return authError(e, http.StatusBadRequest, CodePasskeyFailed, "The passkey could not be verified.")
	case errors.Is(err, ErrPasskeysUnavailable):
		e.App.Logger().Warn("passkeys are not available", "error", err)
		return authError(e, http.StatusServiceUnavailable, CodePasskeysUnavailable, "Passkeys are not available here.")
	default:
		return registrationError(e, err)
	}
}

func handlePasskeyRegisterOptions(e *core.RequestEvent) error {
	var req PasskeyRegisterOptionsRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return badRequest(e)
	}
	options, state, err := BeginPasskeyRegistration(e.App, req.Invite, req.LoginName, req.DisplayName)
	if err != nil {
		return passkeyError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, PasskeyOptionsResponse{Options: options, State: state})
}

func handlePasskeyRegister(e *core.RequestEvent) error {
	var req PasskeyRegisterRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return badRequest(e)
	}
	member, codes, err := FinishPasskeyRegistration(e.App, req.Invite, req.State, req.Credential)
	if err != nil {
		return passkeyError(e, err)
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
		return passkeyError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, PasskeyOptionsResponse{Options: options, State: state})
}

// handlePasskeySignIn answers 204 with the session cookie, or 401 with the same body for every failure: the response
// does not tell an unknown passkey from a bad signature, a used state or a member who has left.
func handlePasskeySignIn(e *core.RequestEvent) error {
	var req PasskeySignInRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return badRequest(e)
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
		return passkeyError(e, err)
	default:
		return e.InternalServerError("", err)
	}
}
