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
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Hiding, reports and their review (C6, D11, H6; M2-Q7, M2-Q8, M2-Q9), T036:
//
//   - POST [ReportPath]: a member reports a recommendation they can see (not their own: 400 [CodeOwnRecommendation])
//     with a reason code (store.ReportReasons) and an optional note (required for store.ReasonOther); once per member
//     and recommendation, whatever became of the first report (409 [CodeAlreadyReported]). In the same transaction the
//     open reports of the recommendation are counted; when they reach the instance's report threshold and the
//     recommendation is not hidden yet, it is hidden as store.HiddenByReports, its author gets a notice (without the
//     number of reports or who reported) and the audit log [AuditAutoHide]. Reports have a per-member budget of
//     their own.
//   - POST [HidePath] (maintainers): hides a recommendation as store.HiddenByMaintainer with a reason (required);
//     notice, audit [AuditHide]. POST [UnhidePath] (maintainers): shows it again ([restore]) and resolves its open
//     reports as store.ResolutionRestored; notice, audit [AuditUnhide].
//   - GET [ReviewQueuePath] (maintainers): every recommendation with open reports or hidden, grouped by
//     recommendation: its projection with its place, the number of open reports, the number per reason and the notes.
//     Never who reported (privacy invariant 5): reporters are not in any response, for anyone.
//   - POST [ResolvePath] (maintainers): resolves the open reports of a recommendation under review: restored (shown
//     again, [restore]), kept_hidden (stays hidden), or deleted — the violation delete (H6), the only way a maintainer
//     deletes someone's recommendation: a reason is required, the recommendation is erased and soft-deleted as every
//     deletion is (store.DeleteRecommendation, M2-Q9), audit [AuditViolationDelete], notice to the author. Audit
//     [AuditResolve] for the other two.
//   - POST [RevealAuthorPath] (maintainers, fresh proof): names the member behind a recommendation under review, for
//     a pseudonym (M2-Q7); every reveal is audited ([AuditRevealPseudonym]).
//
// A member removed from the circle with their recommendations hidden (store.HiddenMemberRemoved, T035) may have had
// recommendations hidden earlier by a maintainer or by reports, which kept that kind. Restoring such a recommendation
// (unhide, or resolving its reports as restored) must not show it again against the removal's choice: it stays hidden
// as store.HiddenMemberRemoved with the removal's reason ([restore]). Only an explicit unhide of a recommendation
// hidden as store.HiddenMemberRemoved shows it: a maintainer's decision about that one recommendation. Resolving the
// reports of such a recommendation as restored resolves them and leaves it hidden.

// Paths of this feature.
const (
	ReportPath       = "/api/osfm/recommendations/{uuid}/report"
	HidePath         = "/api/osfm/recommendations/{uuid}/hide"
	UnhidePath       = "/api/osfm/recommendations/{uuid}/unhide"
	ReviewQueuePath  = "/api/osfm/reports"
	ResolvePath      = "/api/osfm/reports/{uuid}/resolve"
	RevealAuthorPath = "/api/osfm/reports/{uuid}/author"
)

// Reason codes of a report ([ReportRequest].Reason): the values of reports.reason (store.ReportReasons, M2-Q8).
const (
	ReportReasonAdvertising      = "advertising"
	ReportReasonAbuse            = "abuse"
	ReportReasonPoliticalIllegal = "political_illegal"
	ReportReasonUnverifiedClaim  = "unverified_claim"
	ReportReasonFalseOrDuplicate = "false_or_duplicate"
	ReportReasonOther            = "other" // needs a note
)

// How a maintainer resolves the reports of a recommendation ([ResolveRequest].Resolution): the values of
// reports.resolution (store.Resolutions).
const (
	ResolutionRestored   = "restored"
	ResolutionKeptHidden = "kept_hidden"
	ResolutionDeleted    = "deleted"
)

// Length limits, in characters.
const (
	// MaxReportNoteLength is the limit of a report's note (store.MaxReportNoteLength).
	MaxReportNoteLength = 500
	// MaxModerationReasonLength is the limit of the reason of a hide, an unhide or a resolution (hidden_reason).
	MaxModerationReasonLength = 500
)

// maxModerationBody is the body limit of the routes of this feature.
const maxModerationBody = 8 << 10

// reportRate is every member's budget of reports: ten at once, then one every six minutes.
var reportRate = ratelimit.Rate{Burst: 10, Every: 6 * time.Minute}

