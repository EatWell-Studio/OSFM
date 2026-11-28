package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// exportOf downloads the personal export with c; its contents and headers are tested in internal/api.
func exportOf(t *testing.T, c *testapp.Client) (api.PersonalExport, string) {
	t.Helper()
	r := c.Get(api.PersonalExportPath).Expect(http.StatusOK)
	return testapp.Decode[api.PersonalExport](r), r.Body.String()
}

// TestPersonalExportSecrets: the credential summary has the kind, device name and dates, and the export carries no
// password hash, TOTP secret, passkey ID or public key, user handle, recovery code, session token or hash.
func TestPersonalExportSecrets(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	pw := a.RegisterPasswordTOTP("test_pw")
	pk := a.RegisterPasskey("test_pk")
	pkSecond := pk.SignIn() // a second session

	for _, m := range []struct {
		name    string
		record  *core.Record
		client  *testapp.Client
		kind    string
		codes   []string
		secrets []string
	}{
		{"password and TOTP", pw.Record, pw.Client, auth.CredentialPasswordTOTP, pw.RecoveryCodes,
			[]string{pw.Secret, pw.Client.Session(), testapp.Password}},
		{"passkey", pk.Record, pk.Client, auth.CredentialPasskey, pk.RecoveryCodes,
			[]string{pk.Client.Session(), pkSecond.Session()}},
	} {
		t.Run(m.name, func(t *testing.T) {
			member, err := a.FindRecordById(store.Members, m.record.Id)
			if err != nil {
				t.Fatal(err)
			}
			secrets := append([]string{member.TokenKey(), member.GetString("password"),
				member.GetString("passkey_user_handle")}, m.secrets...)
			for _, code := range m.codes {
				secrets = append(secrets, code, strings.ReplaceAll(code, "-", ""))
			}
			for collection, fields := range map[string][]string{
				store.Credentials:   {store.FieldUUID, "credential_id", "public_key", "password_hash", "totp_secret_enc"},
				store.RecoveryCodes: {"code_hash"},
				store.Sessions:      {"token_hash"},
			} {
				records, err := a.FindAllRecords(collection, dbx.HashExp{"member": member.Id})
				if err != nil {
					t.Fatal(err)
				}
				if len(records) == 0 {
					t.Fatalf("no %s to look for", collection)
				}
				for _, r := range records {
					for _, f := range fields {
						secrets = append(secrets, r.GetString(f))
					}
				}
			}

			ex, body := exportOf(t, m.client)
			if len(ex.Credentials) != 1 || ex.Credentials[0].Kind != m.kind || ex.Credentials[0].CreatedAt.IsZero() {
				t.Errorf("credentials %+v", ex.Credentials)
			}
			for _, s := range secrets {
				if len(s) >= 8 && strings.Contains(body, s) {
					t.Errorf("the export contains a secret %q", s)
				}
			}
			for _, s := range []string{"password_hash", "totp_secret", "public_key", "credential_id", "token", "hash",
				"recovery"} {
				if strings.Contains(body, s) {
					t.Errorf("the export contains %q", s)
				}
			}
		})
	}
	// Passkeys keep the name the member gave them.
	if _, err := a.DB().Update(store.Credentials, dbx.Params{"device_name": "Test Laptop"},
		dbx.HashExp{"member": pk.Record.Id}).Execute(); err != nil {
		t.Fatal(err)
	}
	if ex, _ := exportOf(t, pk.Client); len(ex.Credentials) != 1 || ex.Credentials[0].DeviceName != "Test Laptop" {
		t.Errorf("credentials %+v", ex.Credentials)
	}
	a.CheckNoInternalIDs()
}
