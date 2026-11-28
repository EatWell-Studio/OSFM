package domain_test

import (
	"testing"
	"time"

	"github.com/EatWell-Studio/OSFM/internal/domain"
)

func TestCanInvite(t *testing.T) {
	joined := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	end := joined.Add(30 * day)
	for _, tc := range []struct {
		name string
		m    domain.Inviter
		days int
		now  time.Time
		want domain.InviteRight
	}{
		{"maintainer, just joined, permission off", domain.Inviter{Maintainer: true, JoinedAt: joined}, 30, joined,
			domain.InviteRight{Allowed: true}},
		{"member in the cooling-off period", domain.Inviter{CanInvite: true, JoinedAt: joined}, 30, joined.Add(day),
			domain.InviteRight{Reason: domain.InviteReasonCooldown, CooldownEndsAt: end}},
		{"a second before the end", domain.Inviter{CanInvite: true, JoinedAt: joined}, 30, end.Add(-time.Second),
			domain.InviteRight{Reason: domain.InviteReasonCooldown, CooldownEndsAt: end}},
		{"at the end", domain.Inviter{CanInvite: true, JoinedAt: joined}, 30, end,
			domain.InviteRight{Allowed: true, CooldownEndsAt: end}},
		{"shorter setting applies at once", domain.Inviter{CanInvite: true, JoinedAt: joined}, 7, joined.Add(8 * day),
			domain.InviteRight{Allowed: true, CooldownEndsAt: joined.Add(7 * day)}},
		{"longer setting applies at once", domain.Inviter{CanInvite: true, JoinedAt: joined}, 60, end.Add(day),
			domain.InviteRight{Reason: domain.InviteReasonCooldown, CooldownEndsAt: joined.Add(60 * day)}},
		{"no cooling-off period", domain.Inviter{CanInvite: true, JoinedAt: joined}, 0, joined,
			domain.InviteRight{Allowed: true, CooldownEndsAt: joined}},
		{"negative days count as none", domain.Inviter{CanInvite: true, JoinedAt: joined}, -5, joined,
			domain.InviteRight{Allowed: true, CooldownEndsAt: joined}},
		{"opened early", domain.Inviter{CanInvite: true, JoinedAt: joined, AllowedFrom: joined.Add(2 * day)}, 30,
			joined.Add(3 * day), domain.InviteRight{Allowed: true, CooldownEndsAt: joined.Add(2 * day)}},
		{"opened early but in the future", domain.Inviter{CanInvite: true, JoinedAt: joined,
			AllowedFrom: joined.Add(5 * day)}, 30, joined.Add(3 * day),
			domain.InviteRight{Reason: domain.InviteReasonCooldown, CooldownEndsAt: joined.Add(5 * day)}},
		{"allowed-from later than the period does not extend it", domain.Inviter{CanInvite: true, JoinedAt: joined,
			AllowedFrom: end.Add(10 * day)}, 30, end.Add(day), domain.InviteRight{Allowed: true, CooldownEndsAt: end}},
		{"permission off after the period", domain.Inviter{JoinedAt: joined}, 30, end.Add(day),
			domain.InviteRight{Reason: domain.InviteReasonDisabled, CooldownEndsAt: end}},
		{"permission off wins over the cooling-off period", domain.Inviter{JoinedAt: joined}, 30, joined,
			domain.InviteRight{Reason: domain.InviteReasonDisabled, CooldownEndsAt: end}},
		{"permission off, opened early", domain.Inviter{JoinedAt: joined, AllowedFrom: joined}, 30, joined.Add(day),
			domain.InviteRight{Reason: domain.InviteReasonDisabled, CooldownEndsAt: joined}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.CanInvite(tc.m, tc.days, tc.now); got != tc.want {
				t.Errorf("CanInvite = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCooldownEndInUTC(t *testing.T) {
	zone := time.FixedZone("test", 8*3600)
	joined := time.Date(2026, 3, 1, 20, 0, 0, 0, zone)
	got := domain.CooldownEnd(domain.Inviter{JoinedAt: joined}, 1)
	if got.Location() != time.UTC || !got.Equal(joined.Add(24*time.Hour)) {
		t.Errorf("CooldownEnd = %v", got)
	}
}

func TestInviteLimitReached(t *testing.T) {
	member := domain.Inviter{CanInvite: true}
	for active, want := range map[int]bool{0: false, domain.MaxActiveInvites - 1: false,
		domain.MaxActiveInvites: true, domain.MaxActiveInvites + 1: true} {
		if got := domain.InviteLimitReached(member, active); got != want {
			t.Errorf("member with %d active invites: %v, want %v", active, got, want)
		}
	}
	if domain.InviteLimitReached(domain.Inviter{Maintainer: true}, 100) {
		t.Error("maintainers have no limit")
	}
}
