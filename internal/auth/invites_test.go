package auth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// reqJSON sends a request with a JSON body (none when empty) and the X-OSFM header for a state-changing method.
func (en *env) reqJSON(method, path, token, body string) *httptest.ResponseRecorder {
	en.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		r.Header.Set(csrf, "1")
	}
	if token != "" {
		r.Header.Add("Cookie", auth.CookieName+"="+token)
	}
	rec := httptest.NewRecorder()
	en.h.ServeHTTP(rec, r)
	return rec
}

// invite creates an invite of inviter valid for the default time and returns its code and record.
func (en *env) invite(inviter *core.Record) (string, *core.Record) {
	en.t.Helper()
	code, invite, err := auth.CreateInvite(en.app, inviter, auth.DefaultInviteDays)
	if err != nil {
		en.t.Fatal(err)
	}
	return code, invite
}

// setInvite changes an invite's columns in SQL.
func (en *env) setInvite(invite *core.Record, params dbx.Params) {
	en.t.Helper()
	if _, err := en.app.DB().Update(store.Invites, params, dbx.HashExp{"id": invite.Id}).Execute(); err != nil {
		en.t.Fatal(err)
	}
}

func (en *env) reload(r *core.Record) *core.Record {
	en.t.Helper()
	fresh, err := en.app.FindRecordById(r.Collection().Name, r.Id)
	if err != nil {
		en.t.Fatal(err)
	}
	return fresh
}

func registerAs(login string) auth.Registration {
	return func(tx core.App, m *core.Record) error {
		m.Set("login_name", login)
		m.Set("display_name", "Test "+login)
		return tx.Save(m)
	}
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String())
	}
	return v
}

func past() string { return types.NowDateTime().Add(-time.Hour).String() }

// TestInviteRoutesNeedAMember: anonymous callers get 401 on every invite route; maintainers get through.
func TestInviteRoutesNeedAMember(t *testing.T) {
	en := newEnv(t)
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)
	_, invite := en.invite(maintainer)
	revoke := strings.Replace(auth.InviteRevokePath, "{uuid}", invite.GetString(store.FieldUUID), 1)
	maintainerToken := en.session(maintainer)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, auth.InvitesPath, ""},
		{http.MethodPost, auth.InvitesPath, `{"days":7}`},
		{http.MethodPost, revoke, ""},
	} {
		if got := en.reqJSON(tc.method, tc.path, "", tc.body).Code; got != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s: %d", tc.method, tc.path, got)
		}
	}
	if got := en.reqJSON(http.MethodGet, auth.InvitesPath, maintainerToken, "").Code; got != http.StatusOK {
		t.Errorf("maintainer list: %d", got)
	}
	if got := en.reqJSON(http.MethodPost, auth.InvitesPath, maintainerToken, "").Code; got != http.StatusCreated {
		t.Errorf("maintainer create: %d", got)
	}
	if got := en.reqJSON(http.MethodPost, revoke, maintainerToken, "").Code; got != http.StatusOK {
		t.Errorf("maintainer revoke: %d", got)
	}
	// A maintainer who has been removed is not one any more; their session is gone anyway.
	maintainer.Set("left_at", types.NowDateTime())
	if err := en.app.Save(maintainer); err != nil {
		t.Fatal(err)
	}
	if auth.IsMaintainer(maintainer) {
		t.Error("a maintainer who left is still a maintainer")
	}
}

// setMember changes a member's columns in SQL.
func (en *env) setMember(member *core.Record, params dbx.Params) {
	en.t.Helper()
	if _, err := en.app.DB().Update(store.Members, params, dbx.HashExp{"id": member.Id}).Execute(); err != nil {
		en.t.Fatal(err)
	}
}

// setCooldownDays sets the invite cooling-off period of the instance settings.
func (en *env) setCooldownDays(days int) {
	en.t.Helper()
	if _, err := en.app.DB().Update(store.InstanceSettings, dbx.Params{"invite_cooldown_days": days},
		nil).Execute(); err != nil {
		en.t.Fatal(err)
	}
}

