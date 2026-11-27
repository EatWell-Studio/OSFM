package api

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Per-member budgets (N5). PocketBase's global rate limit counts per client address; a signed-in member is counted by
// their uuid as well, so that one account cannot flood the circle with writes from many addresses. The budgets are
// token buckets in memory (internal/ratelimit): bounded, forgotten on restart, never logged.

// Limits configures the per-member budgets ([Options].Limits). A zero Rate disables that budget.
type Limits struct {
	// Writes is every member's budget of state-changing requests to the routes that carry [server.WriteBudget], all
	// of them together.
	Writes ratelimit.Rate
	// Capacity is the number of members each budget table holds at most; zero or less means
	// auth.DefaultLimitCapacity.
	Capacity int
}

// DefaultLimits returns the budgets [Bind] uses when [Options].Limits is nil: 60 writes at once, then one every two
// seconds (30 a minute), far more than a person entering recommendations needs.
func DefaultLimits() Limits {
	return Limits{Writes: ratelimit.Rate{Burst: 60, Every: 2 * time.Second}, Capacity: auth.DefaultLimitCapacity}
}

// budgets is one table of per-member token buckets.
type budgets struct{ b *ratelimit.Budgets }

func newBudgets(rate ratelimit.Rate, capacity int) *budgets {
	if capacity <= 0 {
		capacity = auth.DefaultLimitCapacity
	}
	return &budgets{b: ratelimit.NewBudgets(rate, capacity, nil)}
}

// BudgetMiddlewarePriority is the priority of the budget middlewares: after the session, CSRF and authentication rate
// limit middlewares of internal/auth (so a request they refuse costs nothing) and before the route's handler.
const BudgetMiddlewarePriority = auth.RateLimitMiddlewarePriority + 1

// middleware takes one token of the member's bucket per request: 429 [CodeRateLimited] with Retry-After when it is
// empty.
func (b *budgets) middleware(id string) *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       id,
		Priority: BudgetMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			m := auth.CurrentMember(e)
			if m == nil {
				return e.UnauthorizedError("", nil)
			}
			if ok, wait := b.b.Take(m.GetString(store.FieldUUID)); !ok {
				e.Response.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
				return fail(e, http.StatusTooManyRequests, CodeRateLimited, "Too many requests. Try again later.")
			}
			return e.Next()
		},
	}
}

// WriteBudgetMiddlewareID is the route middleware of [server.WriteBudget].
const WriteBudgetMiddlewareID = "osfmAPIWriteBudget"

// WriteBudget returns the route middleware that counts the request against the member's write budget
// ([Limits].Writes, shared by all write routes). Bind it to every state-changing route of a feature.
func (s *server) WriteBudget() *hook.Handler[*core.RequestEvent] {
	return s.writes.middleware(WriteBudgetMiddlewareID)
}

// MemberBudget returns a route middleware with a budget of its own, per member, for routes that need a different rate
// than the write budget (place search: a few requests a minute). Call it once per budget, in the register function,
// and bind the returned middleware to the routes that share it.
func (s *server) MemberBudget(id string, rate ratelimit.Rate) *hook.Handler[*core.RequestEvent] {
	return newBudgets(rate, s.limits.Capacity).middleware(id)
}
