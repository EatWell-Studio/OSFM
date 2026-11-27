package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// First-start setup (G3; architecture §3.3, §5.3). An instance is unconfigured while it has no maintainer: no member
// with the role maintainer has ever been created (one who has left or was deleted still counts, so that setup happens
// once). Until then the web app sends every visitor to the setup wizard: the setup code (below), the instance's name,
// the first maintainer, and the cities. The first maintainer is the only member created without an invite (D1), with
// the same mandatory two-step verification (D3), password and TOTP or a passkey, and the same recovery codes. The
// instance name is not saved on its own: the browser keeps it and sends it with the requests that create the
// maintainer. It is sealed into the pending enrolment (or ceremony state) with everything else, is the issuer of the
// maintainer's TOTP secret, and is saved in the transaction that creates the maintainer, so there is never a name
// without a maintainer. The new maintainer, signed in, then gives the instance its cities (and may correct the name),
// and setup is complete once they are saved ([completeSetup]): a maintainer exists and the instance has cities.
//
// Setup code. A freshly deployed instance is reachable before anyone has configured it; without a further check,
// whoever opened it first would become its maintainer. So creating the first maintainer needs a setup code that only
// whoever runs the server can read:
//
//   - When the server starts unconfigured, it generates a code (80 random bits, written like a recovery code,
//     "XXXX-XXXX-XXXX-XXXX"), prints it to the server log and writes it to <data>/secrets/setup_code (mode 0600), from
//     where `osfm setup-code` prints it again. Each start makes a new code; only the latest one is valid.
//   - Only the code's hash is kept in memory. A code entered is normalised as a recovery code is and its hash compared
//     in constant time.
//   - Every step of the account creation needs the code; the pending enrolment or passkey ceremony state is bound to
//     its hash, as the registration binds them to the invite.
//   - Once the first maintainer exists, the code is forgotten and the file deleted; every public setup route then
//     answers 404 whatever code it is given, because the check that no maintainer exists comes first.
//
// The transaction that creates the first maintainer checks again, inside itself, that there is still no maintainer
// ([registerFirstMaintainer]). PocketBase runs write transactions one after another on its single write connection, so
// of two setups racing with the same code exactly one creates a maintainer; the other finds it and fails.

// Steps of the setup wizard, as GET /api/osfm/setup reports them.
const (
	// SetupStepMaintainer: the instance has no maintainer; the wizard asks for the setup code and the instance name
	// and creates the first maintainer.
	SetupStepMaintainer = "maintainer"
	// SetupStepCities: the signed-in maintainer has yet to give the instance its cities.
	SetupStepCities = "cities"
)

// Audit log actions of the setup.
const (
	// AuditSetupMaintainer: the first maintainer was created by the setup wizard; details say with which credential.
	AuditSetupMaintainer = "setup.first_maintainer"
	// AuditSettingsUpdate: the instance settings were changed; details hold the new name and, when they changed, the
	// cities' slugs.
	AuditSettingsUpdate = "settings.update"
	// AuditSetupComplete: the setup wizard was completed.
	AuditSetupComplete = "setup.complete"
)

// Errors of the setup.
var (
	// ErrSetupDone is returned by every setup step once it no longer applies: the first maintainer exists (for the
	// steps that create one), or the instance has its cities (for [completeSetup]). The routes answer 404.
	ErrSetupDone = errors.New("auth: the instance has been set up")
	// ErrInvalidSetupCode is returned for a setup code that is missing, malformed or not the current one.
	ErrInvalidSetupCode = errors.New("auth: invalid setup code")
)

// SetupCodeFileName is the name of the file in the secrets directory that holds the current setup code while the
// instance is unconfigured.
const SetupCodeFileName = "setup_code"

// ceremonySetup is the passkey ceremony kind of the first maintainer's registration: its state cannot be used for an
// invited registration, nor the reverse.
const ceremonySetup = "passkey.setup"

// setupEnrolmentAAD binds a pending password enrolment to the setup, so that it cannot be used with an invite.
var setupEnrolmentAAD = []byte("password+totp first maintainer")

// setupCodeKey is the app store key of the [setupCode] of an app, set by [Bind].
const setupCodeKey = "osfmSetupCode"

// setupCode is the current setup code of an app: only its hash, and the file it was written to.
type setupCode struct {
	file   string // empty: no file
	logger *slog.Logger

	mu   sync.Mutex
	hash string // hashRecoveryCode of the canonical code; empty while there is none
}

