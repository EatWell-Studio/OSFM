package domain

import "time"

// Who may invite (D2, D6, D12; M2-Q11). Maintainers always may. A member may when their invite permission is on
// (members.can_invite, on by default, switched off and on by a maintainer) and their cooling-off period is over. The
// period ends at the earlier of
//
//   - joined_at plus the cooling-off days of the instance settings as they are now (a change of the setting applies to
//     every member at once), and
//   - invite_allowed_from, which a maintainer sets to open inviting early.
//
// A member may have at most [MaxActiveInvites] active invites (neither used, revoked nor expired) at a time;
// maintainers have no such limit: they set the circle up and invite in batches.

// Reasons a member may not invite ([InviteRight].Reason).
const (
	InviteReasonCooldown = "cooldown" // the cooling-off period is not over yet
	InviteReasonDisabled = "disabled" // a maintainer switched the member's invite permission off
)

// MaxActiveInvites is the number of active invites a member who is not a maintainer may have at a time (M2-Q11).
const MaxActiveInvites = 3

// Inviter is what the invite rules need to know about a member.
type Inviter struct {
	// Maintainer is true for a maintainer (D6).
	Maintainer bool
	// CanInvite is the member's invite permission (members.can_invite).
	CanInvite bool
	// JoinedAt is when the member joined (members.joined_at).
	JoinedAt time.Time
	// AllowedFrom is when a maintainer opened inviting early (members.invite_allowed_from); zero when they did not.
	AllowedFrom time.Time
}

// InviteRight is whether a member may invite now, and why not.
type InviteRight struct {
	// Allowed is true when the member may create an invite now (within [MaxActiveInvites] for members).
	Allowed bool
	// Reason is empty when Allowed, otherwise [InviteReasonDisabled] or [InviteReasonCooldown]. A member whose
	// permission is off is told so even during the cooling-off period: opening early would not help them.
	Reason string
	// CooldownEndsAt is when the member's cooling-off period ends or ended (in UTC); zero for maintainers.
	CooldownEndsAt time.Time
}

// CooldownEnd returns when the cooling-off period of m ends: the earlier of m.JoinedAt plus cooldownDays days and
// m.AllowedFrom (when set). A negative cooldownDays counts as 0.
func CooldownEnd(m Inviter, cooldownDays int) time.Time {
	end := m.JoinedAt.UTC().Add(time.Duration(max(0, cooldownDays)) * 24 * time.Hour)
	if !m.AllowedFrom.IsZero() && m.AllowedFrom.Before(end) {
		end = m.AllowedFrom.UTC()
	}
	return end
}

// CanInvite reports whether m may invite at time now, with the instance's cooling-off period of cooldownDays days.
// It does not count active invites; see [InviteLimitReached].
func CanInvite(m Inviter, cooldownDays int, now time.Time) InviteRight {
	if m.Maintainer {
		return InviteRight{Allowed: true}
	}
	end := CooldownEnd(m, cooldownDays)
	switch {
	case !m.CanInvite:
		return InviteRight{Reason: InviteReasonDisabled, CooldownEndsAt: end}
	case now.Before(end):
		return InviteRight{Reason: InviteReasonCooldown, CooldownEndsAt: end}
	default:
		return InviteRight{Allowed: true, CooldownEndsAt: end}
	}
}

// InviteLimitReached reports whether m, who has active open invites, may not create another one: a member who is not
// a maintainer has [MaxActiveInvites] or more.
func InviteLimitReached(m Inviter, active int) bool {
	return !m.Maintainer && active >= MaxActiveInvites
}
