package main

import (
	"net/http"
	"strings"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of the M1 routes of internal/auth: the session, invites, registration and sign-in,
// passkeys, the setup wizard, credential management, recovery, reset links and the member list. See registerProbes.
func init() {
	registerProbes(probeSet{
		probes: map[string][]idscan.Request{
			"GET " + auth.MePath: {
				{Path: auth.MePath, Status: http.StatusOK},
			},
			"POST " + auth.LogoutPath: {
				{Method: http.MethodPost, Path: auth.LogoutPath, Header: csrfHeader, Status: http.StatusNoContent},
			},
			// Maintainers see every invite, members their own (T034): the seeded member has none.
			"GET " + auth.InvitesPath: {
				{Path: auth.InvitesPath, Status: http.StatusOK},
			},
			// The seeded member may not invite (permission off): 403 invite_not_allowed.
			"POST " + auth.InvitesPath: {
				{Method: http.MethodPost, Path: auth.InvitesPath, Body: `{"days":7}`, Header: csrfHeader, Status: http.StatusCreated,
					MemberStatus: http.StatusForbidden},
				{Method: http.MethodPost, Path: auth.InvitesPath, Header: csrfHeader, Status: http.StatusCreated,
					MemberStatus: http.StatusForbidden},
				{Method: http.MethodPost, Path: auth.InvitesPath, Body: `{"days":99}`, Header: csrfHeader, Status: http.StatusBadRequest},
			},
			// The seeded invites are the maintainer's: another member's invite is 404 for a member.
			"POST " + auth.InviteRevokePath: {
				// Revoking is idempotent, so the maintainer sees the same status every time.
				{Method: http.MethodPost, Path: revokePath(idscan.ExpiredInviteUUID), Header: csrfHeader, Status: http.StatusOK,
					MemberStatus: http.StatusNotFound},
				{Method: http.MethodPost, Path: revokePath(idscan.UsedInviteUUID), Header: csrfHeader, Status: http.StatusConflict,
					MemberStatus: http.StatusNotFound},
				{Method: http.MethodPost, Path: revokePath("0190a1b2-0000-7000-8000-00000000ffff"), Header: csrfHeader,
					Status: http.StatusNotFound},
				{Method: http.MethodPost, Path: revokePath("x"), Header: csrfHeader, Status: http.StatusNotFound},
			},
			// Public: the registration page checks the code before showing the form.
			"POST " + auth.InviteCheckPath: {
				{Method: http.MethodPost, Path: auth.InviteCheckPath, Body: `{"invite":"` + idscan.InviteCode + `"}`,
					Header: csrfHeader, Status: http.StatusOK},
				{Method: http.MethodPost, Path: auth.InviteCheckPath, Body: `{"code":"x"}`, Header: csrfHeader,
					Status: http.StatusOK},
				{Method: http.MethodPost, Path: auth.InviteCheckPath, Body: `{`, Header: csrfHeader,
					Status: http.StatusBadRequest},
			},
			// Public: registration with password and TOTP. The start probes reach every answer; a pending enrolment is sealed
			// with the instance's master key, so the confirm probes can only reach the refusals (the success path is tested
			// in internal/auth).
			"POST " + auth.RegisterPasswordStartPath: {
				{Method: http.MethodPost, Path: auth.RegisterPasswordStartPath, Header: csrfHeader, Status: http.StatusOK,
					Body: `{"invite":"` + idscan.InviteCode + `","loginName":"test_scan","password":"test scan password"}`},
				{Method: http.MethodPost, Path: auth.RegisterPasswordStartPath, Header: csrfHeader, Status: http.StatusConflict,
					Body: `{"invite":"` + idscan.InviteCode + `","loginName":"test_member","password":"test scan password"}`},
				{Method: http.MethodPost, Path: auth.RegisterPasswordStartPath, Header: csrfHeader, Status: http.StatusForbidden,
					Body: `{"invite":"x","loginName":"test_scan","password":"test scan password"}`},
				{Method: http.MethodPost, Path: auth.RegisterPasswordStartPath, Header: csrfHeader, Body: `{`,
					Status: http.StatusBadRequest},
			},
			"POST " + auth.RegisterPasswordConfirmPath: {
				{Method: http.MethodPost, Path: auth.RegisterPasswordConfirmPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"invite":"` + idscan.InviteCode + `","enrolment":"x","code":"123456"}`},
				{Method: http.MethodPost, Path: auth.RegisterPasswordConfirmPath, Header: csrfHeader, Body: `{`,
					Status: http.StatusBadRequest},
			},
			// Public: sign-in with password, then TOTP or a recovery code. The seeded member has no password, so the password
			// step refuses (the same answer as for a wrong password); tickets are sealed like enrolments.
			"POST " + auth.SignInPasswordPath: {
				{Method: http.MethodPost, Path: auth.SignInPasswordPath, Header: csrfHeader, Status: http.StatusUnauthorized,
					Body: `{"loginName":"test_member","password":"test scan password"}`},
				{Method: http.MethodPost, Path: auth.SignInPasswordPath, Header: csrfHeader, Body: `{`,
					Status: http.StatusBadRequest},
			},
			"POST " + auth.SignInTOTPPath: {
				{Method: http.MethodPost, Path: auth.SignInTOTPPath, Header: csrfHeader, Body: `{"ticket":"x","code":"123456"}`,
					Status: http.StatusUnauthorized},
			},
			"POST " + auth.SignInRecoveryPath: {
				{Method: http.MethodPost, Path: auth.SignInRecoveryPath, Header: csrfHeader,
					Body: `{"ticket":"x","code":"0000-0000-0000-0000"}`, Status: http.StatusUnauthorized},
			},
			// Public: passkey registration and sign-in. The successful second steps need an authenticator; they are scanned
			// by TestPasskeyResponsesHaveNoInternalIDs.
			"POST " + auth.PasskeyRegisterOptionsPath: {
				{Method: http.MethodPost, Path: auth.PasskeyRegisterOptionsPath, Header: csrfHeader, Status: http.StatusOK,
					Body: `{"invite":"` + idscan.InviteCode + `","loginName":"test_probe","displayName":"Test Probe"}`},
				{Method: http.MethodPost, Path: auth.PasskeyRegisterOptionsPath, Header: csrfHeader, Status: http.StatusConflict,
					Body: `{"invite":"` + idscan.InviteCode + `","loginName":"test_member"}`},
				{Method: http.MethodPost, Path: auth.PasskeyRegisterOptionsPath, Header: csrfHeader, Status: http.StatusForbidden,
					Body: `{"invite":"x","loginName":"test_probe"}`},
				{Method: http.MethodPost, Path: auth.PasskeyRegisterOptionsPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"invite":"` + idscan.InviteCode + `","loginName":"x"}`},
				{Method: http.MethodPost, Path: auth.PasskeyRegisterOptionsPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{`},
			},
			"POST " + auth.PasskeyRegisterPath: {
				{Method: http.MethodPost, Path: auth.PasskeyRegisterPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"invite":"` + idscan.InviteCode + `","state":"x","credential":{}}`},
				{Method: http.MethodPost, Path: auth.PasskeyRegisterPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{`},
			},
			"POST " + auth.PasskeySignInOptionsPath: {
				{Method: http.MethodPost, Path: auth.PasskeySignInOptionsPath, Header: csrfHeader, Status: http.StatusOK},
			},
			"POST " + auth.PasskeySignInPath: {
				{Method: http.MethodPost, Path: auth.PasskeySignInPath, Header: csrfHeader, Status: http.StatusUnauthorized,
					Body: `{"state":"x","credential":{"id":"dGVzdC1jcmVk","rawId":"dGVzdC1jcmVk","type":"public-key"}}`},
				{Method: http.MethodPost, Path: auth.PasskeySignInPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{`},
			},
			// The setup wizard. The seeded instance has a maintainer and cities, so every setup route answers 404 (members get
			// 403 from the maintainer-only one first); the setup itself is scanned by TestSetupResponsesHaveNoInternalIDs.
			"GET " + auth.SetupPath: {
				{Path: auth.SetupPath, Status: http.StatusNotFound},
			},
			"POST " + auth.SetupCheckPath: {
				{Method: http.MethodPost, Path: auth.SetupCheckPath, Header: csrfHeader, Body: `{"code":"0000-0000-0000-0000"}`,
					Status: http.StatusNotFound},
				{Method: http.MethodPost, Path: auth.SetupCheckPath, Header: csrfHeader, Body: `{`, Status: http.StatusNotFound},
			},
			"POST " + auth.SetupPasswordStartPath: {
				{Method: http.MethodPost, Path: auth.SetupPasswordStartPath, Header: csrfHeader, Status: http.StatusNotFound,
					Body: `{"setupCode":"0000-0000-0000-0000","loginName":"test_scan","password":"test scan password"}`},
			},
			"POST " + auth.SetupPasswordConfirmPath: {
				{Method: http.MethodPost, Path: auth.SetupPasswordConfirmPath, Header: csrfHeader, Status: http.StatusNotFound,
					Body: `{"setupCode":"0000-0000-0000-0000","enrolment":"x","code":"123456"}`},
			},
			"POST " + auth.SetupPasskeyOptionsPath: {
				{Method: http.MethodPost, Path: auth.SetupPasskeyOptionsPath, Header: csrfHeader, Status: http.StatusNotFound,
					Body: `{"setupCode":"0000-0000-0000-0000","loginName":"test_probe"}`},
			},
			"POST " + auth.SetupPasskeyPath: {
				{Method: http.MethodPost, Path: auth.SetupPasskeyPath, Header: csrfHeader, Status: http.StatusNotFound,
					Body: `{"setupCode":"0000-0000-0000-0000","state":"x","credential":{}}`},
			},
			// Maintainers only (maintainerRoutes).
			"POST " + auth.SetupCompletePath: {
				{Method: http.MethodPost, Path: auth.SetupCompletePath, Header: csrfHeader, Status: http.StatusNotFound,
					Body: `{"name":"Test Circle","cities":[{"slug":"testtown","name":"Test Town","bbox":[1,1,2,2],"center":[1.5,1.5]}]}`},
				{Method: http.MethodPost, Path: auth.SetupCompletePath, Header: csrfHeader, Body: `{`,
					Status: http.StatusBadRequest},
			},
			// The member's own credentials (signed in; the scan's sessions are new, so they count as freshly verified). The
			// seeded member and maintainer each have one passkey and no password.
			"GET " + auth.MeCredentialsPath: {
				{Path: auth.MeCredentialsPath, Status: http.StatusOK},
			},
			"POST " + auth.MeVerifyPath: {
				{Method: http.MethodPost, Path: auth.MeVerifyPath, Header: csrfHeader, Status: http.StatusConflict,
					Body: `{"password":"test scan password","code":"123456"}`},
				{Method: http.MethodPost, Path: auth.MeVerifyPath, Header: csrfHeader, Body: `{`, Status: http.StatusBadRequest},
			},
			"POST " + auth.MeVerifyPasskeyOptionsPath: {
				{Method: http.MethodPost, Path: auth.MeVerifyPasskeyOptionsPath, Header: csrfHeader, Status: http.StatusOK},
			},
			"POST " + auth.MeVerifyPasskeyPath: {
				{Method: http.MethodPost, Path: auth.MeVerifyPasskeyPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"state":"x","credential":{"id":"dGVzdC1jcmVk","rawId":"dGVzdC1jcmVk","type":"public-key"}}`},
			},
			"POST " + auth.MePasskeyOptionsPath: {
				{Method: http.MethodPost, Path: auth.MePasskeyOptionsPath, Header: csrfHeader, Status: http.StatusOK},
			},
			"POST " + auth.MePasskeysPath: {
				{Method: http.MethodPost, Path: auth.MePasskeysPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"state":"x","credential":{}}`},
			},
			// The member's passkey: the member renames it (200) but cannot remove it (409, the last credential); for the
			// maintainer it does not exist (404).
			"POST " + auth.MePasskeyRenamePath: {
				{Method: http.MethodPost, Path: passkeyPath(auth.MePasskeyRenamePath, idscan.MemberPasskeyUUID),
					Header: csrfHeader, Body: `{"deviceName":"Test Phone"}`},
				{Method: http.MethodPost, Path: passkeyPath(auth.MePasskeyRenamePath, "0190a1b2-0000-7000-8000-00000000ffff"),
					Header: csrfHeader, Body: `{"deviceName":"Test Phone"}`, Status: http.StatusNotFound},
				{Method: http.MethodPost, Path: passkeyPath(auth.MePasskeyRenamePath, "x"), Header: csrfHeader,
					Body: `{"deviceName":"Test Phone"}`, Status: http.StatusNotFound},
			},
			"POST " + auth.MePasskeyRemovePath: {
				{Method: http.MethodPost, Path: passkeyPath(auth.MePasskeyRemovePath, idscan.MemberPasskeyUUID),
					Header: csrfHeader},
				{Method: http.MethodPost, Path: passkeyPath(auth.MePasskeyRemovePath, "0190a1b2-0000-7000-8000-00000000ffff"),
					Header: csrfHeader, Status: http.StatusNotFound},
			},
			"POST " + auth.MePasswordStartPath: {
				{Method: http.MethodPost, Path: auth.MePasswordStartPath, Header: csrfHeader, Status: http.StatusOK,
					Body: `{"password":"test scan password"}`},
				{Method: http.MethodPost, Path: auth.MePasswordStartPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"password":"x"}`},
			},
			"POST " + auth.MePasswordConfirmPath: {
				{Method: http.MethodPost, Path: auth.MePasswordConfirmPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"enrolment":"x","code":"123456"}`},
			},
			"POST " + auth.MePasswordRemovePath: {
				{Method: http.MethodPost, Path: auth.MePasswordRemovePath, Header: csrfHeader, Status: http.StatusConflict},
			},
			"POST " + auth.MePasswordChangePath: {
				{Method: http.MethodPost, Path: auth.MePasswordChangePath, Header: csrfHeader, Status: http.StatusConflict,
					Body: `{"password":"test scan password"}`},
			},
			"POST " + auth.MeTOTPStartPath: {
				{Method: http.MethodPost, Path: auth.MeTOTPStartPath, Header: csrfHeader, Status: http.StatusConflict},
			},
			"POST " + auth.MeTOTPConfirmPath: {
				{Method: http.MethodPost, Path: auth.MeTOTPConfirmPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"enrolment":"x","code":"123456"}`},
			},
			"POST " + auth.MeRecoveryCodesPath: {
				{Method: http.MethodPost, Path: auth.MeRecoveryCodesPath, Header: csrfHeader, Status: http.StatusCreated},
			},
			// Public: the recovery of a passkey-only member. The seeded recovery code works once (whichever identity comes
			// first), so its probe has no status; tickets are sealed with the master key, so the enrolment probes reach the
			// refusals (the successes are tested in internal/auth).
			"POST " + auth.RecoverPath: {
				{Method: http.MethodPost, Path: auth.RecoverPath, Header: csrfHeader,
					Body: `{"loginName":"test_member","code":"` + idscan.RecoveryCode + `"}`},
				{Method: http.MethodPost, Path: auth.RecoverPath, Header: csrfHeader, Status: http.StatusUnauthorized,
					Body: `{"loginName":"test_member","code":"0000-0000-0000-0000"}`},
				{Method: http.MethodPost, Path: auth.RecoverPath, Header: csrfHeader, Body: `{`, Status: http.StatusBadRequest},
			},
			"POST " + auth.RecoverPasswordStartPath: {
				{Method: http.MethodPost, Path: auth.RecoverPasswordStartPath, Header: csrfHeader,
					Status: http.StatusUnauthorized, Body: `{"ticket":"x","password":"test scan password"}`},
			},
			"POST " + auth.RecoverPasswordConfirmPath: {
				{Method: http.MethodPost, Path: auth.RecoverPasswordConfirmPath, Header: csrfHeader,
					Status: http.StatusUnauthorized, Body: `{"ticket":"x","enrolment":"x","code":"123456"}`},
			},
			"POST " + auth.RecoverPasskeyOptionsPath: {
				{Method: http.MethodPost, Path: auth.RecoverPasskeyOptionsPath, Header: csrfHeader,
					Status: http.StatusUnauthorized, Body: `{"ticket":"x"}`},
			},
			"POST " + auth.RecoverPasskeyPath: {
				{Method: http.MethodPost, Path: auth.RecoverPasskeyPath, Header: csrfHeader, Status: http.StatusUnauthorized,
					Body: `{"ticket":"x","state":"x","credential":{}}`},
			},
			// Public: using a reset link. The seeded active link stays valid throughout (no probe completes a reset).
			"POST " + auth.ResetCheckPath: {
				{Method: http.MethodPost, Path: auth.ResetCheckPath, Header: csrfHeader, Status: http.StatusOK,
					Body: `{"token":"` + idscan.ResetToken + `"}`},
				{Method: http.MethodPost, Path: auth.ResetCheckPath, Header: csrfHeader, Status: http.StatusOK,
					Body: `{"token":"x"}`},
				{Method: http.MethodPost, Path: auth.ResetCheckPath, Header: csrfHeader, Body: `{`, Status: http.StatusBadRequest},
			},
			"POST " + auth.ResetPasswordStartPath: {
				{Method: http.MethodPost, Path: auth.ResetPasswordStartPath, Header: csrfHeader, Status: http.StatusOK,
					Body: `{"token":"` + idscan.ResetToken + `","password":"test scan password"}`},
				{Method: http.MethodPost, Path: auth.ResetPasswordStartPath, Header: csrfHeader, Status: http.StatusForbidden,
					Body: `{"token":"x","password":"test scan password"}`},
			},
			"POST " + auth.ResetPasswordConfirmPath: {
				{Method: http.MethodPost, Path: auth.ResetPasswordConfirmPath, Header: csrfHeader,
					Status: http.StatusBadRequest, Body: `{"token":"` + idscan.ResetToken + `","enrolment":"x","code":"123456"}`},
				{Method: http.MethodPost, Path: auth.ResetPasswordConfirmPath, Header: csrfHeader,
					Status: http.StatusForbidden, Body: `{"token":"x","enrolment":"x","code":"123456"}`},
			},
			"POST " + auth.ResetPasskeyOptionsPath: {
				{Method: http.MethodPost, Path: auth.ResetPasskeyOptionsPath, Header: csrfHeader, Status: http.StatusOK,
					Body: `{"token":"` + idscan.ResetToken + `"}`},
				{Method: http.MethodPost, Path: auth.ResetPasskeyOptionsPath, Header: csrfHeader, Status: http.StatusForbidden,
					Body: `{"token":"x"}`},
			},
			"POST " + auth.ResetPasskeyPath: {
				{Method: http.MethodPost, Path: auth.ResetPasskeyPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"token":"` + idscan.ResetToken + `","state":"x","credential":{}}`},
			},
			// Maintainers only (maintainerRoutes): the member list and reset links.
			"GET " + auth.MembersPath: {
				{Path: auth.MembersPath, Status: http.StatusOK},
			},
			"GET " + auth.ResetLinksPath: {
				{Path: auth.ResetLinksPath, Status: http.StatusOK},
			},
			"POST " + auth.ResetLinksPath: {
				{Method: http.MethodPost, Path: auth.ResetLinksPath, Header: csrfHeader, Status: http.StatusCreated,
					Body: `{"member":"` + idscan.ResetMemberUUID + `"}`},
				{Method: http.MethodPost, Path: auth.ResetLinksPath, Header: csrfHeader, Status: http.StatusNotFound,
					Body: `{"member":"0190a1b2-0000-7000-8000-00000000ffff"}`},
				{Method: http.MethodPost, Path: auth.ResetLinksPath, Header: csrfHeader, Status: http.StatusNotFound,
					Body: `{"member":"` + idscan.LeftMemberUUID + `"}`},
				{Method: http.MethodPost, Path: auth.ResetLinksPath, Header: csrfHeader, Status: http.StatusNotFound,
					Body: `{"loginName":"test_reset"}`},
				{Method: http.MethodPost, Path: auth.ResetLinksPath, Header: csrfHeader, Status: http.StatusConflict,
					Body: `{"member":"` + idscan.MaintainerUUID + `"}`},
				{Method: http.MethodPost, Path: auth.ResetLinksPath, Header: csrfHeader, Body: `{`,
					Status: http.StatusBadRequest},
			},
			"POST " + auth.ResetLinkRevokePath: {
				{Method: http.MethodPost, Path: resetRevokePath(idscan.ExpiredResetLinkUUID), Header: csrfHeader,
					Status: http.StatusOK},
				{Method: http.MethodPost, Path: resetRevokePath(idscan.UsedResetLinkUUID), Header: csrfHeader,
					Status: http.StatusConflict},
				{Method: http.MethodPost, Path: resetRevokePath("0190a1b2-0000-7000-8000-00000000ffff"), Header: csrfHeader,
					Status: http.StatusNotFound},
				{Method: http.MethodPost, Path: resetRevokePath("x"), Header: csrfHeader, Status: http.StatusNotFound},
			},
		},
		// Public: sign-in, registration, setup, recovery and credential reset (E1).
		public: []string{
			"POST " + auth.InviteCheckPath,
			"POST " + auth.RegisterPasswordStartPath,
			"POST " + auth.RegisterPasswordConfirmPath,
			"POST " + auth.SignInPasswordPath,
			"POST " + auth.SignInTOTPPath,
			"POST " + auth.SignInRecoveryPath,
			"POST " + auth.PasskeyRegisterOptionsPath,
			"POST " + auth.PasskeyRegisterPath,
			"POST " + auth.PasskeySignInOptionsPath,
			"POST " + auth.PasskeySignInPath,
			"GET " + auth.SetupPath,
			"POST " + auth.SetupCheckPath,
			"POST " + auth.SetupPasswordStartPath,
			"POST " + auth.SetupPasswordConfirmPath,
			"POST " + auth.SetupPasskeyOptionsPath,
			"POST " + auth.SetupPasskeyPath,
			"POST " + auth.RecoverPath,
			"POST " + auth.RecoverPasswordStartPath,
			"POST " + auth.RecoverPasswordConfirmPath,
			"POST " + auth.RecoverPasskeyOptionsPath,
			"POST " + auth.RecoverPasskeyPath,
			"POST " + auth.ResetCheckPath,
			"POST " + auth.ResetPasswordStartPath,
			"POST " + auth.ResetPasswordConfirmPath,
			"POST " + auth.ResetPasskeyOptionsPath,
			"POST " + auth.ResetPasskeyPath,
		},
		maintainerOnly: []string{
			"POST " + auth.SetupCompletePath,
			"GET " + auth.MembersPath,
			"GET " + auth.ResetLinksPath,
			"POST " + auth.ResetLinksPath,
			"POST " + auth.ResetLinkRevokePath,
		},
	})
}

func revokePath(uuid string) string {
	return strings.Replace(auth.InviteRevokePath, "{uuid}", uuid, 1)
}

func resetRevokePath(uuid string) string {
	return strings.Replace(auth.ResetLinkRevokePath, "{uuid}", uuid, 1)
}

func passkeyPath(pattern, uuid string) string {
	return strings.Replace(pattern, "{uuid}", uuid, 1)
}
