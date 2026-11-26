package webapp

import "net/http"

// ContentSecurityPolicy is the content security policy of the main listener (§4.7). Everything comes from the
// instance itself: scripts, styles, images, fonts, the map's glyphs, sprites, tiles and its module worker (a file of
// its own, so no blob: or data: allowance), the manifest and the service worker. No inline script or style
// attribute is needed. The directives that do not fall back to default-src are set explicitly: no <base> to another
// origin, forms submit to the instance only, and no page may frame the app (clickjacking). Plugins are off.
const ContentSecurityPolicy = "default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; " +
	"object-src 'none'"

// PermissionsPolicy allows geolocation to the app's own pages ("show my location", used in the browser only, A8)
// and turns off powerful features the app has no use for, in this and in any framed document. Features that
// Chromium does not recognise are left out: it reports them as errors in the console. WebAuthn (passkeys) and the
// clipboard keep their defaults, which allow the app's own origin.
const PermissionsPolicy = "geolocation=(self), camera=(), microphone=(), display-capture=(), payment=(), usb=(), " +
	"serial=(), hid=(), bluetooth=(), midi=(), browsing-topics=()"

// HSTSValue is the Strict-Transport-Security value when [Options].HSTS is set: one year, for the instance's own host
// only. includeSubDomains and preload are left to the operator (at the reverse proxy): an instance often lives on a
// subdomain of a domain whose other hosts are not the instance's to pin.
const HSTSValue = "max-age=31536000"

// Headers returns the security headers of every response on the main listener, with Strict-Transport-Security when
// hsts is set. Browsers ignore Strict-Transport-Security on a response that did not come over HTTPS, so sending it
// behind a TLS-terminating reverse proxy is what makes it take effect, and a direct plain-HTTP request to the
// listener is unaffected.
func Headers(hsts bool) http.Header {
	h := http.Header{}
	h.Set("Content-Security-Policy", ContentSecurityPolicy)
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	// The same as frame-ancestors 'none', for browsers that predate it.
	h.Set("X-Frame-Options", "DENY")
	h.Set("Permissions-Policy", PermissionsPolicy)
	// Pages of other origins that the app opens (the OpenStreetMap copyright link, navigation apps) get no handle on
	// its window, and it none on theirs.
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	// Other origins cannot load the app's responses as subresources (an image, a script, a tile), even with the
	// member's cookie: same-site is not enough when a sibling subdomain belongs to someone else.
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	if hsts {
		h.Set("Strict-Transport-Security", HSTSValue)
	}
	return h
}
