package api

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The invite tree, removing members and branches, and the maintainer role (D6, D7, D10; M2-Q7, M2-Q10, M2-Q12), T035's
// part of the governance feature. Every route is for maintainers only:
//
//   - GET [MemberTreePath]: the members as a tree by who invited whom. Each member is named by uuid and main name with
//     their role, join and leave dates and invite permission; never a login name or a pseudonym (M2-Q7).
//   - POST [MemberRemovePath]: removes a member who is not a maintainer (and not the caller). It sets members.left_at,
//     which ends their sessions and revokes their unused invites at once (the members hook of internal/auth); with
//     [RecommendationsHide] every live recommendation of theirs, main name and pseudonym alike (M2-Q10), is hidden as
//     store.HiddenMemberRemoved with the reason. Audit [AuditMemberRemove].
//   - POST [BranchPreviewPath] and [BranchPath]: a branch is a member and everyone they invited, directly or through
//     others (also through members who left). The preview lists the members of the branch who are in the circle:
//     maintainers apart, as skipped. Executing sends the uuids the preview listed as affected; the server computes the
//     branch again in the transaction that acts and answers 409 [CodeBranchChanged] when it differs, so that nobody is
//     acted on who was not shown. [BranchStopInvites] switches the invite permission off ([auth.AuditInvitePermission]
//     per member), [BranchRemove] removes each as above. Audit [AuditBranch] for the whole, besides the entries per
//     member.
//   - POST [MemberRolePath]: makes a member a maintainer or a maintainer a member again; not the caller themselves, and
//     never the last maintainer (M2-Q12). Audit [AuditMemberRole].
//
// Removing, executing a branch and changing a role need a fresh proof (auth.RequireFresh) and count against the write
// budget. Members who left, were removed or are deleted cannot be acted on (404).
//
// Leaving the circle (T038) shares the mechanics: [markLeft], [hideRecommendationsOf] and [countMaintainers].

// Paths of the invite tree, removal and role routes.
const (
	MemberTreePath    = "/api/osfm/members/tree"
	MemberRemovePath  = "/api/osfm/members/{uuid}/remove"
	BranchPreviewPath = "/api/osfm/members/{uuid}/branch/preview"
	BranchPath        = "/api/osfm/members/{uuid}/branch"
	MemberRolePath    = "/api/osfm/members/{uuid}/role"
)

// What happens to a removed member's recommendations ([RemoveMemberRequest].Recommendations).
const (
	// RecommendationsHide hides all of them (store.HiddenMemberRemoved): members no longer see them, maintainers do,
	// with the kind and the reason.
	RecommendationsHide = "hide"
	// RecommendationsKeep leaves them shown: main-name ones as by a member who left, pseudonym ones with the pseudonym
	// (M2-Q10).
	RecommendationsKeep = "keep"
)

// What a branch action does ([BranchRequest].Action).
const (
	BranchStopInvites = "stop_invites" // switch off the invite permission of every member of the branch
	BranchRemove      = "remove"       // remove every member of the branch
)

// Roles of a member ([TreeMember].Role, [RoleRequest].Role): the values of members.role.
const (
	RoleMember     = "member"
	RoleMaintainer = "maintainer"
)

// MaxRemovalReasonLength is the length limit of the reason of a removal, in characters.
const MaxRemovalReasonLength = 500

// Error codes of the routes of this file (409).
const (
	CodeCannotRemoveSelf       = "cannot_remove_self"       // a maintainer cannot remove themselves
	CodeCannotRemoveMaintainer = "cannot_remove_maintainer" // take the maintainer role away first
	CodeCannotDemoteSelf       = "cannot_demote_self"       // M2-Q12: nobody locks themselves out by mistake
	CodeLastMaintainer         = "last_maintainer"          // the circle would have no maintainer left
	CodeBranchChanged          = "branch_changed"           // the branch is not what the preview showed: preview again
)