// Error codes of this feature, besides invalid_request, not_found and forbidden.
const (
	CodeOwnRecommendation = "own_recommendation" // 400: nobody reports their own recommendation
	CodeAlreadyReported   = "already_reported"   // 409: the member reported this recommendation before
	CodeAlreadyHidden     = "already_hidden"     // 409: hide of a recommendation that is hidden
	CodeNotHidden         = "not_hidden"         // 409: unhide or kept_hidden of a recommendation that is shown
	CodeNotUnderReview    = "not_under_review"   // 409: no open reports and not hidden: nothing to review
)

// Audit log actions of this feature. Details never name a reporter.
const (
	// AuditAutoHide: the open reports reached the threshold and hid the recommendation (no actor; details: threshold).
	AuditAutoHide = "recommendations.auto_hide"
	// AuditHide: a maintainer hid a recommendation (details: reason).
	AuditHide = "recommendations.hide"
	// AuditUnhide: a maintainer unhid a recommendation (details: kind — the hidden kind before —, reason, reports —
	// the open reports resolved as restored — and shown, false when it stays hidden because its author was removed).
	AuditUnhide = "recommendations.unhide"
	// AuditResolve: a maintainer resolved the reports of a recommendation as restored or kept_hidden (details:
	// resolution, reason, reports, shown).
	AuditResolve = "reports.resolve"
	// AuditViolationDelete: a maintainer deleted a recommendation as a violation (details: reason, reports, kind).
	AuditViolationDelete = "recommendations.violation_delete"
	// AuditRevealPseudonym: a maintainer looked up the member behind a recommendation under review (target: the
	// member; details: recommendation, its uuid). Written for every reveal (M2-Q7).
	AuditRevealPseudonym = "members.reveal_pseudonym"
)

// ReportRequest is the body of POST [ReportPath].
type ReportRequest struct {
	// Reason is one of the ReportReason* codes; required.
	Reason string `json:"reason"`
	// Note says more, at most MaxReportNoteLength characters; required with ReportReasonOther.
	Note string `json:"note,omitempty"`
}

// ReportReceipt is the response of POST [ReportPath] (201). It says nothing about other reports.
type ReportReceipt struct {
	Recommendation string `json:"recommendation"`
}

// HideRequest is the body of POST [HidePath].
type HideRequest struct {
	// Reason is shown to the author and to maintainers with the hidden recommendation; required, at most
	// MaxModerationReasonLength characters.
	Reason string `json:"reason"`
}

// UnhideRequest is the body of POST [UnhidePath]; the body may also be empty.
type UnhideRequest struct {
	// Reason is for the audit log; optional.
	Reason string `json:"reason,omitempty"`
}

// ResolveRequest is the body of POST [ResolvePath].
type ResolveRequest struct {
	// Resolution is ResolutionRestored, ResolutionKeptHidden or ResolutionDeleted; required.
	Resolution string `json:"resolution"`
	// Reason is for the audit log, at most MaxModerationReasonLength characters; required with ResolutionDeleted.
	Reason string `json:"reason,omitempty"`
}

// ResolveResult is the response of POST [ResolvePath].
type ResolveResult struct {
	Resolution string `json:"resolution"`
	// Resolved is the number of open reports resolved now.
	Resolved int `json:"resolved"`
	// Recommendation is the recommendation as it is now; null after ResolutionDeleted. After ResolutionRestored it
	// can still be hidden (as member_removed: its author was removed with their recommendations hidden).
	Recommendation *RecommendationView `json:"recommendation" tstype:"RecommendationView | null,required"`
}

// ReviewQueue is GET [ReviewQueuePath]: the recommendations under review, those with open reports first (the most
// recently reported first), then the hidden ones without open reports.
type ReviewQueue struct {
	Items []ReviewItem `json:"items"`
}

// ReviewItem is a recommendation under review. It names no reporter.
type ReviewItem struct {
	// Recommendation is the maintainer's projection, with hidden set when it is hidden.
	Recommendation RecommendationView `json:"recommendation"`
	Place          ReviewPlace        `json:"place"`
	// OpenReports is the number of reports nobody resolved yet; only they count towards the threshold.
	OpenReports int `json:"openReports"`
	// Reasons are the number of open reports per reason, in the order of the reason codes, without zeros.
	Reasons []ReasonCount `json:"reasons"`
	// Notes are the notes of the open reports, newest first.
	Notes []ReportNote `json:"notes"`
	// LastReportedAt is when the newest open report was made; null without open reports.
	LastReportedAt *time.Time `json:"lastReportedAt" tstype:"string | null,required"`
}

