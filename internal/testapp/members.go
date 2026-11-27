package testapp

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pquerna/otp/totp"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/auth/passkeytest"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Password is the made-up password of the members [App.RegisterPasswordTOTP] creates.
const Password = "test password, made up" // #nosec G101 -- made up

// Invite creates an invite as the seeded maintainer, through the invite route, and returns its code.
func (a *App) Invite() string {
	a.t.Helper()
	created := Decode[auth.CreatedInvite](a.As(Maintainer).Post(auth.InvitesPath, auth.CreateInviteRequest{Days: 7}).
		Expect(http.StatusCreated))
	return created.Code
}

// PasswordMember is a member who registered with password and TOTP.
type PasswordMember struct {
	Record    *core.Record
	LoginName string
	// Secret is the TOTP secret in base32, as the authenticator app has it.
	Secret        string
	RecoveryCodes []string
	// Client is signed in since the registration.
	Client *Client

	app      *App
	lastStep int64 // the last TOTP time step used
}

// RegisterPasswordTOTP registers a member with a new invite, password and TOTP, through the registration routes, as
// the web app does; the member is signed in afterwards (PasswordMember.Client).
func (a *App) RegisterPasswordTOTP(loginName string) *PasswordMember {
	a.t.Helper()
	invite := a.Invite()
	c := a.Client()
	start := Decode[auth.RegisterPasswordStartResponse](c.Post(auth.RegisterPasswordStartPath,
		auth.RegisterPasswordStartRequest{Invite: invite, LoginName: loginName, DisplayName: "Test " + loginName,
			Password: Password}).Expect(http.StatusOK))
	m := &PasswordMember{LoginName: loginName, Secret: start.Secret, Client: c, app: a, lastStep: -1}
	registered := Decode[auth.RegisteredResponse](c.Post(auth.RegisterPasswordConfirmPath,
		auth.RegisterPasswordConfirmRequest{Invite: invite, Enrolment: start.Enrolment, Code: m.Code()}).
		Expect(http.StatusCreated))
	m.RecoveryCodes = registered.RecoveryCodes
	m.Record = a.memberByLoginName(loginName)
	return m
}

// Code returns a TOTP code the server accepts now: of a time step later than the last one this member used (the
// server takes each step once), within the one step of clock skew it allows. It waits when the steps it may use are
// used up.
func (m *PasswordMember) Code() string {
	m.app.t.Helper()
	const period = 30
	for {
		current := time.Now().Unix() / period
		step := max(current, m.lastStep+1)
		if step <= current+1 {
			m.lastStep = step
			code, err := totp.GenerateCode(m.Secret, time.Unix(step*period, 0))
			if err != nil {
				m.app.t.Fatal(err)
			}
			return code
		}
		time.Sleep(time.Until(time.Unix((step-1)*period, 0)) + 50*time.Millisecond)
	}
}

// SignIn signs the member in on a new client, with password and a TOTP code, and returns the client.
func (m *PasswordMember) SignIn() *Client {
	m.app.t.Helper()
	c := m.app.Client()
	ticket := Decode[auth.SignInTicketResponse](c.Post(auth.SignInPasswordPath,
		auth.SignInPasswordRequest{LoginName: m.LoginName, Password: Password}).Expect(http.StatusOK))
	c.Post(auth.SignInTOTPPath, auth.SignInCodeRequest{Ticket: ticket.Ticket, Code: m.Code()}).Expect(http.StatusOK)
	if c.Session() == "" {
		m.app.t.Fatal("testapp: no session cookie after signing in")
	}
	return c
}

// PasskeyMember is a member who registered with a passkey.
type PasskeyMember struct {
	Record        *core.Record
	LoginName     string
	Authenticator *passkeytest.Authenticator
	RecoveryCodes []string
	// Client is signed in since the registration.
	Client *Client

	app *App
}

// RegisterPasskey registers a member with a new invite and a passkey of a software authenticator, through the
// passkey routes, as a browser does; the member is signed in afterwards (PasskeyMember.Client).
func (a *App) RegisterPasskey(loginName string) *PasskeyMember {
	a.t.Helper()
	invite := a.Invite()
	_, origins := a.Config.PasskeyRP()
	m := &PasskeyMember{LoginName: loginName, Authenticator: &passkeytest.Authenticator{Origin: origins[0]},
		Client: a.Client(), app: a}
	options := Decode[passkeyOptions](m.Client.Post(auth.PasskeyRegisterOptionsPath, auth.PasskeyRegisterOptionsRequest{
		Invite: invite, LoginName: loginName, DisplayName: "Test " + loginName}).Expect(http.StatusOK))
	credential, err := m.Authenticator.Create(options.Options)
	if err != nil {
		a.t.Fatal(err)
	}
	registered := Decode[auth.PasskeyRegisterResponse](m.Client.Post(auth.PasskeyRegisterPath,
		auth.PasskeyRegisterRequest{Invite: invite, State: options.State, Credential: credential}).
		Expect(http.StatusCreated))
	m.RecoveryCodes = registered.RecoveryCodes
	m.Record = a.memberByLoginName(loginName)
	return m
}

// SignIn signs the member in on a new client with the passkey and returns the client.
func (m *PasskeyMember) SignIn() *Client {
	m.app.t.Helper()
	c := m.app.Client()
	options := Decode[passkeyOptions](c.Post(auth.PasskeySignInOptionsPath, nil).Expect(http.StatusOK))
	assertion, err := m.Authenticator.Get(options.Options)
	if err != nil {
		m.app.t.Fatal(err)
	}
	c.Post(auth.PasskeySignInPath, auth.PasskeySignInRequest{State: options.State, Credential: assertion}).
		Expect(http.StatusNoContent)
	if c.Session() == "" {
		m.app.t.Fatal("testapp: no session cookie after signing in")
	}
	return c
}

// passkeyOptions is auth.PasskeyOptionsResponse with the options kept as JSON for the authenticator.
type passkeyOptions struct {
	Options json.RawMessage `json:"options"`
	State   string          `json:"state"`
}

func (a *App) memberByLoginName(loginName string) *core.Record {
	a.t.Helper()
	r, err := a.FindFirstRecordByData(store.Members, "login_name", loginName)
	if err != nil {
		a.t.Fatalf("testapp: member %s: %v", loginName, err)
	}
	return r
}