func setupCodeOf(app core.App) *setupCode {
	if s, ok := app.Store().Get(setupCodeKey).(*setupCode); ok {
		return s
	}
	return &setupCode{} // without Bind: no code, nothing matches
}

// generate makes a new code, replacing any previous one, writes it to the file and returns it in display form.
func (s *setupCode) generate() (string, error) {
	b := make([]byte, recoveryCodeBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: setup code: %w", err)
	}
	canonical := crockford.EncodeToString(b)
	display := displayRecoveryCode(canonical)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hash = hashRecoveryCode(canonical)
	if s.file != "" {
		if err := writeSetupCodeFile(s.file, display); err != nil {
			return "", err
		}
	}
	return display, nil
}

// writeSetupCodeFile replaces the file with one, readable by its owner only, that holds code.
func writeSetupCodeFile(path, code string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("auth: setup code file: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: from the configuration
	if err != nil {
		return fmt.Errorf("auth: setup code file: %w", err)
	}
	if _, err := f.WriteString(code + "\n"); err != nil {
		_ = f.Close()
		return fmt.Errorf("auth: setup code file: %w", err)
	}
	return f.Close()
}

// matches reports whether code is the current setup code, comparing hashes in constant time.
func (s *setupCode) matches(code string) bool {
	canonical, ok := normalizeRecoveryCode(code)
	s.mu.Lock()
	want := s.hash
	s.mu.Unlock()
	if !ok || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashRecoveryCode(canonical)), []byte(want)) == 1
}

// clear forgets the code and deletes its file: setup has no further use for it.
func (s *setupCode) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hash = ""
	if s.file == "" {
		return
	}
	if err := os.Remove(s.file); err != nil && !errors.Is(err, fs.ErrNotExist) && s.logger != nil {
		s.logger.Warn("could not delete the setup code file", slog.String("path", s.file), slog.Any("error", err))
	}
}

// setupBindHash is what a pending enrolment or ceremony state of the setup is bound to: the hash of the canonical
// setup code (empty for a malformed one, which never gets that far).
func setupBindHash(code string) string {
	canonical, ok := normalizeRecoveryCode(code)
	if !ok {
		return ""
	}
	return hashRecoveryCode(canonical)
}

// NeedsSetup reports whether the instance is unconfigured: no member with the role maintainer exists, counting those
// who have left or were soft-deleted.
func NeedsSetup(app core.App) (bool, error) {
	var n int64
	err := app.DB().Select("COUNT(*)").From(store.Members).Where(dbx.HashExp{"role": RoleMaintainer}).Row(&n)
	return n == 0, err
}

// startSetup runs when the app starts serving: while the instance is unconfigured it generates the setup code and
// logs it; otherwise it removes a setup code file left behind.
func startSetup(app core.App, logger *slog.Logger) error {
	s := setupCodeOf(app)
	needs, err := NeedsSetup(app)
	if err != nil {
		return err
	}
	if !needs {
		s.clear()
		return nil
	}
	code, err := s.generate()
	if err != nil {
		return err
	}
	attrs := []any{slog.String("setup_code", code)}
	if s.file != "" {
		attrs = append(attrs, slog.String("file", s.file))
	}
	logger.Warn("This instance has no maintainer yet. Open it in a browser and enter this setup code in the setup "+
		"wizard to create the first maintainer. Only the code from the latest start is valid; `osfm setup-code` "+
		"prints it again.", attrs...)
	return nil
}

// setupStep returns the step of the setup wizard for the request's member (nil without a session): the maintainer
// step while the instance is unconfigured, the cities step for a maintainer while the instance has no cities, and
// "" otherwise (nothing to set up for this caller).
func setupStep(app core.App, member *core.Record) (string, error) {
	needs, err := NeedsSetup(app)
	if err != nil {
		return "", err
	}
	if needs {
		return SetupStepMaintainer, nil
	}
	if !IsMaintainer(member) {
		return "", nil
	}
	done, err := hasCities(app)
	if err != nil || done {
		return "", err
	}
	return SetupStepCities, nil
}