// ReviewPlace is the place of a recommendation under review.
type ReviewPlace struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// ReasonCount is the number of open reports with one reason.
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// ReportNote is the note of an open report, with its reason and when it was made; never who wrote it.
type ReportNote struct {
	Reason     string    `json:"reason"`
	Note       string    `json:"note"`
	ReportedAt time.Time `json:"reportedAt"`
}

// RevealedAuthor is the response of POST [RevealAuthorPath]: the member who wrote the recommendation.
type RevealedAuthor struct {
	UUID string `json:"uuid"`
	// DisplayName is the member's main name.
	DisplayName string `json:"displayName"`
	// InCircle is false when the member left or was removed.
	InCircle bool `json:"inCircle"`
}

// registerReports registers the routes of this feature.
func registerReports(se *core.ServeEvent, s *server) {
	se.Router.POST(ReportPath, handleReport).Bind(s.MemberBudget("osfmReportBudget", reportRate))
	se.Router.POST(HidePath, handleHide).Bind(MaintainersOnly(), s.WriteBudget())
	se.Router.POST(UnhidePath, handleUnhide).Bind(MaintainersOnly(), s.WriteBudget())
	se.Router.GET(ReviewQueuePath, handleReviewQueue).Bind(MaintainersOnly())
	se.Router.POST(ResolvePath, handleResolve).Bind(MaintainersOnly(), s.WriteBudget())
	se.Router.POST(RevealAuthorPath, handleRevealAuthor).Bind(MaintainersOnly(), auth.RequireFresh(), s.WriteBudget())
}

// errRefused carries a 400 answer with a code out of a transaction (which it rolls back).
type errRefused struct{ code string }

func (e *errRefused) Error() string { return "api: refused: " + e.code }

// answerModeration answers err: 400 with the code of an *errRefused, else as [answerConflict].
func answerModeration(e *core.RequestEvent, err error) error {
	var refused *errRefused
	if errors.As(err, &refused) {
		return fail(e, http.StatusBadRequest, refused.code, "This cannot be done.")
	}
	return answerConflict(e, err)
}

// findVisible returns the recommendation with uuid id that v may see, or an error wrapping sql.ErrNoRows.
func findVisible(app core.App, v domain.Viewer, id string) (*core.Record, error) {
	return store.VisibleRecommendations(app, v).Where(dbx.HashExp{store.Recommendations + "." + store.FieldUUID: id}).One()
}

// cleanText trims s and checks that it is valid UTF-8 of at most limit characters; required makes empty invalid.
func cleanText(s string, limit int, required bool) (string, bool) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > limit || (required && s == "") {
		return "", false
	}
	return s, true
}

// openReportsOf returns the open reports of the recommendation record rec, newest first.
func openReportsOf(app core.App, rec *core.Record) ([]*core.Record, error) {
	return store.Query(app, store.Reports).Where(dbx.HashExp{"recommendation": rec.Id, "resolution": ""}).
		OrderBy("created DESC", "id DESC").All()
}

// resolveOpenReports resolves every open report of rec as resolution, by actor, and returns how many there were.
func resolveOpenReports(tx core.App, actor, rec *core.Record, resolution string) (int, error) {
	open, err := openReportsOf(tx, rec)
	if err != nil {
		return 0, err
	}
	t := types.NowDateTime()
	for _, r := range open {
		r.Set("resolution", resolution)
		r.Set("resolved_at", t)
		r.Set("resolved_by", actor.Id)
		if err := tx.Save(r); err != nil {
			return 0, err
		}
	}
	return len(open), nil
}

// setHidden hides rec as kind, by actor (nil when hidden automatically), with reason, through tx.
func setHidden(tx core.App, rec *core.Record, kind string, actorID, reason string) error {
	rec.Set("hidden", true)
	rec.Set("hidden_kind", kind)
	rec.Set("hidden_at", types.NowDateTime())
	rec.Set("hidden_by", actorID)
	rec.Set("hidden_reason", reason)
	return tx.Save(rec)
}