// Audit log actions of the routes of this file.
const (
	// AuditMemberRemove: a maintainer removed a member (details: recommendations, reason, hidden — the number of
	// recommendations hidden — and branch, the uuid of the branch's root when removed with a branch).
	AuditMemberRemove = "members.remove"
	// AuditMemberRole: a maintainer changed a member's role (details: from, to).
	AuditMemberRole = "members.role"
	// AuditBranch: a maintainer acted on a branch (target: its root; details: action, members — the number acted on —
	// and, for a removal, recommendations).
	AuditBranch = "members.branch"
)

// TreeMember is a member in the invite tree with the members they invited. Members who left or were removed stay in
// the tree (with LeftAt), so that the branches below them can still be seen; deleted members are left out and the
// members they invited hang from the nearest member above who is not deleted.
type TreeMember struct {
	UUID        string    `json:"uuid"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joinedAt"`
	// LeftAt is when the member left or was removed; null while they are in the circle.
	LeftAt *time.Time `json:"leftAt" tstype:"string | null,required"`
	// CanInvite is the member's invite permission (maintainers may always invite whatever it says).
	CanInvite bool `json:"canInvite"`
	// InviteCooldown is true while the member's invite cooling-off period runs; always false for maintainers and for
	// members who left.
	InviteCooldown bool `json:"inviteCooldown"`
	// Invited are the members this member invited, in the order they joined.
	Invited []TreeMember `json:"invited"`
}

// MemberTree is GET [MemberTreePath]: the members nobody (still listed) invited, the first maintainer among them, each
// with the members they invited.
type MemberTree struct {
	Roots []TreeMember `json:"roots"`
}

// RemoveMemberRequest is the body of POST [MemberRemovePath].
type RemoveMemberRequest struct {
	// Recommendations is RecommendationsHide or RecommendationsKeep; required.
	Recommendations string `json:"recommendations"`
	// Reason says why, for the audit log and, with RecommendationsHide, for the maintainers who see the hidden
	// recommendations; required, at most MaxRemovalReasonLength characters.
	Reason string `json:"reason"`
}

// RemovedMember is the response of POST [MemberRemovePath].
type RemovedMember struct {
	UUID string `json:"uuid"`
	// HiddenRecommendations is the number of the member's recommendations hidden now (0 with RecommendationsKeep;
	// recommendations hidden before are left as they were).
	HiddenRecommendations int `json:"hiddenRecommendations"`
}

// BranchPreviewRequest is the body of POST [BranchPreviewPath].
type BranchPreviewRequest struct {
	// Action is BranchStopInvites or BranchRemove; required.
	Action string `json:"action"`
}

