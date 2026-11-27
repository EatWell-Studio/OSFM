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

// Paths of the "me" page's credential routes (credentials.go, stepup.go). All need a session; all but GET
// [MeCredentialsPath] are POST (so they need the X-OSFM header). The ones marked "fresh" carry [RequireFresh].
const (
	// MeCredentialsPath lists the member's credentials (GET).
	MeCredentialsPath = boundary.OSFMPrefix + "me/credentials"
	// MeVerifyPath re-verifies the session with the password and a TOTP or recovery code.
	MeVerifyPath = boundary.OSFMPrefix + "me/verify"
	// MeVerifyPasskeyOptionsPath and MeVerifyPasskeyPath re-verify it with one of the member's passkeys.
	MeVerifyPasskeyOptionsPath = boundary.OSFMPrefix + "me/verify/passkey/options"
	MeVerifyPasskeyPath        = boundary.OSFMPrefix + "me/verify/passkey"
	// MePasskeyOptionsPath and MePasskeysPath add a passkey (fresh).
	MePasskeyOptionsPath = boundary.OSFMPrefix + "me/passkeys/options"
	MePasskeysPath       = boundary.OSFMPrefix + "me/passkeys"
	// MePasskeyRenamePath renames a passkey.
	MePasskeyRenamePath = boundary.OSFMPrefix + "me/passkeys/{uuid}/rename"
	// MePasskeyRemovePath removes a passkey (fresh).
	MePasskeyRemovePath = boundary.OSFMPrefix + "me/passkeys/{uuid}/remove"
	// MePasswordStartPath and MePasswordConfirmPath set up password and TOTP for a member who has none (fresh).
	MePasswordStartPath   = boundary.OSFMPrefix + "me/password-totp/start"
	MePasswordConfirmPath = boundary.OSFMPrefix + "me/password-totp/confirm"
	// MePasswordRemovePath removes the password and TOTP (fresh).
	MePasswordRemovePath = boundary.OSFMPrefix + "me/password-totp/remove"
	// MePasswordChangePath changes the password (fresh).
	MePasswordChangePath = boundary.OSFMPrefix + "me/password"
	// MeTOTPStartPath and MeTOTPConfirmPath replace the TOTP secret (fresh).
	MeTOTPStartPath   = boundary.OSFMPrefix + "me/totp/start"
	MeTOTPConfirmPath = boundary.OSFMPrefix + "me/totp/confirm"
	// MeRecoveryCodesPath generates new recovery codes (fresh).
	MeRecoveryCodesPath = boundary.OSFMPrefix + "me/recovery-codes"
)

// Error codes of the credential routes.
const (
	CodeLastCredential     = "last_credential"      // #nosec G101 -- an error code, not a credential
	CodeCredentialNotFound = "credential_not_found" // #nosec G101 -- an error code, not a credential
	CodeNoPassword         = "no_password"
	CodeNoPasskey          = "no_passkey"
	CodePasswordExists     = "password_exists"
	CodeDeviceNameInvalid  = "device_name_invalid"
)

// VerifyRequest is the body of POST [MeVerifyPath]: the password and a TOTP code (6 digits) or a recovery code.
type VerifyRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// VerifiedResponse is the response of a re-verification: the session is fresh until verifiedUntil.
type VerifiedResponse struct {
	VerifiedUntil time.Time `json:"verifiedUntil"`
}

// CeremonyRequest is the body of the second request of a passkey ceremony on the "me" page; deviceName names a
// passkey being added.
type CeremonyRequest struct {
	State      string          `json:"state"`
	Credential json.RawMessage `json:"credential"`
	DeviceName string          `json:"deviceName"`
}

// RenamePasskeyRequest is the body of POST [MePasskeyRenamePath].
type RenamePasskeyRequest struct {
	DeviceName string `json:"deviceName"`
}

// PasswordRequest is the body of POST [MePasswordStartPath] and [MePasswordChangePath].
type PasswordRequest struct {
	Password string `json:"password"`
}

// EnrolmentConfirmRequest is the body of POST [MePasswordConfirmPath] and [MeTOTPConfirmPath]: the pending enrolment
// and one code from the authenticator app.
type EnrolmentConfirmRequest struct {
	Enrolment string `json:"enrolment"`
	Code      string `json:"code"`
}

