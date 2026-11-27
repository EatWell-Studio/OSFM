package boundary

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

// LogMaxDays is how long PocketBase keeps request and application log entries (§3.9).
const LogMaxDays = 7

// ApplyLogSettings sets PocketBase's log settings to what OSFM requires and saves them if they differ: entries are kept
// for [LogMaxDays] days, and request entries record neither the client's IP address nor the ID of the authenticated
// record. [Bind] applies them at every bootstrap, so a change made in the admin UI lasts only until the next start.
//
// PocketBase deletes older entries every 6 hours, so an entry lives at most LogMaxDays days and 6 hours.
func ApplyLogSettings(app core.App) error {
	s := app.Settings()
	if s.Logs.MaxDays == LogMaxDays && !s.Logs.LogIP && !s.Logs.LogAuthId {
		return nil
	}
	s.Logs.MaxDays = LogMaxDays
	s.Logs.LogIP = false
	s.Logs.LogAuthId = false
	return app.Save(s)
}

// ApplyTrustedProxy sets PocketBase's trusted proxy headers to headers, taking the rightmost valid address of the
// header (the one the nearest proxy added; addresses further left come from the client and could be anything), and
// saves the settings if they differ. With no headers it leaves the settings alone, so a value set in the admin UI
// stays. The client address it yields (RealIP) is what PocketBase's and OSFM's rate limits count by.
func ApplyTrustedProxy(app core.App, headers []string) error {
	if len(headers) == 0 {
		return nil
	}
	s := app.Settings()
	if slices.Equal(s.TrustedProxy.Headers, headers) && !s.TrustedProxy.UseLeftmostIP {
		return nil
	}
	s.TrustedProxy.Headers = slices.Clone(headers)
	s.TrustedProxy.UseLeftmostIP = false
	return app.Save(s)
}