// checkSetupCode returns [ErrSetupDone] once a maintainer exists and [ErrInvalidSetupCode] unless code is the current
// setup code, in that order: after setup, no code tells anything.
func checkSetupCode(app core.App, code string) error {
	needs, err := NeedsSetup(app)
	if err != nil {
		return err
	}
	if !needs {
		return ErrSetupDone
	}
	if !setupCodeOf(app).matches(code) {
		return ErrInvalidSetupCode
	}
	return nil
}

// registerFirstMaintainer creates the first maintainer: the counterpart of [RegisterWithInvite] for the setup. In one
// transaction it checks that no maintainer exists ([ErrSetupDone]) and that code is the setup code
// ([ErrInvalidSetupCode]), prepares a new member record (role maintainer, may invite, joined_at now, no inviter),
// calls register to fill it in and save it with its credentials, checks that the record was saved unchanged in role
// and inviter, saves instanceName as the instance's name, and writes the audit_log entries [AuditSetupMaintainer]
// (method, the credential kind) and [AuditSettingsUpdate]. Any failure rolls everything back, the name included.
// After the commit the setup code is forgotten.
func registerFirstMaintainer(app core.App, code, instanceName, method string, register Registration) (*core.Record,
	error) {
	name, err := settings.NormalizeName(instanceName)
	if err != nil {
		return nil, err
	}
	var member *core.Record
	err = app.RunInTransaction(func(tx core.App) error {
		if err := checkSetupCode(tx, code); err != nil {
			return err
		}
		var err error
		if member, err = NewMemberRecord(tx); err != nil {
			return err
		}
		member.Set("role", RoleMaintainer)
		member.Set("can_invite", true)
		member.Set("joined_at", now().UTC())
		if err := register(tx, member); err != nil {
			return err
		}
		saved, err := tx.FindRecordById(store.Members, member.Id)
		if err != nil {
			return fmt.Errorf("the registration did not save the member: %w", err)
		}
		if saved.GetString("role") != RoleMaintainer || saved.GetString("invited_by") != "" || !CanSignIn(saved) {
			return errors.New("the registration changed the first maintainer's role or inviter")
		}
		if err := writeAudit(tx, nil, AuditSetupMaintainer, store.Members, saved.GetString(store.FieldUUID),
			map[string]any{"method": method}); err != nil {
			return err
		}
		if err := settings.SaveName(tx, name); err != nil {
			return err
		}
		return writeAudit(tx, saved, AuditSettingsUpdate, store.InstanceSettings, "", map[string]any{"name": name})
	})
	switch {
	case errors.Is(err, ErrSetupDone), errors.Is(err, ErrInvalidSetupCode), errors.Is(err, errLoginNameTaken):
		return nil, err
	case err != nil:
		return nil, fmt.Errorf("auth: create the first maintainer: %w", err)
	}
	setupCodeOf(app).clear()
	return member, nil
}

// startSetupPassword is step 1 of creating the first maintainer with password and TOTP: the setup code instead of an
// invite, otherwise as [startPasswordRegistration]. The instance name is checked, sealed into the enrolment and used
// as the TOTP issuer.
func startSetupPassword(ctx context.Context, app core.App, code, instanceName, loginName, displayName,
	password string) (*PasswordEnrolmentStart, error) {
	if err := checkSetupCode(app, code); err != nil {
		return nil, err
	}
	name, err := settings.NormalizeName(instanceName)
	if err != nil {
		return nil, err
	}
	return startPasswordEnrolment(ctx, app, setupEnrolmentAAD, setupBindHash(code), name, loginName, displayName,
		password)
}

// confirmSetupPassword is step 2: as [confirmPasswordRegistration], creating the first maintainer.
func confirmSetupPassword(app core.App, code, enrolment, totpCode string) (*core.Record, []string, error) {
	if err := checkSetupCode(app, code); err != nil {
		return nil, nil, err
	}
	pending, err := passwordRegistration(app, setupEnrolmentAAD, setupBindHash(code), enrolment, totpCode)
	if err != nil {
		return nil, nil, err
	}
	// The name sealed into the enrolment, whatever this request says.
	member, err := registerFirstMaintainer(app, code, pending.newInstance, CredentialPasswordTOTP, pending.register)
	if err != nil {
		return nil, nil, err
	}
	return member, *pending.codes, nil
}