// RecoveryCodesResponse is the response of POST [MeRecoveryCodesPath] (201): the new codes, shown this once.
type RecoveryCodesResponse struct {
	RecoveryCodes []string `json:"recoveryCodes"`
}

func bindCredentialRoutes(se *core.ServeEvent) {
	se.Router.GET(MeCredentialsPath, handleListCredentials)
	se.Router.POST(MeVerifyPath, handleVerify).Bind(RateLimit(LimitStepUp))
	se.Router.POST(MeVerifyPasskeyOptionsPath, handleVerifyPasskeyOptions).Bind(RateLimit(LimitPublic))
	se.Router.POST(MeVerifyPasskeyPath, handleVerifyPasskey).Bind(RateLimit(LimitPublic))
	se.Router.POST(MePasskeyRenamePath, handleRenamePasskey)
	for path, h := range map[string]func(*core.RequestEvent) error{
		MePasskeyOptionsPath:  handleAddPasskeyOptions,
		MePasskeysPath:        handleAddPasskey,
		MePasskeyRemovePath:   handleRemovePasskey,
		MePasswordConfirmPath: handleAddPasswordConfirm,
		MePasswordRemovePath:  handleRemovePassword,
		MeTOTPStartPath:       handleReplaceTOTPStart,
		MeTOTPConfirmPath:     handleReplaceTOTPConfirm,
		MeRecoveryCodesPath:   handleRegenerateRecoveryCodes,
	} {
		se.Router.POST(path, h).Bind(RequireFresh())
	}
	// These hash a password (argon2id): the request budget applies.
	se.Router.POST(MePasswordStartPath, handleAddPasswordStart).Bind(RequireFresh(), RateLimit(LimitPublic))
	se.Router.POST(MePasswordChangePath, handleChangePassword).Bind(RequireFresh(), RateLimit(LimitPublic))
}

// credentialError answers the errors of the credential routes; the others as the registration and passkey routes do.
func credentialError(e *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errLastCredential):
		return authError(e, http.StatusConflict, CodeLastCredential,
			"This is the last way to sign in; add another one first.")
	case errors.Is(err, errCredentialNotFound):
		return authError(e, http.StatusNotFound, CodeCredentialNotFound, "No such credential.")
	case errors.Is(err, errNoPassword):
		return authError(e, http.StatusConflict, CodeNoPassword, "There is no password to use.")
	case errors.Is(err, errNoPasskey):
		return authError(e, http.StatusConflict, CodeNoPasskey, "There is no passkey to use.")
	case errors.Is(err, errPasswordExists):
		return authError(e, http.StatusConflict, CodePasswordExists, "A password is set already.")
	case errors.Is(err, errDeviceNameInvalid):
		return authError(e, http.StatusBadRequest, CodeDeviceNameInvalid,
			"A device name has at most 64 characters and no control characters.")
	case errors.Is(err, errBadCredentials):
		return authError(e, http.StatusUnauthorized, CodeCredentialsInvalid, "Wrong password.")
	case errors.Is(err, errCodeInvalid):
		return authError(e, http.StatusUnauthorized, CodeCodeInvalid, "The code is not valid.")
	default:
		return passkeyError(e, err)
	}
}

// credentialsResponse answers status with the member's credentials after a change.
func credentialsResponse(e *core.RequestEvent, status int) error {
	list, err := listCredentials(e.App, CurrentMember(e), currentSession(e))
	if err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(status, list)
}

func handleListCredentials(e *core.RequestEvent) error {
	return credentialsResponse(e, http.StatusOK)
}

func verified(e *core.RequestEvent, method string) error {
	session := currentSession(e)
	if err := markVerified(e.App, session); err != nil {
		return e.InternalServerError("", err)
	}
	m := CurrentMember(e)
	auditApp(e.App, m, AuditVerify, m.GetString(store.FieldUUID), map[string]any{"method": method})
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, VerifiedResponse{VerifiedUntil: verifiedUntil(session)})
}

func handleVerify(e *core.RequestEvent) error {
	var req VerifyRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	method, err := verifyWithPassword(e.Request.Context(), e.App, CurrentMember(e), req.Password, req.Code)
	if err != nil {
		return credentialError(e, err)
	}
	return verified(e, method)
}

func handleVerifyPasskeyOptions(e *core.RequestEvent) error {
	options, state, err := beginVerifyPasskey(e.App, CurrentMember(e))
	if err != nil {
		return credentialError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, PasskeyOptionsResponse{Options: options, State: state})
}