// restore shows the hidden recommendation rec again, through tx, and reports whether it is shown now. A recommendation
// whose author was removed from the circle with their recommendations hidden stays hidden instead, as
// store.HiddenMemberRemoved with the removal's reason (see the feature's documentation). A recommendation hidden as
// store.HiddenMemberRemoved is shown only when explicit is true (an unhide of it); otherwise it is left as it is.
func restore(tx core.App, rec *core.Record, explicit bool) (bool, error) {
	if store.IsHidden(rec) && rec.GetString("hidden_kind") == store.HiddenMemberRemoved {
		if !explicit {
			return false, nil
		}
	} else {
		removal, err := removalWithHide(tx, rec.GetString("author"))
		if err != nil {
			return false, err
		}
		if removal != nil {
			return false, setHidden(tx, rec, store.HiddenMemberRemoved, removal.actor, removal.reason)
		}
	}
	rec.Set("hidden", false)
	rec.Set("hidden_kind", "")
	rec.Set("hidden_at", "")
	rec.Set("hidden_by", "")
	rec.Set("hidden_reason", "")
	return true, tx.Save(rec)
}

// removedWithHide is how a member was removed with their recommendations hidden: by whom and why.
type removedWithHide struct{ actor, reason string }

// removalWithHide returns how the member with record ID memberID was removed when a maintainer removed them with their
// recommendations hidden (the audit entry [AuditMemberRemove] says so); nil for a member in the circle, one who left
// by themselves, or one removed with their recommendations kept.
func removalWithHide(tx core.App, memberID string) (*removedWithHide, error) {
	m, err := store.Query(tx, store.Members).IncludeDeleted().Where(dbx.HashExp{store.Members + ".id": memberID}).One()
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if m.GetDateTime("left_at").IsZero() {
		return nil, nil
	}
	entry, err := store.Query(tx, store.AuditLog).
		Where(dbx.HashExp{"action": AuditMemberRemove, "target_type": store.Members,
			"target_uuid": m.GetString(store.FieldUUID)}).
		OrderBy("created DESC", "id DESC").One()
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var details struct {
		Recommendations string `json:"recommendations"`
		Reason          string `json:"reason"`
	}
	if err := entry.UnmarshalJSONField("details", &details); err != nil {
		return nil, err
	}
	if details.Recommendations != RecommendationsHide {
		return nil, nil
	}
	return &removedWithHide{actor: entry.GetString("actor"), reason: details.Reason}, nil
}

// notifyAuthor writes a notice of kind about rec for its author, through tx.
func notifyAuthor(tx core.App, rec *core.Record, kind string) error {
	return domain.Notify(store.NoticesIn(tx), rec.GetString("author"), kind, rec.GetString(store.FieldUUID))
}

// viewOf projects rec for v after a change; nil when v may not see it any more.
func viewOf(app core.App, v domain.Viewer, rec *core.Record) (*RecommendationView, error) {
	views, err := RecommendationViews(app, v, []*core.Record{rec})
	if err != nil || len(views) == 0 {
		return nil, err
	}
	return &views[0], nil
}

func handleReport(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	var req ReportRequest
	if err := readJSON(e, &req, maxModerationBody); err != nil {
		return requestError(e, err)
	}
	note, ok := cleanText(req.Note, MaxReportNoteLength, req.Reason == ReportReasonOther)
	if !ok || !slices.Contains(store.ReportReasons, req.Reason) {
		return badRequest(e)
	}
	v := ViewerOf(e)
	reporter := auth.CurrentMember(e)
	err := e.App.RunInTransaction(func(tx core.App) error {
		rec, err := findVisible(tx, v, id)
		if err != nil {
			return err
		}
		if v.IsAuthor(rec.GetString("author")) {
			return &errRefused{CodeOwnRecommendation}
		}
		n, err := store.Query(tx, store.Reports).Where(dbx.HashExp{"recommendation": rec.Id, "reporter": reporter.Id}).Count()
		if err != nil {
			return err
		}
		if n > 0 {
			return &errConflict{CodeAlreadyReported}
		}
		report, err := newRecord(tx, store.Reports)
		if err != nil {
			return err
		}
		report.Set("recommendation", rec.Id)
		report.Set("reporter", reporter.Id)
		report.Set("reason", req.Reason)
		report.Set("note", note)
		if err := tx.Save(report); err != nil {
			return err
		}
		if store.IsHidden(rec) {
			return nil
		}
		open, err := store.Query(tx, store.Reports).Where(dbx.HashExp{"recommendation": rec.Id, "resolution": ""}).Count()
		if err != nil {
			return err
		}
		circle, err := settings.LoadCircle(tx)
		if err != nil {
			return err
		}
		if open < int64(circle.ReportThreshold) {
			return nil
		}
		if err := setHidden(tx, rec, store.HiddenByReports, "", ""); err != nil {
			return err
		}
		if err := notifyAuthor(tx, rec, domain.NoticeHiddenByReports); err != nil {
			return err
		}
		return store.WriteAudit(tx, nil, AuditAutoHide, store.Recommendations, id,
			map[string]any{"threshold": circle.ReportThreshold})
	})
	if err != nil {
		return answerModeration(e, err)
	}
	return respond(e, http.StatusCreated, ReportReceipt{Recommendation: id})
}