// TestMemberInvites: members invite once their permission is on and the cooling-off period is over (D12, M2-Q11);
// otherwise 403 invite_not_allowed with the reason and the end of the period. They see and revoke only their own
// invites, have at most domain.MaxActiveInvites active ones (maintainers have no limit), and the members they invite
// have them as inviter.
func TestMemberInvites(t *testing.T) {
	en := newEnv(t)
	en.setCooldownDays(30)
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)
	member := en.member("test_member", auth.RoleMember)
	token, maintainerToken := en.session(member), en.session(maintainer)
	_, maintainerInvite := en.invite(maintainer)
	day := 24 * time.Hour

	notAllowed := func(step, reason string, end time.Time) {
		t.Helper()
		rec := en.reqJSON(http.MethodPost, auth.InvitesPath, token, `{"days":7}`)
		e := decode[auth.AuthError](t, rec)
		if rec.Code != http.StatusForbidden || e.Code != auth.CodeInviteNotAllowed || e.Data["reason"] != reason ||
			e.Data["cooldownEndsAt"] != end.UTC().Format(time.RFC3339) {
			t.Errorf("%s: %d %+v, want 403 %s until %s", step, rec.Code, e, reason, end.UTC().Format(time.RFC3339))
		}
	}
	created := func(step string) auth.InviteInfo {
		t.Helper()
		rec := en.reqJSON(http.MethodPost, auth.InvitesPath, token, `{"days":7}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s: %d %s", step, rec.Code, rec.Body.String())
		}
		return decode[auth.CreatedInvite](t, rec).Invite
	}

	// The permission is off (members from before M2-Q11, or switched off by a maintainer): that is the reason even
	// during the cooling-off period.
	joined := time.Now().UTC().Add(-2 * day).Truncate(time.Second)
	en.setMember(member, dbx.Params{"joined_at": joined.Format(types.DefaultDateLayout)})
	notAllowed("permission off", "disabled", joined.Add(30*day))

	en.setMember(member, dbx.Params{"can_invite": true})
	notAllowed("cooling-off period", "cooldown", joined.Add(30*day))
	// A shorter period applies at once; a longer one too.
	en.setCooldownDays(1)
	created("period shortened")
	en.setCooldownDays(30)
	notAllowed("period lengthened", "cooldown", joined.Add(30*day))
	// Opened early by a maintainer.
	en.setMember(member, dbx.Params{"invite_allowed_from": past()})
	mine := created("opened early")
	if mine.CreatedBy == nil || mine.CreatedBy.UUID != member.GetString(store.FieldUUID) {
		t.Errorf("created by %+v", mine.CreatedBy)
	}
	created("third")

	// At most three active invites; a revoked (or used, or expired) one frees a place.
	rec := en.reqJSON(http.MethodPost, auth.InvitesPath, token, `{"days":7}`)
	if e := decode[auth.AuthError](t, rec); rec.Code != http.StatusConflict || e.Code != auth.CodeInviteLimitReached ||
		e.Data["limit"] != float64(domain.MaxActiveInvites) {
		t.Errorf("fourth invite: %d %+v", rec.Code, e)
	}
	revokeMine := strings.Replace(auth.InviteRevokePath, "{uuid}", mine.UUID, 1)
	if rec := en.reqJSON(http.MethodPost, revokeMine, token, ""); rec.Code != http.StatusOK {
		t.Errorf("revoking my invite: %d %s", rec.Code, rec.Body.String())
	}
	created("after revoking one")

	// Members see only their own invites, maintainers all of them.
	list := decode[auth.InviteList](t, en.reqJSON(http.MethodGet, auth.InvitesPath, token, "")).Invites
	if len(list) != 4 {
		t.Errorf("the member sees %d invites, want their 4", len(list))
	}
	for _, inv := range list {
		if inv.CreatedBy == nil || inv.CreatedBy.UUID != member.GetString(store.FieldUUID) {
			t.Errorf("the member sees an invite by %+v", inv.CreatedBy)
		}
	}
	if n := len(decode[auth.InviteList](t, en.reqJSON(http.MethodGet, auth.InvitesPath, maintainerToken, "")).Invites); n != 5 {
		t.Errorf("the maintainer sees %d invites, want 5", n)
	}
	// Another member's invite is 404 for a member, as a missing one is; it stays active.
	revokeOther := strings.Replace(auth.InviteRevokePath, "{uuid}", maintainerInvite.GetString(store.FieldUUID), 1)
	if rec := en.reqJSON(http.MethodPost, revokeOther, token, ""); rec.Code != http.StatusNotFound {
		t.Errorf("revoking another member's invite: %d", rec.Code)
	}
	if auth.InviteState(en.reload(maintainerInvite), time.Now()) != auth.InviteActive {
		t.Error("a member revoked another member's invite")
	}
	// A maintainer revokes any invite.
	revokeSecond := strings.Replace(auth.InviteRevokePath, "{uuid}", list[0].UUID, 1)
	if rec := en.reqJSON(http.MethodPost, revokeSecond, maintainerToken, ""); rec.Code != http.StatusOK {
		t.Errorf("the maintainer revoking a member's invite: %d", rec.Code)
	}

	// The member's invite registers a member invited by them, who may invite after their own cooling-off period.
	code, _, err := auth.CreateInvite(en.app, en.reload(member), 1)
	if err != nil {
		t.Fatal(err)
	}
	invited, err := auth.RegisterWithInvite(en.app, code, registerAs("test_invited"))
	if err != nil {
		t.Fatal(err)
	}
	invited = en.reload(invited)
	if invited.GetString("invited_by") != member.Id || !invited.GetBool("can_invite") ||
		invited.GetDateTime("joined_at").IsZero() {
		t.Errorf("invited member: invited_by %q, can_invite %v, joined_at %v", invited.GetString("invited_by"),
			invited.GetBool("can_invite"), invited.GetDateTime("joined_at"))
	}
	right, err := auth.InviteRightOf(en.app, invited)
	if err != nil {
		t.Fatal(err)
	}
	if right.Allowed || right.Reason != domain.InviteReasonCooldown ||
		!right.CooldownEndsAt.Equal(invited.GetDateTime("joined_at").Time().Add(30*day)) {
		t.Errorf("the new member's right: %+v", right)
	}

	// The permission switched off again: the member may still list and revoke their invites, not create one.
	en.setMember(member, dbx.Params{"can_invite": false})
	if rec := en.reqJSON(http.MethodPost, auth.InvitesPath, token, ""); rec.Code != http.StatusForbidden {
		t.Errorf("create with the permission off: %d", rec.Code)
	}
	if rec := en.reqJSON(http.MethodGet, auth.InvitesPath, token, ""); rec.Code != http.StatusOK {
		t.Errorf("list with the permission off: %d", rec.Code)
	}

	// Maintainers have no limit and no cooling-off period.
	for i := range domain.MaxActiveInvites + 1 {
		if rec := en.reqJSON(http.MethodPost, auth.InvitesPath, maintainerToken, ""); rec.Code != http.StatusCreated {
			t.Errorf("maintainer invite %d: %d", i+1, rec.Code)
		}
	}
}

// TestConcurrentMemberInvites: a member with one place left who sends several create requests at once gets exactly
// one invite.
func TestConcurrentMemberInvites(t *testing.T) {
	en := newEnv(t)
	en.setCooldownDays(0)
	member := en.member("test_member", auth.RoleMember)
	en.setMember(member, dbx.Params{"can_invite": true})
	member = en.reload(member)
	for range domain.MaxActiveInvites - 1 {
		en.invite(member)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded, limited := 0, 0
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := auth.CreateInvite(en.app, member, 1)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, auth.ErrInviteLimit):
				limited++
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if succeeded != 1 || limited != 4 {
		t.Errorf("%d created, %d refused", succeeded, limited)
	}
}

// TestCreateInvite: the code is 256 random bits, base64url; only its SHA-256 is stored; the validity is the chosen
// number of days; the creation is audited.
func TestCreateInvite(t *testing.T) {
	en := newEnv(t)
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)
	token := en.session(maintainer)

	before := time.Now().UTC()
	rec := en.reqJSON(http.MethodPost, auth.InvitesPath, token, `{"days":3}`)
	if rec.Code != http.StatusCreated || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d, Cache-Control %q: %s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
	created := decode[auth.CreatedInvite](t, rec)
	raw, err := base64.RawURLEncoding.DecodeString(created.Code)
	if err != nil || len(raw) != 32 {
		t.Fatalf("code %q: %d bytes, %v", created.Code, len(raw), err)
	}
	if created.Invite.State != auth.InviteActive || created.Invite.CreatedBy == nil ||
		created.Invite.CreatedBy.UUID != maintainer.GetString(store.FieldUUID) ||
		created.Invite.UsedBy != nil || created.Invite.UsedAt != nil || created.Invite.RevokedAt != nil {
		t.Errorf("invite %+v", created.Invite)
	}
	if want := before.Add(3 * 24 * time.Hour); created.Invite.ExpiresAt.Before(want.Add(-time.Second)) ||
		created.Invite.ExpiresAt.After(want.Add(time.Minute)) {
		t.Errorf("expires %v, want about %v", created.Invite.ExpiresAt, want)
	}

	invite, err := store.FindByUUID(en.app, store.Invites, created.Invite.UUID)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(created.Code))
	if invite.GetString("code_hash") != hex.EncodeToString(sum[:]) {
		t.Error("the stored hash is not the code's SHA-256")
	}
	for field, value := range invite.FieldsData() {
		if s, ok := value.(string); ok && strings.Contains(s, created.Code) {
			t.Errorf("the code is stored in %s", field)
		}
	}
	entry, err := en.app.FindFirstRecordByFilter(store.AuditLog, "action = {:a} && target_uuid = {:u}",
		dbx.Params{"a": auth.AuditCreateInvite, "u": created.Invite.UUID})
	if err != nil {
		t.Fatalf("audit entry: %v", err)
	}
	if entry.GetString("actor") != maintainer.Id || entry.GetString("target_type") != store.Invites {
		t.Errorf("audit entry: actor %q, target_type %q", entry.GetString("actor"), entry.GetString("target_type"))
	}

	// The default validity, and the bounds.
	rec = en.reqJSON(http.MethodPost, auth.InvitesPath, token, "")
	if def := decode[auth.CreatedInvite](t, rec); def.Invite.ExpiresAt.Sub(before) < 7*24*time.Hour-time.Second ||
		def.Invite.ExpiresAt.Sub(before) > 7*24*time.Hour+time.Minute {
		t.Errorf("default expiry %v", def.Invite.ExpiresAt)
	}
	if def := decode[auth.CreatedInvite](t, rec); def.Code == created.Code {
		t.Error("two invites with the same code")
	}
	for _, body := range []string{`{"days":0}`, `{"days":31}`, `{"days":-1}`, `{"days":"7"}`, `{"days":7.5}`, `[`} {
		if got := en.reqJSON(http.MethodPost, auth.InvitesPath, token, body).Code; got != http.StatusBadRequest {
			t.Errorf("%s: %d", body, got)
		}
	}
}

// TestCheckInviteGivesNothingAway: the public check answers the same for a code that never existed, one that is
// malformed, expired, used or revoked, and one whose inviter has left; only a usable code is valid.
func TestCheckInviteGivesNothingAway(t *testing.T) {
	en := newEnv(t)
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)
	leaving := en.member("test_leaving", auth.RoleMaintainer)

	valid, _ := en.invite(maintainer)
	expired, expiredInvite := en.invite(maintainer)
	en.setInvite(expiredInvite, dbx.Params{"expires_at": past()})
	used, _ := en.invite(maintainer)
	if _, err := auth.RegisterWithInvite(en.app, used, registerAs("test_new")); err != nil {
		t.Fatal(err)
	}
	revoked, revokedInvite := en.invite(maintainer)
	if err := auth.RevokeInvite(en.app, revokedInvite, maintainer); err != nil {
		t.Fatal(err)
	}
	orphaned, orphanedInvite := en.invite(leaving)
	// Past the hooks, which would revoke the invite: the check refuses it all the same.
	if _, err := en.app.DB().Update(store.Members, dbx.Params{"left_at": types.NowDateTime().String()},
		dbx.HashExp{"id": leaving.Id}).Execute(); err != nil {
		t.Fatal(err)
	}
	if auth.InviteState(en.reload(orphanedInvite), time.Now()) != auth.InviteActive {
		t.Fatal("setup: the orphaned invite should still look active")
	}
	neverExisted := base64.RawURLEncoding.EncodeToString(make([]byte, 32))

	check := func(code string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(auth.CheckInviteRequest{Code: code})
		return en.reqJSON(http.MethodPost, auth.InviteCheckPath, "", string(body))
	}
	ok := check(valid)
	if ok.Code != http.StatusOK || !decode[auth.CheckInviteResponse](t, ok).Valid {
		t.Fatalf("valid code: %d %s", ok.Code, ok.Body.String())
	}

	reference := check(neverExisted)
	if reference.Code != http.StatusOK || reference.Body.String() != `{"valid":false}` {
		t.Fatalf("unknown code: %d %s", reference.Code, reference.Body.String())
	}
	for name, code := range map[string]string{
		"expired": expired, "used": used, "revoked": revoked, "inviter left": orphaned,
		"empty": "", "malformed": "not-a-code", "too long": valid + "A", "too short": valid[:42],
	} {
		rec := check(code)
		if rec.Code != reference.Code || rec.Body.String() != reference.Body.String() {
			t.Errorf("%s: %d %s, want the same as for an unknown code", name, rec.Code, rec.Body.String())
		}
		for k := range reference.Header() {
			if k == "Date" {
				continue
			}
			if rec.Header().Get(k) != reference.Header().Get(k) {
				t.Errorf("%s: header %s %q, want %q", name, k, rec.Header().Get(k), reference.Header().Get(k))
			}
		}
	}
	// The check does not consume the invite, and needs the X-OSFM header like every write.
	if !decode[auth.CheckInviteResponse](t, check(valid)).Valid {
		t.Error("checking consumed the invite")
	}
	r := httptest.NewRequest(http.MethodPost, auth.InviteCheckPath, strings.NewReader(`{"code":"x"}`))
	rec := httptest.NewRecorder()
	en.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Errorf("without X-OSFM: %d", rec.Code)
	}
	if got := en.reqJSON(http.MethodPost, auth.InviteCheckPath, "", "{").Code; got != http.StatusBadRequest {
		t.Errorf("malformed body: %d", got)
	}
}

// TestRegisterWithInvite: a usable invite creates a member invited by the inviter and is consumed; expired, used and
// revoked invites cannot be used; a failed registration leaves no member behind and the invite unused.
func TestRegisterWithInvite(t *testing.T) {
	en := newEnv(t)
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)

	code, invite := en.invite(maintainer)
	m, err := auth.RegisterWithInvite(en.app, code, registerAs("test_new"))
	if err != nil {
		t.Fatal(err)
	}
	m = en.reload(m)
	if m.GetString("role") != auth.RoleMember || m.GetString("invited_by") != maintainer.Id ||
		m.GetDateTime("joined_at").IsZero() || !auth.CanSignIn(m) {
		t.Errorf("member: role %q, invited_by %q, joined_at %v", m.GetString("role"), m.GetString("invited_by"),
			m.GetDateTime("joined_at"))
	}
	invite = en.reload(invite)
	if invite.GetString("used_by") != m.Id || invite.GetDateTime("used_at").IsZero() ||
		auth.InviteState(invite, time.Now()) != auth.InviteUsed {
		t.Errorf("invite after use: used_by %q, used_at %v", invite.GetString("used_by"), invite.GetDateTime("used_at"))
	}

	members := func() int64 {
		n, err := en.app.CountRecords(store.Members)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := members()
	// Used, expired, revoked, unknown and malformed codes.
	expired, expiredInvite := en.invite(maintainer)
	en.setInvite(expiredInvite, dbx.Params{"expires_at": past()})
	revoked, revokedInvite := en.invite(maintainer)
	if err := auth.RevokeInvite(en.app, revokedInvite, maintainer); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]string{
		"used": code, "expired": expired, "revoked": revoked,
		"unknown": base64.RawURLEncoding.EncodeToString(make([]byte, 32)), "malformed": "x", "empty": "",
	} {
		called := false
		_, err := auth.RegisterWithInvite(en.app, c, func(tx core.App, m *core.Record) error {
			called = true
			return registerAs("test_"+strings.ReplaceAll(name, " ", ""))(tx, m)
		})
		if !errors.Is(err, auth.ErrInvalidInvite) {
			t.Errorf("%s: %v, want ErrInvalidInvite", name, err)
		}
		if called {
			t.Errorf("%s: the registration ran", name)
		}
	}
	if n := members(); n != before {
		t.Errorf("%d members created with invalid invites", n-before)
	}

	// A registration that fails, one that does not save the member and one that makes a maintainer: all rolled back.
	fresh, freshInvite := en.invite(maintainer)
	for name, register := range map[string]auth.Registration{
		"fails": func(tx core.App, m *core.Record) error {
			if err := registerAs("test_fails")(tx, m); err != nil {
				return err
			}
			return errors.New("made-up failure after saving")
		},
		"does not save": func(core.App, *core.Record) error { return nil },
		"maintainer": func(tx core.App, m *core.Record) error {
			m.Set("role", auth.RoleMaintainer)
			return registerAs("test_maint")(tx, m)
		},
		"other inviter": func(tx core.App, m *core.Record) error {
			m.Set("invited_by", "")
			return registerAs("test_noinviter")(tx, m)
		},
	} {
		if _, err := auth.RegisterWithInvite(en.app, fresh, register); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if n := members(); n != before {
		t.Errorf("%d members left behind by failed registrations", n-before)
	}
	if auth.InviteState(en.reload(freshInvite), time.Now()) != auth.InviteActive {
		t.Error("a failed registration consumed the invite")
	}
	if _, err := auth.RegisterWithInvite(en.app, fresh, registerAs("test_second")); err != nil {
		t.Errorf("the invite after failed registrations: %v", err)
	}
}

// TestConcurrentRegistration: two registrations with the same code at the same time; exactly one succeeds.
func TestConcurrentRegistration(t *testing.T) {
	en := newEnv(t)
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)
	for round := range 5 {
		code, invite := en.invite(maintainer)
		start := make(chan struct{})
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range errs {
			wg.Go(func() {
				<-start
				_, errs[i] = auth.RegisterWithInvite(en.app, code,
					registerAs(fmt.Sprintf("test_r%d_%d", round, i)))
			})
		}
		close(start)
		wg.Wait()

		won := 0
		for _, err := range errs {
			switch {
			case err == nil:
				won++
			case !errors.Is(err, auth.ErrInvalidInvite):
				t.Errorf("round %d: %v", round, err)
			}
		}
		if won != 1 {
			t.Fatalf("round %d: %d registrations succeeded (%v), want 1", round, won, errs)
		}
		n, err := en.app.CountRecords(store.Members, dbx.HashExp{"invited_by": maintainer.Id})
		if err != nil || n != int64(round+1) {
			t.Fatalf("round %d: %d invited members, %v", round, n, err)
		}
		if en.reload(invite).GetString("used_by") == "" {
			t.Fatalf("round %d: invite not linked to the member", round)
		}
	}
}

// TestRevokeInvite: revoking is audited and idempotent; a used invite cannot be revoked; a revoked one cannot be used.
func TestRevokeInvite(t *testing.T) {
	en := newEnv(t)
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)
	token := en.session(maintainer)
	path := func(r *core.Record) string {
		return strings.Replace(auth.InviteRevokePath, "{uuid}", r.GetString(store.FieldUUID), 1)
	}
	audits := func(action string) int64 {
		n, err := en.app.CountRecords(store.AuditLog, dbx.HashExp{"action": action})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	code, invite := en.invite(maintainer)
	rec := en.reqJSON(http.MethodPost, path(invite), token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if got := decode[auth.RevokedInvite](t, rec).Invite; got.State != auth.InviteRevoked || got.RevokedAt == nil {
		t.Errorf("revoked invite %+v", got)
	}
	if n := audits(auth.AuditRevokeInvite); n != 1 {
		t.Errorf("%d revoke audit entries", n)
	}
	if got := en.reqJSON(http.MethodPost, path(invite), token, "").Code; got != http.StatusOK {
		t.Errorf("revoke again: %d", got)
	}
	if n := audits(auth.AuditRevokeInvite); n != 1 {
		t.Errorf("revoking again wrote an audit entry (%d)", n)
	}
	if _, err := auth.RegisterWithInvite(en.app, code, registerAs("test_late")); !errors.Is(err, auth.ErrInvalidInvite) {
		t.Errorf("register with a revoked invite: %v", err)
	}

	usedCode, used := en.invite(maintainer)
	if _, err := auth.RegisterWithInvite(en.app, usedCode, registerAs("test_new")); err != nil {
		t.Fatal(err)
	}
	if got := en.reqJSON(http.MethodPost, path(used), token, "").Code; got != http.StatusConflict {
		t.Errorf("revoke a used invite: %d", got)
	}
	if auth.InviteState(en.reload(used), time.Now()) != auth.InviteUsed {
		t.Error("the used invite changed")
	}

	for _, p := range []string{
		strings.Replace(auth.InviteRevokePath, "{uuid}", "0190a1b2-0000-7000-8000-000000000001", 1),
		strings.Replace(auth.InviteRevokePath, "{uuid}", invite.Id, 1),
		strings.Replace(auth.InviteRevokePath, "{uuid}", "not-a-uuid", 1),
	} {
		if got := en.reqJSON(http.MethodPost, p, token, "").Code; got != http.StatusNotFound {
			t.Errorf("%s: %d", p, got)
		}
	}
}

// TestListInvites: every state, who used an invite by uuid and main name, and nothing secret.
func TestListInvites(t *testing.T) {
	en := newEnv(t)
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)
	token := en.session(maintainer)

	_, active := en.invite(maintainer)
	usedCode, used := en.invite(maintainer)
	newMember, err := auth.RegisterWithInvite(en.app, usedCode, registerAs("test_new"))
	if err != nil {
		t.Fatal(err)
	}
	_, expired := en.invite(maintainer)
	en.setInvite(expired, dbx.Params{"expires_at": past()})
	_, revoked := en.invite(maintainer)
	if err := auth.RevokeInvite(en.app, revoked, maintainer); err != nil {
		t.Fatal(err)
	}

	rec := en.reqJSON(http.MethodGet, auth.InvitesPath, token, "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("list: %d", rec.Code)
	}
	list := decode[auth.InviteList](t, rec)
	states := map[string]auth.InviteInfo{}
	for _, inv := range list.Invites {
		states[inv.UUID] = inv
	}
	for want, r := range map[string]*core.Record{
		auth.InviteActive: active, auth.InviteUsed: used, auth.InviteExpired: expired, auth.InviteRevoked: revoked,
	} {
		got, ok := states[r.GetString(store.FieldUUID)]
		if !ok || got.State != want {
			t.Errorf("invite %s: %+v, want state %s", want, got, want)
		}
	}
	u := states[used.GetString(store.FieldUUID)]
	if u.UsedBy == nil || u.UsedBy.UUID != newMember.GetString(store.FieldUUID) || u.UsedBy.DisplayName != "Test test_new" ||
		u.UsedAt == nil {
		t.Errorf("used invite %+v", u)
	}
	body := rec.Body.String()
	for _, r := range []*core.Record{active, used, expired, revoked} {
		if strings.Contains(body, r.GetString("code_hash")) || strings.Contains(body, r.Id) {
			t.Error("the list contains a hash or an internal ID")
		}
	}
	if len(list.Invites) != 4 || list.Invites[0].UUID != revoked.GetString(store.FieldUUID) {
		t.Errorf("list order: %+v", list.Invites)
	}
}

// TestLeavingRevokesInvites: when an inviter leaves, their open invites are revoked; used ones stay as they are.
func TestLeavingRevokesInvites(t *testing.T) {
	en := newEnv(t)
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)
	other := en.member("test_other", auth.RoleMaintainer)
	_, open := en.invite(maintainer)
	usedCode, used := en.invite(maintainer)
	if _, err := auth.RegisterWithInvite(en.app, usedCode, registerAs("test_new")); err != nil {
		t.Fatal(err)
	}
	_, others := en.invite(other)

	maintainer.Set("left_at", types.NowDateTime())
	if err := en.app.Save(maintainer); err != nil {
		t.Fatal(err)
	}
	if s := auth.InviteState(en.reload(open), time.Now()); s != auth.InviteRevoked {
		t.Errorf("open invite: %s", s)
	}
	if s := auth.InviteState(en.reload(used), time.Now()); s != auth.InviteUsed {
		t.Errorf("used invite: %s", s)
	}
	if s := auth.InviteState(en.reload(others), time.Now()); s != auth.InviteActive {
		t.Errorf("another maintainer's invite: %s", s)
	}
	if _, err := en.app.FindFirstRecordByFilter(store.AuditLog, "action = {:a} && target_uuid = {:u}",
		dbx.Params{"a": auth.AuditRevokeInvites, "u": maintainer.GetString(store.FieldUUID)}); err != nil {
		t.Errorf("audit entry: %v", err)
	}
}

// TestInviteWriteBudget: creating and revoking invites count against the member's write budget, over all their
// sessions; listing does not.
func TestInviteWriteBudget(t *testing.T) {
	en := newEnvWith(t, auth.Options{Limits: &auth.Limits{MemberWrites: ratelimit.Rate{Burst: 2, Every: time.Hour}}})
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)
	other := en.member("test_other_maintainer", auth.RoleMaintainer)
	for range 3 {
		en.reqJSON(http.MethodGet, auth.InvitesPath, en.session(maintainer), "")
	}
	created := decode[auth.CreatedInvite](t, en.reqJSON(http.MethodPost, auth.InvitesPath, en.session(maintainer), ""))
	revoke := strings.Replace(auth.InviteRevokePath, "{uuid}", created.Invite.UUID, 1)
	if rec := en.reqJSON(http.MethodPost, revoke, en.session(maintainer), ""); rec.Code != http.StatusOK {
		t.Errorf("revoke: %d", rec.Code)
	}
	rec := en.reqJSON(http.MethodPost, auth.InvitesPath, en.session(maintainer), "")
	if e := decode[auth.AuthError](t, rec); rec.Code != http.StatusTooManyRequests || e.Code != auth.CodeRateLimited ||
		rec.Header().Get("Retry-After") == "" {
		t.Errorf("third write: %d %+v", rec.Code, e)
	}
	if rec := en.reqJSON(http.MethodPost, auth.InvitesPath, en.session(other), ""); rec.Code != http.StatusCreated {
		t.Errorf("another member's write: %d", rec.Code)
	}
}