func handleVerifyPasskey(e *core.RequestEvent) error {
	var req CeremonyRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return badRequest(e)
	}
	if err := finishVerifyPasskey(e.App, CurrentMember(e), req.State, req.Credential); err != nil {
		return credentialError(e, err)
	}
	return verified(e, MethodPasskey)
}

func handleRenamePasskey(e *core.RequestEvent) error {
	var req RenamePasskeyRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	if err := renamePasskey(e.App, CurrentMember(e), e.Request.PathValue("uuid"), req.DeviceName); err != nil {
		return credentialError(e, err)
	}
	return credentialsResponse(e, http.StatusOK)
}

func handleRemovePasskey(e *core.RequestEvent) error {
	if err := removePasskey(e.App, CurrentMember(e), currentSession(e), e.Request.PathValue("uuid")); err != nil {
		return credentialError(e, err)
	}
	return credentialsResponse(e, http.StatusOK)
}

func handleAddPasskeyOptions(e *core.RequestEvent) error {
	options, state, err := beginAddPasskey(e.App, CurrentMember(e))
	if err != nil {
		return credentialError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, PasskeyOptionsResponse{Options: options, State: state})
}

func handleAddPasskey(e *core.RequestEvent) error {
	var req CeremonyRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return badRequest(e)
	}
	if err := finishAddPasskey(e.App, CurrentMember(e), req.State, req.Credential, req.DeviceName); err != nil {
		return credentialError(e, err)
	}
	return credentialsResponse(e, http.StatusCreated)
}

func enrolmentStarted(e *core.RequestEvent, start *PasswordEnrolmentStart) error {
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, RegisterPasswordStartResponse{
		Enrolment: start.Enrolment, Secret: start.Secret, OTPAuthURI: start.OTPAuthURI, ExpiresAt: start.Expires,
	})
}

func handleAddPasswordStart(e *core.RequestEvent) error {
	var req PasswordRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	start, err := startAddPassword(e.Request.Context(), e.App, CurrentMember(e), req.Password)
	if err != nil {
		return credentialError(e, err)
	}
	return enrolmentStarted(e, start)
}

// enrolmentError answers a failed confirmation: a wrong code is 400 here, as in the registration (the password is not
// in question).
func enrolmentError(e *core.RequestEvent, err error) error {
	if errors.Is(err, errCodeInvalid) || errors.Is(err, errEnrolmentInvalid) {
		return registrationError(e, err)
	}
	return credentialError(e, err)
}

func handleAddPasswordConfirm(e *core.RequestEvent) error {
	var req EnrolmentConfirmRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	if err := confirmAddPassword(e.App, CurrentMember(e), req.Enrolment, req.Code); err != nil {
		return enrolmentError(e, err)
	}
	return credentialsResponse(e, http.StatusCreated)
}

func handleRemovePassword(e *core.RequestEvent) error {
	if err := removePassword(e.App, CurrentMember(e), currentSession(e)); err != nil {
		return credentialError(e, err)
	}
	return credentialsResponse(e, http.StatusOK)
}

func handleChangePassword(e *core.RequestEvent) error {
	var req PasswordRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	err := changePassword(e.Request.Context(), e.App, CurrentMember(e), currentSession(e), req.Password)
	if err != nil {
		if errors.Is(err, errNoPassword) {
			return credentialError(e, err)
		}
		return registrationError(e, err) // the password rules
	}
	return credentialsResponse(e, http.StatusOK)
}

func handleReplaceTOTPStart(e *core.RequestEvent) error {
	start, err := startReplaceTOTP(e.Request.Context(), e.App, CurrentMember(e))
	if err != nil {
		return credentialError(e, err)
	}
	return enrolmentStarted(e, start)
}

func handleReplaceTOTPConfirm(e *core.RequestEvent) error {
	var req EnrolmentConfirmRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	if err := confirmReplaceTOTP(e.App, CurrentMember(e), currentSession(e), req.Enrolment, req.Code); err != nil {
		return enrolmentError(e, err)
	}
	return credentialsResponse(e, http.StatusOK)
}

func handleRegenerateRecoveryCodes(e *core.RequestEvent) error {
	codes, err := regenerateRecoveryCodes(e.App, CurrentMember(e))
	if err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusCreated, RecoveryCodesResponse{RecoveryCodes: codes})
}