func handleHide(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	var req HideRequest
	if err := readJSON(e, &req, maxModerationBody); err != nil {
		return requestError(e, err)
	}
	reason, ok := cleanText(req.Reason, MaxModerationReasonLength, true)
	if !ok {
		return badRequest(e)
	}
	v := ViewerOf(e)
	actor := auth.CurrentMember(e)
	var rec *core.Record
	err := e.App.RunInTransaction(func(tx core.App) error {
		if err := checkStillMaintainer(tx, actor); err != nil {
			return err
		}
		var err error
		if rec, err = findVisible(tx, v, id); err != nil {
			return err
		}
		if store.IsHidden(rec) {
			return &errConflict{CodeAlreadyHidden}
		}
		if err := setHidden(tx, rec, store.HiddenByMaintainer, actor.Id, reason); err != nil {
			return err
		}
		if err := notifyAuthor(tx, rec, domain.NoticeHiddenByMaintainer); err != nil {
			return err
		}
		return store.WriteAudit(tx, actor, AuditHide, store.Recommendations, id, map[string]any{"reason": reason})
	})
	if err != nil {
		return answerModeration(e, err)
	}
	return respondView(e, v, rec)
}

// respondView answers the maintainer's projection of rec after a change.
func respondView(e *core.RequestEvent, v domain.Viewer, rec *core.Record) error {
	view, err := viewOf(e.App, v, rec)
	if err != nil {
		return e.InternalServerError("", err)
	}
	if view == nil {
		return notFound(e)
	}
	return respond(e, http.StatusOK, view)
}

func handleUnhide(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	var req UnhideRequest
	if err := readJSON(e, &req, maxModerationBody); err != nil && !errors.Is(err, errEmptyBody) {
		return requestError(e, err)
	}
	reason, ok := cleanText(req.Reason, MaxModerationReasonLength, false)
	if !ok {
		return badRequest(e)
	}
	v := ViewerOf(e)
	actor := auth.CurrentMember(e)
	var rec *core.Record
	err := e.App.RunInTransaction(func(tx core.App) error {
		if err := checkStillMaintainer(tx, actor); err != nil {
			return err
		}
		var err error
		if rec, err = findVisible(tx, v, id); err != nil {
			return err
		}
		if !store.IsHidden(rec) {
			return &errConflict{CodeNotHidden}
		}
		kind := hiddenKindOf(rec)
		shown, err := restore(tx, rec, true)
		if err != nil {
			return err
		}
		n, err := resolveOpenReports(tx, actor, rec, store.ResolutionRestored)
		if err != nil {
			return err
		}
		if shown {
			if err := notifyAuthor(tx, rec, domain.NoticeRestored); err != nil {
				return err
			}
		}
		return store.WriteAudit(tx, actor, AuditUnhide, store.Recommendations, id,
			map[string]any{"kind": kind, "reason": reason, "reports": n, "shown": shown})
	})
	if err != nil {
		return answerModeration(e, err)
	}
	return respondView(e, v, rec)
}

// hiddenKindOf returns the hidden kind of the hidden recommendation rec (store.HiddenByMaintainer for a row hidden
// without one, as the projection shows it).
func hiddenKindOf(rec *core.Record) string {
	if k := rec.GetString("hidden_kind"); k != "" {
		return k
	}
	return store.HiddenByMaintainer
}