// beginSetupPasskey starts creating the first maintainer with a passkey: as [BeginPasskeyRegistration], with the
// setup code instead of an invite and the instance name sealed into the ceremony state.
func beginSetupPasskey(app core.App, code, instanceName, loginName, displayName string) (*protocol.CredentialCreation,
	string, error) {
	p, err := passkeysOf(app)
	if err != nil {
		return nil, "", err
	}
	if err := checkSetupCode(app, code); err != nil {
		return nil, "", err
	}
	name, err := settings.NormalizeName(instanceName)
	if err != nil {
		return nil, "", err
	}
	if loginName = NormalizeLoginName(loginName); !validLoginName(loginName) {
		return nil, "", errLoginNameInvalid
	}
	var ok bool
	if displayName, ok = normalizeDisplayName(displayName, loginName); !ok {
		return nil, "", errDisplayNameInvalid
	}
	return beginPasskeyEnrolment(app, p, ceremonySetup, setupBindHash(code), name, loginName, displayName)
}

// finishSetupPasskey completes it: as [FinishPasskeyRegistration], creating the first maintainer.
func finishSetupPasskey(app core.App, code, state string, response []byte) (*core.Record, []string, error) {
	p, err := passkeysOf(app)
	if err != nil {
		return nil, nil, err
	}
	if err := checkSetupCode(app, code); err != nil {
		return nil, nil, err
	}
	pending, err := passkeyRegistration(p, ceremonySetup, setupBindHash(code), state, response)
	if err != nil {
		return nil, nil, err
	}
	member, err := registerFirstMaintainer(app, code, pending.newInstance, "passkey", pending.register)
	if err != nil {
		return nil, nil, err
	}
	return member, *pending.codes, nil
}

// completeSetup is the last step of the wizard, on behalf of actor, a maintainer: it checks the cities and the
// instance's name ([settings.Normalize]; an empty name keeps the one saved with the first maintainer) and saves them,
// in one transaction that first checks that the instance has no cities yet ([ErrSetupDone] otherwise) and writes the
// audit_log entries [AuditSettingsUpdate] and [AuditSetupComplete]. It returns the settings as saved.
func completeSetup(app core.App, actor *core.Record, in settings.Instance) (settings.Instance, error) {
	if err := requireNoCities(app); err != nil {
		return settings.Instance{}, err
	}
	if strings.TrimSpace(in.Name) == "" {
		current, err := settings.Load(app)
		if err != nil {
			return settings.Instance{}, err
		}
		in.Name = current.Name
	}
	inst, err := settings.Normalize(in)
	if err != nil {
		return settings.Instance{}, err
	}
	err = app.RunInTransaction(func(tx core.App) error {
		if err := requireNoCities(tx); err != nil {
			return err
		}
		if err := settings.Save(tx, inst); err != nil {
			return err
		}
		slugs := make([]string, 0, len(inst.Cities))
		for _, c := range inst.Cities {
			slugs = append(slugs, c.Slug)
		}
		if err := writeAudit(tx, actor, AuditSettingsUpdate, store.InstanceSettings, "",
			map[string]any{"name": inst.Name, "cities": slugs}); err != nil {
			return err
		}
		return writeAudit(tx, actor, AuditSetupComplete, store.InstanceSettings, "", nil)
	})
	if errors.Is(err, ErrSetupDone) {
		return settings.Instance{}, err
	}
	if err != nil {
		return settings.Instance{}, fmt.Errorf("auth: complete setup: %w", err)
	}
	return inst, nil
}

// hasCities reports whether the instance settings have cities: the setup is complete then.
func hasCities(app core.App) (bool, error) {
	inst, err := settings.Load(app)
	return len(inst.Cities) > 0, err
}

// requireNoCities returns [ErrSetupDone] when the instance has cities.
func requireNoCities(app core.App) error {
	done, err := hasCities(app)
	if err == nil && done {
		return ErrSetupDone
	}
	return err
}

// ReadSetupCode returns the setup code from the file the server writes it to, for `osfm setup-code`. It fails when the
// instance has a maintainer already (there is no setup code then) or the server has not written the file.
func ReadSetupCode(app core.App, file string) (string, error) {
	needs, err := NeedsSetup(app)
	if err != nil {
		return "", err
	}
	if !needs {
		return "", errors.New("this instance has a maintainer already: there is no setup code")
	}
	b, err := os.ReadFile(file) //nolint:gosec // G304: from the configuration
	if errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("no setup code yet: start the server (osfm serve), which generates one")
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
