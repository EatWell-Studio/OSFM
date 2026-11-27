package api

import (
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/config"
)

// Options configures [Bind].
type Options struct {
	// Config is the instance configuration (data directory, public URL, OSFM_* variables), for features that need it
	// (backups, place search). It may be nil in tests that do not use those features.
	Config *config.Config
	// Limits are the per-member request budgets; nil means [DefaultLimits].
	Limits *Limits
}

// features are the register functions of the features, each in its own file (see the package documentation). The
// list is complete for M2: a feature fills its function and leaves this list alone.
var features = []func(se *core.ServeEvent, s *server){
	registerSettings,
	registerPlaces,
	registerMap,
	registerRecommendations,
	registerReports,
	registerNotices,
	registerProfile,
	registerGovernance,
	registerLeave,
	registerPersonalExport,
	registerLayerExport,
	registerBackups,
	registerGeocode,
}

// Bind registers the routes of every feature on app when it serves. The session layer (internal/auth) must be bound
// too: it answers 401 without a session and 403 without the X-OSFM header before any handler here runs.
func Bind(app core.App, opts Options) {
	limits := DefaultLimits()
	if opts.Limits != nil {
		limits = *opts.Limits
	}
	s := &server{opts: opts, limits: limits, writes: newBudgets(limits.Writes, limits.Capacity)}
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		for _, register := range features {
			register(se, s)
		}
		return se.Next()
	})
}

// server is what the routes of all features share. Features may add methods to it in their own files.
type server struct {
	opts   Options
	limits Limits
	writes *budgets
}

// Config returns the instance configuration, nil when [Options] has none.
func (s *server) Config() *config.Config { return s.opts.Config }