func handleResolve(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	var req ResolveRequest
	if err := readJSON(e, &req, maxModerationBody); err != nil {
		return requestError(e, err)
	}
	reason, ok := cleanText(req.Reason, MaxModerationReasonLength, req.Resolution == ResolutionDeleted)
	if !ok || !slices.Contains(store.Resolutions, req.Resolution) {
		return badRequest(e)
	}
	v := ViewerOf(e)
	actor := auth.CurrentMember(e)
	var rec *core.Record
	result := ResolveResult{Resolution: req.Resolution}
	err := e.App.RunInTransaction(func(tx core.App) error {
		if err := checkStillMaintainer(tx, actor); err != nil {
			return err
		}
		var err error
		if rec, err = findVisible(tx, v, id); err != nil {
			return err
		}
		open, err := openReportsOf(tx, rec)
		if err != nil {
			return err
		}
		hidden := store.IsHidden(rec)
		if len(open) == 0 && !hidden {
			return &errConflict{CodeNotUnderReview}
		}
		switch req.Resolution {
		case ResolutionRestored:
			shown := !hidden
			if hidden {
				if shown, err = restore(tx, rec, false); err != nil {
					return err
				}
			}
			if result.Resolved, err = resolveOpenReports(tx, actor, rec, store.ResolutionRestored); err != nil {
				return err
			}
			if hidden && shown {
				if err := notifyAuthor(tx, rec, domain.NoticeRestored); err != nil {
					return err
				}
			}
			return store.WriteAudit(tx, actor, AuditResolve, store.Recommendations, id, map[string]any{
				"resolution": req.Resolution, "reason": reason, "reports": result.Resolved, "shown": shown})
		case ResolutionKeptHidden:
			if !hidden {
				return &errConflict{CodeNotHidden}
			}
			if result.Resolved, err = resolveOpenReports(tx, actor, rec, store.ResolutionKeptHidden); err != nil {
				return err
			}
			return store.WriteAudit(tx, actor, AuditResolve, store.Recommendations, id, map[string]any{
				"resolution": req.Resolution, "reason": reason, "reports": result.Resolved, "shown": false})
		default: // ResolutionDeleted: the violation delete (H6, M2-Q9)
			kind := ""
			if hidden {
				kind = hiddenKindOf(rec)
			}
			if result.Resolved, err = resolveOpenReports(tx, actor, rec, store.ResolutionDeleted); err != nil {
				return err
			}
			if err := store.DeleteRecommendation(tx, rec); err != nil {
				return err
			}
			if err := notifyAuthor(tx, rec, domain.NoticeViolationDeleted); err != nil {
				return err
			}
			return store.WriteAudit(tx, actor, AuditViolationDelete, store.Recommendations, id,
				map[string]any{"reason": reason, "reports": result.Resolved, "kind": kind})
		}
	})
	if err != nil {
		return answerModeration(e, err)
	}
	if req.Resolution != ResolutionDeleted {
		if result.Recommendation, err = viewOf(e.App, v, rec); err != nil {
			return e.InternalServerError("", err)
		}
	}
	return respond(e, http.StatusOK, result)
}

func handleReviewQueue(e *core.RequestEvent) error {
	queue, err := LoadReviewQueue(e.App, ViewerOf(e))
	if err != nil {
		return e.InternalServerError("", err)
	}
	return respond(e, http.StatusOK, queue)
}

