package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/settings"
)

// Paths of the setup wizard (setup.go). All are POST except [SetupPath], and all public ([Public]) except
// [SetupCompletePath], which only maintainers may use. Every one of them answers 404 once its step no longer applies.
const (
	// SetupPath reports the wizard's step for the caller (GET): 200 {"step"} or 404.
	SetupPath = boundary.OSFMPrefix + "setup"
	// SetupCheckPath tells whether a setup code is the current one: {"code"} in, {"valid"} out.
	SetupCheckPath = boundary.OSFMPrefix + "setup/check"
	// SetupPasswordStartPath and SetupPasswordConfirmPath create the first maintainer with password and TOTP, as
	// [RegisterPasswordStartPath] and [RegisterPasswordConfirmPath] register an invited member.
	SetupPasswordStartPath   = boundary.OSFMPrefix + "setup/password/start"
	SetupPasswordConfirmPath = boundary.OSFMPrefix + "setup/password/confirm"
	// SetupPasskeyOptionsPath and SetupPasskeyPath create the first maintainer with a passkey, as
	// [PasskeyRegisterOptionsPath] and [PasskeyRegisterPath] register an invited member.
	SetupPasskeyOptionsPath = boundary.OSFMPrefix + "setup/passkey/options"
	SetupPasskeyPath        = boundary.OSFMPrefix + "setup/passkey"
	// SetupCompletePath saves the instance's name and cities and completes the setup (maintainers only).
	SetupCompletePath = boundary.OSFMPrefix + "setup/complete"
)

// Error codes of the setup routes, besides those they share with the registration.
const (
	CodeSetupDone           = "setup_done"
	CodeSetupCodeInvalid    = "setup_code_invalid"
	CodeInstanceNameInvalid = "instance_name_invalid"
	CodeCitiesInvalid       = "cities_invalid"
)

// maxSetupCompleteBody bounds the body of POST [SetupCompletePath]: [settings.MaxCities] cities of a few hundred bytes.
const maxSetupCompleteBody = 32 << 10

// SetupStatusResponse is the response of GET [SetupPath]: [SetupStepMaintainer] or [SetupStepInstance].
type SetupStatusResponse struct {
	Step string `json:"step"`
}

// SetupCheckRequest is the body of POST [SetupCheckPath].
type SetupCheckRequest struct {
	Code string `json:"code"`
}

// SetupCheckResponse is the response of POST [SetupCheckPath].
type SetupCheckResponse struct {
	Valid bool `json:"valid"`
}