// BranchMember is a member of a branch as the preview lists them.
type BranchMember struct {
	UUID        string `json:"uuid"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	// Depth is the number of invitations between the branch's root and the member: 0 for the root.
	Depth int `json:"depth"`
}

// BranchPreview is the response of POST [BranchPreviewPath] and of POST [BranchPath]: the members of the branch who
// are in the circle, in tree order. Affected are the ones the action applies to; Skipped are the maintainers, listed
// but never acted on (D10).
type BranchPreview struct {
	Action   string         `json:"action"`
	Affected []BranchMember `json:"affected"`
	Skipped  []BranchMember `json:"skipped"`
}

// BranchRequest is the body of POST [BranchPath].
type BranchRequest struct {
	// Action is BranchStopInvites or BranchRemove; required.
	Action string `json:"action"`
	// Affected are the uuids of [BranchPreview].Affected as the preview listed them (in any order); required, an empty
	// list when the preview listed nobody. A different set is answered with 409 CodeBranchChanged.
	Affected []string `json:"affected"`
	// Recommendations and Reason are as in [RemoveMemberRequest], for BranchRemove only (and then required).
	Recommendations string `json:"recommendations,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// RoleRequest is the body of POST [MemberRolePath].
type RoleRequest struct {
	// Role is RoleMaintainer or RoleMember; required.
	Role string `json:"role"`
}

// MemberRole is the response of POST [MemberRolePath]: the member's role after the change.
type MemberRole struct {
	UUID string `json:"uuid"`
	Role string `json:"role"`
}

// registerMembers registers the routes of this file; registerGovernance calls it.
func registerMembers(se *core.ServeEvent, s *server) {
	se.Router.GET(MemberTreePath, handleMemberTree).Bind(MaintainersOnly())
	se.Router.POST(MemberRemovePath, handleRemoveMember).Bind(MaintainersOnly(), auth.RequireFresh(), s.WriteBudget())
	se.Router.POST(BranchPreviewPath, handleBranchPreview).Bind(MaintainersOnly())
	se.Router.POST(BranchPath, handleBranch).Bind(MaintainersOnly(), auth.RequireFresh(), s.WriteBudget())
	se.Router.POST(MemberRolePath, handleMemberRole).Bind(MaintainersOnly(), auth.RequireFresh(), s.WriteBudget())
}

// errConflict carries a 409 answer out of a transaction (which it rolls back).
type errConflict struct{ code string }

func (e *errConflict) Error() string { return "api: conflict: " + e.code }

// answerConflict answers err: 403 for errNotMaintainer, 404 for sql.ErrNoRows, 409 with the code of an *errConflict, 500 otherwise.
func answerConflict(e *core.RequestEvent, err error) error {
	var conflict *errConflict
	switch {
	case errors.Is(err, errNotMaintainer):
		return forbidden(e)
	case errors.Is(err, sql.ErrNoRows):
		return notFound(e)
	case errors.As(err, &conflict):
		return fail(e, http.StatusConflict, conflict.code, "This cannot be done now.")
	default:
		return e.InternalServerError("", err)
	}
}

// memberGraph is every member record (deleted ones too) with who invited whom.
type memberGraph struct {
	all      []*core.Record            // in the order they joined
	byID     map[string]*core.Record   // by internal ID
	children map[string][]*core.Record // by the inviter's internal ID, in the order they joined
}

func loadMemberGraph(app core.App) (*memberGraph, error) {
	all, err := store.Query(app, store.Members).IncludeDeleted().OrderBy("joined_at ASC", "uuid ASC").All()
	if err != nil {
		return nil, err
	}
	g := &memberGraph{all: all, byID: make(map[string]*core.Record, len(all)), children: map[string][]*core.Record{}}
	for _, m := range all {
		g.byID[m.Id] = m
	}
	for _, m := range all {
		if inviter := m.GetString("invited_by"); inviter != "" && inviter != m.Id && g.byID[inviter] != nil {
			g.children[inviter] = append(g.children[inviter], m)
		}
	}
	return g, nil
}

// shownParent returns the member m hangs from in the tree: their inviter, or the nearest member above who is not
// deleted; nil for a root.
func (g *memberGraph) shownParent(m *core.Record) *core.Record {
	p := g.byID[m.GetString("invited_by")]
	for steps := 0; p != nil && store.IsDeleted(p) && steps < len(g.all); steps++ {
		p = g.byID[p.GetString("invited_by")]
	}
	if p == nil || p == m || store.IsDeleted(p) {
		return nil
	}
	return p
}

func handleMemberTree(e *core.RequestEvent) error {
	tree, err := LoadMemberTree(e.App)
	if err != nil {
		return e.InternalServerError("", err)
	}
	return respond(e, http.StatusOK, tree)
}

// LoadMemberTree returns the invite tree of every member who is not deleted.
func LoadMemberTree(app core.App) (MemberTree, error) {
	g, err := loadMemberGraph(app)
	if err != nil {
		return MemberTree{}, err
	}
	circle, err := settings.LoadCircle(app)
	if err != nil {
		return MemberTree{}, err
	}
	t := time.Now().UTC()
	shown := map[string][]*core.Record{}
	var roots []*core.Record
	for _, m := range g.all {
		if store.IsDeleted(m) {
			continue
		}
		if p := g.shownParent(m); p != nil {
			shown[p.Id] = append(shown[p.Id], m)
		} else {
			roots = append(roots, m)
		}
	}
	visited := map[string]bool{}
	var build func(m *core.Record) TreeMember
	build = func(m *core.Record) TreeMember {
		visited[m.Id] = true
		node := treeMember(m, circle.InviteCooldownDays, t)
		for _, c := range shown[m.Id] {
			if !visited[c.Id] {
				node.Invited = append(node.Invited, build(c))
			}
		}
		return node
	}
	out := MemberTree{Roots: []TreeMember{}}
	for _, m := range roots {
		out.Roots = append(out.Roots, build(m))
	}
	// Members in a cycle of invitations (never written by OSFM) would have no root: list them as roots.
	for _, m := range g.all {
		if !store.IsDeleted(m) && !visited[m.Id] {
			out.Roots = append(out.Roots, build(m))
		}
	}
	return out, nil
}

func treeMember(m *core.Record, cooldownDays int, t time.Time) TreeMember {
	joined := m.GetDateTime("joined_at")
	if joined.IsZero() {
		joined = m.GetDateTime(store.FieldCreated)
	}
	node := TreeMember{
		UUID:        m.GetString(store.FieldUUID),
		DisplayName: m.GetString("display_name"),
		Role:        m.GetString("role"),
		JoinedAt:    joined.Time().UTC(),
		LeftAt:      optionalUTC(m.GetDateTime("left_at").Time()),
		CanInvite:   m.GetBool("can_invite"),
		Invited:     []TreeMember{},
	}
	if auth.CanSignIn(m) && !auth.IsMaintainer(m) {
		node.InviteCooldown = t.Before(domain.CooldownEnd(auth.InviterOf(m), cooldownDays))
	}
	return node
}

// branchOf returns the members of the branch of root who are in the circle, in tree order: affected (not
// maintainers) and skipped (maintainers). The branch follows invitations through members who left or were deleted.
func (g *memberGraph) branchOf(root *core.Record) (affected, skipped []*core.Record, depth map[string]int) {
	depth = map[string]int{}
	var walk func(m *core.Record, d int)
	walk = func(m *core.Record, d int) {
		if _, seen := depth[m.Id]; seen {
			return
		}
		depth[m.Id] = d
		if auth.CanSignIn(m) {
			if auth.IsMaintainer(m) {
				skipped = append(skipped, m)
			} else {
				affected = append(affected, m)
			}
		}
		for _, c := range g.children[m.Id] {
			walk(c, d+1)
		}
	}
	walk(root, 0)
	return affected, skipped, depth
}

// loadBranch finds the member with uuid id (not deleted; sql.ErrNoRows otherwise) and returns their branch.
func loadBranch(app core.App, id, action string) (BranchPreview, []*core.Record, error) {
	root, err := store.FindByUUID(app, store.Members, id)
	if err != nil {
		return BranchPreview{}, nil, err
	}
	g, err := loadMemberGraph(app)
	if err != nil {
		return BranchPreview{}, nil, err
	}
	affected, skipped, depth := g.branchOf(g.byID[root.Id])
	list := func(ms []*core.Record) []BranchMember {
		out := make([]BranchMember, 0, len(ms))
		for _, m := range ms {
			out = append(out, BranchMember{UUID: m.GetString(store.FieldUUID), DisplayName: m.GetString("display_name"),
				Role: m.GetString("role"), Depth: depth[m.Id]})
		}
		return out
	}
	return BranchPreview{Action: action, Affected: list(affected), Skipped: list(skipped)}, affected, nil
}

func validBranchAction(action string) bool {
	return action == BranchStopInvites || action == BranchRemove
}

func handleBranchPreview(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	var req BranchPreviewRequest
	if err := readJSON(e, &req, DefaultMaxBody); err != nil {
		return requestError(e, err)
	}
	if !validBranchAction(req.Action) {
		return badRequest(e)
	}
	preview, _, err := loadBranch(e.App, id, req.Action)
	if err != nil {
		return answerConflict(e, err)
	}
	return respond(e, http.StatusOK, preview)
}

// removal is a checked removal request: whether to hide the recommendations, and the reason.
type removal struct {
	hide   bool
	reason string
}

// parseRemoval checks the recommendations choice and the reason of a removal.
func parseRemoval(recommendations, reason string) (removal, bool) {
	reason = strings.TrimSpace(reason)
	if (recommendations != RecommendationsHide && recommendations != RecommendationsKeep) || reason == "" ||
		utf8.RuneCountInString(reason) > MaxRemovalReasonLength || !utf8.ValidString(reason) {
		return removal{}, false
	}
	return removal{hide: recommendations == RecommendationsHide, reason: reason}, true
}

func (r removal) choice() string {
	if r.hide {
		return RecommendationsHide
	}
	return RecommendationsKeep
}

func handleBranch(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	var req BranchRequest
	if err := readJSON(e, &req, DefaultMaxBody); err != nil {
		return requestError(e, err)
	}
	if !validBranchAction(req.Action) || req.Affected == nil {
		return badRequest(e)
	}
	var how removal
	if req.Action == BranchRemove {
		if how, ok = parseRemoval(req.Recommendations, req.Reason); !ok {
			return badRequest(e)
		}
	}
	sent := slices.Clone(req.Affected)
	slices.Sort(sent)

	actor := auth.CurrentMember(e)
	var preview BranchPreview
	err := e.App.RunInTransaction(func(tx core.App) error {
		if err := checkStillMaintainer(tx, actor); err != nil {
			return err
		}
		var affected []*core.Record
		var err error
		if preview, affected, err = loadBranch(tx, id, req.Action); err != nil {
			return err
		}
		now := make([]string, 0, len(affected))
		for _, m := range affected {
			now = append(now, m.GetString(store.FieldUUID))
		}
		slices.Sort(now)
		if !slices.Equal(now, sent) {
			return &errConflict{CodeBranchChanged}
		}
		for _, m := range affected {
			switch req.Action {
			case BranchStopInvites:
				if !m.GetBool("can_invite") {
					continue
				}
				m.Set("can_invite", false)
				if err := tx.Save(m); err != nil {
					return err
				}
				if err := store.WriteAudit(tx, actor, auth.AuditInvitePermission, store.Members,
					m.GetString(store.FieldUUID), map[string]any{"canInvite": false, "branch": id}); err != nil {
					return err
				}
			case BranchRemove:
				if _, err := removeMember(tx, actor, m, how, id); err != nil {
					return err
				}
			}
		}
		details := map[string]any{"action": req.Action, "members": len(affected)}
		if req.Action == BranchRemove {
			details["recommendations"] = how.choice()
		}
		return store.WriteAudit(tx, actor, AuditBranch, store.Members, id, details)
	})
	if err != nil {
		return answerConflict(e, err)
	}
	return respond(e, http.StatusOK, preview)
}

// checkStillMaintainer re-reads the caller in the transaction: a maintainer whose role was taken away since the
// request started (by another maintainer at the same moment) may not act any more. It returns errNotMaintainer then.
func checkStillMaintainer(tx core.App, actor *core.Record) error {
	m, err := tx.FindRecordById(store.Members, actor.Id)
	if err != nil || !auth.IsMaintainer(m) {
		return errNotMaintainer
	}
	return nil
}

var errNotMaintainer = errors.New("api: the caller is no longer a maintainer")

func handleRemoveMember(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	var req RemoveMemberRequest
	if err := readJSON(e, &req, DefaultMaxBody); err != nil {
		return requestError(e, err)
	}
	how, ok := parseRemoval(req.Recommendations, req.Reason)
	if !ok {
		return badRequest(e)
	}
	actor := auth.CurrentMember(e)
	var hidden int
	err := e.App.RunInTransaction(func(tx core.App) error {
		if err := checkStillMaintainer(tx, actor); err != nil {
			return err
		}
		m, err := store.FindByUUID(tx, store.Members, id)
		if err != nil {
			return err
		}
		switch {
		case !auth.CanSignIn(m):
			return sql.ErrNoRows // left or removed already
		case m.Id == actor.Id:
			return &errConflict{CodeCannotRemoveSelf}
		case auth.IsMaintainer(m):
			return &errConflict{CodeCannotRemoveMaintainer}
		}
		hidden, err = removeMember(tx, actor, m, how, "")
		return err
	})
	if err != nil {
		return answerConflict(e, err)
	}
	return respond(e, http.StatusOK, RemovedMember{UUID: id, HiddenRecommendations: hidden})
}

// removeMember removes m, a member in the circle who is not a maintainer, on behalf of actor, in the transaction tx:
// marks them as left (their sessions end and their unused invites are revoked by the members hook), hides their
// recommendations when how says so, and writes the audit entry. branch is the uuid of the branch's root when the
// removal is part of a branch action, else "". It returns the number of recommendations hidden.
func removeMember(tx core.App, actor, m *core.Record, how removal, branch string) (int, error) {
	hidden := 0
	if how.hide {
		var err error
		if hidden, err = hideRecommendationsOf(tx, actor, m, store.HiddenMemberRemoved, how.reason); err != nil {
			return 0, err
		}
	}
	if err := markLeft(tx, m); err != nil {
		return 0, err
	}
	details := map[string]any{"recommendations": how.choice(), "reason": how.reason, "hidden": hidden}
	if branch != "" {
		details["branch"] = branch
	}
	return hidden, store.WriteAudit(tx, actor, AuditMemberRemove, store.Members, m.GetString(store.FieldUUID), details)
}

// markLeft sets members.left_at of m to now and saves it, through tx. The members hook of internal/auth then deletes
// all of m's sessions and revokes their unused invites in the same transaction (D7, D9); auth.CanSignIn is false from
// now on, so m cannot sign in again and their next request is 401.
func markLeft(tx core.App, m *core.Record) error {
	m.Set("left_at", time.Now().UTC())
	return tx.Save(m)
}

// hideRecommendationsOf hides every live recommendation of member that is not hidden already (main-name and
// pseudonym ones alike, M2-Q10) as kind (store.HiddenMemberRemoved), by actor, with reason, through tx. Recommendations
// hidden before keep their kind and reason. It returns the number hidden.
func hideRecommendationsOf(tx core.App, actor, member *core.Record, kind, reason string) (int, error) {
	recs, err := store.Query(tx, store.Recommendations).Where(dbx.HashExp{"author": member.Id}).All()
	if err != nil {
		return 0, err
	}
	t := time.Now().UTC()
	n := 0
	for _, r := range recs {
		if store.IsHidden(r) {
			continue
		}
		r.Set("hidden", true)
		r.Set("hidden_kind", kind)
		r.Set("hidden_at", t)
		r.Set("hidden_reason", reason)
		if actor != nil {
			r.Set("hidden_by", actor.Id)
		}
		if err := tx.Save(r); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// countMaintainers returns the number of maintainers in the circle: role maintainer, not left, not deleted.
func countMaintainers(tx core.App) (int, error) {
	var n int
	err := tx.DB().Select("COUNT(*)").From(store.Members).
		Where(dbx.HashExp{"role": auth.RoleMaintainer, "left_at": "", store.FieldDeletedAt: ""}).Row(&n)
	return n, err
}

func handleMemberRole(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	var req RoleRequest
	if err := readJSON(e, &req, DefaultMaxBody); err != nil {
		return requestError(e, err)
	}
	if req.Role != RoleMember && req.Role != RoleMaintainer {
		return badRequest(e)
	}
	actor := auth.CurrentMember(e)
	err := e.App.RunInTransaction(func(tx core.App) error {
		if err := checkStillMaintainer(tx, actor); err != nil {
			return err
		}
		m, err := store.FindByUUID(tx, store.Members, id)
		if err != nil {
			return err
		}
		if !auth.CanSignIn(m) {
			return sql.ErrNoRows
		}
		from := m.GetString("role")
		if from == req.Role {
			return nil
		}
		if m.Id == actor.Id {
			return &errConflict{CodeCannotDemoteSelf}
		}
		m.Set("role", req.Role)
		if err := tx.Save(m); err != nil {
			return err
		}
		if req.Role == RoleMember {
			n, err := countMaintainers(tx)
			if err != nil {
				return err
			}
			if n == 0 {
				return &errConflict{CodeLastMaintainer}
			}
		}
		return store.WriteAudit(tx, actor, AuditMemberRole, store.Members, id, map[string]any{"from": from, "to": req.Role})
	})
	if err != nil {
		return answerConflict(e, err)
	}
	return respond(e, http.StatusOK, MemberRole{UUID: id, Role: req.Role})
}