// LoadReviewQueue returns the recommendations under review that v (a maintainer) may see: those with open reports and
// the hidden ones.
func LoadReviewQueue(app core.App, v domain.Viewer) (ReviewQueue, error) {
	var reported []string
	if err := app.DB().Select("recommendation").Distinct(true).From(store.Reports).
		Where(dbx.HashExp{"resolution": ""}).Column(&reported); err != nil {
		return ReviewQueue{}, err
	}
	recs, err := store.VisibleRecommendations(app, v).
		Where(dbx.Or(dbx.In(store.Recommendations+".id", anyOf(reported)...), dbx.Not(store.Shown(store.Recommendations)))).
		All()
	if err != nil {
		return ReviewQueue{}, err
	}
	out := ReviewQueue{Items: []ReviewItem{}}
	if len(recs) == 0 {
		return out, nil
	}
	recIDs := make([]string, 0, len(recs))
	placeIDs := make([]string, 0, len(recs))
	for _, r := range recs {
		recIDs = append(recIDs, r.Id)
		placeIDs = append(placeIDs, r.GetString("place"))
	}
	reports, err := store.Query(app, store.Reports).
		Where(dbx.In("recommendation", anyOf(recIDs)...)).Where(dbx.HashExp{"resolution": ""}).
		OrderBy("created DESC", "id DESC").All()
	if err != nil {
		return ReviewQueue{}, err
	}
	byRec := map[string][]*core.Record{}
	for _, r := range reports {
		byRec[r.GetString("recommendation")] = append(byRec[r.GetString("recommendation")], r)
	}
	places, err := store.Query(app, store.Places).Where(dbx.In(store.Places+".id", anyOf(placeIDs)...)).All()
	if err != nil {
		return ReviewQueue{}, err
	}
	placeByID := map[string]*core.Record{}
	for _, p := range places {
		placeByID[p.Id] = p
	}
	views, err := RecommendationViews(app, v, recs)
	if err != nil {
		return ReviewQueue{}, err
	}
	viewByUUID := map[string]RecommendationView{}
	for _, view := range views {
		viewByUUID[view.UUID] = view
	}
	type sortable struct {
		item     ReviewItem
		hiddenAt time.Time
	}
	var items []sortable
	for _, r := range recs {
		view, ok := viewByUUID[r.GetString(store.FieldUUID)]
		p := placeByID[r.GetString("place")]
		if !ok || p == nil {
			continue
		}
		item := ReviewItem{
			Recommendation: view,
			Place:          ReviewPlace{UUID: p.GetString(store.FieldUUID), Name: p.GetString("name")},
			Reasons:        []ReasonCount{},
			Notes:          []ReportNote{},
		}
		counts := map[string]int{}
		for _, rep := range byRec[r.Id] {
			item.OpenReports++
			counts[rep.GetString("reason")]++
			at := rep.GetDateTime(store.FieldCreated).Time().UTC()
			if item.LastReportedAt == nil {
				item.LastReportedAt = &at
			}
			if note := rep.GetString("note"); note != "" {
				item.Notes = append(item.Notes, ReportNote{Reason: rep.GetString("reason"), Note: note, ReportedAt: at})
			}
		}
		for _, reason := range store.ReportReasons {
			if counts[reason] > 0 {
				item.Reasons = append(item.Reasons, ReasonCount{Reason: reason, Count: counts[reason]})
			}
		}
		items = append(items, sortable{item: item, hiddenAt: r.GetDateTime("hidden_at").Time()})
	}
	slices.SortStableFunc(items, func(a, b sortable) int {
		switch {
		case (a.item.OpenReports > 0) != (b.item.OpenReports > 0):
			if a.item.OpenReports > 0 {
				return -1
			}
			return 1
		case a.item.LastReportedAt != nil && b.item.LastReportedAt != nil && !a.item.LastReportedAt.Equal(*b.item.LastReportedAt):
			return b.item.LastReportedAt.Compare(*a.item.LastReportedAt)
		case !a.hiddenAt.Equal(b.hiddenAt):
			return b.hiddenAt.Compare(a.hiddenAt)
		default:
			return strings.Compare(a.item.Recommendation.UUID, b.item.Recommendation.UUID)
		}
	})
	for _, s := range items {
		out.Items = append(out.Items, s.item)
	}
	return out, nil
}

func handleRevealAuthor(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	v := ViewerOf(e)
	actor := auth.CurrentMember(e)
	var revealed RevealedAuthor
	err := e.App.RunInTransaction(func(tx core.App) error {
		if err := checkStillMaintainer(tx, actor); err != nil {
			return err
		}
		rec, err := findVisible(tx, v, id)
		if err != nil {
			return err
		}
		open, err := store.Query(tx, store.Reports).Where(dbx.HashExp{"recommendation": rec.Id, "resolution": ""}).Count()
		if err != nil {
			return err
		}
		if open == 0 && !store.IsHidden(rec) {
			return &errConflict{CodeNotUnderReview}
		}
		author, err := store.Query(tx, store.Members).IncludeDeleted().
			Where(dbx.HashExp{store.Members + ".id": rec.GetString("author")}).One()
		if err != nil {
			return err
		}
		revealed = RevealedAuthor{UUID: author.GetString(store.FieldUUID), DisplayName: author.GetString("display_name"),
			InCircle: auth.CanSignIn(author)}
		return store.WriteAudit(tx, actor, AuditRevealPseudonym, store.Members, revealed.UUID,
			map[string]any{"recommendation": id})
	})
	if err != nil {
		return answerModeration(e, err)
	}
	return respond(e, http.StatusOK, revealed)
}