// SetupPasswordStartRequest is the body of POST [SetupPasswordStartPath]; the response is a
// [RegisterPasswordStartResponse].
type SetupPasswordStartRequest struct {
	SetupCode   string `json:"setupCode"`
	LoginName   string `json:"loginName"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

// SetupPasswordConfirmRequest is the body of POST [SetupPasswordConfirmPath]; the response is a [RegisteredResponse]
// (201) with the session cookie.
type SetupPasswordConfirmRequest struct {
	SetupCode string `json:"setupCode"`
	Enrolment string `json:"enrolment"`
	Code      string `json:"code"`
}

// SetupPasskeyOptionsRequest is the body of POST [SetupPasskeyOptionsPath]; the response is a
// [PasskeyOptionsResponse].
type SetupPasskeyOptionsRequest struct {
	SetupCode   string `json:"setupCode"`
	LoginName   string `json:"loginName"`
	DisplayName string `json:"displayName"`
}

// SetupPasskeyRequest is the body of POST [SetupPasskeyPath]; the response is a [PasskeyRegisterResponse] (201) with
// the session cookie.
type SetupPasskeyRequest struct {
	SetupCode  string          `json:"setupCode"`
	State      string          `json:"state"`
	Credential json.RawMessage `json:"credential"`
}

// The body of POST [SetupCompletePath] and its response are a [settings.Instance].

func bindSetupRoutes(se *core.ServeEvent) {
	Public(se.Router.GET(SetupPath, handleSetupStatus))
	for path, handler := range map[string]func(*core.RequestEvent) error{
		SetupCheckPath:           handleSetupCheck,
		SetupPasswordStartPath:   handleSetupPasswordStart,
		SetupPasswordConfirmPath: handleSetupPasswordConfirm,
		SetupPasskeyOptionsPath:  handleSetupPasskeyOptions,
		SetupPasskeyPath:         handleSetupPasskey,
	} {
		Public(se.Router.POST(path, handler)).Bind(RateLimit(LimitPublic)).BindFunc(whileUnconfigured)
	}
	se.Router.POST(SetupCompletePath, handleSetupComplete).Bind(RequireMaintainer())
}

// whileUnconfigured answers 404 before anything else, the request body included, once the instance has a maintainer:
// the routes that create the first maintainer are gone then. The handlers check again where it matters, inside the
// transaction that creates the maintainer.
func whileUnconfigured(e *core.RequestEvent) error {
	needs, err := NeedsSetup(e.App)
	if err != nil {
		return e.InternalServerError("", err)
	}
	if !needs {
		return setupError(e, ErrSetupDone)
	}
	return e.Next()
}

// setupError answers the errors of the setup routes; the others as the registration does.
func setupError(e *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, ErrSetupDone):
		return authError(e, http.StatusNotFound, CodeSetupDone, "The instance has been set up.")
	case errors.Is(err, ErrInvalidSetupCode):
		return authError(e, http.StatusForbidden, CodeSetupCodeInvalid, "The setup code is not valid.")
	case errors.Is(err, settings.ErrInvalidName):
		return authError(e, http.StatusBadRequest, CodeInstanceNameInvalid,
			"An instance name has 1 to 100 characters and no control characters.")
	case errors.Is(err, settings.ErrInvalidCities):
		return authError(e, http.StatusBadRequest, CodeCitiesInvalid, err.Error())
	default:
		return passkeyError(e, err)
	}
}

func handleSetupStatus(e *core.RequestEvent) error {
	step, err := setupStep(e.App, CurrentMember(e))
	if err != nil {
		return e.InternalServerError("", err)
	}
	if step == "" {
		return setupError(e, ErrSetupDone)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, SetupStatusResponse{Step: step})
}

// handleSetupCheck answers {"valid"} like the invite check: the same response for every code that is not the current
// one.
func handleSetupCheck(e *core.RequestEvent) error {
	var req SetupCheckRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	err := checkSetupCode(e.App, req.Code)
	if err != nil && !errors.Is(err, ErrInvalidSetupCode) {
		return setupError(e, err)
	}
	if err != nil {
		attemptFailed(e)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, SetupCheckResponse{Valid: err == nil})
}

func handleSetupPasswordStart(e *core.RequestEvent) error {
	var req SetupPasswordStartRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	start, err := startSetupPassword(e.Request.Context(), e.App, req.SetupCode, req.LoginName, req.DisplayName,
		req.Password)
	if err != nil {
		return setupError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, RegisterPasswordStartResponse{
		Enrolment: start.Enrolment, Secret: start.Secret, OTPAuthURI: start.OTPAuthURI, ExpiresAt: start.Expires,
	})
}

func handleSetupPasswordConfirm(e *core.RequestEvent) error {
	var req SetupPasswordConfirmRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	member, codes, err := confirmSetupPassword(e.App, req.SetupCode, req.Enrolment, req.Code)
	if err != nil {
		return setupError(e, err)
	}
	if err := SignIn(e, member); err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusCreated, RegisteredResponse{Me: meOf(member), RecoveryCodes: codes})
}

func handleSetupPasskeyOptions(e *core.RequestEvent) error {
	var req SetupPasskeyOptionsRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return badRequest(e)
	}
	options, state, err := beginSetupPasskey(e.App, req.SetupCode, req.LoginName, req.DisplayName)
	if err != nil {
		return setupError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, PasskeyOptionsResponse{Options: options, State: state})
}

func handleSetupPasskey(e *core.RequestEvent) error {
	var req SetupPasskeyRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return badRequest(e)
	}
	member, codes, err := finishSetupPasskey(e.App, req.SetupCode, req.State, req.Credential)
	if err != nil {
		return setupError(e, err)
	}
	if err := SignIn(e, member); err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusCreated, PasskeyRegisterResponse{RecoveryCodes: codes})
}

func handleSetupComplete(e *core.RequestEvent) error {
	var req settings.Instance
	if err := readJSONLimit(e, &req, maxSetupCompleteBody); err != nil {
		return badRequest(e)
	}
	inst, err := completeSetup(e.App, CurrentMember(e), req)
	if err != nil {
		return setupError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, inst)
}
